// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tejzpr/medha-mcp/internal/config"
	"github.com/tejzpr/medha-mcp/internal/database"
	"github.com/tejzpr/medha-mcp/internal/server"
	"github.com/tejzpr/medha-mcp/internal/tools"
	"gorm.io/gorm/logger"
)

// TestServerV2Context verifies that the server properly initializes with v2 ToolContext
func TestServerV2Context(t *testing.T) {
	// Create temp directory for test
	tempDir, err := os.MkdirTemp("", "medha-server-v2-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Setup test database
	dbPath := filepath.Join(tempDir, "test.db")
	dbCfg := &database.Config{
		Type:       "sqlite",
		SQLitePath: dbPath,
		LogLevel:   logger.Silent,
	}

	dbMgr, err := database.NewManager(dbCfg)
	require.NoError(t, err)
	defer dbMgr.Close()

	// Run migrations
	err = database.Migrate(dbMgr.SystemDB())
	require.NoError(t, err)

	// Create test config
	cfg := &config.Config{
		Security: config.SecurityConfig{
			TokenTTL: 24,
		},
		Embeddings: config.EmbeddingConfig{
			Enabled: false, // Disable embeddings for this test
		},
	}

	encryptionKey := make([]byte, 32)

	// Create MCP server
	mcpServer, err := server.NewMCPServer(cfg, dbMgr, encryptionKey)
	require.NoError(t, err)
	assert.NotNil(t, mcpServer)

	// Verify database manager is accessible
	assert.NotNil(t, mcpServer.GetDBManager())
	assert.Equal(t, dbMgr, mcpServer.GetDBManager())

	// Verify embeddings are not enabled when disabled in config
	assert.False(t, mcpServer.HasEmbeddings())
}

// TestToolContextWithUserDB verifies that ToolContext properly sets up UserDB
func TestToolContextWithUserDB(t *testing.T) {
	// Create temp directory for test
	tempDir, err := os.MkdirTemp("", "medha-toolctx-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create repo structure
	repoPath := filepath.Join(tempDir, "test-repo")
	medhaDir := filepath.Join(repoPath, ".medha")
	err = os.MkdirAll(medhaDir, 0755)
	require.NoError(t, err)

	// Initialize git repo
	gitDir := filepath.Join(repoPath, ".git")
	err = os.MkdirAll(gitDir, 0755)
	require.NoError(t, err)

	// Setup system database
	dbPath := filepath.Join(tempDir, "system.db")
	dbCfg := &database.Config{
		Type:       "sqlite",
		SQLitePath: dbPath,
		LogLevel:   logger.Silent,
	}

	dbMgr, err := database.NewManager(dbCfg)
	require.NoError(t, err)
	defer dbMgr.Close()

	// Create tool context with manager
	toolCtx, err := tools.NewToolContextWithManager(dbMgr, repoPath)
	require.NoError(t, err)
	assert.NotNil(t, toolCtx)

	// Verify both databases are available
	assert.NotNil(t, toolCtx.SystemDB, "SystemDB should be set")
	assert.NotNil(t, toolCtx.UserDB, "UserDB should be set")
	assert.NotNil(t, toolCtx.DBMgr, "DBMgr should be set")

	// Verify UserDB is different from SystemDB (per-user isolation)
	assert.True(t, toolCtx.HasUserDB(), "HasUserDB should return true")

	// Verify the user DB file was created
	userDBPath := filepath.Join(medhaDir, "medha.db")
	_, err = os.Stat(userDBPath)
	assert.NoError(t, err, "User database file should exist")
}

// TestToolsRequireUserDB verifies that tools fail gracefully without UserDB
func TestToolsRequireUserDB(t *testing.T) {
	// Create a v1-style context without UserDB
	tempDir, err := os.MkdirTemp("", "medha-v1-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Setup system database only
	dbPath := filepath.Join(tempDir, "system.db")
	dbCfg := &database.Config{
		Type:       "sqlite",
		SQLitePath: dbPath,
		LogLevel:   logger.Silent,
	}

	db, err := database.Connect(dbCfg)
	require.NoError(t, err)
	defer database.Close(db)

	err = database.Migrate(db)
	require.NoError(t, err)

	// Create v1-style tool context (without UserDB)
	toolCtx := tools.NewToolContext(db, tempDir)
	assert.Nil(t, toolCtx.UserDB, "UserDB should be nil in v1 context")

	// Test that recall handler returns error when UserDB is nil
	recallHandler := tools.RecallHandler(toolCtx, 1)
	request := mcp.CallToolRequest{}
	request.Params.Arguments = map[string]interface{}{
		"list_all": true,
	}

	result, err := recallHandler(context.Background(), request)
	require.NoError(t, err) // Handler shouldn't return Go error
	assert.NotNil(t, result)

	// The result should be an error about missing UserDB
	textResult, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok)
	assert.Contains(t, textResult.Text, "per-user database not available")
}

// TestVecTableCreation verifies that sqlite-vec table is created when available
func TestVecTableCreation(t *testing.T) {
	// Create temp directory for test
	tempDir, err := os.MkdirTemp("", "medha-vec-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create repo structure
	repoPath := filepath.Join(tempDir, "test-repo")
	medhaDir := filepath.Join(repoPath, ".medha")
	err = os.MkdirAll(medhaDir, 0755)
	require.NoError(t, err)

	// Setup system database
	dbPath := filepath.Join(tempDir, "system.db")
	dbCfg := &database.Config{
		Type:       "sqlite",
		SQLitePath: dbPath,
		LogLevel:   logger.Silent,
	}

	dbMgr, err := database.NewManager(dbCfg)
	require.NoError(t, err)
	defer dbMgr.Close()

	// Get user DB with vec support
	userDB, err := dbMgr.GetUserDBWithVec(repoPath, 1536)
	require.NoError(t, err)
	assert.NotNil(t, userDB)

	// Check if sqlite-vec is available
	if database.IsVecAvailable(userDB) {
		// Verify vec_embeddings table exists
		var count int64
		err = userDB.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='vec_embeddings'").Scan(&count).Error
		require.NoError(t, err)
		assert.Equal(t, int64(1), count, "vec_embeddings table should exist")
	} else {
		t.Log("sqlite-vec not available, skipping vec table check")
	}
}

// TestOptimisticLockingIntegration verifies that optimistic locking works end-to-end
func TestOptimisticLockingIntegration(t *testing.T) {
	setup := setupTestEnvironment(t)
	defer setup.Cleanup()

	ctx := context.Background()
	rememberHandler := tools.RememberHandler(setup.ToolCtx, setup.User.ID)
	recallHandler := tools.RecallHandler(setup.ToolCtx, setup.User.ID)

	// Create initial memory
	createReq := mcp.CallToolRequest{}
	createReq.Params.Arguments = map[string]interface{}{
		"title":   "Locking Test Memory",
		"content": "Initial content for locking test",
		"tags":    []interface{}{"test", "locking"},
	}

	result, err := rememberHandler(ctx, createReq)
	require.NoError(t, err)
	textContent, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok)
	require.NotContains(t, textContent.Text, "error", "Should create memory successfully")

	// Get the slug from the response
	// The response format is "Memory created: Title\nSlug: xxx\nPath: yyy"
	require.Contains(t, textContent.Text, "Slug:")

	// Verify memory was created and has version 1
	var mem database.UserMemory
	err = setup.ToolCtx.UserDB.Where("title = ?", "Locking Test Memory").First(&mem).Error
	require.NoError(t, err)
	assert.Equal(t, int64(1), mem.Version, "Initial version should be 1")

	// Update the memory
	updateReq := mcp.CallToolRequest{}
	updateReq.Params.Arguments = map[string]interface{}{
		"title":   "Locking Test Memory",
		"slug":    mem.Slug,
		"content": "Updated content for locking test",
	}

	result, err = rememberHandler(ctx, updateReq)
	require.NoError(t, err)
	textContent, ok = result.Content[0].(mcp.TextContent)
	require.True(t, ok)
	require.Contains(t, textContent.Text, "updated", "Should update memory successfully")

	// Verify version was incremented
	err = setup.ToolCtx.UserDB.Where("slug = ?", mem.Slug).First(&mem).Error
	require.NoError(t, err)
	assert.Equal(t, int64(2), mem.Version, "Version should be incremented to 2 after update")

	// Verify recall can find the updated memory
	recallReq := mcp.CallToolRequest{}
	recallReq.Params.Arguments = map[string]interface{}{
		"topic": "locking test",
	}

	result, err = recallHandler(ctx, recallReq)
	require.NoError(t, err)
	textContent, ok = result.Content[0].(mcp.TextContent)
	require.True(t, ok)
	assert.Contains(t, textContent.Text, "Updated content", "Should find updated content")
}

// TestForgetRestoreWithLocking verifies optimistic locking in forget/restore flow
func TestForgetRestoreWithLocking(t *testing.T) {
	setup := setupTestEnvironment(t)
	defer setup.Cleanup()

	ctx := context.Background()
	rememberHandler := tools.RememberHandler(setup.ToolCtx, setup.User.ID)
	forgetHandler := tools.ForgetHandler(setup.ToolCtx, setup.User.ID)
	restoreHandler := tools.RestoreHandler(setup.ToolCtx, setup.User.ID)

	// Create a memory to archive
	createReq := mcp.CallToolRequest{}
	createReq.Params.Arguments = map[string]interface{}{
		"title":   "Memory to Archive",
		"content": "This will be archived and restored",
		"slug":    "archive-restore-test",
	}

	_, err := rememberHandler(ctx, createReq)
	require.NoError(t, err)

	// Verify initial version
	var mem database.UserMemory
	err = setup.ToolCtx.UserDB.Where("slug = ?", "archive-restore-test").First(&mem).Error
	require.NoError(t, err)
	initialVersion := mem.Version

	// Archive (forget) the memory
	forgetReq := mcp.CallToolRequest{}
	forgetReq.Params.Arguments = map[string]interface{}{
		"slug": "archive-restore-test",
	}

	result, err := forgetHandler(ctx, forgetReq)
	require.NoError(t, err)
	textContent, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok)
	require.Contains(t, textContent.Text, "archived", "Should archive successfully")

	// Verify version was incremented after archive
	err = setup.ToolCtx.UserDB.Unscoped().Where("slug = ?", "archive-restore-test").First(&mem).Error
	require.NoError(t, err)
	assert.Equal(t, initialVersion+1, mem.Version, "Version should increment after archive")

	// Restore the memory
	restoreReq := mcp.CallToolRequest{}
	restoreReq.Params.Arguments = map[string]interface{}{
		"slug": "archive-restore-test",
	}

	result, err = restoreHandler(ctx, restoreReq)
	require.NoError(t, err)
	textContent, ok = result.Content[0].(mcp.TextContent)
	require.True(t, ok)
	require.Contains(t, textContent.Text, "restored", "Should restore successfully")

	// Verify version was incremented after restore
	err = setup.ToolCtx.UserDB.Where("slug = ?", "archive-restore-test").First(&mem).Error
	require.NoError(t, err)
	assert.Equal(t, initialVersion+2, mem.Version, "Version should increment after restore")
}

// TestConnectSupersedesWithLocking verifies optimistic locking when creating supersedes relationships
func TestConnectSupersedesWithLocking(t *testing.T) {
	setup := setupTestEnvironment(t)
	defer setup.Cleanup()

	ctx := context.Background()
	rememberHandler := tools.RememberHandler(setup.ToolCtx, setup.User.ID)
	connectHandler := tools.ConnectHandler(setup.ToolCtx, setup.User.ID)

	// Create two memories
	for _, slug := range []string{"old-memory", "new-memory"} {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"title":   "Memory " + slug,
			"content": "Content for " + slug,
			"slug":    slug,
		}
		_, err := rememberHandler(ctx, req)
		require.NoError(t, err)
	}

	// Get initial version of old memory
	var oldMem database.UserMemory
	err := setup.ToolCtx.UserDB.Where("slug = ?", "old-memory").First(&oldMem).Error
	require.NoError(t, err)
	initialVersion := oldMem.Version

	// Create supersedes relationship
	connectReq := mcp.CallToolRequest{}
	connectReq.Params.Arguments = map[string]interface{}{
		"from":         "new-memory",
		"to":           "old-memory",
		"relationship": "supersedes",
	}

	result, err := connectHandler(ctx, connectReq)
	require.NoError(t, err)
	textContent, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok)
	require.Contains(t, textContent.Text, "supersedes", "Should create supersedes relationship")

	// Verify old memory's version was incremented (it got marked as superseded)
	err = setup.ToolCtx.UserDB.Where("slug = ?", "old-memory").First(&oldMem).Error
	require.NoError(t, err)
	assert.Equal(t, initialVersion+1, oldMem.Version, "Old memory version should increment when superseded")
	// Check that SupersededBy field is set (not IsSuperseded)
	assert.NotNil(t, oldMem.SupersededBy, "Old memory should have SupersededBy set")
	assert.Equal(t, "new-memory", *oldMem.SupersededBy, "SupersededBy should be the new memory slug")
}
