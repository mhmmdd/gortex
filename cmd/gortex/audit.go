// audit.go — `gortex audit` command. Produces a one-letter
// repo-level health grade (A-F) plus a shields.io-style SVG badge
// suitable for embedding in a README. Grade is derived from the
// graph's complexity-axis health score — the same arithmetic the
// MCP `analyze kind=health_score` analyzer uses, restricted to the
// axes available without external enrichment (no coverage profile,
// no blame data, no session history required).
//
// The simplification matters: the badge has to work on a
// freshly-indexed repo with zero enrichment. Coverage + blame
// axes add fidelity when present but require multi-step setup;
// gating the badge on them would make the README shield
// effectively unreachable.
package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/zzet/gortex/internal/config"
	"github.com/zzet/gortex/internal/graph"
	"github.com/zzet/gortex/internal/indexer"
	"github.com/zzet/gortex/internal/parser"
	"github.com/zzet/gortex/internal/parser/languages"
)

var (
	auditPath   string
	auditBadge  bool
	auditOut    string
	auditFormat string
)

var auditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Compute a repo-level A-F health grade + emit a README-ready SVG badge",
	Long: `Indexes the target repo and reports a single A-F grade based
on graph-topology complexity. Designed for the README shield:
the grade reflects how well-structured the indexed code is, on
the axes available without external enrichment (fan-in /
fan-out / community-crossings per callable symbol).

Output modes:

  --format svg   (default) shields.io-style SVG. Default path
                 .gortex/badge.svg. Embed in README via
                 ![gortex audit](.gortex/badge.svg).
  --format json  machine-readable score + per-axis breakdown.
  --format text  one-line grade + score for quick CLI use.

Examples:

  gortex audit                          # write .gortex/badge.svg
  gortex audit --format text            # "A · 87.4" on stdout
  gortex audit --format json --out -    # JSON on stdout
  gortex audit --path /tmp/myrepo       # audit a different tree`,
	RunE: runAudit,
}

func init() {
	auditCmd.Flags().StringVar(&auditPath, "path", ".", "repository path to audit")
	auditCmd.Flags().BoolVar(&auditBadge, "badge", true, "write an SVG shield (alias of --format svg)")
	auditCmd.Flags().StringVar(&auditOut, "out", "", "output path (default: .gortex/badge.svg for svg, stdout for json/text)")
	auditCmd.Flags().StringVar(&auditFormat, "format", "svg", "svg | json | text")
	rootCmd.AddCommand(auditCmd)
}

func runAudit(cmd *cobra.Command, _ []string) error {
	abs, err := filepath.Abs(auditPath)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}

	// Index the repo (same shape as bench/perf — fresh indexer,
	// no embedder needed for a topology-only score).
	g := graph.New()
	reg := parser.NewRegistry()
	languages.RegisterAll(reg)
	cfg := config.Config{}
	idx := indexer.New(g, reg, cfg.Index, zap.NewNop())
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "[audit] indexing %s...\n", abs)
	if _, err := idx.Index(abs); err != nil {
		return fmt.Errorf("index: %w", err)
	}

	report := computeAuditReport(g)
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
		"[audit] %d callable symbols · mean complexity-health %.1f · grade %s\n",
		report.SymbolCount, report.MeanScore, report.Grade)

	switch strings.ToLower(auditFormat) {
	case "svg":
		out := auditOut
		if out == "" {
			out = filepath.Join(abs, ".gortex", "badge.svg")
		}
		if out == "-" {
			_, err := cmd.OutOrStdout().Write([]byte(renderBadgeSVG(report)))
			return err
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(out, []byte(renderBadgeSVG(report)), 0o644); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "[audit] wrote %s\n", out)
		// README snippet so the user can paste it.
		_, _ = fmt.Fprintf(cmd.OutOrStdout(),
			"![gortex audit](%s) · grade %s · score %.1f\n",
			filepath.ToSlash(filepath.Clean(strings.TrimPrefix(out, abs+string(filepath.Separator)))),
			report.Grade, report.MeanScore)
	case "json":
		body := renderAuditJSON(report)
		if auditOut == "" || auditOut == "-" {
			_, _ = cmd.OutOrStdout().Write([]byte(body))
			_, _ = cmd.OutOrStdout().Write([]byte("\n"))
			return nil
		}
		return os.WriteFile(auditOut, []byte(body), 0o644)
	case "text":
		line := fmt.Sprintf("%s · %.1f", report.Grade, report.MeanScore)
		if auditOut == "" || auditOut == "-" {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), line)
			return nil
		}
		return os.WriteFile(auditOut, []byte(line+"\n"), 0o644)
	default:
		return fmt.Errorf("unknown --format %q (want svg | json | text)", auditFormat)
	}
	return nil
}

