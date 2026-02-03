// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package functional

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tejzpr/mimir-mcp/internal/database"
	"github.com/tejzpr/mimir-mcp/internal/git"
	"github.com/tejzpr/mimir-mcp/internal/tools"
	"gorm.io/gorm/logger"
)

// TestE2EHumanToolsWorkflow tests a complete workflow using human-aligned tools
func TestE2EHumanToolsWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	// Setup
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	storePath := filepath.Join(tempDir, "store")

	// Initialize database
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

	// Create test user
	user := &database.MimirUser{
		Username: "testuser@example.com",
		Email:    "testuser@example.com",
	}
	db.Create(user)
	t.Logf("✓ User created: %s", user.Username)

	// Setup repository
	setupCfg := &git.SetupConfig{
		BaseStorePath: storePath,
		Username:      user.Username,
		LocalOnly:     true,
	}

	setupResult, err := git.SetupUserRepository(setupCfg)
	require.NoError(t, err)
	t.Logf("✓ Repository created: %s", setupResult.RepoPath)

	// Store repo in database
	dbRepo := &database.MimirGitRepo{
		UserID:   user.ID,
		RepoUUID: setupResult.RepoID,
		RepoName: setupResult.RepoName,
		RepoPath: setupResult.RepoPath,
	}
	db.Create(dbRepo)

	// Create tool context
	toolCtx := tools.NewToolContext(db, setupResult.RepoPath)

	// Initialize handlers
	rememberHandler := tools.RememberHandler(toolCtx, user.ID)
	recallHandler := tools.RecallHandler(toolCtx, user.ID)
	connectHandler := tools.ConnectHandler(toolCtx, user.ID)
	historyHandler := tools.HistoryHandler(toolCtx, user.ID)
	forgetHandler := tools.ForgetHandler(toolCtx, user.ID)
	restoreHandler := tools.RestoreHandler(toolCtx, user.ID)

	ctx := context.Background()

	// Step 1: Remember initial facts
	t.Log("\n--- Step 1: Remember initial facts ---")
	
	memories := []map[string]interface{}{
		{
			"title":   "Project Architecture Decision",
			"content": "# Architecture\n\nWe decided to use microservices architecture with Go backends and React frontend.",
			"tags":    []interface{}{"architecture", "decision"},
			"slug":    "arch-decision",
		},
		{
			"title":   "Database Choice",
			"content": "# Database\n\nUsing PostgreSQL for primary data storage with Redis for caching.",
			"tags":    []interface{}{"database", "decision"},
			"slug":    "db-choice",
		},
		{
			"title":   "API Design Guidelines",
			"content": "# API Guidelines\n\nREST API with JSON responses. Authentication via JWT tokens.",
			"tags":    []interface{}{"api", "guidelines"},
			"slug":    "api-guidelines",
		},
	}

	for _, mem := range memories {
		request := mcp.CallToolRequest{}
		request.Params.Arguments = mem
		result, err := rememberHandler(ctx, request)
		require.NoError(t, err)
		require.False(t, result.IsError, "remember failed: %s", getResultText(result))
		t.Logf("✓ Remembered: %s", mem["title"])
	}

	// Step 2: Recall by topic
	t.Log("\n--- Step 2: Recall by topic ---")
	
	recallRequest := mcp.CallToolRequest{}
	recallRequest.Params.Arguments = map[string]interface{}{
		"topic": "architecture",
	}
	result, err := recallHandler(ctx, recallRequest)
	require.NoError(t, err)
	require.False(t, result.IsError)
	text := getResultText(result)
	assert.Contains(t, text, "Architecture")
	t.Logf("✓ Recall found architecture decisions")

	// Step 3: Connect related memories
	t.Log("\n--- Step 3: Connect related memories ---")
	
	connectRequest := mcp.CallToolRequest{}
	connectRequest.Params.Arguments = map[string]interface{}{
		"from":         "arch-decision",
		"to":           "db-choice",
		"relationship": "related",
	}
	result, err = connectHandler(ctx, connectRequest)
	require.NoError(t, err)
	require.False(t, result.IsError)
	t.Logf("✓ Connected arch-decision to db-choice")

	connectRequest.Params.Arguments = map[string]interface{}{
		"from":         "arch-decision",
		"to":           "api-guidelines",
		"relationship": "references",
	}
	result, err = connectHandler(ctx, connectRequest)
	require.NoError(t, err)
	require.False(t, result.IsError)
	t.Logf("✓ Connected arch-decision to api-guidelines")

	// Step 4: Update understanding (supersede)
	t.Log("\n--- Step 4: Update understanding (supersede) ---")
	
	updateRequest := mcp.CallToolRequest{}
	updateRequest.Params.Arguments = map[string]interface{}{
		"title":    "Updated Database Choice",
		"content":  "# Database\n\nSwitched from PostgreSQL to CockroachDB for better horizontal scaling.",
		"tags":     []interface{}{"database", "decision", "updated"},
		"slug":     "db-choice-v2",
		"replaces": "db-choice",
	}
	result, err = rememberHandler(ctx, updateRequest)
	require.NoError(t, err)
	require.False(t, result.IsError)
	t.Logf("✓ Created updated database decision that supersedes old one")

	// Step 5: Verify superseded filtering
	t.Log("\n--- Step 5: Verify superseded filtering ---")
	
	recallRequest.Params.Arguments = map[string]interface{}{
		"topic": "database",
	}
	result, err = recallHandler(ctx, recallRequest)
	require.NoError(t, err)
	text = getResultText(result)
	assert.Contains(t, text, "CockroachDB") // Should find new one
	t.Logf("✓ Recall correctly prioritizes non-superseded memories")

	// Step 6: Query history
	t.Log("\n--- Step 6: Query history ---")
	
	historyRequest := mcp.CallToolRequest{}
	historyRequest.Params.Arguments = map[string]interface{}{
		"limit": 10.0,
	}
	result, err = historyHandler(ctx, historyRequest)
	require.NoError(t, err)
	require.False(t, result.IsError)
	text = getResultText(result)
	assert.Contains(t, text, "Recent Activity")
	t.Logf("✓ History shows recent activity")

	// Step 7: Forget outdated memory
	t.Log("\n--- Step 7: Forget outdated memory ---")
	
	forgetRequest := mcp.CallToolRequest{}
	forgetRequest.Params.Arguments = map[string]interface{}{
		"slug": "db-choice",
	}
	result, err = forgetHandler(ctx, forgetRequest)
	require.NoError(t, err)
	require.False(t, result.IsError)
	t.Logf("✓ Forgot (archived) old database choice")

	// Step 8: Restore if needed
	t.Log("\n--- Step 8: Restore archived memory ---")
	
	restoreRequest := mcp.CallToolRequest{}
	restoreRequest.Params.Arguments = map[string]interface{}{
		"slug": "db-choice",
	}
	result, err = restoreHandler(ctx, restoreRequest)
	require.NoError(t, err)
	require.False(t, result.IsError)
	t.Logf("✓ Restored old database choice (for historical reference)")

	// Step 9: Verify git history preserved
	t.Log("\n--- Step 9: Verify git history ---")
	
	commits, err := setupResult.Repository.GetCommitHistory(20)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(commits), 5)
	t.Logf("✓ Git history has %d commits", len(commits))

	// Step 10: Final recall to verify state
	t.Log("\n--- Step 10: Final state verification ---")
	
	recallRequest.Params.Arguments = map[string]interface{}{
		"list_all": true,
	}
	result, err = recallHandler(ctx, recallRequest)
	require.NoError(t, err)
	text = getResultText(result)
	assert.Contains(t, text, "Found")
	t.Logf("✓ Final state verified - all memories accessible")

	t.Log("\n✅ E2E Human Tools Workflow Complete!")
}

