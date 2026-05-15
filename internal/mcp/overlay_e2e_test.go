package mcp

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/zzet/gortex/internal/config"
	"github.com/zzet/gortex/internal/daemon"
	"github.com/zzet/gortex/internal/graph"
	"github.com/zzet/gortex/internal/indexer"
	"github.com/zzet/gortex/internal/parser"
	"github.com/zzet/gortex/internal/parser/languages"
	"github.com/zzet/gortex/internal/query"
)

// setupOverlayServer builds a fully-wired MCP server with an attached
// OverlayManager, an indexed temp repo, and a single Go file the tests
// can overlay-edit. Returns the server, the repo root, the absolute
// file path, and a teardown function.
func setupOverlayServer(t *testing.T) (srv *Server, dir, file string) {
	t.Helper()
	dir = t.TempDir()
	file = filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(file, []byte(`package main

func Disk() {}
`), 0o644))

	g := graph.New()
	reg := parser.NewRegistry()
	languages.RegisterAll(reg)
	cfg := config.Default()
	idx := indexer.New(g, reg, cfg.Index, zap.NewNop())
	_, err := idx.Index(dir)
	require.NoError(t, err)

	eng := query.NewEngine(g)
	srv = NewServer(eng, g, idx, nil, zap.NewNop(), nil)
	srv.SetOverlayManager(daemon.NewOverlayManager(time.Minute))
	srv.RunAnalysis()
	return srv, dir, file
}

func callToolByName(t *testing.T, srv *Server, ctx context.Context, name string, args map[string]any) *mcplib.CallToolResult {
	t.Helper()
	tool := srv.MCPServer().GetTool(name)
	require.NotNilf(t, tool, "tool %q must be registered", name)
	req := mcplib.CallToolRequest{Params: mcplib.CallToolParams{
		Name:      name,
		Arguments: args,
	}}
	res, err := tool.Handler(ctx, req)
	require.NoError(t, err)
	return res
}

