package languages

import (
	"testing"

	"github.com/zzet/gortex/internal/graph"
	"github.com/zzet/gortex/internal/parser"
)

// TestChainReturnTypeResolution exercises the shared chained-receiver /
// factory-chain resolver across the idioms of the AST languages that feed it
// (Go, TypeScript, C#). The resolver reads only return_type / receiver Meta, so
// one synthetic result covers every language: each sub-case models a different
// language's static-factory or fluent-builder shape and asserts the receiver
// expression resolves to the type the chain evaluates to.
func TestChainReturnTypeResolution(t *testing.T) {
	mkSym := func(name, recv, ret string) *graph.Node {
		meta := map[string]any{}
		kind := graph.KindFunction
		if recv != "" {
			meta["receiver"] = recv
			kind = graph.KindMethod
		}
		if ret != "" {
			meta["return_type"] = ret
		}
		return &graph.Node{Name: name, Kind: kind, Meta: meta}
	}

	result := &parser.ExtractionResult{Nodes: []*graph.Node{
		// Go: func NewServer() *Server; func (*Server) Router() *Router
		mkSym("NewServer", "", "Server"),
		mkSym("Router", "Server", "Router"),
		// TypeScript: function createClient(): ApiClient; ApiClient.users(): UserApi
		mkSym("createClient", "", "ApiClient"),
		mkSym("users", "ApiClient", "UserApi"),
		// C#: static Builder Create(); Widget Builder.Build()
		mkSym("Create", "", "Builder"),
		mkSym("Build", "Builder", "Widget"),
		// A method that shares a name with a factory, to prove the free
		// function wins as a chain seed over a same-named method.
		mkSym("NewServer", "Other", "Decoy"),
	}}

	cases := []struct {
		name string
		expr string
		tenv typeEnv
		want string
	}{
		// Factory chains: the base segment is a called free function, not a
		// typed variable — the J3 capability.
		{"go_factory_single", "NewServer()", nil, "Server"},
		{"go_factory_chain", "NewServer().Router()", nil, "Router"},
		{"ts_factory_chain", "createClient().users()", nil, "UserApi"},
		{"csharp_factory_chain", "Create().Build()", nil, "Widget"},
		// A typed variable still seeds the chain as before.
		{"var_method_chain", "srv.Router()", typeEnv{"srv": "Server"}, "Router"},
		// An ordinary method call on an untyped variable must NOT be treated
		// as a factory seed (base is not itself a call).
		{"untyped_var_no_factory", "obj.Router()", nil, ""},
		// Unresolvable: the base factory has no known return type.
		{"unknown_factory", "mystery().Build()", nil, ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := resolveChainType(c.expr, c.tenv, result)
			if got != c.want {
				t.Errorf("resolveChainType(%q) = %q, want %q", c.expr, got, c.want)
			}
		})
	}
}

// TestChainTypeDartCppPascalFactoryReceiver drives the chained-static-factory
// resolver end-to-end through the *real* Dart, C++, and Pascal extractors —
// not synthetic nodes. It proves the return_type stamping wired into each
// extractor (Dart method/factory return types, C++ class-method return types,
// and Pascal constructors that yield their enclosing class) feeds the shared
// `resolveChainType` walker so a `Type.factory().builder()` chain resolves to
// the type the chain evaluates to. The `::` / `.` member-access normalisation
// lets one walker serve all three idioms — including C++'s `Foo::create()`
// scope-resolution syntax — which is the cross-language reach codegraph's
// single-language receiver heuristic does not attempt.
func TestChainTypeDartCppPascalFactoryReceiver(t *testing.T) {
	cases := []struct {
		name string
		lang string
		file string
		src  string
		expr string
		want string
	}{
		{
			name: "dart_static_factory_chain",
			lang: "dart",
			file: "widget.dart",
			src: "class Widget {\n" +
				"  static Widget create() => Widget();\n" +
				"  Widget build() => this;\n" +
				"}\n",
			expr: "Widget.create().build()",
			want: "Widget",
		},
		{
			// C++ uses scope-resolution `::` for the static factory; the
			// resolver normalises it to `.` so the same walker types the chain.
			name: "cpp_scope_resolution_chain",
			lang: "cpp",
			file: "foo.cpp",
			src: "class Foo {\n" +
				"public:\n" +
				"  static Foo create() { return Foo(); }\n" +
				"  Foo build() { return *this; }\n" +
				"};\n",
			expr: "Foo::create().build()",
			want: "Foo",
		},
		{
			// Pascal: a constructor yields an instance of its class, so the
			// chain seed comes from the constructor's synthesised return type.
			name: "pascal_constructor_chain",
			lang: "pascal",
			file: "unit.pas",
			src: "unit U;\n" +
				"interface\n" +
				"type\n" +
				"  TFoo = class\n" +
				"    constructor Create;\n" +
				"    function Bar: TFoo;\n" +
				"  end;\n" +
				"implementation\n" +
				"end.\n",
			expr: "TFoo.Create.Bar",
			want: "TFoo",
		},
		{
			// Decoy: an unknown base type must never seed the static-factory
			// chain — keeps the inference from typing arbitrary identifiers.
			name: "dart_unknown_base_decoy",
			lang: "dart",
			file: "widget.dart",
			src: "class Widget {\n" +
				"  static Widget create() => Widget();\n" +
				"}\n",
			expr: "Mystery.create().build()",
			want: "",
		},
	}

	extract := func(t *testing.T, lang, file, src string) *parser.ExtractionResult {
		t.Helper()
		var ex parser.Extractor
		switch lang {
		case "dart":
			ex = NewDartExtractor()
		case "cpp":
			ex = NewCppExtractor()
		case "pascal":
			ex = NewPascalExtractor()
		default:
			t.Fatalf("unhandled lang %q", lang)
		}
		res, err := ex.Extract(file, []byte(src))
		if err != nil {
			t.Fatalf("Extract(%s) error: %v", lang, err)
		}
		return res
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := extract(t, c.lang, c.file, c.src)
			got := resolveChainType(c.expr, typeEnv{}, res)
			if got != c.want {
				t.Errorf("resolveChainType(%q) [%s] = %q, want %q", c.expr, c.lang, got, c.want)
			}
		})
	}
}