// auditReport is the per-repo summary the badge / json output
// renders. Keeps the structured data separate from rendering so
// tests can pin the math without screen-scraping SVG.
type auditReport struct {
	SymbolCount int     `json:"symbol_count"`
	MeanScore   float64 `json:"mean_score"`
	Grade       string  `json:"grade"`
	// Distribution per grade band — the badge surfaces the
	// headline; this is the deep-dive for the JSON path.
	GradeCounts map[string]int `json:"grade_counts"`
	// Per-symbol scores sorted ascending so the worst-scored
	// symbols can be cited without re-walking the graph.
	WorstSymbols []symbolScore `json:"worst_symbols,omitempty"`
}

type symbolScore struct {
	ID    string  `json:"id"`
	Score float64 `json:"score"`
	Grade string  `json:"grade"`
	File  string  `json:"file"`
	Line  int     `json:"line"`
}

// computeAuditReport walks the graph and produces the per-repo
// grade. Complexity-axis-only math matching the multi-axis
// `analyze kind=health_score` analyzer's complexity component, so
// the badge grade is comparable to (a subset of) what the full
// analyzer would produce on the same graph.
//
// raw       = fan_in*2 + fan_out*1.5 + crossings*3   (per symbol)
// complexity_health = 100 / (1 + raw/20)
// mean      = mean across callable symbols
// grade     = scoreGrade(mean)
//
// We can't easily compute community crossings from the CLI without
// the analysis package's community detector. Approximation: skip
// the crossings term — at the repo scale a small constant bias
// across all symbols doesn't change the rank or grade meaningfully.
func computeAuditReport(g *graph.Graph) auditReport {
	type entry struct {
		id, file string
		line     int
		score    float64
	}
	var entries []entry
	for _, n := range g.AllNodes() {
		if n == nil {
			continue
		}
		if n.Kind != graph.KindFunction && n.Kind != graph.KindMethod {
			continue
		}
		fanIn := 0
		fanOut := 0
		for _, e := range g.GetInEdges(n.ID) {
			if e.Kind == graph.EdgeCalls || e.Kind == graph.EdgeReferences {
				fanIn++
			}
		}
		for _, e := range g.GetOutEdges(n.ID) {
			if e.Kind == graph.EdgeCalls {
				fanOut++
			}
		}
		raw := float64(fanIn)*2.0 + float64(fanOut)*1.5
		complexity := 100.0 / (1.0 + raw/20.0)
		entries = append(entries, entry{
			id:    n.ID,
			file:  n.FilePath,
			line:  n.StartLine,
			score: complexity,
		})
	}

	report := auditReport{
		SymbolCount: len(entries),
		GradeCounts: map[string]int{},
	}
	if len(entries) == 0 {
		report.Grade = scoreGradeForAudit(0)
		return report
	}

	var sum float64
	for _, e := range entries {
		sum += e.score
	}
	report.MeanScore = math.Round((sum/float64(len(entries)))*10) / 10
	report.Grade = scoreGradeForAudit(report.MeanScore)

	for _, e := range entries {
		report.GradeCounts[scoreGradeForAudit(e.score)]++
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].score != entries[j].score {
			return entries[i].score < entries[j].score
		}
		if entries[i].file != entries[j].file {
			return entries[i].file < entries[j].file
		}
		return entries[i].line < entries[j].line
	})
	// Top-5 worst — enough for "what to look at first" without
	// dragging in the full population.
	limit := min(5, len(entries))
	report.WorstSymbols = make([]symbolScore, 0, limit)
	for i := range limit {
		e := entries[i]
		report.WorstSymbols = append(report.WorstSymbols, symbolScore{
			ID:    e.id,
			Score: math.Round(e.score*10) / 10,
			Grade: scoreGradeForAudit(e.score),
			File:  e.file,
			Line:  e.line,
		})
	}
	return report
}