func toolText(res *mcplib.CallToolResult) string {
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(mcplib.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

// TestOverlay_QueryConsumption_GetFileSummary is the core I19 contract:
// after the editor pushes an overlay adding a new function, the very
// next get_file_summary call must surface that function. The test
// proves the overlay-apply middleware runs around tool dispatch and
// that the indexer-from-content path produces graph entries query
// tools observe.
func TestOverlay_QueryConsumption_GetFileSummary(t *testing.T) {
	srv, dir, file := setupOverlayServer(t)
	sessID := "test-session-1"
	require.NoError(t, srv.OverlayManager().RegisterWithID(sessID, ""))
	require.NoError(t, srv.OverlayManager().Push(sessID, daemon.OverlayFile{
		Path: file,
		Content: `package main

func Disk() {}

func Overlay() {}
`,
	}, nil))

	ctx := WithSessionID(context.Background(), sessID)
	res := callToolByName(t, srv, ctx, "get_file_summary", map[string]any{
		"path": filepath.Base(file),
	})
	body := toolText(res)
	require.Containsf(t, body, "Overlay", "get_file_summary must surface the overlay-added symbol; got %s", body)
	require.Contains(t, body, "Disk")

	// Post-call revert: a query without the session ID must NOT see the
	// overlay function. This guarantees the middleware restored the
	// on-disk view after the previous tool returned.
	bare := callToolByName(t, srv, context.Background(), "get_file_summary", map[string]any{
		"path": filepath.Base(file),
	})
	require.NotContainsf(t, toolText(bare), "Overlay",
		"post-tool revert must restore the on-disk view")
	_ = dir
}

// TestOverlay_DriftSurfacesAsToolError verifies that a stale overlay
// (BaseSHA recorded at editor-open time disagreeing with the current
// on-disk SHA) makes the next tool call fail with an MCP error result
// rather than silently returning stale data. The client is expected to
// re-read the file and resubmit a fresh overlay.
func TestOverlay_DriftSurfacesAsToolError(t *testing.T) {
	srv, _, file := setupOverlayServer(t)
	sessID := "test-session-drift"
	require.NoError(t, srv.OverlayManager().RegisterWithID(sessID, ""))
	require.NoError(t, srv.OverlayManager().Push(sessID, daemon.OverlayFile{
		Path:    file,
		Content: "package main\n\nfunc Overlay() {}\n",
		BaseSHA: "0000000000000000000000000000000000000000", // intentionally wrong
	}, nil))

	ctx := WithSessionID(context.Background(), sessID)
	res := callToolByName(t, srv, ctx, "get_file_summary", map[string]any{
		"path": filepath.Base(file),
	})
	require.True(t, res.IsError, "drift must surface as an MCP tool error")
	require.Contains(t, toolText(res), "overlay base SHA mismatch")
}

// TestOverlay_BaseSHA_MatchProceeds confirms the drift-detection
// happy path: when the editor's BaseSHA matches the on-disk git-blob
// hash, the overlay applies and the new symbol is visible. Without
// this we'd have no positive coverage of the SHA check — only the
// negative path.
func TestOverlay_BaseSHA_MatchProceeds(t *testing.T) {
	srv, _, file := setupOverlayServer(t)

	// Compute the on-disk git blob SHA the same way overlay.go does
	// (`blob <len>\0<content>` → sha1). The editor would normally
	// read this from `git ls-files -s` or its LSP host's didOpen
	// version metadata.
	data, err := os.ReadFile(file)
	require.NoError(t, err)
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(data))
	_, _ = h.Write(data)
	baseSHA := hex.EncodeToString(h.Sum(nil))

	sessID := "test-session-match"
	require.NoError(t, srv.OverlayManager().RegisterWithID(sessID, ""))
	require.NoError(t, srv.OverlayManager().Push(sessID, daemon.OverlayFile{
		Path:    file,
		Content: "package main\n\nfunc Disk() {}\n\nfunc Overlay() {}\n",
		BaseSHA: baseSHA,
	}, nil))

	ctx := WithSessionID(context.Background(), sessID)
	res := callToolByName(t, srv, ctx, "get_file_summary", map[string]any{
		"path": filepath.Base(file),
	})
	require.False(t, res.IsError)
	require.Contains(t, toolText(res), "Overlay")
}

// TestOverlay_NoSessionNoOp is the fast-path: a tools/call with no
// overlay session bound to the context must NOT pay any overlay
// apply/revert cost and must observe the on-disk view. Failing this
// would mean overlay support imposes overhead on every non-overlay
// MCP call — the regression that gates wide adoption.
func TestOverlay_NoSessionNoOp(t *testing.T) {
	srv, _, file := setupOverlayServer(t)
	// A registered session with no overlays attached: the fast-path
	// (FileCount==0) must skip the apply pass entirely.
	require.NoError(t, srv.OverlayManager().RegisterWithID("idle", ""))
	ctx := WithSessionID(context.Background(), "idle")
	res := callToolByName(t, srv, ctx, "get_file_summary", map[string]any{
		"path": filepath.Base(file),
	})
	require.False(t, res.IsError)
	require.Contains(t, toolText(res), "Disk")
	require.NotContains(t, toolText(res), "Overlay")
}

// TestOverlay_MCP_RegisterPushList exercises the MCP-tool surface for
// overlay management: overlay_register, overlay_push, overlay_list.
// This is the path an IDE extension takes when it'd rather speak the
// MCP protocol than reach for the parallel /v1/overlay/* HTTP API.
func TestOverlay_MCP_RegisterPushList(t *testing.T) {
	srv, _, file := setupOverlayServer(t)
	sessID := "test-mcp-register"

	ctx := WithSessionID(context.Background(), sessID)
	regRes := callToolByName(t, srv, ctx, "overlay_register", map[string]any{})
	require.False(t, regRes.IsError, "overlay_register: %s", toolText(regRes))

	pushRes := callToolByName(t, srv, ctx, "overlay_push", map[string]any{
		"path":    file,
		"content": "package main\n\nfunc Overlay() {}\n",
	})
	require.False(t, pushRes.IsError, "overlay_push: %s", toolText(pushRes))

	listRes := callToolByName(t, srv, ctx, "overlay_list", map[string]any{})
	listText := toolText(listRes)
	require.Contains(t, listText, file, "overlay_list must mention the pushed path: %s", listText)
	require.Contains(t, listText, `"count":1`)
}

// TestOverlay_DeletedFileGoneFromGraph verifies the tombstone path:
// when an overlay is pushed with deleted=true, the file's symbols
// must vanish from the graph for the duration of the call — the
// editor wants to preview "delete this file" without staging the
// deletion to disk.
func TestOverlay_DeletedFileGoneFromGraph(t *testing.T) {
	srv, _, file := setupOverlayServer(t)
	sessID := "test-mcp-del"
	require.NoError(t, srv.OverlayManager().RegisterWithID(sessID, ""))
	require.NoError(t, srv.OverlayManager().Push(sessID, daemon.OverlayFile{
		Path:    file,
		Deleted: true,
	}, nil))

	ctx := WithSessionID(context.Background(), sessID)
	res := callToolByName(t, srv, ctx, "get_file_summary", map[string]any{
		"path": filepath.Base(file),
	})
	// File summary against a tombstoned file: either it returns a
	// structured "file not in graph" error, or it succeeds with an
	// empty symbol set. Both are correct post-conditions; only the
	// Disk symbol leaking back through would be a regression.
	require.NotContains(t, toolText(res), "Disk",
		"deletion overlay must hide the tombstoned file's symbols")
}
