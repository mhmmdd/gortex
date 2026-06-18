package golden

import "github.com/zzet/gortex/internal/graph"

// capabilities is the registry of ported extraction features under golden
// regression. Each entry is self-contained — a snippet plus the nodes/edges the
// feature must produce — so a failure points straight at the capability that
// regressed. Extraction-layer only: every assertion is satisfiable from the raw
// extractor output, no resolver or daemon required.
var capabilities = []Capability{
	{
		Name:     "java/annotation-type-as-interface",
		Language: "java",
		FileName: "Audited.java",
		Source: `package com.app;
public @interface Audited {
    String value() default "";
}
`,
		WantNodes: []nodeWant{
			{Kind: graph.KindInterface, Name: "Audited"},
		},
	},
	{
		Name:     "javascript/arrow-class-field-as-method",
		Language: "javascript",
		FileName: "comp.js",
		Source: `class Counter {
  inc = () => { this.n++; };
}
`,
		WantNodes: []nodeWant{
			{Kind: graph.KindMethod, Name: "inc"},
		},
	},
	{
		Name:     "java/anonymous-class-synthetic-type",
		Language: "java",
		FileName: "Host.java",
		Source: `package com.app;
class Host {
    void wire() {
        Runnable r = new Runnable() {
            public void run() {}
        };
    }
}
`,
		WantNodes: []nodeWant{
			{Kind: graph.KindType, Name: "Runnable$anon@4", MetaKey: "anonymous", MetaVal: true},
		},
		WantEdges: []edgeWant{
			{Kind: graph.EdgeExtends, ToSub: "unresolved::Runnable"},
		},
	},
	{
		Name:     "csharp/anonymous-type-synthetic-type",
		Language: "csharp",
		FileName: "Host.cs",
		Source: `namespace App;
class Host {
    void Wire() {
        var p = new { Name = "x", Age = 5 };
    }
}
`,
		WantNodes: []nodeWant{
			{Kind: graph.KindType, Name: "anon@4", MetaKey: "anonymous", MetaVal: true},
		},
		WantEdges: []edgeWant{
			{Kind: graph.EdgeExtends, ToSub: "unresolved::object"},
		},
	},
	{
		Name:     "typescript/per-binding-import",
		Language: "typescript",
		FileName: "app.ts",
		Source: `import { Router, json as parseJson } from "express";
`,
		WantEdges: []edgeWant{
			{Kind: graph.EdgeImports, ToSub: "unresolved::import::express::Router"},
			{Kind: graph.EdgeImports, ToSub: "unresolved::import::express::json", Alias: "parseJson"},
		},
	},
	{
		Name:     "typescript/alias-aware-re-export",
		Language: "typescript",
		FileName: "barrel.ts",
		Source: `export { a, b as c } from "up";
export * as ns from "nsmod";
`,
		WantEdges: []edgeWant{
			{Kind: graph.EdgeReExports, ToSub: "unresolved::import::up::a"},
			{Kind: graph.EdgeReExports, ToSub: "unresolved::import::up::b", Alias: "c"},
			{Kind: graph.EdgeReExports, ToSub: "unresolved::import::nsmod", Alias: "ns"},
		},
	},
	{
		Name:     "javascript/per-binding-import-and-re-export",
		Language: "javascript",
		FileName: "barrel.js",
		Source: `import { foo, bar as baz } from "mod";
export { x as y } from "down";
`,
		WantEdges: []edgeWant{
			{Kind: graph.EdgeImports, ToSub: "unresolved::import::mod::foo"},
			{Kind: graph.EdgeImports, ToSub: "unresolved::import::mod::bar", Alias: "baz"},
			{Kind: graph.EdgeReExports, ToSub: "unresolved::import::down::x", Alias: "y"},
		},
	},
}