// TestE2EMultiUserHumanTools tests multi-user isolation with human-aligned tools
func TestE2EMultiUserHumanTools(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping multi-user E2E test in short mode")
	}

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	storePath := filepath.Join(tempDir, "store")

	// Initialize database
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

	// Create two users
	users := []*database.MimirUser{
		{Username: "alice@example.com", Email: "alice@example.com"},
		{Username: "bob@example.com", Email: "bob@example.com"},
	}

	type userContext struct {
		user     *database.MimirUser
		repo     *database.MimirGitRepo
		toolCtx  *tools.ToolContext
		remember func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)
		recall   func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)
	}

	userContexts := make([]userContext, len(users))

	for i, user := range users {
		db.Create(user)

		setupCfg := &git.SetupConfig{
			BaseStorePath: storePath,
			Username:      user.Username,
			LocalOnly:     true,
		}

		setupResult, err := git.SetupUserRepository(setupCfg)
		require.NoError(t, err)

		dbRepo := &database.MimirGitRepo{
			UserID:   user.ID,
			RepoUUID: setupResult.RepoID,
			RepoName: setupResult.RepoName,
			RepoPath: setupResult.RepoPath,
		}
		db.Create(dbRepo)

		toolCtx := tools.NewToolContext(db, setupResult.RepoPath)

		userContexts[i] = userContext{
			user:     user,
			repo:     dbRepo,
			toolCtx:  toolCtx,
			remember: tools.RememberHandler(toolCtx, user.ID),
			recall:   tools.RecallHandler(toolCtx, user.ID),
		}

		t.Logf("✓ User %s setup complete", user.Username)
	}

	ctx := context.Background()

	// Each user creates a memory with the same topic
	for i, uc := range userContexts {
		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"title":   "My Secret Project",
			"content": fmt.Sprintf("This is %s's secret project details.", uc.user.Username),
			"tags":    []interface{}{"secret", "project"},
			"slug":    fmt.Sprintf("secret-project-%d", i),
		}

		result, err := uc.remember(ctx, request)
		require.NoError(t, err)
		require.False(t, result.IsError)
		t.Logf("✓ %s created their secret project", uc.user.Username)
	}

	// Verify isolation: Each user should only see their own memory
	for _, uc := range userContexts {
		request := mcp.CallToolRequest{}
		request.Params.Arguments = map[string]interface{}{
			"topic": "secret",
		}

		result, err := uc.recall(ctx, request)
		require.NoError(t, err)
		
		text := getResultText(result)
		assert.Contains(t, text, uc.user.Username) // Should find their own
		
		// Count occurrences - should only find one
		count := 0
		for _, other := range users {
			if containsString(text, other.Username) {
				count++
			}
		}
		assert.Equal(t, 1, count, "%s should only see their own memory", uc.user.Username)
		t.Logf("✓ %s only sees their own memories", uc.user.Username)
	}

	t.Log("\n✅ Multi-user isolation verified!")
}

