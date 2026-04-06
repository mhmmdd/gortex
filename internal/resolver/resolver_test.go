package resolver

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/zzet/gortex/internal/graph"
)

func TestResolveAll_InternalCall(t *testing.T) {
	g := graph.New()
	g.AddNode(&graph.Node{ID: "pkg/a.go", Kind: graph.KindFile, Name: "a.go", FilePath: "pkg/a.go", Language: "go"})
	g.AddNode(&graph.Node{ID: "pkg/b.go", Kind: graph.KindFile, Name: "b.go", FilePath: "pkg/b.go", Language: "go"})
	g.AddNode(&graph.Node{ID: "pkg/a.go::Foo", Kind: graph.KindFunction, Name: "Foo", FilePath: "pkg/a.go", Language: "go"})
	g.AddNode(&graph.Node{ID: "pkg/b.go::Bar", Kind: graph.KindFunction, Name: "Bar", FilePath: "pkg/b.go", Language: "go"})

	// Foo calls Bar (unresolved).
	callEdge := &graph.Edge{From: "pkg/a.go::Foo", To: "unresolved::Bar", Kind: graph.EdgeCalls, FilePath: "pkg/a.go", Line: 5}
	g.AddEdge(callEdge)

	r := New(g)
	stats := r.ResolveAll()

	assert.Equal(t, 1, stats.Resolved)
	assert.Equal(t, "pkg/b.go::Bar", callEdge.To)
}

func TestResolveAll_ExternalImport(t *testing.T) {
	g := graph.New()
	g.AddNode(&graph.Node{ID: "main.go", Kind: graph.KindFile, Name: "main.go", FilePath: "main.go", Language: "go"})

	importEdge := &graph.Edge{From: "main.go", To: "unresolved::import::fmt", Kind: graph.EdgeImports, FilePath: "main.go", Line: 3}
	g.AddEdge(importEdge)

	r := New(g)
	stats := r.ResolveAll()

	assert.Equal(t, 1, stats.External)
	assert.Equal(t, "external::fmt", importEdge.To)
}

func TestResolveAll_MethodCall(t *testing.T) {
	g := graph.New()
	g.AddNode(&graph.Node{ID: "pkg/a.go", Kind: graph.KindFile, Name: "a.go", FilePath: "pkg/a.go", Language: "go"})
	g.AddNode(&graph.Node{ID: "pkg/a.go::Caller", Kind: graph.KindFunction, Name: "Caller", FilePath: "pkg/a.go", Language: "go"})
	g.AddNode(&graph.Node{ID: "pkg/b.go::Server.Start", Kind: graph.KindMethod, Name: "Start", FilePath: "pkg/b.go", Language: "go"})

	callEdge := &graph.Edge{From: "pkg/a.go::Caller", To: "unresolved::*.Start", Kind: graph.EdgeCalls, FilePath: "pkg/a.go", Line: 10}
	g.AddEdge(callEdge)

	r := New(g)
	stats := r.ResolveAll()

	assert.Equal(t, 1, stats.Resolved)
	assert.Equal(t, "pkg/b.go::Server.Start", callEdge.To)
}

func TestResolveAll_Unresolvable(t *testing.T) {
	g := graph.New()
	g.AddNode(&graph.Node{ID: "a.go::Foo", Kind: graph.KindFunction, Name: "Foo", FilePath: "a.go", Language: "go"})

	callEdge := &graph.Edge{From: "a.go::Foo", To: "unresolved::NonExistent", Kind: graph.EdgeCalls, FilePath: "a.go", Line: 5}
	g.AddEdge(callEdge)

	r := New(g)
	stats := r.ResolveAll()

	assert.Equal(t, 1, stats.Unresolved)
}

func TestResolveFile(t *testing.T) {
	g := graph.New()
	g.AddNode(&graph.Node{ID: "a.go", Kind: graph.KindFile, Name: "a.go", FilePath: "a.go", Language: "go"})
	g.AddNode(&graph.Node{ID: "a.go::Foo", Kind: graph.KindFunction, Name: "Foo", FilePath: "a.go", Language: "go"})
	g.AddNode(&graph.Node{ID: "b.go::Bar", Kind: graph.KindFunction, Name: "Bar", FilePath: "b.go", Language: "go"})

	callEdge := &graph.Edge{From: "a.go::Foo", To: "unresolved::Bar", Kind: graph.EdgeCalls, FilePath: "a.go", Line: 5}
	g.AddEdge(callEdge)

	r := New(g)
	stats := r.ResolveFile("a.go")

	assert.Equal(t, 1, stats.Resolved)
	assert.Equal(t, "b.go::Bar", callEdge.To)
}
