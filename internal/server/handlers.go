// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/tejzpr/medha-mcp/internal/auth"
	"github.com/tejzpr/medha-mcp/internal/crypto"
	"github.com/tejzpr/medha-mcp/internal/database"
	"github.com/tejzpr/medha-mcp/internal/git"
)

// HTTPServer handles HTTP routes
type HTTPServer struct {
	mcpServer      *MCPServer
	localAuth      *auth.LocalAuthenticator
	authMiddleware *auth.Middleware
	encryptionKey  []byte
}

// NewHTTPServer creates a new HTTP server (local auth only)
func NewHTTPServer(mcpServer *MCPServer, samlAuth *auth.SAMLAuthenticator, localAuth *auth.LocalAuthenticator, authType string, encryptionKey []byte) *HTTPServer {
	authMiddleware := auth.NewMiddleware(mcpServer.GetTokenManager())

	return &HTTPServer{
		mcpServer:      mcpServer,
		localAuth:      localAuth,
		authMiddleware: authMiddleware,
		encryptionKey:  encryptionKey,
	}
}

// RegisterRoutes registers all HTTP routes (local auth only)
func (h *HTTPServer) RegisterRoutes(mux *http.ServeMux) {
	// Auth routes
	mux.HandleFunc("/auth", h.ServeAuthPage)
	mux.HandleFunc("/auth/local", h.HandleLocalAuth)

	// MCP routes (protected)
	mux.Handle("/mcp", h.authMiddleware.RequireAuth(http.HandlerFunc(h.HandleMCP)))
}

// ServeAuthPage serves the authentication web interface
func (h *HTTPServer) ServeAuthPage(w http.ResponseWriter, r *http.Request) {
	// Serve the HTML file
	htmlPath := filepath.Join("web", "auth", "index.html")
	http.ServeFile(w, r, htmlPath)
}

// HandleLocalAuth handles local authentication using system username
func (h *HTTPServer) HandleLocalAuth(w http.ResponseWriter, r *http.Request) {
	db := h.mcpServer.dbMgr.SystemDB()

	// Authenticate using local username
	user, token, err := h.localAuth.Authenticate(db)
	if err != nil {
		http.Error(w, "Local authentication failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Get PAT from form if provided
	patToken := r.FormValue("pat_token")
	repoURL := r.FormValue("repo_url")
	localOnly := r.FormValue("local_only") == "true"

	// Setup user repository
	homeDir, _ := os.UserHomeDir()
	storePath := filepath.Join(homeDir, ".medha", "store")

	setupCfg := &git.SetupConfig{
		BaseStorePath: storePath,
		Username:      user.Username, // Use username for deterministic folder naming
		RepoURL:       repoURL,
		PAT:           patToken,
		LocalOnly:     localOnly,
	}

	// Check if repo already exists (check both database and filesystem)
	var existingRepo database.MedhaGitRepo
	expectedRepoPath := git.GetUserRepositoryPath(storePath, user.Username)

	err = db.Where("user_id = ?", user.ID).First(&existingRepo).Error
	if err != nil {
		// No repo in database, check if folder exists on disk (recovery scenario)
		if _, statErr := os.Stat(expectedRepoPath); statErr == nil {
			// Folder exists but not in DB - recover by adding to DB
			encryptedPAT := ""
			if patToken != "" {
				encryptedPAT, _ = crypto.EncryptPAT(patToken, h.encryptionKey)
			}
			repo := &database.MedhaGitRepo{
				UserID:            user.ID,
				RepoUUID:          user.Username,
				RepoName:          fmt.Sprintf("medha-%s", user.Username),
				RepoURL:           repoURL,
				RepoPath:          expectedRepoPath,
				PATTokenEncrypted: encryptedPAT,
			}
			db.Create(repo)
		} else {
			// Create new repository
			result, err := git.SetupUserRepository(setupCfg)
			if err != nil {
				http.Error(w, "Failed to setup repository: "+err.Error(), http.StatusInternalServerError)
				return
			}

			// Encrypt PAT if provided
			encryptedPAT := ""
			if patToken != "" {
				encryptedPAT, _ = crypto.EncryptPAT(patToken, h.encryptionKey)
			}

			// Store repo in database
			repo := &database.MedhaGitRepo{
				UserID:            user.ID,
				RepoUUID:          result.RepoID,
				RepoName:          result.RepoName,
				RepoURL:           repoURL,
				RepoPath:          result.RepoPath,
				PATTokenEncrypted: encryptedPAT,
			}
			db.Create(repo)
		}
	}

	// Return success
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"success":  "true",
		"token":    token.AccessToken,
		"username": user.Username,
	})
}

// HandleMCP handles MCP protocol requests
func (h *HTTPServer) HandleMCP(w http.ResponseWriter, r *http.Request) {
	db := h.mcpServer.dbMgr.SystemDB()

	// Get user ID from context
	userID, ok := auth.GetUserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Get user's repository
	var repo database.MedhaGitRepo
	if err := db.Where("user_id = ?", userID).First(&repo).Error; err != nil {
		http.Error(w, "Repository not found", http.StatusNotFound)
		return
	}

	// Register tools for this user
	if err := h.mcpServer.RegisterToolsForUser(userID, repo.RepoPath); err != nil {
		http.Error(w, "Failed to register tools", http.StatusInternalServerError)
		return
	}

	// Forward to MCP server
	// Note: This is simplified - actual implementation would use mcp-go HTTP transport
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