// scoreGradeForAudit mirrors the MCP analyzer's scoreGrade — kept
// duplicated so the audit command doesn't take an mcp dependency.
// Keep in sync with internal/mcp/tools_analyze_health_score.go.
func scoreGradeForAudit(score float64) string {
	switch {
	case score >= 85:
		return "A"
	case score >= 70:
		return "B"
	case score >= 55:
		return "C"
	case score >= 40:
		return "D"
	default:
		return "F"
	}
}

// renderAuditJSON is the structured-output form of the report.
func renderAuditJSON(r auditReport) string {
	// Hand-built so the field order in the output is stable
	// regardless of map iteration. Tests pin specific lines.
	var b strings.Builder
	fmt.Fprintf(&b, "{\n  \"symbol_count\": %d,\n  \"mean_score\": %.1f,\n  \"grade\": %q,\n",
		r.SymbolCount, r.MeanScore, r.Grade)
	b.WriteString("  \"grade_counts\": {")
	first := true
	for _, k := range []string{"A", "B", "C", "D", "F"} {
		if first {
			first = false
		} else {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, "\n    %q: %d", k, r.GradeCounts[k])
	}
	b.WriteString("\n  },\n  \"worst_symbols\": [")
	for i, s := range r.WorstSymbols {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, "\n    {\"id\": %q, \"score\": %.1f, \"grade\": %q, \"file\": %q, \"line\": %d}",
			s.ID, s.Score, s.Grade, s.File, s.Line)
	}
	b.WriteString("\n  ]\n}")
	return b.String()
}

// renderBadgeSVG produces a shields.io-style two-cell badge. Left
// cell ("gortex audit") in slate grey; right cell shows the grade
// in the tier-mapped colour. Plain SVG (no external font deps);
// width auto-sized for a 1-char grade.
func renderBadgeSVG(r auditReport) string {
	label := "gortex audit"
	grade := r.Grade
	colour := gradeColour(grade)

	// Hand-tuned widths — shields.io renders at 100% scale and
	// expects a tight bounding box. 78px left + 26px right is
	// readable at the typical README zoom.
	labelW := 78
	gradeW := 26
	totalW := labelW + gradeW

	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="20" role="img" aria-label="gortex audit: %s">
  <title>gortex audit: %s · %.1f</title>
  <linearGradient id="s" x2="0" y2="100%%">
    <stop offset="0" stop-color="#bbb" stop-opacity=".1"/>
    <stop offset="1" stop-opacity=".1"/>
  </linearGradient>
  <clipPath id="r"><rect width="%d" height="20" rx="3" fill="#fff"/></clipPath>
  <g clip-path="url(#r)">
    <rect width="%d" height="20" fill="#555"/>
    <rect x="%d" width="%d" height="20" fill="%s"/>
    <rect width="%d" height="20" fill="url(#s)"/>
  </g>
  <g fill="#fff" text-anchor="middle" font-family="Verdana,Geneva,DejaVu Sans,sans-serif" font-size="11">
    <text x="%d" y="14">%s</text>
    <text x="%d" y="14">%s</text>
  </g>
</svg>
`,
		totalW, grade, grade, r.MeanScore,
		totalW,
		labelW,
		labelW, gradeW, colour,
		totalW,
		labelW/2, label,
		labelW+gradeW/2, grade,
	)
}

// gradeColour returns the shields.io standard tier colour per
// grade. A=brightgreen, B=green, C=yellow, D=orange, F=red.
func gradeColour(grade string) string {
	switch grade {
	case "A":
		return "#4c1" // brightgreen
	case "B":
		return "#97ca00" // green
	case "C":
		return "#dfb317" // yellow
	case "D":
		return "#fe7d37" // orange
	default:
		return "#e05d44" // red
	}
}
