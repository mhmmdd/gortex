package graph

import "sync"

// GraphStats holds summary counts of the graph contents.
type GraphStats struct {
	TotalNodes int            `json:"total_nodes"`
	TotalEdges int            `json:"total_edges"`
	ByKind     map[string]int `json:"by_kind"`
	ByLanguage map[string]int `json:"by_language"`
}

// Graph is a thread-safe in-memory knowledge graph.
type Graph struct {
	nodes    map[string]*Node
	outEdges map[string][]*Edge
	inEdges  map[string][]*Edge
	byFile   map[string][]*Node
	byName   map[string][]*Node
	byQual   map[string]*Node
	mu       sync.RWMutex
}

// New creates an empty graph.
func New() *Graph {
	return &Graph{
		nodes:    make(map[string]*Node),
		outEdges: make(map[string][]*Edge),
		inEdges:  make(map[string][]*Edge),
		byFile:   make(map[string][]*Node),
		byName:   make(map[string][]*Node),
		byQual:   make(map[string]*Node),
	}
}

// AddNode inserts a node into the graph and all indexes.
func (g *Graph) AddNode(n *Node) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.nodes[n.ID] = n
	g.byFile[n.FilePath] = append(g.byFile[n.FilePath], n)
	g.byName[n.Name] = append(g.byName[n.Name], n)
	if n.QualName != "" {
		g.byQual[n.QualName] = n
	}
}

// AddEdge inserts a directed edge into the graph.
func (g *Graph) AddEdge(e *Edge) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.outEdges[e.From] = append(g.outEdges[e.From], e)
	g.inEdges[e.To] = append(g.inEdges[e.To], e)
}

// GetNode returns a node by ID, or nil if not found.
func (g *Graph) GetNode(id string) *Node {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.nodes[id]
}

// GetNodeByQualName returns a node by fully-qualified name, or nil.
func (g *Graph) GetNodeByQualName(qualName string) *Node {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.byQual[qualName]
}

// FindNodesByName returns all nodes matching the short name.
func (g *Graph) FindNodesByName(name string) []*Node {
	g.mu.RLock()
	defer g.mu.RUnlock()
	src := g.byName[name]
	out := make([]*Node, len(src))
	copy(out, src)
	return out
}

// GetFileNodes returns all nodes defined in the given file.
func (g *Graph) GetFileNodes(filePath string) []*Node {
	g.mu.RLock()
	defer g.mu.RUnlock()
	src := g.byFile[filePath]
	out := make([]*Node, len(src))
	copy(out, src)
	return out
}

// GetOutEdges returns outgoing edges for a node.
func (g *Graph) GetOutEdges(nodeID string) []*Edge {
	g.mu.RLock()
	defer g.mu.RUnlock()
	src := g.outEdges[nodeID]
	out := make([]*Edge, len(src))
	copy(out, src)
	return out
}

// GetInEdges returns incoming edges for a node.
func (g *Graph) GetInEdges(nodeID string) []*Edge {
	g.mu.RLock()
	defer g.mu.RUnlock()
	src := g.inEdges[nodeID]
	out := make([]*Edge, len(src))
	copy(out, src)
	return out
}

// EvictFile removes all nodes and edges belonging to the given file path.
// Returns counts of removed nodes and edges.
func (g *Graph) EvictFile(filePath string) (nodesRemoved, edgesRemoved int) {
	g.mu.Lock()
	defer g.mu.Unlock()

	nodes := g.byFile[filePath]
	if len(nodes) == 0 {
		return 0, 0
	}

	// Collect IDs of nodes being evicted for edge cleanup.
	evictedIDs := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		evictedIDs[n.ID] = true
	}

	// Remove nodes from all indexes.
	for _, n := range nodes {
		delete(g.nodes, n.ID)
		if n.QualName != "" {
			delete(g.byQual, n.QualName)
		}
		// Remove from byName index.
		g.byName[n.Name] = removeNode(g.byName[n.Name], n.ID)
		if len(g.byName[n.Name]) == 0 {
			delete(g.byName, n.Name)
		}
	}
	delete(g.byFile, filePath)
	nodesRemoved = len(nodes)

	// Remove edges that reference evicted nodes or originate from this file.
	edgesRemoved = g.evictEdges(evictedIDs, filePath)

	return nodesRemoved, edgesRemoved
}

// evictEdges removes edges associated with evicted node IDs or file path.
// Must be called under write lock.
func (g *Graph) evictEdges(evictedIDs map[string]bool, filePath string) int {
	removed := 0

	// Collect all unique node IDs that have edges to clean.
	// We scan outEdges and inEdges for any entries referencing evicted nodes.
	for nodeID, edges := range g.outEdges {
		filtered := edges[:0]
		for _, e := range edges {
			if evictedIDs[e.From] || evictedIDs[e.To] || e.FilePath == filePath {
				removed++
			} else {
				filtered = append(filtered, e)
			}
		}
		if len(filtered) == 0 {
			delete(g.outEdges, nodeID)
		} else {
			g.outEdges[nodeID] = filtered
		}
	}

	for nodeID, edges := range g.inEdges {
		filtered := edges[:0]
		for _, e := range edges {
			if evictedIDs[e.From] || evictedIDs[e.To] || e.FilePath == filePath {
				// Already counted in outEdges pass; just filter.
			} else {
				filtered = append(filtered, e)
			}
		}
		if len(filtered) == 0 {
			delete(g.inEdges, nodeID)
		} else {
			g.inEdges[nodeID] = filtered
		}
	}

	return removed
}

// NodeCount returns the total number of nodes.
func (g *Graph) NodeCount() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.nodes)
}

// EdgeCount returns the total number of edges.
func (g *Graph) EdgeCount() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	count := 0
	for _, edges := range g.outEdges {
		count += len(edges)
	}
	return count
}

// AllNodes returns a snapshot of all nodes.
func (g *Graph) AllNodes() []*Node {
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make([]*Node, 0, len(g.nodes))
	for _, n := range g.nodes {
		out = append(out, n)
	}
	return out
}

// AllEdges returns a snapshot of all edges.
func (g *Graph) AllEdges() []*Edge {
	g.mu.RLock()
	defer g.mu.RUnlock()
	var out []*Edge
	for _, edges := range g.outEdges {
		out = append(out, edges...)
	}
	return out
}

// Stats returns summary counts by kind and language.
func (g *Graph) Stats() GraphStats {
	g.mu.RLock()
	defer g.mu.RUnlock()

	byKind := make(map[string]int)
	byLang := make(map[string]int)
	for _, n := range g.nodes {
		byKind[string(n.Kind)]++
		if n.Language != "" {
			byLang[n.Language]++
		}
	}

	edgeCount := 0
	for _, edges := range g.outEdges {
		edgeCount += len(edges)
	}

	return GraphStats{
		TotalNodes: len(g.nodes),
		TotalEdges: edgeCount,
		ByKind:     byKind,
		ByLanguage: byLang,
	}
}

func removeNode(nodes []*Node, id string) []*Node {
	for i, n := range nodes {
		if n.ID == id {
			return append(nodes[:i], nodes[i+1:]...)
		}
	}
	return nodes
}
