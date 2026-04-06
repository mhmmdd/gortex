package languages

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/zzet/gortex/internal/graph"
	"github.com/zzet/gortex/internal/parser"
)

const (
	pyQFunction = `(function_definition
		name: (identifier) @func.name) @func.def`

	pyQClass = `(class_definition
		name: (identifier) @class.name) @class.def`

	pyQImport = `(import_statement
		name: (dotted_name) @import.name) @import.def`

	pyQImportFrom = `(import_from_statement
		module_name: (dotted_name) @import.module) @import.def`

	pyQCall = `(call
		function: (identifier) @call.name) @call.expr`

	pyQCallAttr = `(call
		function: (attribute
			attribute: (identifier) @call.method)) @call.expr`

	pyQAssignment = `(assignment
		left: (identifier) @var.name) @var.def`
)

// PythonExtractor extracts Python source files.
type PythonExtractor struct {
	lang *sitter.Language
}

func NewPythonExtractor() *PythonExtractor {
	return &PythonExtractor{lang: python.GetLanguage()}
}

func (e *PythonExtractor) Language() string     { return "python" }
func (e *PythonExtractor) Extensions() []string { return []string{".py"} }

func (e *PythonExtractor) Extract(filePath string, src []byte) (*parser.ExtractionResult, error) {
	tree, err := parser.ParseFile(src, e.lang)
	if err != nil {
		return nil, err
	}
	defer tree.Close()

	root := tree.RootNode()
	result := &parser.ExtractionResult{}

	fileNode := &graph.Node{
		ID: filePath, Kind: graph.KindFile, Name: filePath,
		FilePath: filePath, StartLine: 1, EndLine: int(root.EndPoint().Row) + 1,
		Language: "python",
	}
	result.Nodes = append(result.Nodes, fileNode)

	seen := make(map[string]bool)

	// Functions (top-level and nested).
	matches, _ := parser.RunQuery(pyQFunction, e.lang, root, src)
	for _, m := range matches {
		name := m.Captures["func.name"].Text
		def := m.Captures["func.def"]
		id := filePath + "::" + name
		if seen[id] {
			continue
		}
		seen[id] = true
		kind := graph.KindFunction

		result.Nodes = append(result.Nodes, &graph.Node{
			ID: id, Kind: kind, Name: name,
			FilePath: filePath, StartLine: def.StartLine + 1, EndLine: def.EndLine + 1,
			Language: "python", Meta: map[string]any{"signature": "def " + name + "(...)"},
		})
		result.Edges = append(result.Edges, &graph.Edge{
			From: fileNode.ID, To: id, Kind: graph.EdgeDefines, FilePath: filePath, Line: def.StartLine + 1,
		})
	}

	// Classes.
	matches, _ = parser.RunQuery(pyQClass, e.lang, root, src)
	for _, m := range matches {
		name := m.Captures["class.name"].Text
		def := m.Captures["class.def"]
		id := filePath + "::" + name
		if seen[id] {
			continue
		}
		seen[id] = true
		result.Nodes = append(result.Nodes, &graph.Node{
			ID: id, Kind: graph.KindType, Name: name,
			FilePath: filePath, StartLine: def.StartLine + 1, EndLine: def.EndLine + 1,
			Language: "python",
		})
		result.Edges = append(result.Edges, &graph.Edge{
			From: fileNode.ID, To: id, Kind: graph.EdgeDefines, FilePath: filePath, Line: def.StartLine + 1,
		})
	}

	// Imports.
	matches, _ = parser.RunQuery(pyQImport, e.lang, root, src)
	for _, m := range matches {
		name := m.Captures["import.name"]
		result.Edges = append(result.Edges, &graph.Edge{
			From: fileNode.ID, To: "unresolved::import::" + name.Text,
			Kind: graph.EdgeImports, FilePath: filePath, Line: name.StartLine + 1,
		})
	}
	matches, _ = parser.RunQuery(pyQImportFrom, e.lang, root, src)
	for _, m := range matches {
		mod := m.Captures["import.module"]
		result.Edges = append(result.Edges, &graph.Edge{
			From: fileNode.ID, To: "unresolved::import::" + mod.Text,
			Kind: graph.EdgeImports, FilePath: filePath, Line: mod.StartLine + 1,
		})
	}

	// Call sites.
	funcRanges := buildFuncRanges(result)

	matches, _ = parser.RunQuery(pyQCall, e.lang, root, src)
	for _, m := range matches {
		name := m.Captures["call.name"].Text
		expr := m.Captures["call.expr"]
		callerID := findEnclosingFunc(funcRanges, expr.StartLine+1)
		if callerID == "" {
			continue
		}
		result.Edges = append(result.Edges, &graph.Edge{
			From: callerID, To: "unresolved::" + name,
			Kind: graph.EdgeCalls, FilePath: filePath, Line: expr.StartLine + 1,
		})
	}

	matches, _ = parser.RunQuery(pyQCallAttr, e.lang, root, src)
	for _, m := range matches {
		method := m.Captures["call.method"].Text
		expr := m.Captures["call.expr"]
		callerID := findEnclosingFunc(funcRanges, expr.StartLine+1)
		if callerID == "" {
			continue
		}
		result.Edges = append(result.Edges, &graph.Edge{
			From: callerID, To: "unresolved::*." + method,
			Kind: graph.EdgeCalls, FilePath: filePath, Line: expr.StartLine + 1,
		})
	}

	// Module-level variables (simple assignments at top level).
	matches, _ = parser.RunQuery(pyQAssignment, e.lang, root, src)
	for _, m := range matches {
		name := m.Captures["var.name"].Text
		def := m.Captures["var.def"]
		// Only top-level: parent is module.
		if def.Node != nil && def.Node.Parent() != nil && def.Node.Parent().Type() == "module" {
			id := filePath + "::" + name
			if seen[id] || strings.HasPrefix(name, "_") {
				continue
			}
			seen[id] = true
			result.Nodes = append(result.Nodes, &graph.Node{
				ID: id, Kind: graph.KindVariable, Name: name,
				FilePath: filePath, StartLine: def.StartLine + 1, EndLine: def.EndLine + 1,
				Language: "python",
			})
			result.Edges = append(result.Edges, &graph.Edge{
				From: fileNode.ID, To: id, Kind: graph.EdgeDefines, FilePath: filePath, Line: def.StartLine + 1,
			})
		}
	}

	return result, nil
}
