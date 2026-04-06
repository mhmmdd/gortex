package languages

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zzet/gortex/internal/graph"
)

func TestRbExtractor_Class(t *testing.T) {
	src := []byte(`class UserService
  def initialize(db)
    @db = db
  end

  def find_user(id)
    @db.query(id)
  end
end
`)
	e := NewRubyExtractor()
	result, err := e.Extract("service.rb", src)
	require.NoError(t, err)

	types := nodesOfKind(result.Nodes, graph.KindType)
	require.Len(t, types, 1)
	assert.Equal(t, "UserService", types[0].Name)

	methods := nodesOfKind(result.Nodes, graph.KindMethod)
	assert.Len(t, methods, 2)

	memberEdges := edgesOfKind(result.Edges, graph.EdgeMemberOf)
	assert.Len(t, memberEdges, 2)
}

func TestRbExtractor_TopLevelMethod(t *testing.T) {
	src := []byte(`def greet(name)
  puts "Hello #{name}"
end
`)
	e := NewRubyExtractor()
	result, err := e.Extract("app.rb", src)
	require.NoError(t, err)

	funcs := nodesOfKind(result.Nodes, graph.KindFunction)
	require.Len(t, funcs, 1)
	assert.Equal(t, "greet", funcs[0].Name)
}

func TestRbExtractor_Require(t *testing.T) {
	src := []byte(`require "json"
require_relative "helper"
`)
	e := NewRubyExtractor()
	result, err := e.Extract("app.rb", src)
	require.NoError(t, err)

	imports := edgesOfKind(result.Edges, graph.EdgeImports)
	assert.GreaterOrEqual(t, len(imports), 1)
}

func TestRbExtractor_Module(t *testing.T) {
	src := []byte(`module Authentication
  def self.verify(token)
    true
  end
end
`)
	e := NewRubyExtractor()
	result, err := e.Extract("auth.rb", src)
	require.NoError(t, err)

	pkgs := nodesOfKind(result.Nodes, graph.KindPackage)
	require.Len(t, pkgs, 1)
	assert.Equal(t, "Authentication", pkgs[0].Name)
}

func TestRbExtractor_Constants(t *testing.T) {
	src := []byte(`MAX_RETRIES = 3
DEFAULT_HOST = "localhost"
`)
	e := NewRubyExtractor()
	result, err := e.Extract("config.rb", src)
	require.NoError(t, err)

	vars := nodesOfKind(result.Nodes, graph.KindVariable)
	require.Len(t, vars, 2)

	names := []string{vars[0].Name, vars[1].Name}
	assert.Contains(t, names, "MAX_RETRIES")
	assert.Contains(t, names, "DEFAULT_HOST")
}

func TestRbExtractor_CallSites(t *testing.T) {
	src := []byte(`class Greeter
  def greet(name)
    puts("Hello")
    logger.info("greeting")
  end
end
`)
	e := NewRubyExtractor()
	result, err := e.Extract("greeter.rb", src)
	require.NoError(t, err)

	calls := edgesOfKind(result.Edges, graph.EdgeCalls)
	assert.GreaterOrEqual(t, len(calls), 1)
}

func TestRbExtractor_NoTopLevelMethodInClass(t *testing.T) {
	// Methods inside a class should NOT appear as top-level functions.
	src := []byte(`class Foo
  def bar
  end
end

def baz
end
`)
	e := NewRubyExtractor()
	result, err := e.Extract("test.rb", src)
	require.NoError(t, err)

	funcs := nodesOfKind(result.Nodes, graph.KindFunction)
	require.Len(t, funcs, 1)
	assert.Equal(t, "baz", funcs[0].Name)

	methods := nodesOfKind(result.Nodes, graph.KindMethod)
	require.Len(t, methods, 1)
	assert.Equal(t, "bar", methods[0].Name)
}
