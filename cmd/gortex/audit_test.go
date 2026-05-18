package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/zzet/gortex/internal/graph"
)

func TestScoreGradeForAudit_Boundaries(t *testing.T) {
	cases := []struct {
		score float64
		want  string
	}{
		{100, "A"},
		{85.01, "A"},
		{85, "A"},
		{84.99, "B"},
		{70, "B"},
		{69.99, "C"},
		{55, "C"},
		{40, "D"},
		{39.99, "F"},
		{0, "F"},
		{-1, "F"},
	}
	for _, c := range cases {
		if got := scoreGradeForAudit(c.score); got != c.want {
			t.Errorf("scoreGradeForAudit(%.2f) = %q, want %q", c.score, got, c.want)
		}
	}
}

func TestComputeAuditReport_EmptyGraphReturnsF(t *testing.T) {
	g := graph.New()
	r := computeAuditReport(g)
	if r.SymbolCount != 0 {
		t.Errorf("empty graph SymbolCount = %d, want 0", r.SymbolCount)
	}
	if r.Grade != "F" {
		t.Errorf("empty graph Grade = %q, want F", r.Grade)
	}
}

func TestComputeAuditReport_ScoresAndGradesPopulated(t *testing.T) {
	g := graph.New()
	// Three callable symbols: low-coupling (high complexity-health),
	// medium-coupling, high-coupling (low complexity-health).
	g.AddNode(&graph.Node{ID: "f.go::low", Name: "low", Kind: graph.KindFunction, FilePath: "f.go", StartLine: 1})
	g.AddNode(&graph.Node{ID: "f.go::med", Name: "med", Kind: graph.KindFunction, FilePath: "f.go", StartLine: 10})
	g.AddNode(&graph.Node{ID: "f.go::hot", Name: "hot", Kind: graph.KindFunction, FilePath: "f.go", StartLine: 20})
	// Hot has 10 callers + 5 callees → raw = 20 + 7.5 = 27.5 →
	// complexity_health = 100 / (1 + 27.5/20) ≈ 42.1 → D
	for i := range 10 {
		g.AddNode(&graph.Node{ID: "c.go::caller_" + itoaAudit(i), Kind: graph.KindFunction})
		g.AddEdge(&graph.Edge{From: "c.go::caller_" + itoaAudit(i), To: "f.go::hot", Kind: graph.EdgeCalls})
	}
	for i := range 5 {
		g.AddNode(&graph.Node{ID: "callee.go::callee_" + itoaAudit(i), Kind: graph.KindFunction})
		g.AddEdge(&graph.Edge{From: "f.go::hot", To: "callee.go::callee_" + itoaAudit(i), Kind: graph.EdgeCalls})
	}
	// Med has 3 callers + 0 callees → raw = 6 → ≈ 76.9 → B
	for i := range 3 {
		g.AddEdge(&graph.Edge{From: "c.go::caller_" + itoaAudit(i), To: "f.go::med", Kind: graph.EdgeCalls})
	}

	r := computeAuditReport(g)
	if r.SymbolCount == 0 {
		t.Fatalf("expected callable symbols in report, got 0")
	}
	if r.MeanScore <= 0 || r.MeanScore > 100 {
		t.Errorf("MeanScore = %.2f, want 0 < score ≤ 100", r.MeanScore)
	}
	if r.Grade == "" {
		t.Errorf("Grade is empty")
	}
	// Worst-symbol should be the hot one (most coupling = lowest
	// complexity-health). Tied-score tie-break is file/line so a
	// solo-hot row sorts first.
	if len(r.WorstSymbols) == 0 || r.WorstSymbols[0].ID != "f.go::hot" {
		t.Errorf("expected hot to be the worst symbol, got %+v", r.WorstSymbols)
	}
}

func TestComputeAuditReport_GradeCountsSumToSymbolCount(t *testing.T) {
	g := graph.New()
	for i := range 20 {
		g.AddNode(&graph.Node{ID: "f.go::F" + itoaAudit(i), Kind: graph.KindFunction})
	}
	r := computeAuditReport(g)
	sum := 0
	for _, n := range r.GradeCounts {
		sum += n
	}
	if sum != r.SymbolCount {
		t.Errorf("grade counts sum to %d, want SymbolCount %d", sum, r.SymbolCount)
	}
}

