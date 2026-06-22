package languages

import (
	"testing"

	"github.com/zzet/gortex/internal/graph"
)

// swiftRefEdge reports whether an edge exists from `from` to
// "unresolved::"+typeName with the given kind and (for EdgeReferences) the
// given ref_context, stamped OriginASTResolved so cross_pkg_guard leaves it alone.
func swiftRefEdge(edges []*graph.Edge, from, typeName string, kind graph.EdgeKind, useKind string) bool {
	for _, e := range edges {
		if e.Kind != kind || e.From != from || e.To != "unresolved::"+typeName {
			continue
		}
		if e.Origin != graph.OriginASTResolved {
			continue
		}
		got := ""
		if e.Meta != nil {
			got, _ = e.Meta["ref_context"].(string)
		}
		if got == useKind {
			return true
		}
	}
	return false
}

// swiftHasRefTo reports whether any EdgeInstantiates / EdgeReferences edge
// targets "unresolved::"+typeName, regardless of owner / ref_context. Used to
// assert that primitives and excluded forms emit nothing.
func swiftHasRefTo(edges []*graph.Edge, typeName string) bool {
	for _, e := range edges {
		if e.Kind != graph.EdgeInstantiates && e.Kind != graph.EdgeReferences {
			continue
		}
		if e.To == "unresolved::"+typeName {
			return true
		}
	}
	return false
}

func TestSwiftExtractor_Instantiation(t *testing.T) {
	// `Foo()` and `Foo.init(...)` are constructions (Swift has no `new`):
	// EdgeInstantiates from the enclosing function. A lowercase callee
	// (`foo()`) is a plain call, never an instantiation.
	src := []byte(`func build() {
    let a = Widget()
    let b = Store.init()
    foo()
    bar.baz()
}
`)
	res, err := NewSwiftExtractor().Extract("b.swift", src)
	if err != nil {
		t.Fatal(err)
	}
	if !swiftRefEdge(res.Edges, "b.swift::build", "Widget", graph.EdgeInstantiates, "") {
		t.Errorf("expected EdgeInstantiates build -> Widget for `Widget()`; edges=%v", res.Edges)
	}
	if !swiftRefEdge(res.Edges, "b.swift::build", "Store", graph.EdgeInstantiates, "") {
		t.Errorf("expected EdgeInstantiates build -> Store for `Store.init()`; edges=%v", res.Edges)
	}
	// Lowercase callees are not instantiations.
	if swiftHasRefTo(res.Edges, "foo") {
		t.Errorf("`foo()` must NOT produce an instantiation edge; edges=%v", res.Edges)
	}
}

func TestSwiftExtractor_InheritanceAndConformance(t *testing.T) {
	// `class X: Base, Proto` references both a superclass and a conformed
	// protocol; `struct S: Codable` references a protocol; `extension X: P`
	// adds a conformance. Each lands an EdgeReferences ref_context=inherit
	// attributed to the declared type.
	src := []byte(`class X: Base, Proto {
}
struct S: Codable {
}
extension X: Equatable {
}
`)
	res, err := NewSwiftExtractor().Extract("i.swift", src)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []struct{ from, typ string }{
		{"i.swift::X", "Base"},
		{"i.swift::X", "Proto"},
		{"i.swift::S", "Codable"},
		{"i.swift::X", "Equatable"},
	} {
		if !swiftRefEdge(res.Edges, want.from, want.typ, graph.EdgeReferences, graph.RefContextInherit) {
			t.Errorf("expected inherit edge %s -> %s; edges=%v", want.from, want.typ, res.Edges)
		}
	}
}

func TestSwiftExtractor_CastsAndTypeTests(t *testing.T) {
	// `x as Foo`, `x as? Foo`, `x as! Foo` (as_expression) and `x is Bar`
	// (check_expression) each reference the RHS type with ref_context=cast.
	src := []byte(`func check(x: Any) {
    let a = x as Foo
    let b = x as? Foo
    let c = x as! Foo
    if x is Bar {
    }
    let n = x as? Int
}
`)
	res, err := NewSwiftExtractor().Extract("c.swift", src)
	if err != nil {
		t.Fatal(err)
	}
	if !swiftRefEdge(res.Edges, "c.swift::check", "Foo", graph.EdgeReferences, graph.RefContextCast) {
		t.Errorf("expected cast edge check -> Foo for `x as Foo`; edges=%v", res.Edges)
	}
	if !swiftRefEdge(res.Edges, "c.swift::check", "Bar", graph.EdgeReferences, graph.RefContextCast) {
		t.Errorf("expected cast edge check -> Bar for `x is Bar`; edges=%v", res.Edges)
	}
	// Primitive cast target `Int` emits nothing.
	if swiftHasRefTo(res.Edges, "Int") {
		t.Errorf("primitive cast target `Int` must NOT produce a reference edge; edges=%v", res.Edges)
	}
}

