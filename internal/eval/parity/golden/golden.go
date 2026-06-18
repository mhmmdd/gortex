// Package golden locks in the per-feature extraction output of the
// capabilities ported to match — and exceed — the reference benchmark suite.
//
// Each Capability feeds a fixed source snippet to the matching language
// extractor and asserts the nodes and edges the feature must produce. Unlike
// the per-language coverage metric (which measures breadth), these are narrow
// regression fences: if a future refactor silently drops a ported capability —
// stops emitting an anonymous-class type, a per-binding import edge, an
// annotation interface — the corresponding golden fails immediately, naming
// the exact node or edge that went missing.
package golden

import (
	"fmt"
	"strings"

	"github.com/zzet/gortex/internal/graph"
	"github.com/zzet/gortex/internal/parser"
	"github.com/zzet/gortex/internal/parser/languages"
)

// extractorFor returns the extractor for a capability's language, or nil for
// an unknown language.
func extractorFor(lang string) parser.Extractor {
	switch lang {
	case "java":
		return languages.NewJavaExtractor()
	case "csharp":
		return languages.NewCSharpExtractor()
	case "typescript":
		return languages.NewTypeScriptExtractor()
	case "javascript":
		return languages.NewJavaScriptExtractor()
	case "go":
		return languages.NewGoExtractor()
	default:
		return nil
	}
}

// nodeWant asserts that a node of the given Kind and Name was extracted. A
// non-empty MetaKey additionally requires Meta[MetaKey] to equal MetaVal.
type nodeWant struct {
	Kind    graph.NodeKind
	Name    string
	MetaKey string
	MetaVal any
}

func (w nodeWant) String() string {
	if w.MetaKey != "" {
		return fmt.Sprintf("node{kind=%s name=%q meta[%s]=%v}", w.Kind, w.Name, w.MetaKey, w.MetaVal)
	}
	return fmt.Sprintf("node{kind=%s name=%q}", w.Kind, w.Name)
}

func (w nodeWant) matches(n *graph.Node) bool {
	if n.Kind != w.Kind || n.Name != w.Name {
		return false
	}
	if w.MetaKey == "" {
		return true
	}
	if n.Meta == nil {
		return false
	}
	return n.Meta[w.MetaKey] == w.MetaVal
}

// edgeWant asserts that an edge of the given Kind exists whose To target
// contains ToSub (To is frequently an unresolved::… path, so a substring match
// keeps the golden robust to id-format details). A non-empty Alias additionally
// requires the edge's Alias to match.
type edgeWant struct {
	Kind  graph.EdgeKind
	ToSub string
	Alias string
}

func (w edgeWant) String() string {
	if w.Alias != "" {
		return fmt.Sprintf("edge{kind=%s to~%q alias=%q}", w.Kind, w.ToSub, w.Alias)
	}
	return fmt.Sprintf("edge{kind=%s to~%q}", w.Kind, w.ToSub)
}

func (w edgeWant) matches(e *graph.Edge) bool {
	if e.Kind != w.Kind || !strings.Contains(e.To, w.ToSub) {
		return false
	}
	if w.Alias != "" && e.Alias != w.Alias {
		return false
	}
	return true
}

// Capability is one ported extraction feature with its golden assertions.
type Capability struct {
	Name      string
	Language  string
	FileName  string
	Source    string
	WantNodes []nodeWant
	WantEdges []edgeWant
}

// check extracts the snippet and returns the wanted nodes / edges that were not
// produced. A nil error with empty slices means the capability holds.
func (c Capability) check() (missingNodes []nodeWant, missingEdges []edgeWant, err error) {
	ext := extractorFor(c.Language)
	if ext == nil {
		return nil, nil, fmt.Errorf("no extractor for language %q", c.Language)
	}
	res, err := ext.Extract(c.FileName, []byte(c.Source))
	if err != nil {
		return nil, nil, err
	}
	for _, wn := range c.WantNodes {
		if !anyNode(res.Nodes, wn.matches) {
			missingNodes = append(missingNodes, wn)
		}
	}
	for _, we := range c.WantEdges {
		if !anyEdge(res.Edges, we.matches) {
			missingEdges = append(missingEdges, we)
		}
	}
	return missingNodes, missingEdges, nil
}

func anyNode(nodes []*graph.Node, pred func(*graph.Node) bool) bool {
	for _, n := range nodes {
		if n != nil && pred(n) {
			return true
		}
	}
	return false
}

func anyEdge(edges []*graph.Edge, pred func(*graph.Edge) bool) bool {
	for _, e := range edges {
		if e != nil && pred(e) {
			return true
		}
	}
	return false
}
