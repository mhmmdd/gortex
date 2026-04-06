package languages

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zzet/gortex/internal/graph"
)

func TestPyExtractor_Function(t *testing.T) {
	src := []byte(`def greet(name):
    return f"Hello {name}"
`)
	e := NewPythonExtractor()
	result, err := e.Extract("app.py", src)
	require.NoError(t, err)

	funcs := nodesOfKind(result.Nodes, graph.KindFunction)
	require.Len(t, funcs, 1)
	assert.Equal(t, "greet", funcs[0].Name)
}

func TestPyExtractor_Class(t *testing.T) {
	src := []byte(`class UserService:
    def __init__(self):
        self.users = []

    def get_user(self, user_id):
        return None
`)
	e := NewPythonExtractor()
	result, err := e.Extract("service.py", src)
	require.NoError(t, err)

	types := nodesOfKind(result.Nodes, graph.KindType)
	require.Len(t, types, 1)
	assert.Equal(t, "UserService", types[0].Name)

	// Class methods are extracted as functions.
	funcs := nodesOfKind(result.Nodes, graph.KindFunction)
	assert.GreaterOrEqual(t, len(funcs), 2)
}

func TestPyExtractor_Imports(t *testing.T) {
	src := []byte(`import os
from pathlib import Path
`)
	e := NewPythonExtractor()
	result, err := e.Extract("app.py", src)
	require.NoError(t, err)

	imports := edgesOfKind(result.Edges, graph.EdgeImports)
	require.Len(t, imports, 2)
}
