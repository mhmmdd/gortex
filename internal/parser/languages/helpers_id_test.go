package languages

import "testing"

// countByName returns how many extracted nodes carry the given name, and the
// set of their (distinct) IDs.
func countByName(nodes []*nodeForCount, name string) (int, []string) {
	var ids []string
	for _, n := range nodes {
		if n.name == name {
			ids = append(ids, n.id)
		}
	}
	return len(ids), ids
}

type nodeForCount struct {
	id   string
	name string
}

// TestDisambiguateID exercises the helper directly: first use keeps the base
// ID, a genuine collision on a different line gains a line suffix, and an exact
// re-match of the same declaration is reported as a drop.
func TestDisambiguateID(t *testing.T) {
	seen := map[string]bool{}

	id1, ok1 := disambiguateID(seen, "f.go::Foo", 10)
	if id1 != "f.go::Foo" || !ok1 {
		t.Fatalf("first use: got (%q,%v) want (f.go::Foo,true)", id1, ok1)
	}
	id2, ok2 := disambiguateID(seen, "f.go::Foo", 20)
	if id2 != "f.go::Foo_L20" || !ok2 {
		t.Fatalf("collision on a different line: got (%q,%v) want (f.go::Foo_L20,true)", id2, ok2)
	}
	if _, ok3 := disambiguateID(seen, "f.go::Foo", 10); ok3 {
		t.Fatalf("exact re-match (same id + line) must be dropped, got ok=true")
	}
	id4, ok4 := disambiguateID(seen, "f.go::Foo", 30)
	if id4 != "f.go::Foo_L30" || !ok4 {
		t.Fatalf("third collision: got (%q,%v) want (f.go::Foo_L30,true)", id4, ok4)
	}
	// A different base ID is unaffected.
	id5, ok5 := disambiguateID(seen, "f.go::Bar", 5)
	if id5 != "f.go::Bar" || !ok5 {
		t.Fatalf("unrelated base: got (%q,%v) want (f.go::Bar,true)", id5, ok5)
	}
}

// TestInitCollisionSurvive proves the Go extractor no longer drops a second
// func init(): both survive as distinct nodes, the collider gaining a line
// suffix, instead of one silently overwriting the other in the graph.
func TestInitCollisionSurvive(t *testing.T) {
	src := []byte("package p\n" +
		"func init() { a() }\n" +
		"func init() { b() }\n" +
		"func a() {}\n" +
		"func b() {}\n")
	res, err := NewGoExtractor().Extract("p.go", src)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	var nodes []*nodeForCount
	for _, n := range res.Nodes {
		nodes = append(nodes, &nodeForCount{id: n.ID, name: n.Name})
	}
	n, ids := countByName(nodes, "init")
	if n != 2 {
		t.Fatalf("expected 2 surviving init nodes, got %d (%v)", n, ids)
	}
	if ids[0] == ids[1] {
		t.Fatalf("the two init nodes share an ID %q — one would overwrite the other", ids[0])
	}
}

// TestOverloadSurvive proves redefinition/overload sets are no longer collapsed
// to a single node across the dynamic-language extractors: a Python @overload
// triple and a Ruby method redeclared in one class both keep every definition.
func TestOverloadSurvive(t *testing.T) {
	t.Run("python_overload", func(t *testing.T) {
		src := []byte("from typing import overload\n" +
			"@overload\n" +
			"def f(x: int) -> int: ...\n" +
			"@overload\n" +
			"def f(x: str) -> str: ...\n" +
			"def f(x): return x\n")
		res, err := NewPythonExtractor().Extract("m.py", src)
		if err != nil {
			t.Fatalf("extract: %v", err)
		}
		var nodes []*nodeForCount
		for _, n := range res.Nodes {
			nodes = append(nodes, &nodeForCount{id: n.ID, name: n.Name})
		}
		if n, ids := countByName(nodes, "f"); n != 3 {
			t.Fatalf("expected 3 surviving overloads of f, got %d (%v)", n, ids)
		}
	})

	t.Run("ruby_redefinition", func(t *testing.T) {
		src := []byte("class Greeter\n" +
			"  def greet\n" +
			"    'hi'\n" +
			"  end\n" +
			"  def greet\n" +
			"    'hello'\n" +
			"  end\n" +
			"end\n")
		res, err := NewRubyExtractor().Extract("g.rb", src)
		if err != nil {
			t.Fatalf("extract: %v", err)
		}
		var nodes []*nodeForCount
		for _, n := range res.Nodes {
			nodes = append(nodes, &nodeForCount{id: n.ID, name: n.Name})
		}
		if n, ids := countByName(nodes, "greet"); n != 2 {
			t.Fatalf("expected 2 surviving greet definitions, got %d (%v)", n, ids)
		}
	})
}
