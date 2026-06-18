package mcp

import (
	"strings"
	"testing"

	"github.com/zzet/gortex/internal/config"
	"github.com/zzet/gortex/internal/graph"
)

func TestSmartContextDefaultOff(t *testing.T) {
	s := &Server{} // no configManager → empty config
	// No include_* args → every in-pack section off.
	got := s.smartContextSections(map[string]any{}, "")
	if got.Any() {
		t.Errorf("smart_context in-pack sections should default off, got %+v", got)
	}

	// An explicit per-call opt-in turns the section on.
	on := s.smartContextSections(map[string]any{"include_call_paths": true}, "")
	if !on.CallPaths {
		t.Errorf("include_call_paths=true should enable CallPaths, got %+v", on)
	}
	if on.Flows || on.Confidence {
		t.Errorf("only CallPaths should be on, got %+v", on)
	}
}

func TestSmartContextDefaultOff_AttachNoop(t *testing.T) {
	s := &Server{}
	result := map[string]any{"relevant_symbols": []string{}}
	// Default-off sections leave the pack untouched.
	s.attachInPackSections(result, s.smartContextSections(map[string]any{}, ""), nil)
	if _, ok := result["in_pack"]; ok {
		t.Errorf("default-off should not add an in_pack block, got %+v", result["in_pack"])
	}
	// Opting call-paths in with no graph / no reachable paths adds no block.
	s.attachInPackSections(result, config.SmartContextSections{CallPaths: true}, nil)
	if _, ok := result["in_pack"]; ok {
		t.Errorf("opt-in with no reachable paths should add no block, got %+v", result["in_pack"])
	}
}

// smartCtxGraph builds a→b→focus, c→focus.
func smartCtxGraph() *graph.Graph {
	g := graph.New()
	for _, id := range []string{"focus", "a", "b", "c"} {
		g.AddNode(&graph.Node{ID: id, Kind: graph.KindFunction, Name: id, FilePath: "x.go"})
	}
	g.AddEdge(&graph.Edge{From: "a", To: "b", Kind: graph.EdgeCalls, Origin: graph.OriginASTResolved})
	g.AddEdge(&graph.Edge{From: "b", To: "focus", Kind: graph.EdgeCalls, Origin: graph.OriginASTResolved})
	g.AddEdge(&graph.Edge{From: "c", To: "focus", Kind: graph.EdgeCalls, Origin: graph.OriginASTResolved})
	return g
}

func TestSmartContextCallPaths(t *testing.T) {
	s := &Server{graph: smartCtxGraph()}
	// focus is the anchor (symbols[0]); a and c are roots.
	symbols := []*graph.Node{{ID: "focus"}, {ID: "a"}, {ID: "c"}}

	result := map[string]any{}
	s.attachInPackSections(result, config.SmartContextSections{CallPaths: true}, symbols)

	blk, ok := result["in_pack"].(map[string]any)
	if !ok {
		t.Fatalf("expected in_pack block, got %T", result["in_pack"])
	}
	cps, ok := blk["call_paths"].([]map[string]any)
	if !ok || len(cps) != 2 {
		t.Fatalf("expected 2 call_paths, got %T len=%d", blk["call_paths"], len(cps))
	}
	// Shortest first: c→focus (len 1) before a→b→focus (len 2).
	if cps[0]["root"] != "c" || cps[0]["length"] != 1 {
		t.Errorf("first path = %+v, want c len 1", cps[0])
	}
	if cps[1]["root"] != "a" || cps[1]["length"] != 2 {
		t.Errorf("second path = %+v, want a len 2", cps[1])
	}
	if cps[0]["anchor"] != "focus" {
		t.Errorf("anchor = %v, want focus", cps[0]["anchor"])
	}

	// Default-off leaves the pack untouched even with reachable symbols.
	off := map[string]any{}
	s.attachInPackSections(off, config.SmartContextSections{}, symbols)
	if _, ok := off["in_pack"]; ok {
		t.Errorf("call-paths off should add no block, got %+v", off["in_pack"])
	}
}

func TestGCXSmartContext_CallPaths(t *testing.T) {
	result := map[string]any{
		"relevant_symbols": []map[string]any{},
		"in_pack": map[string]any{
			"call_paths": []map[string]any{
				{"root": "c", "anchor": "focus", "length": 1, "confidence": 0.9, "nodes": []string{"c", "focus"}},
			},
		},
	}
	out, err := encodeSmartContext(result)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "smart_context.call_paths") {
		t.Errorf("GCX output missing call_paths section:\n%s", s)
	}
	if !strings.Contains(s, "c>focus") {
		t.Errorf("GCX output missing joined nodes 'c>focus':\n%s", s)
	}
}