func TestComputeAuditReport_NonCallableKindsSkipped(t *testing.T) {
	g := graph.New()
	g.AddNode(&graph.Node{ID: "f.go::T", Name: "T", Kind: graph.KindType})
	g.AddNode(&graph.Node{ID: "f.go::V", Name: "V", Kind: graph.KindVariable})
	g.AddNode(&graph.Node{ID: "f.go::F", Name: "F", Kind: graph.KindFunction})
	r := computeAuditReport(g)
	if r.SymbolCount != 1 {
		t.Errorf("SymbolCount = %d, want 1 (only function counted)", r.SymbolCount)
	}
}

func TestRenderBadgeSVG_ValidAndContainsGrade(t *testing.T) {
	for _, grade := range []string{"A", "B", "C", "D", "F"} {
		r := auditReport{Grade: grade, MeanScore: 75.5, SymbolCount: 100}
		svg := renderBadgeSVG(r)
		if !strings.HasPrefix(svg, "<svg ") {
			t.Errorf("badge for grade %q doesn't start with <svg: %q", grade, svg[:50])
		}
		if !strings.Contains(svg, ">"+grade+"</text>") {
			t.Errorf("badge for grade %q doesn't surface the grade letter: %s", grade, svg)
		}
		if !strings.Contains(svg, "gortex audit") {
			t.Errorf("badge for grade %q missing label: %s", grade, svg)
		}
	}
}

func TestRenderBadgeSVG_GradeColours(t *testing.T) {
	cases := map[string]string{
		"A": "#4c1",
		"B": "#97ca00",
		"C": "#dfb317",
		"D": "#fe7d37",
		"F": "#e05d44",
	}
	for grade, colour := range cases {
		svg := renderBadgeSVG(auditReport{Grade: grade, MeanScore: 50})
		if !strings.Contains(svg, colour) {
			t.Errorf("grade %q badge missing colour %q", grade, colour)
		}
	}
}

func TestGradeColour(t *testing.T) {
	if gradeColour("A") != "#4c1" {
		t.Errorf("A colour wrong")
	}
	// Unknown grade falls through to red — the safe default for a
	// missing / corrupt grade.
	if gradeColour("Z") != "#e05d44" {
		t.Errorf("unknown grade should fall through to red")
	}
}

func TestRenderAuditJSON_RoundTrip(t *testing.T) {
	r := auditReport{
		SymbolCount:  42,
		MeanScore:    72.3,
		Grade:        "B",
		GradeCounts:  map[string]int{"A": 10, "B": 20, "C": 5, "D": 5, "F": 2},
		WorstSymbols: []symbolScore{{ID: "f.go::Bad", Score: 12.5, Grade: "F", File: "f.go", Line: 99}},
	}
	body := renderAuditJSON(r)
	var got map[string]any
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("render produced invalid JSON: %v\n%s", err, body)
	}
	if got["grade"] != "B" || got["symbol_count"].(float64) != 42 {
		t.Errorf("round-trip lost data: %+v", got)
	}
	worst, ok := got["worst_symbols"].([]any)
	if !ok || len(worst) != 1 {
		t.Errorf("worst_symbols wrong shape: %+v", got["worst_symbols"])
	}
}

func TestAuditCmd_Registered(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "audit" {
			found = true
			break
		}
	}
	if !found {
		t.Error("rootCmd missing `audit` subcommand")
	}
}

// itoaAudit is a tiny int→string helper that avoids pulling in
// strconv just for the test. Same shape as the helper in
// bench/perf/runner_test.go.
func itoaAudit(i int) string {
	if i == 0 {
		return "0"
	}
	var out []byte
	for i > 0 {
		out = append([]byte{byte('0' + i%10)}, out...)
		i /= 10
	}
	return string(out)
}
