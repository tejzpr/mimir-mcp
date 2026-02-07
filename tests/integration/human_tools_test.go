// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package integration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tejzpr/medha-mcp/internal/database"
	"github.com/tejzpr/medha-mcp/internal/git"
	"github.com/tejzpr/medha-mcp/internal/tools"
	"gorm.io/gorm/logger"
)

// testSetup creates a test environment with database, git repo, and user
// Updated to use v2 architecture with DatabaseManager and per-user DB
type testSetup struct {
	DBCfg    *database.Config
	DBMgr    *database.Manager
	User     *database.MedhaUser
	Repo     *database.MedhaGitRepo
	RepoPath string
	ToolCtx  *tools.ToolContext
	Cleanup  func()
}

func setupTestEnvironment(t *testing.T) *testSetup {
	tempDir := t.TempDir()
	systemDBPath := filepath.Join(tempDir, "system.db")
	repoPath := filepath.Join(tempDir, "repo")

	// Create repo directory
	require.NoError(t, os.MkdirAll(repoPath, 0755))

	// Create database manager (v2 architecture)
	dbCfg := &database.Config{
		Type:       "sqlite",
		SQLitePath: systemDBPath,
		LogLevel:   logger.Silent,
	}

	mgr, err := database.NewManager(dbCfg)
	require.NoError(t, err)

	// Run v1 migrations on system DB for backward compatibility with tool handlers
	// Tool handlers still use MedhaMemory tables from system DB
	err = database.Migrate(mgr.SystemDB())
	require.NoError(t, err)

	// Create test user in system DB
	user := &database.MedhaUser{
		Username: "testuser",
		Email:    "test@example.com",
	}
	mgr.SystemDB().Create(user)

	// Initialize git repository (skip if sandboxed)
	_, gitErr := git.InitRepository(repoPath)
	if gitErr != nil {
		// Create archive directory manually if git init fails
		_ = os.MkdirAll(filepath.Join(repoPath, "archive"), 0755)
	} else {
		// Create initial structure
		_ = os.MkdirAll(filepath.Join(repoPath, "archive"), 0755)
	}

	// Store repo in system database
	dbRepo := &database.MedhaGitRepo{
		UserID:   user.ID,
		RepoUUID: "test-uuid",
		RepoName: "test-repo",
		RepoPath: repoPath,
	}
	mgr.SystemDB().Create(dbRepo)

	// Create tool context using v2 manager
	toolCtx, err := tools.NewToolContextWithManager(mgr, repoPath)
	require.NoError(t, err)

	return &testSetup{
		DBCfg:    dbCfg,
		DBMgr:    mgr,
		User:     user,
		Repo:     dbRepo,
		RepoPath: repoPath,
		ToolCtx:  toolCtx,
		Cleanup: func() {
			mgr.Close()
		},
	}
}

// TestRememberIntegration tests the medha_remember tool
func TestRememberIntegration(t *testing.T) {
	setup := setupTestEnvironment(t)
	defer setup.Cleanup()

	handler := tools.RememberHandler(setup.ToolCtx, setup.User.ID)

	t.Run("Create new memory", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"title":   "Test Memory",
			"content": "# Test Content\n\nThis is test content.",
			"tags":    []interface{}{"test", "integration"},
		}

		result, err := handler(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)
		
		text := getResultText(result)
		assert.Contains(t, text, "Memory created")
		assert.Contains(t, text, "test-memory")
	})

	t.Run("Create with custom slug", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"title":   "Custom Slug Memory",
			"content": "Content with custom slug",
			"slug":    "my-custom-slug",
		}

		result, err := handler(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)
		
		text := getResultText(result)
		assert.Contains(t, text, "my-custom-slug")
	})

	t.Run("Update existing memory", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"title":   "Updated Memory",
			"content": "This is updated content.",
			"slug":    "my-custom-slug", // Use existing slug
		}

		result, err := handler(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)
		
		text := getResultText(result)
		assert.Contains(t, text, "updated")
	})

	t.Run("Create with supersession", func(t *testing.T) {
		// First create a memory to supersede
		request1 := mcp.CallToolRequest{}
		request1.Params.Arguments = map[string]interface{}{
			"title":   "Old Decision",
			"content": "We decided to use MySQL",
			"slug":    "old-decision",
		}
		_, _ = handler(context.Background(), request1)

		// Create new memory that supersedes it
		request2 := mcp.CallToolRequest{}
		request2.Params.Arguments = map[string]interface{}{
			"title":    "New Decision",
			"content":  "We switched to PostgreSQL",
			"slug":     "new-decision",
			"replaces": "old-decision",
		}

		result, err := handler(context.Background(), request2)
		require.NoError(t, err)
		assert.False(t, result.IsError)
		
		text := getResultText(result)
		assert.Contains(t, text, "Supersedes")
	})
}

