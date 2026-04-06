package query

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zzet/gortex/internal/graph"
)

// buildTestGraph creates a small realistic graph:
//
//	main.go::main -> calls -> pkg/server.go::Start
//	pkg/server.go::Start -> calls -> pkg/db.go::Connect
//	pkg/db.go::Connect -> calls -> pkg/db.go::Ping
//	pkg/server.go -> imports -> pkg/db.go
//	pkg/db.go::DBImpl -> implements -> pkg/db.go::DB (interface)
func buildTestGraph() *graph.Graph {
	g := graph.New()

	nodes := []*graph.Node{
		{ID: "main.go", Kind: graph.KindFile, Name: "main.go", FilePath: "main.go", Language: "go"},
		{ID: "main.go::main", Kind: graph.KindFunction, Name: "main", FilePath: "main.go", Language: "go",
			StartLine: 5, EndLine: 10, Meta: map[string]any{"signature": "func main()"}},
		{ID: "pkg/server.go", Kind: graph.KindFile, Name: "server.go", FilePath: "pkg/server.go", Language: "go"},
		{ID: "pkg/server.go::Start", Kind: graph.KindFunction, Name: "Start", FilePath: "pkg/server.go", Language: "go",
			StartLine: 10, EndLine: 20},
		{ID: "pkg/db.go", Kind: graph.KindFile, Name: "db.go", FilePath: "pkg/db.go", Language: "go"},
		{ID: "pkg/db.go::Connect", Kind: graph.KindFunction, Name: "Connect", FilePath: "pkg/db.go", Language: "go",
			StartLine: 5, EndLine: 15},
		{ID: "pkg/db.go::Ping", Kind: graph.KindFunction, Name: "Ping", FilePath: "pkg/db.go", Language: "go",
			StartLine: 17, EndLine: 22},
		{ID: "pkg/db.go::DB", Kind: graph.KindInterface, Name: "DB", FilePath: "pkg/db.go", Language: "go",
			StartLine: 1, EndLine: 4},
		{ID: "pkg/db.go::DBImpl", Kind: graph.KindType, Name: "DBImpl", FilePath: "pkg/db.go", Language: "go",
			StartLine: 24, EndLine: 30},
	}
	for _, n := range nodes {
		g.AddNode(n)
	}

	edges := []*graph.Edge{
		{From: "main.go::main", To: "pkg/server.go::Start", Kind: graph.EdgeCalls, FilePath: "main.go", Line: 7},
		{From: "pkg/server.go::Start", To: "pkg/db.go::Connect", Kind: graph.EdgeCalls, FilePath: "pkg/server.go", Line: 12},
		{From: "pkg/db.go::Connect", To: "pkg/db.go::Ping", Kind: graph.EdgeCalls, FilePath: "pkg/db.go", Line: 10},
		{From: "pkg/server.go", To: "pkg/db.go", Kind: graph.EdgeImports, FilePath: "pkg/server.go", Line: 3},
		{From: "pkg/db.go::DBImpl", To: "pkg/db.go::DB", Kind: graph.EdgeImplements, FilePath: "pkg/db.go", Line: 24},
		{From: "main.go::main", To: "pkg/db.go::DBImpl", Kind: graph.EdgeReferences, FilePath: "main.go", Line: 8},
	}
	for _, e := range edges {
		g.AddEdge(e)
	}

	return g
}

func TestGetSymbol(t *testing.T) {
	e := NewEngine(buildTestGraph())
	n := e.GetSymbol("pkg/db.go::Connect")
	require.NotNil(t, n)
	assert.Equal(t, "Connect", n.Name)

	assert.Nil(t, e.GetSymbol("nonexistent"))
}

func TestFindSymbols(t *testing.T) {
	e := NewEngine(buildTestGraph())

	results := e.FindSymbols("Connect")
	require.Len(t, results, 1)

	results = e.FindSymbols("Connect", graph.KindFunction)
	require.Len(t, results, 1)

	results = e.FindSymbols("Connect", graph.KindType)
	assert.Empty(t, results)
}

func TestGetFileSymbols(t *testing.T) {
	e := NewEngine(buildTestGraph())
	sg := e.GetFileSymbols("pkg/db.go")
	assert.GreaterOrEqual(t, len(sg.Nodes), 4) // file + Connect + Ping + DB + DBImpl
}