func TestSwiftExtractor_StaticAccess(t *testing.T) {
	// `Foo.shared` / `Foo.Constant`: a navigation_expression whose head is a
	// bare Capitalized identifier → EdgeReferences ref_context=static_access. A
	// `self.x` head (lowercase / self receiver) emits nothing.
	src := []byte(`func use() {
    let a = Manager.shared
    let b = Config.Default
    self.value = 1
    instance.field = 2
}
`)
	res, err := NewSwiftExtractor().Extract("s.swift", src)
	if err != nil {
		t.Fatal(err)
	}
	if !swiftRefEdge(res.Edges, "s.swift::use", "Manager", graph.EdgeReferences, graph.RefContextStaticAccess) {
		t.Errorf("expected static_access edge use -> Manager for `Manager.shared`; edges=%v", res.Edges)
	}
	if !swiftRefEdge(res.Edges, "s.swift::use", "Config", graph.EdgeReferences, graph.RefContextStaticAccess) {
		t.Errorf("expected static_access edge use -> Config for `Config.Default`; edges=%v", res.Edges)
	}
	// `self.value` and `instance.field` have lowercase / self heads.
	if swiftHasRefTo(res.Edges, "self") || swiftHasRefTo(res.Edges, "instance") {
		t.Errorf("self / lowercase navigation heads must NOT produce static_access edges; edges=%v", res.Edges)
	}
}

func TestSwiftExtractor_GenericArgs(t *testing.T) {
	// Element types named inside a generic argument clause (`Array<Foo>`,
	// `Dictionary<String, Foo>`, `Result<Foo, E>`) or the array / dictionary
	// sugar (`[Foo]`, `[K: Foo]`) are lost by the type-annotation pass (which
	// normalises a type to its head and drops the `<…>` args). The reference
	// form must recover each element type as an EdgeReferences ref_context=
	// generic_arg in every type position — a var / property annotation, a
	// parameter, and a (nested) return type — while a primitive argument
	// (`Array<Int>`) emits nothing.
	src := []byte(`func build(items: [Widget], lookup: Dictionary<String, Cache>) {
    let xs: Array<Element> = []
    let ns: Array<Int> = []
}
func make() -> Result<Box<Inner>, MyError> {
    fatalError()
}
`)
	res, err := NewSwiftExtractor().Extract("g.swift", src)
	if err != nil {
		t.Fatal(err)
	}

	// Var / property annotation inside build(): `Array<Element>` → Element.
	if !swiftRefEdge(res.Edges, "g.swift::build", "Element", graph.EdgeReferences, "generic_arg") {
		t.Errorf("expected generic_arg edge build -> Element for `Array<Element>`; edges=%v", res.Edges)
	}
	// Parameter array sugar `[Widget]` → Widget.
	if !swiftRefEdge(res.Edges, "g.swift::build", "Widget", graph.EdgeReferences, "generic_arg") {
		t.Errorf("expected generic_arg edge build -> Widget for `[Widget]`; edges=%v", res.Edges)
	}
	// Parameter generic `Dictionary<String, Cache>` → Cache (String dropped).
	if !swiftRefEdge(res.Edges, "g.swift::build", "Cache", graph.EdgeReferences, "generic_arg") {
		t.Errorf("expected generic_arg edge build -> Cache for `Dictionary<String, Cache>`; edges=%v", res.Edges)
	}
	// Nested return generic `Result<Box<Inner>, MyError>`: the outer clause
	// contributes Box and MyError; the walker visits the inner `<Inner>`
	// clause separately for Inner.
	for _, typ := range []string{"Box", "MyError", "Inner"} {
		if !swiftRefEdge(res.Edges, "g.swift::make", typ, graph.EdgeReferences, "generic_arg") {
			t.Errorf("expected generic_arg edge make -> %s in `Result<Box<Inner>, MyError>`; edges=%v", typ, res.Edges)
		}
	}

	// A primitive generic argument (`Array<Int>`) must NOT emit a generic_arg
	// reference; String (the dictionary key) must not either.
	for _, e := range res.Edges {
		if e.Kind != graph.EdgeReferences || e.Meta == nil {
			continue
		}
		if uk, _ := e.Meta["ref_context"].(string); uk != "generic_arg" {
			continue
		}
		switch e.To {
		case "unresolved::Int", "unresolved::String":
			t.Errorf("primitive generic argument must NOT produce a generic_arg edge; got %v", e)
		}
	}
}

func TestSwiftExtractor_ReferenceFormNegatives(t *testing.T) {
	// Nothing in this function names a user type via a reference form: a
	// lowercase call, a self access, and a primitive annotation must each
	// stay silent.
	src := []byte(`func quiet() {
    foo()
    self.x = 1
    let n: Int = 0
}
`)
	res, err := NewSwiftExtractor().Extract("q.swift", src)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range res.Edges {
		if e.Kind == graph.EdgeInstantiates {
			t.Errorf("no instantiation expected in quiet(); got %v", e)
		}
		if e.Kind == graph.EdgeReferences {
			t.Errorf("no reference form expected in quiet(); got %v", e)
		}
	}
	if swiftHasRefTo(res.Edges, "Int") {
		t.Errorf("primitive `Int` must NOT produce a reference edge; edges=%v", res.Edges)
	}
}
