package query

import "github.com/zzet/gortex/internal/graph"

// SubGraph is a JSON-serializable result from a graph query.
type SubGraph struct {
	Nodes      []*graph.Node `json:"nodes"`
	Edges      []*graph.Edge `json:"edges"`
	TotalNodes int           `json:"total_nodes"`
	TotalEdges int           `json:"total_edges"`
	Truncated  bool          `json:"truncated"`
}

// QueryOptions controls traversal depth, result limits, and detail level.
type QueryOptions struct {
	Depth  int    `json:"depth"`
	Limit  int    `json:"limit"`
	Detail string `json:"detail"` // "brief" or "full"
}

// DefaultOptions returns options with sensible defaults.
func DefaultOptions() QueryOptions {
	return QueryOptions{
		Depth:  3,
		Limit:  50,
		Detail: "brief",
	}
}
