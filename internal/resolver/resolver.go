package resolver

import (
	"path/filepath"
	"strings"

	"github.com/zzet/gortex/internal/graph"
)

const unresolvedPrefix = "unresolved::"

// ResolveStats holds counts from a resolution pass.
type ResolveStats struct {
	Resolved   int `json:"resolved"`
	Unresolved int `json:"unresolved"`
	External   int `json:"external"`
}

// Resolver resolves unresolved edge targets to actual graph node IDs.
type Resolver struct {
	graph *graph.Graph
}

// New creates a Resolver for the given graph.
func New(g *graph.Graph) *Resolver {
	return &Resolver{graph: g}
}

// ResolveAll resolves all unresolved edges in the graph.
func (r *Resolver) ResolveAll() *ResolveStats {
	stats := &ResolveStats{}

	edges := r.graph.AllEdges()
	for _, e := range edges {
		if !strings.HasPrefix(e.To, unresolvedPrefix) {
			continue
		}
		r.resolveEdge(e, stats)
	}
	return stats
}

// ResolveFile resolves unresolved edges originating from a specific file.
func (r *Resolver) ResolveFile(filePath string) *ResolveStats {
	stats := &ResolveStats{}

	// Get all nodes in the file, then check their outgoing edges.
	nodes := r.graph.GetFileNodes(filePath)
	for _, n := range nodes {
		edges := r.graph.GetOutEdges(n.ID)
		for _, e := range edges {
			if !strings.HasPrefix(e.To, unresolvedPrefix) {
				continue
			}
			r.resolveEdge(e, stats)
		}
	}
	return stats
}

func (r *Resolver) resolveEdge(e *graph.Edge, stats *ResolveStats) {
	target := strings.TrimPrefix(e.To, unresolvedPrefix)

	switch {
	case strings.HasPrefix(target, "import::"):
		r.resolveImport(e, strings.TrimPrefix(target, "import::"), stats)
	case strings.HasPrefix(target, "*."):
		r.resolveMethodCall(e, strings.TrimPrefix(target, "*."), stats)
	default:
		r.resolveFunctionCall(e, target, stats)
	}
}

func (r *Resolver) resolveImport(e *graph.Edge, importPath string, stats *ResolveStats) {
	// Look for a package node with matching qualified name.
	node := r.graph.GetNodeByQualName(importPath)
	if node != nil {
		e.To = node.ID
		stats.Resolved++
		return
	}

	// Look for file nodes whose directory matches the import path suffix.
	// This handles in-repo packages.
	candidates := r.graph.AllNodes()
	for _, n := range candidates {
		if n.Kind != graph.KindFile {
			continue
		}
		dir := filepath.Dir(n.FilePath)
		if strings.HasSuffix(dir, lastPathComponent(importPath)) || dir == importPath {
			e.To = n.ID
			stats.Resolved++
			return
		}
	}

	// External/unresolvable import — create a stub target ID.
	e.To = "external::" + importPath
	stats.External++
}

func (r *Resolver) resolveFunctionCall(e *graph.Edge, funcName string, stats *ResolveStats) {
	candidates := r.graph.FindNodesByName(funcName)
	if len(candidates) == 0 {
		stats.Unresolved++
		return
	}

	// Prefer same-package (same directory) match.
	callerFile := e.FilePath
	callerDir := filepath.Dir(callerFile)

	for _, c := range candidates {
		if (c.Kind == graph.KindFunction || c.Kind == graph.KindMethod) &&
			filepath.Dir(c.FilePath) == callerDir {
			e.To = c.ID
			stats.Resolved++
			return
		}
	}

	// Fall back to first function match.
	for _, c := range candidates {
		if c.Kind == graph.KindFunction || c.Kind == graph.KindMethod {
			e.To = c.ID
			stats.Resolved++
			return
		}
	}

	stats.Unresolved++
}

func (r *Resolver) resolveMethodCall(e *graph.Edge, methodName string, stats *ResolveStats) {
	candidates := r.graph.FindNodesByName(methodName)
	if len(candidates) == 0 {
		stats.Unresolved++
		return
	}

	// Prefer same-package match for methods.
	callerDir := filepath.Dir(e.FilePath)
	for _, c := range candidates {
		if c.Kind == graph.KindMethod && filepath.Dir(c.FilePath) == callerDir {
			e.To = c.ID
			stats.Resolved++
			return
		}
	}

	// Fall back to any method match.
	for _, c := range candidates {
		if c.Kind == graph.KindMethod {
			e.To = c.ID
			stats.Resolved++
			return
		}
	}

	stats.Unresolved++
}

func lastPathComponent(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) == 0 {
		return path
	}
	return parts[len(parts)-1]
}
