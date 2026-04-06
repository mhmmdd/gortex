package graph

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeNode(id, name string, kind NodeKind, file, lang string) *Node {
	return &Node{
		ID:        id,
		Kind:      kind,
		Name:      name,
		QualName:  "pkg." + name,
		FilePath:  file,
		StartLine: 1,
		EndLine:   10,
		Language:  lang,
	}
}

func TestAddAndGetNode(t *testing.T) {
	g := New()
	n := makeNode("a.go::Foo", "Foo", KindFunction, "a.go", "go")
	g.AddNode(n)

	assert.Equal(t, n, g.GetNode("a.go::Foo"))
	assert.Equal(t, n, g.GetNodeByQualName("pkg.Foo"))
	assert.Equal(t, []*Node{n}, g.FindNodesByName("Foo"))
	assert.Equal(t, []*Node{n}, g.GetFileNodes("a.go"))
	assert.Nil(t, g.GetNode("nonexistent"))
}

func TestAddAndGetEdge(t *testing.T) {
	g := New()
	n1 := makeNode("a.go::Foo", "Foo", KindFunction, "a.go", "go")
	n2 := makeNode("b.go::Bar", "Bar", KindFunction, "b.go", "go")
	g.AddNode(n1)
	g.AddNode(n2)

	e := &Edge{From: n1.ID, To: n2.ID, Kind: EdgeCalls, FilePath: "a.go", Line: 5}
	g.AddEdge(e)

	out := g.GetOutEdges(n1.ID)
	require.Len(t, out, 1)
	assert.Equal(t, EdgeCalls, out[0].Kind)

	in := g.GetInEdges(n2.ID)
	require.Len(t, in, 1)
	assert.Equal(t, n1.ID, in[0].From)
}

func TestEvictFile(t *testing.T) {
	g := New()
	n1 := makeNode("a.go::Foo", "Foo", KindFunction, "a.go", "go")
	n2 := makeNode("a.go::Bar", "Bar", KindFunction, "a.go", "go")
	n3 := makeNode("b.go::Baz", "Baz", KindFunction, "b.go", "go")
	g.AddNode(n1)
	g.AddNode(n2)
	g.AddNode(n3)

	g.AddEdge(&Edge{From: n1.ID, To: n3.ID, Kind: EdgeCalls, FilePath: "a.go", Line: 1})
	g.AddEdge(&Edge{From: n3.ID, To: n2.ID, Kind: EdgeCalls, FilePath: "b.go", Line: 2})

	nodesRm, edgesRm := g.EvictFile("a.go")
	assert.Equal(t, 2, nodesRm)
	assert.Equal(t, 2, edgesRm) // both edges reference evicted node IDs

	assert.Nil(t, g.GetNode("a.go::Foo"))
	assert.Nil(t, g.GetNode("a.go::Bar"))
	assert.NotNil(t, g.GetNode("b.go::Baz"))

	// Edge from b.go to a.go::Bar should also be cleaned from inEdges.
	assert.Empty(t, g.GetOutEdges("a.go::Foo"))
}

func TestEvictFile_NoNodes(t *testing.T) {
	g := New()
	n, e := g.EvictFile("nonexistent.go")
	assert.Equal(t, 0, n)
	assert.Equal(t, 0, e)
}

func TestNodeAndEdgeCount(t *testing.T) {
	g := New()
	g.AddNode(makeNode("a.go::A", "A", KindFunction, "a.go", "go"))
	g.AddNode(makeNode("b.go::B", "B", KindType, "b.go", "go"))
	g.AddEdge(&Edge{From: "a.go::A", To: "b.go::B", Kind: EdgeReferences, FilePath: "a.go", Line: 1})

	assert.Equal(t, 2, g.NodeCount())
	assert.Equal(t, 1, g.EdgeCount())
}

func TestStats(t *testing.T) {
	g := New()
	g.AddNode(makeNode("a.go::A", "A", KindFunction, "a.go", "go"))
	g.AddNode(makeNode("b.go::B", "B", KindType, "b.go", "go"))
	g.AddNode(makeNode("c.ts::C", "C", KindFunction, "c.ts", "typescript"))
	g.AddEdge(&Edge{From: "a.go::A", To: "b.go::B", Kind: EdgeReferences, FilePath: "a.go", Line: 1})

	s := g.Stats()
	assert.Equal(t, 3, s.TotalNodes)
	assert.Equal(t, 1, s.TotalEdges)
	assert.Equal(t, 2, s.ByKind["function"])
	assert.Equal(t, 1, s.ByKind["type"])
	assert.Equal(t, 2, s.ByLanguage["go"])
	assert.Equal(t, 1, s.ByLanguage["typescript"])
}

func TestAllNodesAndEdges(t *testing.T) {
	g := New()
	g.AddNode(makeNode("a.go::A", "A", KindFunction, "a.go", "go"))
	g.AddNode(makeNode("b.go::B", "B", KindFunction, "b.go", "go"))
	g.AddEdge(&Edge{From: "a.go::A", To: "b.go::B", Kind: EdgeCalls, FilePath: "a.go", Line: 1})

	assert.Len(t, g.AllNodes(), 2)
	assert.Len(t, g.AllEdges(), 1)
}

func TestConcurrency(t *testing.T) {
	g := New()
	var wg sync.WaitGroup

	// Concurrent writers.
	for i := range 50 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := "file.go::" + string(rune('A'+i))
			n := makeNode(id, string(rune('A'+i)), KindFunction, "file.go", "go")
			n.QualName = "" // avoid collision
			g.AddNode(n)
		}(i)
	}

	// Concurrent readers.
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = g.NodeCount()
			_ = g.GetFileNodes("file.go")
			_ = g.Stats()
		}()
	}

	wg.Wait()
}

func TestNodeBrief(t *testing.T) {
	n := &Node{
		ID: "a.go::Foo", Kind: KindFunction, Name: "Foo",
		QualName: "pkg.Foo", FilePath: "a.go", StartLine: 10, EndLine: 20,
		Language: "go", Meta: map[string]any{"signature": "func Foo()"},
	}
	b := n.Brief()
	assert.Equal(t, "a.go::Foo", b["id"])
	assert.Equal(t, "Foo", b["name"])
	assert.Equal(t, NodeKind("function"), b["kind"])
	assert.Equal(t, "a.go", b["file_path"])
	assert.Equal(t, 10, b["start_line"])
	// Should NOT contain meta, qual_name, end_line, language.
	_, hasMeta := b["meta"]
	assert.False(t, hasMeta)
}

func TestValidNodeKind(t *testing.T) {
	assert.True(t, ValidNodeKind(KindFunction))
	assert.True(t, ValidNodeKind(KindFile))
	assert.False(t, ValidNodeKind(NodeKind("unknown")))
}
