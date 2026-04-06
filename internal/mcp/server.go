package mcp

import (
	"go.uber.org/zap"

	"github.com/mark3labs/mcp-go/server"
	"github.com/zzet/gortex/internal/indexer"
	"github.com/zzet/gortex/internal/query"
)

// Version is set at build time.
var Version = "dev"

// Server wraps the MCP server with Gortex-specific tools.
type Server struct {
	mcpServer *server.MCPServer
	engine    *query.Engine
	indexer   *indexer.Indexer
	watcher   *indexer.Watcher
	logger    *zap.Logger
}

// NewServer creates an MCP server with all Gortex tools registered.
func NewServer(engine *query.Engine, idx *indexer.Indexer, watcher *indexer.Watcher, logger *zap.Logger) *Server {
	s := &Server{
		mcpServer: server.NewMCPServer("gortex", Version,
			server.WithToolCapabilities(false),
			server.WithRecovery(),
		),
		engine:  engine,
		indexer: idx,
		watcher: watcher,
		logger:  logger,
	}
	s.registerCoreTools()
	s.registerCodingTools()
	return s
}

// ServeStdio starts the MCP server on stdin/stdout.
func (s *Server) ServeStdio() error {
	return server.ServeStdio(s.mcpServer)
}
