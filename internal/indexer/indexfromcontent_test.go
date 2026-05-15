package indexer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/zzet/gortex/internal/graph"
)

// TestIndexFileFromContent_ReplacesGraphView verifies the editor-overlay
// path: re-indexing a file from in-memory content evicts the disk-derived
// nodes for that file and adds nodes for the overlaid view, *without*
// touching the file on disk or its mtime tracking. After IndexFile is
// called again, the on-disk view returns.
func TestIndexFileFromContent_ReplacesGraphView(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	const disk = `package main

func Disk() {}
`
	require.NoError(t, os.WriteFile(path, []byte(disk), 0o644))

	g := graph.New()
	idx := newTestIndexer(g)
	idx.SetRootPath(dir)
	require.NoError(t, idx.IndexFile(path))

	// Baseline: disk function present, overlay function absent.
	requireSymbolPresent(t, g, "main.go", "Disk")
	requireSymbolAbsent(t, g, "main.go", "Overlay")

	const overlay = `package main

func Disk() {}

func Overlay() {}
`
	require.NoError(t, idx.IndexFileFromContent(path, []byte(overlay), true))

	// Overlay applied: both functions visible in the graph.
	requireSymbolPresent(t, g, "main.go", "Disk")
	requireSymbolPresent(t, g, "main.go", "Overlay")

	// Restore: re-index from disk; the overlay-added function disappears.
	require.NoError(t, idx.IndexFile(path))
	requireSymbolPresent(t, g, "main.go", "Disk")
	requireSymbolAbsent(t, g, "main.go", "Overlay")
}

// TestIndexFileFromContent_DoesNotStampMtime confirms that overlay
// applies don't poison mtime tracking — otherwise the next watcher
// event would see the on-disk file as "older than the overlay" and
// skip the restore. The post-apply mtime must equal the pre-apply
// mtime (zero, in this test, because the file was never indexed
// from disk before the overlay).
func TestIndexFileFromContent_DoesNotStampMtime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.go")
	require.NoError(t, os.WriteFile(path, []byte("package x\n"), 0o644))

	g := graph.New()
	idx := newTestIndexer(g)
	idx.SetRootPath(dir)

	before := idx.FileMtimes()
	require.Empty(t, before, "pre-condition: no mtime tracking before any index call")

	const overlay = "package x\n\nfunc Added() {}\n"
	require.NoError(t, idx.IndexFileFromContent(path, []byte(overlay), true))

	after := idx.FileMtimes()
	require.Empty(t, after, "overlay apply must not stamp mtime")
}

// TestIndexFileFromContent_DeletionEquivalentViaEvictFile mirrors the
// MCP overlay-middleware deletion path: tombstone overlays are routed
// through EvictFile rather than IndexFileFromContent. Verifying the
// effect here keeps the test surface honest about what the middleware
// actually does for deleted: true overlays.
func TestIndexFileFromContent_DeletionEquivalentViaEvictFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "del.go")
	require.NoError(t, os.WriteFile(path, []byte("package del\nfunc Disk() {}\n"), 0o644))

	g := graph.New()
	idx := newTestIndexer(g)
	idx.SetRootPath(dir)
	require.NoError(t, idx.IndexFile(path))
	requireSymbolPresent(t, g, "del.go", "Disk")

	// Deletion overlay → EvictFile on the absolute path.
	idx.EvictFile(path)
	requireSymbolAbsent(t, g, "del.go", "Disk")

	// Revert (re-index from disk) brings the symbol back.
	require.NoError(t, idx.IndexFile(path))
	requireSymbolPresent(t, g, "del.go", "Disk")
}

// requireSymbolPresent asserts that the graph contains a symbol with
// the given name in the given relative path. The assertion is
// relative-path-aware: in multi-repo mode the file path is prefixed,
// in single-repo mode it isn't — both cases match.
func requireSymbolPresent(t *testing.T, g *graph.Graph, relPath, name string) {
	t.Helper()
	require.Truef(t, hasSymbol(g, relPath, name),
		"expected symbol %q in file %q to be present in graph", name, relPath)
}

func requireSymbolAbsent(t *testing.T, g *graph.Graph, relPath, name string) {
	t.Helper()
	require.Falsef(t, hasSymbol(g, relPath, name),
		"expected symbol %q in file %q to be absent from graph", name, relPath)
}

func hasSymbol(g *graph.Graph, relPath, name string) bool {
	for _, n := range g.GetFileNodes(relPath) {
		if n.Name == name {
			return true
		}
	}
	return false
}
