package parser

import "github.com/zzet/gortex/internal/graph"

// Extractor extracts graph nodes and edges from a single source file.
type Extractor interface {
	Language() string
	Extensions() []string
	Extract(filePath string, src []byte) (*ExtractionResult, error)
}

// ExtractionResult holds the nodes and edges extracted from a single file.
type ExtractionResult struct {
	Nodes []*graph.Node
	Edges []*graph.Edge
}