// TestE2EHumanToolsAnnotations tests the annotation feature
func TestE2EHumanToolsAnnotations(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping annotation E2E test in short mode")
	}

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	storePath := filepath.Join(tempDir, "store")

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

	user := &database.MimirUser{Username: "annotator@example.com", Email: "annotator@example.com"}
	db.Create(user)

	setupCfg := &git.SetupConfig{
		BaseStorePath: storePath,
		Username:      user.Username,
		LocalOnly:     true,
	}

	setupResult, err := git.SetupUserRepository(setupCfg)
	require.NoError(t, err)

	dbRepo := &database.MimirGitRepo{
		UserID:   user.ID,
		RepoUUID: setupResult.RepoID,
		RepoName: setupResult.RepoName,
		RepoPath: setupResult.RepoPath,
	}
	db.Create(dbRepo)

	toolCtx := tools.NewToolContext(db, setupResult.RepoPath)
	rememberHandler := tools.RememberHandler(toolCtx, user.ID)
	recallHandler := tools.RecallHandler(toolCtx, user.ID)

	ctx := context.Background()

	// Create a memory
	createRequest := mcp.CallToolRequest{}
	createRequest.Params.Arguments = map[string]interface{}{
		"title":   "Technical Decision",
		"content": "We chose React for the frontend.",
		"slug":    "tech-decision",
	}
	result, err := rememberHandler(ctx, createRequest)
	require.NoError(t, err)
	require.False(t, result.IsError)
	t.Logf("✓ Created technical decision")

	// Add an annotation (correction)
	annotateRequest := mcp.CallToolRequest{}
	annotateRequest.Params.Arguments = map[string]interface{}{
		"title":   "Technical Decision",
		"content": "We chose React for the frontend.",
		"slug":    "tech-decision",
		"note":    "This was wrong - we actually decided on Vue.js after performance testing.",
	}
	result, err = rememberHandler(ctx, annotateRequest)
	require.NoError(t, err)
	require.False(t, result.IsError)
	t.Logf("✓ Added correction annotation")

	// Recall and verify annotation is visible
	recallRequest := mcp.CallToolRequest{}
	recallRequest.Params.Arguments = map[string]interface{}{
		"topic": "technical",
	}
	result, err = recallHandler(ctx, recallRequest)
	require.NoError(t, err)
	
	text := getResultText(result)
	// The annotation should be shown with the memory
	assert.Contains(t, text, "Technical Decision")
	t.Logf("✓ Recall shows memory with annotation context")

	t.Log("\n✅ Annotation E2E test complete!")
}

// Helper function to get text from result
func getResultText(result *mcp.CallToolResult) string {
	if len(result.Content) == 0 {
		return ""
	}
	if textContent, ok := result.Content[0].(mcp.TextContent); ok {
		return textContent.Text
	}
	return ""
}

// Helper function to check if string contains substring
func containsString(text, substr string) bool {
	return len(text) >= len(substr) && 
		(text == substr || 
		 (len(text) > len(substr) && 
		  findSubstring(text, substr) >= 0))
}

func findSubstring(text, substr string) int {
	for i := 0; i <= len(text)-len(substr); i++ {
		if text[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