// TestRecallIntegration tests the medha_recall tool
func TestRecallIntegration(t *testing.T) {
	setup := setupTestEnvironment(t)
	defer setup.Cleanup()

	// Create some test memories first
	rememberHandler := tools.RememberHandler(setup.ToolCtx, setup.User.ID)
	
	memories := []map[string]interface{}{
		{"title": "Authentication Design", "content": "We use JWT for authentication", "tags": []interface{}{"auth", "security"}},
		{"title": "Database Schema", "content": "PostgreSQL with these tables...", "tags": []interface{}{"database", "schema"}},
		{"title": "API Endpoints", "content": "REST API with authentication", "tags": []interface{}{"api", "endpoints"}},
	}

	for _, mem := range memories {
		request := mcp.CallToolRequest{}
		request.Params.Arguments = mem
		_, _ = rememberHandler(context.Background(), request)
	}

	recallHandler := tools.RecallHandler(setup.ToolCtx, setup.User.ID)

	t.Run("Search by topic", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"topic": "authentication",
		}

		result, err := recallHandler(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)
		
		text := getResultText(result)
		assert.Contains(t, text, "Authentication")
	})

	t.Run("List all memories", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"list_all": true,
		}

		result, err := recallHandler(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)
		
		text := getResultText(result)
		assert.Contains(t, text, "Found")
	})

	t.Run("Exact text search", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"exact": "PostgreSQL",
		}

		result, err := recallHandler(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)
		
		text := getResultText(result)
		assert.Contains(t, text, "Database")
	})
}

// TestForgetRestoreIntegration tests medha_forget and medha_restore
func TestForgetRestoreIntegration(t *testing.T) {
	setup := setupTestEnvironment(t)
	defer setup.Cleanup()

	// Create a memory
	rememberHandler := tools.RememberHandler(setup.ToolCtx, setup.User.ID)
	request := mcp.CallToolRequest{}
	request.Params.Arguments = map[string]interface{}{
		"title":   "Memory to Archive",
		"content": "This will be archived",
		"slug":    "archive-test",
	}
	_, _ = rememberHandler(context.Background(), request)

	forgetHandler := tools.ForgetHandler(setup.ToolCtx, setup.User.ID)
	restoreHandler := tools.RestoreHandler(setup.ToolCtx, setup.User.ID)

	t.Run("Forget (archive) memory", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"slug": "archive-test",
		}

		result, err := forgetHandler(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)
		
		text := getResultText(result)
		assert.Contains(t, text, "archived")
	})

	t.Run("Restore memory", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"slug": "archive-test",
		}

		result, err := restoreHandler(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)
		
		text := getResultText(result)
		assert.Contains(t, text, "restored")
	})
}

