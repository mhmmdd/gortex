package mcp

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"go.uber.org/zap"

	"github.com/zzet/gortex/internal/daemon"
)

// SetOverlayManager wires an editor-overlay manager into the MCP
// server. After this call:
//
//   - Every `tools/call` is wrapped by an apply/revert middleware that
//     merges the calling session's overlay buffers into the in-memory
//     graph for the duration of the call. Tools that walk the graph
//     (find_usages, get_call_chain, get_file_summary, …) and tools that
//     read symbol source (get_symbol_source, get_editing_context, …)
//     therefore see overlay content with no per-tool changes.
//
//   - The `overlay_register` / `overlay_push` / `overlay_list` /
//     `overlay_delete` / `overlay_drop` MCP tools become live so an
//     IDE extension speaking MCP can manage its own overlays without
//     reaching for the `/v1/overlay/*` HTTP endpoints.
//
// Passing nil leaves the server in pre-overlay behaviour (reads come
// from disk; overlay tools are not registered). Calling twice
// re-registers the overlay tools idempotently.
func (s *Server) SetOverlayManager(mgr *daemon.OverlayManager) {
	s.overlays = mgr
	if mgr == nil {
		return
	}
	s.registerOverlayToolsOnce.Do(func() {
		s.registerOverlayTools()
	})
}

// OverlayManager returns the wired editor-overlay manager, or nil
// when overlay support is disabled for this server instance.
func (s *Server) OverlayManager() *daemon.OverlayManager { return s.overlays }

// wrapToolHandler returns a tool handler decorated with the overlay
// apply/revert middleware. Tool registration helpers (`s.addTool`)
// route every handler through this so the dispatcher and the HTTP
// `CallToolStrict` path both pay the same middleware cost — the HTTP
// path bypasses mcp-go's hook surface, so we can't rely on Hooks alone.
//
// When the server has no overlay manager, or the calling context has
// no overlay session, this is a transparent pass-through (one map
// lookup, zero parsing).
func (s *Server) wrapToolHandler(h mcpserver.ToolHandlerFunc) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		revert, err := s.applyOverlaysForCtx(ctx)
		if err != nil {
			// Drift / push-back-required surfaces as a structured tool
			// error result — the client must re-read the file and
			// resubmit a fresh overlay. We return (result, nil) so the
			// JSON-RPC framing carries the message rather than a
			// transport error.
			return mcp.NewToolResultError(err.Error()), nil
		}
		if revert != nil {
			defer revert()
		}
		return h(ctx, req)
	}
}

// applyOverlaysForCtx applies every overlay attached to the calling
// session to the live in-memory graph and returns a revert closure.
// Returns (nil, nil) when the request has no overlay session, the
// session has no files, or no overlay manager is wired — the
// fast-path that 99% of tool calls take.
//
// On drift the function returns a non-nil error and leaves the graph
// in its disk-restored state (every partial apply is rolled back).
// The caller surfaces the error as a structured MCP tool result so the
// client knows to re-read and resubmit the affected overlay.
//
// Concurrency: applies are serialised through s.overlayApplyMu so two
// in-flight tool calls can't interleave their evict/re-add pairs on
// the same file. The lock is held for the full apply+tool+revert
// window — for IDE-driven workloads (1-3 overlay files, parse < 50 ms
// per file) this is a sub-100 ms serialisation, which dominates the
// editor's keystroke cadence either way.
func (s *Server) applyOverlaysForCtx(ctx context.Context) (revert func(), err error) {
	if s == nil || s.overlays == nil {
		return nil, nil
	}
	sessID := SessionIDFromContext(ctx)
	if sessID == "" {
		return nil, nil
	}
	if s.overlays.FileCount(sessID) == 0 {
		return nil, nil
	}
	_, files, err := s.overlays.SnapshotFor(sessID)
	if err != nil {
		// Session evaporated between the FileCount fast-path and the
		// snapshot read. Treat as "no overlay" — the client will
		// re-register if it cares.
		return nil, nil
	}
	if len(files) == 0 {
		return nil, nil
	}

	s.overlayApplyMu.Lock()
	applied := make([]string, 0, len(files))
	// Track per-application kind so revert restores deletions by
	// re-indexing-from-disk while applied-content paths also revert
	// to disk. Both kinds use the same restore call (IndexFile reads
	// from disk; for paths that don't exist on disk it's a no-op
	// because the in-memory state is already evicted).
	for _, ov := range files {
		absPath, resolveErr := s.resolveOverlayAbsPath(ov.Path)
		if resolveErr != nil {
			s.revertOverlays(applied)
			s.overlayApplyMu.Unlock()
			return nil, resolveErr
		}
		if absPath == "" {
			// Path didn't resolve to a tracked workspace root —
			// silently skip; the disk path will still be honoured if
			// the file ever falls under a tracked repo later.
			continue
		}
		if ov.BaseSHA != "" {
			if !overlaySHAMatches(absPath, ov.BaseSHA) {
				s.revertOverlays(applied)
				s.overlayApplyMu.Unlock()
				return nil, fmt.Errorf("%w: %s", daemon.ErrOverlayDrift, ov.Path)
			}
		}
		if applyErr := s.applyOneOverlay(absPath, ov); applyErr != nil {
			s.revertOverlays(applied)
			s.overlayApplyMu.Unlock()
			return nil, applyErr
		}
		applied = append(applied, absPath)
	}

	// Lock stays held until revert; tool handler runs serialised
	// against any other overlay-active request. Revert releases the
	// lock so the next overlay-active request can proceed.
	return func() {
		s.revertOverlays(applied)
		s.overlayApplyMu.Unlock()
	}, nil
}

