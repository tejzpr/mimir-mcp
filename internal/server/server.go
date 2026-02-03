// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package server

import (
	"github.com/mark3labs/mcp-go/server"
	"github.com/tejzpr/mimir-mcp/internal/auth"
	"github.com/tejzpr/mimir-mcp/internal/config"
	"github.com/tejzpr/mimir-mcp/internal/tools"
	"gorm.io/gorm"
)

// MCPServer wraps the mcp-go server with our configuration
type MCPServer struct {
	mcpServer     *server.MCPServer
	config        *config.Config
	db            *gorm.DB
	tokenManager  *auth.TokenManager
	encryptionKey []byte
}

// NewMCPServer creates a new MCP server instance
func NewMCPServer(cfg *config.Config, db *gorm.DB, encryptionKey []byte) (*MCPServer, error) {
	// Create MCP server
	mcpServer := server.NewMCPServer(
		"Mimir",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	// Create token manager
	tokenManager := auth.NewTokenManager(db, cfg.Security.TokenTTL)

	srv := &MCPServer{
		mcpServer:     mcpServer,
		config:        cfg,
		db:            db,
		tokenManager:  tokenManager,
		encryptionKey: encryptionKey,
	}

	// Register tools - Note: These will be registered per-session in actual implementation
	// For now, we're creating the server structure
	
	return srv, nil
}

// RegisterToolsForUser registers all MCP tools for a specific user
func (s *MCPServer) RegisterToolsForUser(userID uint, repoPath string) error {
	toolCtx := tools.NewToolContext(s.db, repoPath)

	// Register human-aligned tools (6 core + 1 sync)
	// These tools express intent rather than implementation, making them
	// easier for LLMs to use correctly.
	
	// mimir_recall: Smart retrieval - "What do I know about X?"
	s.mcpServer.AddTool(tools.NewRecallTool(), tools.RecallHandler(toolCtx, userID))
	
	// mimir_remember: Store/update information - "Store this for later"
	s.mcpServer.AddTool(tools.NewRememberTool(), tools.RememberHandler(toolCtx, userID))
	
	// mimir_history: Temporal queries - "When did I learn about X?"
	s.mcpServer.AddTool(tools.NewHistoryTool(), tools.HistoryHandler(toolCtx, userID))
	
	// mimir_connect: Link/unlink memories - "These are related"
	s.mcpServer.AddTool(tools.NewConnectTool(), tools.ConnectHandler(toolCtx, userID))
	
	// mimir_forget: Archive memories - "No longer relevant"
	s.mcpServer.AddTool(tools.NewForgetTool(), tools.ForgetHandler(toolCtx, userID))
	
	// mimir_restore: Undelete memories - "Bring back that archived memory"
	s.mcpServer.AddTool(tools.NewRestoreTool(), tools.RestoreHandler(toolCtx, userID))
	
	// mimir_sync: Git synchronization (kept for explicit sync operations)
	s.mcpServer.AddTool(tools.NewSyncTool(), tools.SyncHandler(toolCtx, userID, s.encryptionKey))

	return nil
}

// GetMCPServer returns the underlying MCP server
func (s *MCPServer) GetMCPServer() *server.MCPServer {
	return s.mcpServer
}

// GetTokenManager returns the token manager
func (s *MCPServer) GetTokenManager() *auth.TokenManager {
	return s.tokenManager
}
