package mcp

import (
	"github.com/zzet/gortex/internal/callpath"
	"github.com/zzet/gortex/internal/config"
	"github.com/zzet/gortex/internal/graph"
)

// smartContextSections resolves which in-pack enrichment sections a
// smart_context call should attach. Per-call include_* params override the
// project's smart_context config; every section is off by default.
func (s *Server) smartContextSections(args map[string]any, relPath string) config.SmartContextSections {
	cfg := config.SmartContextConfig{}
	if s.configManager != nil {
		cfg = s.configManager.GetRepoConfig(repoPrefixForPath(s, relPath)).MCP.SmartContext
	}
	return cfg.Resolve(
		boolPtrArg(args, "include_call_paths"),
		boolPtrArg(args, "include_flows"),
		boolPtrArg(args, "include_confidence"),
	)
}

// boolPtrArg returns a *bool: the parsed value when the caller passed the key,
// nil when absent — so an unset flag inherits config rather than forcing false.
func boolPtrArg(args map[string]any, key string) *bool {
	if v, set := boolArg(args, key); set {
		return &v
	}
	return nil
}

// attachInPackSections records the opt-in in-pack enrichment sections on the
// assembled pack under result["in_pack"]. Only sections with content are
// written, so the default pack stays untouched; later passes attach the flow
// spine and confidence verdict to the same block.
func (s *Server) attachInPackSections(result map[string]any, sections config.SmartContextSections, symbols []*graph.Node) {
	block := map[string]any{}
	if sections.CallPaths {
		if cp := s.inPackCallPaths(symbols); len(cp) > 0 {
			block["call_paths"] = cp
		}
	}
	if len(block) > 0 {
		result["in_pack"] = block
	}
}

// inPackCallPaths builds the anchored call-paths section: the focus symbol (the
// first pack symbol) is the anchor and the rest are roots, so each row shows how
// another pack symbol reaches the focus over the call graph. Returns nil when
// fewer than two symbols are in the pack or none reach the focus.
func (s *Server) inPackCallPaths(symbols []*graph.Node) []map[string]any {
	if s.graph == nil || len(symbols) < 2 {
		return nil
	}
	anchor := symbols[0].ID
	roots := make([]string, 0, len(symbols)-1)
	for _, n := range symbols[1:] {
		if n != nil && n.ID != "" {
			roots = append(roots, n.ID)
		}
	}
	anchored := callpath.New(s.graph).PathsToAnchor(roots, anchor, callpath.Options{MaxDepth: 8})
	if len(anchored) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(anchored))
	for _, ap := range anchored {
		out = append(out, map[string]any{
			"root":       ap.Root,
			"anchor":     anchor,
			"length":     ap.Path.Length,
			"confidence": ap.Path.Confidence,
			"nodes":      ap.Path.Nodes,
		})
	}
	return out
}