// applyOneOverlay evicts the file from the graph and, when the
// overlay carries content, re-indexes from the supplied bytes.
// Deletion overlays leave the file evicted.
func (s *Server) applyOneOverlay(absPath string, ov daemon.OverlayFile) error {
	if ov.Deleted {
		if s.multiIndexer != nil {
			s.multiIndexer.EvictFileByAbs(absPath)
			return nil
		}
		if s.indexer != nil {
			s.indexer.EvictFile(absPath)
		}
		return nil
	}
	if s.multiIndexer != nil {
		return s.multiIndexer.IndexFileFromContent(absPath, []byte(ov.Content))
	}
	if s.indexer != nil {
		return s.indexer.IndexFileFromContent(absPath, []byte(ov.Content), true)
	}
	return nil
}

// revertOverlays re-indexes each applied path from disk so the
// post-tool state matches the saved-buffer view. Called under
// overlayApplyMu; safe to call with an empty slice. Errors during
// revert are logged (debug) but not returned: a tool call is finished
// and the next watcher-driven IndexFile will heal a stuck state.
func (s *Server) revertOverlays(absPaths []string) {
	for _, abs := range absPaths {
		var err error
		switch {
		case s.multiIndexer != nil:
			err = s.multiIndexer.ReindexFromDisk(abs)
		case s.indexer != nil:
			err = s.indexer.IndexFile(abs)
		}
		if err != nil && s.logger != nil {
			s.logger.Debug("overlay revert: re-index from disk failed",
				zap.String("path", abs), zap.Error(err))
		}
	}
}

// resolveOverlayAbsPath turns an overlay-supplied path into the
// absolute filesystem path used by indexer apply calls. Accepts:
//
//   - Absolute paths — returned unchanged after symlink-safe cleaning.
//   - Repo-prefixed paths (multi-repo mode, e.g. "ade/internal/foo.go")
//     — resolved via MultiIndexer.ResolveFilePath.
//   - Repo-relative paths (single-repo mode) — joined onto the
//     Indexer's root path.
//
// Returns ("", nil) when the path doesn't resolve to a known
// workspace; the caller skips such overlays without failing the
// request, mirroring how the on-disk indexer treats untracked files.
func (s *Server) resolveOverlayAbsPath(p string) (string, error) {
	if p == "" {
		return "", errors.New("overlay path is empty")
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p), nil
	}
	if s.multiIndexer != nil {
		if abs := s.multiIndexer.ResolveFilePath(p); abs != "" {
			return abs, nil
		}
	}
	if s.indexer != nil {
		if root := s.indexer.RootPath(); root != "" {
			return filepath.Join(root, p), nil
		}
	}
	return "", nil
}

// overlaySHAMatches re-computes the git blob SHA of the on-disk file
// and compares it to the expected SHA captured at editor-open time.
// Matches the git blob hash format (`blob <len>\0<content>`) so the
// editor can pass the SHA it reads from `git ls-files -s` / the
// LSP `textDocument/didOpen` baseline without any client-side
// reformatting. Returns false on any read error: the safer default
// is "drift detected" — re-read and resubmit.
func overlaySHAMatches(absPath, expected string) bool {
	expected = strings.ToLower(strings.TrimSpace(expected))
	if expected == "" {
		return true
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return false
	}
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(data))
	_, _ = h.Write(data)
	return hex.EncodeToString(h.Sum(nil)) == expected
}

// overlayApplyMu serialises overlay-active tool calls. Declared on
// Server (server.go); declared here as a package-local sentinel so the
// linter doesn't flag the struct field as unused before the wire-up
// step adds it.
var _ sync.Mutex
