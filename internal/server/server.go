// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package server

import (
	"fmt"
	"os"

	"github.com/mark3labs/mcp-go/server"
	"github.com/tejzpr/medha-mcp/internal/auth"
	"github.com/tejzpr/medha-mcp/internal/config"
	"github.com/tejzpr/medha-mcp/internal/database"
	"github.com/tejzpr/medha-mcp/internal/embeddings"
	"github.com/tejzpr/medha-mcp/internal/tools"
)

// MCPServer wraps the mcp-go server with our configuration
type MCPServer struct {
	mcpServer        *server.MCPServer
	config           *config.Config
	dbMgr            *database.Manager
	tokenManager     *auth.TokenManager
	encryptionKey    []byte
	embeddingService *embeddings.Service // Optional embedding service for semantic search
}

// NewMCPServer creates a new MCP server instance
func NewMCPServer(cfg *config.Config, dbMgr *database.Manager, encryptionKey []byte) (*MCPServer, error) {
	// Create MCP server
	mcpServer := server.NewMCPServer(
		"Medha",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	// Create token manager
	tokenManager := auth.NewTokenManager(dbMgr.SystemDB(), cfg.Security.TokenTTL)

	srv := &MCPServer{
		mcpServer:     mcpServer,
		config:        cfg,
		dbMgr:         dbMgr,
		tokenManager:  tokenManager,
		encryptionKey: encryptionKey,
	}

	// Initialize embedding service if enabled
	if cfg.Embeddings.Enabled {
		embSvc, err := srv.initEmbeddingService()
		if err != nil {
			// Log warning but don't fail - embeddings are optional
			fmt.Fprintf(os.Stderr, "Warning: Failed to initialize embedding service: %v\n", err)
		} else {
			srv.embeddingService = embSvc
		}
	}

	return srv, nil
}

// initEmbeddingService initializes the embedding service based on config
func (s *MCPServer) initEmbeddingService() (*embeddings.Service, error) {
	cfg := s.config.Embeddings

	// Get API key from environment
	apiKey := os.Getenv(cfg.APIKeyEnv)
	if apiKey == "" && cfg.Provider == config.EmbeddingProviderOpenAI {
		// Try default env var
		apiKey = os.Getenv("OPENAI_API_KEY")
	}

	if apiKey == "" {
		return nil, fmt.Errorf("embedding API key not found (set %s or OPENAI_API_KEY)", cfg.APIKeyEnv)
	}

	// Set defaults
	dimensions := cfg.Dimensions
	if dimensions == 0 {
		dimensions = embeddings.DefaultEmbeddingDimensions
	}

	model := cfg.Model
	if model == "" {
		model = "text-embedding-3-small"
	}

	// Create embedding client based on provider
	var client embeddings.Client
	switch cfg.Provider {
	case config.EmbeddingProviderOpenAI, "":
		baseURL := cfg.BaseURL
		if baseURL == "" {
			baseURL = "https://api.openai.com/v1"
		}
		client = embeddings.NewOpenAIClient(baseURL, apiKey, model, dimensions)
	default:
		return nil, fmt.Errorf("unsupported embedding provider: %s", cfg.Provider)
	}

	// Create embedding service
	svc := embeddings.NewService(s.dbMgr.SystemDB(), client, model, "v1", dimensions)
	return svc, nil
}

// RegisterToolsForUser registers all MCP tools for a specific user
// Uses v2 architecture with per-user databases
func (s *MCPServer) RegisterToolsForUser(userID uint, repoPath string) error {
	// Create v2 tool context with database manager
	toolCtx, err := tools.NewToolContextWithManager(s.dbMgr, repoPath)
	if err != nil {
		return fmt.Errorf("failed to create tool context: %w", err)
	}

	// Set embedding service if available
	if s.embeddingService != nil {
		toolCtx.SetEmbeddingService(s.embeddingService)
	}

	// Register human-aligned tools (6 core + 1 sync)
	// These tools express intent rather than implementation, making them
	// easier for LLMs to use correctly.

	// medha_recall: Smart retrieval - "What do I know about X?"
	s.mcpServer.AddTool(tools.NewRecallTool(), tools.RecallHandler(toolCtx, userID))

	// medha_remember: Store/update information - "Store this for later"
	s.mcpServer.AddTool(tools.NewRememberTool(), tools.RememberHandler(toolCtx, userID))

	// medha_history: Temporal queries - "When did I learn about X?"
	s.mcpServer.AddTool(tools.NewHistoryTool(), tools.HistoryHandler(toolCtx, userID))

	// medha_connect: Link/unlink memories - "These are related"
	s.mcpServer.AddTool(tools.NewConnectTool(), tools.ConnectHandler(toolCtx, userID))

	// medha_forget: Archive memories - "No longer relevant"
	s.mcpServer.AddTool(tools.NewForgetTool(), tools.ForgetHandler(toolCtx, userID))

	// medha_restore: Undelete memories - "Bring back that archived memory"
	s.mcpServer.AddTool(tools.NewRestoreTool(), tools.RestoreHandler(toolCtx, userID))

	// medha_sync: Git synchronization (kept for explicit sync operations)
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

// GetDBManager returns the database manager
func (s *MCPServer) GetDBManager() *database.Manager {
	return s.dbMgr
}

// HasEmbeddings returns true if embedding service is available
func (s *MCPServer) HasEmbeddings() bool {
	return s.embeddingService != nil
}
