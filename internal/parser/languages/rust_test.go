package languages

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zzet/gortex/internal/graph"
)

func TestRsExtractor_Function(t *testing.T) {
	src := []byte(`fn greet(name: &str) -> String {
    format!("Hello {}", name)
}
`)
	e := NewRustExtractor()
	result, err := e.Extract("main.rs", src)
	require.NoError(t, err)

	funcs := nodesOfKind(result.Nodes, graph.KindFunction)
	require.Len(t, funcs, 1)
	assert.Equal(t, "greet", funcs[0].Name)
}

func TestRsExtractor_Struct(t *testing.T) {
	src := []byte(`struct Config {
    port: u16,
    host: String,
}
`)
	e := NewRustExtractor()
	result, err := e.Extract("config.rs", src)
	require.NoError(t, err)

	types := nodesOfKind(result.Nodes, graph.KindType)
	require.Len(t, types, 1)
	assert.Equal(t, "Config", types[0].Name)
}

func TestRsExtractor_Trait(t *testing.T) {
	src := []byte(`trait Repository {
    fn find_by_id(&self, id: &str) -> Option<User>;
}
`)
	e := NewRustExtractor()
	result, err := e.Extract("store.rs", src)
	require.NoError(t, err)

	ifaces := nodesOfKind(result.Nodes, graph.KindInterface)
	require.Len(t, ifaces, 1)
	assert.Equal(t, "Repository", ifaces[0].Name)
}

func TestRsExtractor_Use(t *testing.T) {
	src := []byte(`use std::collections::HashMap;
use tokio::net::TcpListener;
`)
	e := NewRustExtractor()
	result, err := e.Extract("main.rs", src)
	require.NoError(t, err)

	imports := edgesOfKind(result.Edges, graph.EdgeImports)
	require.Len(t, imports, 2)
}