func TestGetDependencies(t *testing.T) {
	e := NewEngine(buildTestGraph())

	// main calls Start (depth 1).
	sg := e.GetDependencies("main.go::main", QueryOptions{Depth: 1, Limit: 50, Detail: "full"})
	assert.GreaterOrEqual(t, len(sg.Nodes), 2) // main + Start

	// Depth 2 should also reach Connect.
	sg = e.GetDependencies("main.go::main", QueryOptions{Depth: 2, Limit: 50, Detail: "full"})
	ids := nodeIDs(sg.Nodes)
	assert.Contains(t, ids, "pkg/server.go::Start")
	assert.Contains(t, ids, "pkg/db.go::Connect")
}

func TestGetDependents(t *testing.T) {
	e := NewEngine(buildTestGraph())

	// Who depends on Connect?
	sg := e.GetDependents("pkg/db.go::Connect", QueryOptions{Depth: 2, Limit: 50})
	ids := nodeIDs(sg.Nodes)
	assert.Contains(t, ids, "pkg/server.go::Start")
}

func TestGetCallChain(t *testing.T) {
	e := NewEngine(buildTestGraph())

	sg := e.GetCallChain("main.go::main", QueryOptions{Depth: 3, Limit: 50})
	ids := nodeIDs(sg.Nodes)
	assert.Contains(t, ids, "pkg/server.go::Start")
	assert.Contains(t, ids, "pkg/db.go::Connect")
	assert.Contains(t, ids, "pkg/db.go::Ping")
}

func TestGetCallers(t *testing.T) {
	e := NewEngine(buildTestGraph())

	sg := e.GetCallers("pkg/db.go::Ping", QueryOptions{Depth: 3, Limit: 50})
	ids := nodeIDs(sg.Nodes)
	assert.Contains(t, ids, "pkg/db.go::Connect")
}

func TestFindImplementations(t *testing.T) {
	e := NewEngine(buildTestGraph())

	impls := e.FindImplementations("pkg/db.go::DB")
	require.Len(t, impls, 1)
	assert.Equal(t, "DBImpl", impls[0].Name)
}

func TestFindUsages(t *testing.T) {
	e := NewEngine(buildTestGraph())

	sg := e.FindUsages("pkg/db.go::DBImpl")
	ids := nodeIDs(sg.Nodes)
	assert.Contains(t, ids, "main.go::main") // references DBImpl
}

func TestGetCluster(t *testing.T) {
	e := NewEngine(buildTestGraph())

	sg := e.GetCluster("pkg/server.go::Start", QueryOptions{Depth: 1, Limit: 50})
	assert.GreaterOrEqual(t, len(sg.Nodes), 3) // Start + main + Connect
}

func TestTruncation(t *testing.T) {
	e := NewEngine(buildTestGraph())

	sg := e.GetCallChain("main.go::main", QueryOptions{Depth: 10, Limit: 2})
	assert.True(t, sg.Truncated)
	assert.LessOrEqual(t, len(sg.Nodes), 2)
}

func TestCycleHandling(t *testing.T) {
	g := graph.New()
	g.AddNode(&graph.Node{ID: "a", Kind: graph.KindFunction, Name: "a", FilePath: "a.go", Language: "go"})
	g.AddNode(&graph.Node{ID: "b", Kind: graph.KindFunction, Name: "b", FilePath: "b.go", Language: "go"})
	g.AddEdge(&graph.Edge{From: "a", To: "b", Kind: graph.EdgeCalls, FilePath: "a.go", Line: 1})
	g.AddEdge(&graph.Edge{From: "b", To: "a", Kind: graph.EdgeCalls, FilePath: "b.go", Line: 1})

	e := NewEngine(g)
	sg := e.GetCallChain("a", QueryOptions{Depth: 10, Limit: 50})
	// Should terminate without infinite loop.
	assert.LessOrEqual(t, len(sg.Nodes), 2)
}

func TestStats(t *testing.T) {
	e := NewEngine(buildTestGraph())
	s := e.Stats()
	assert.Equal(t, 9, s.TotalNodes)
	assert.Equal(t, 6, s.TotalEdges)
	assert.Equal(t, 9, s.ByLanguage["go"])
}

func TestBriefModeStripsMeta(t *testing.T) {
	e := NewEngine(buildTestGraph())
	sg := e.GetDependencies("main.go::main", QueryOptions{Depth: 1, Limit: 50, Detail: "brief"})
	for _, n := range sg.Nodes {
		assert.Nil(t, n.Meta)
	}
}

func nodeIDs(nodes []*graph.Node) []string {
	ids := make([]string, len(nodes))
	for i, n := range nodes {
		ids[i] = n.ID
	}
	return ids
}