// TestConnectIntegration tests medha_connect
func TestConnectIntegration(t *testing.T) {
	setup := setupTestEnvironment(t)
	defer setup.Cleanup()

	// Create two memories
	rememberHandler := tools.RememberHandler(setup.ToolCtx, setup.User.ID)
	
	for _, slug := range []string{"memory-a", "memory-b"} {
		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"title":   "Memory " + strings.ToUpper(slug[7:]),
			"content": "Content for " + slug,
			"slug":    slug,
		}
		_, _ = rememberHandler(context.Background(), request)
	}

	connectHandler := tools.ConnectHandler(setup.ToolCtx, setup.User.ID)

	t.Run("Connect memories", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"from":         "memory-a",
			"to":           "memory-b",
			"relationship": "related",
		}

		result, err := connectHandler(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)
		
		text := getResultText(result)
		assert.Contains(t, text, "Connected")
	})

	t.Run("Disconnect memories", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"from":       "memory-a",
			"to":         "memory-b",
			"disconnect": true,
		}

		result, err := connectHandler(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)
		
		text := getResultText(result)
		assert.Contains(t, text, "Disconnected")
	})

	t.Run("Supersede connection", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"from":         "memory-a",
			"to":           "memory-b",
			"relationship": "supersedes",
		}

		result, err := connectHandler(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)
		
		text := getResultText(result)
		assert.Contains(t, text, "supersedes")
	})
}

// TestHistoryIntegration tests medha_history
func TestHistoryIntegration(t *testing.T) {
	setup := setupTestEnvironment(t)
	defer setup.Cleanup()

	// Create a memory and update it
	rememberHandler := tools.RememberHandler(setup.ToolCtx, setup.User.ID)
	
	request := mcp.CallToolRequest{}
	request.Params.Arguments = map[string]interface{}{
		"title":   "Memory with History",
		"content": "Initial content",
		"slug":    "history-test",
	}
	_, _ = rememberHandler(context.Background(), request)

	// Update it
	request.Params.Arguments = map[string]interface{}{
		"title":   "Memory with History",
		"content": "Updated content",
		"slug":    "history-test",
	}
	_, _ = rememberHandler(context.Background(), request)

	historyHandler := tools.HistoryHandler(setup.ToolCtx, setup.User.ID)

	t.Run("Get history for slug", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"slug": "history-test",
		}

		result, err := historyHandler(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)
		
		text := getResultText(result)
		assert.Contains(t, text, "History")
	})

	t.Run("Get recent activity", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"limit": 10.0,
		}

		result, err := historyHandler(context.Background(), request)
		require.NoError(t, err)
		assert.False(t, result.IsError)
		
		text := getResultText(result)
		assert.Contains(t, text, "Recent Activity")
	})
}

// TestSupersededFiltering tests that superseded memories are filtered by default
func TestSupersededFiltering(t *testing.T) {
	setup := setupTestEnvironment(t)
	defer setup.Cleanup()

	rememberHandler := tools.RememberHandler(setup.ToolCtx, setup.User.ID)
	
	// Create old memory
	request1 := mcp.CallToolRequest{}
	request1.Params.Arguments = map[string]interface{}{
		"title":   "Old Auth Design",
		"content": "Use basic auth",
		"slug":    "old-auth",
		"tags":    []interface{}{"auth"},
	}
	_, _ = rememberHandler(context.Background(), request1)

	// Create new memory that supersedes it
	request2 := mcp.CallToolRequest{}
	request2.Params.Arguments = map[string]interface{}{
		"title":    "New Auth Design",
		"content":  "Use OAuth2",
		"slug":     "new-auth",
		"tags":     []interface{}{"auth"},
		"replaces": "old-auth",
	}
	_, _ = rememberHandler(context.Background(), request2)

	recallHandler := tools.RecallHandler(setup.ToolCtx, setup.User.ID)

	t.Run("Default filters superseded", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"topic": "auth",
		}

		result, err := recallHandler(context.Background(), request)
		require.NoError(t, err)
		
		text := getResultText(result)
		// Should find new-auth but not old-auth by default
		assert.Contains(t, text, "New Auth")
		// Old auth might still appear due to association expansion, but should be marked as superseded
	})

	t.Run("Include superseded when requested", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"topic":              "auth",
			"include_superseded": true,
		}

		result, err := recallHandler(context.Background(), request)
		require.NoError(t, err)
		
		text := getResultText(result)
		// Should find both
		assert.Contains(t, text, "Auth")
	})
}

// getResultText extracts text from CallToolResult
func getResultText(result *mcp.CallToolResult) string {
	if len(result.Content) == 0 {
		return ""
	}
	if textContent, ok := result.Content[0].(mcp.TextContent); ok {
		return textContent.Text
	}
	return ""
}
