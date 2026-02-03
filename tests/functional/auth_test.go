// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package functional

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tejzpr/mimir-mcp/internal/auth"
	"github.com/tejzpr/mimir-mcp/internal/database"
	"github.com/tejzpr/mimir-mcp/internal/git"
	"gorm.io/gorm/logger"
)

// TestLocalAuthFlow tests the complete local authentication flow
// This simulates what happens when a user runs mimir locally via Cursor
func TestLocalAuthFlow(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	storePath := filepath.Join(tempDir, "store")

	// 1. Initialize database
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
	t.Log("✓ Database initialized")

	// 2. Local authentication (uses whoami)
	tokenManager := auth.NewTokenManager(db, 24)
	localAuth := auth.NewLocalAuthenticator(tokenManager)

	user, token, err := localAuth.Authenticate(db)
	require.NoError(t, err)
	assert.NotNil(t, user)
	assert.NotNil(t, token)
	assert.NotEmpty(t, user.Username)
	t.Logf("✓ Local user authenticated: %s (ID: %d)", user.Username, user.ID)

	// 3. Setup repository with local username
	setupCfg := &git.SetupConfig{
		BaseStorePath: storePath,
		Username:      user.Username,
		LocalOnly:     true,
	}

	// Check if repo already exists in DB
	var existingRepo database.MimirGitRepo
	err = db.Where("user_id = ?", user.ID).First(&existingRepo).Error
	if err != nil {
		// Create new repository
		result, err := git.SetupUserRepository(setupCfg)
		require.NoError(t, err)
		t.Logf("✓ Repository created: %s at %s", result.RepoName, result.RepoPath)

		// Store repo in database
		dbRepo := &database.MimirGitRepo{
			UserID:   user.ID,
			RepoUUID: result.RepoID,
			RepoName: result.RepoName,
			RepoPath: result.RepoPath,
		}
		db.Create(dbRepo)
	}

	// 4. Verify repository setup
	var repo database.MimirGitRepo
	err = db.Where("user_id = ?", user.ID).First(&repo).Error
	require.NoError(t, err)

	// Verify folder name is deterministic based on local username
	expectedRepoName := fmt.Sprintf("mimir-%s", user.Username)
	assert.Equal(t, expectedRepoName, repo.RepoName)
	assert.Equal(t, user.Username, repo.RepoUUID) // RepoUUID now stores username
	t.Logf("✓ Repository verified: %s", repo.RepoPath)

	// 5. Verify folder exists on disk
	info, err := os.Stat(repo.RepoPath)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
	t.Log("✓ Repository folder exists on disk")
}

// TestSAMLAuthFlow tests the complete SAML authentication flow
// This simulates what happens when a user authenticates via SAML (e.g., Okta, Duo)
func TestSAMLAuthFlow(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	storePath := filepath.Join(tempDir, "store")

	// 1. Initialize database
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
	t.Log("✓ Database initialized")

	// 2. Simulate SAML authentication (email-style username)
	samlUsername := "john.doe@company.com"
	samlEmail := "john.doe@company.com"

	// Create user as SAML would
	user := &database.MimirUser{
		Username: samlUsername,
		Email:    samlEmail,
	}
	result := db.Create(user)
	require.NoError(t, result.Error)
	t.Logf("✓ SAML user created: %s (ID: %d)", user.Username, user.ID)

	// 3. Setup repository with SAML username
	setupCfg := &git.SetupConfig{
		BaseStorePath: storePath,
		Username:      user.Username,
		LocalOnly:     true, // No PAT for this test
	}

	setupResult, err := git.SetupUserRepository(setupCfg)
	require.NoError(t, err)
	t.Logf("✓ Repository created: %s at %s", setupResult.RepoName, setupResult.RepoPath)

	// Store repo in database
	dbRepo := &database.MimirGitRepo{
		UserID:   user.ID,
		RepoUUID: setupResult.RepoID,
		RepoName: setupResult.RepoName,
		RepoPath: setupResult.RepoPath,
	}
	db.Create(dbRepo)

	// 4. Verify repository setup
	var repo database.MimirGitRepo
	err = db.Where("user_id = ?", user.ID).First(&repo).Error
	require.NoError(t, err)

	// Verify folder name is deterministic based on SAML username
	expectedRepoName := fmt.Sprintf("mimir-%s", samlUsername)
	assert.Equal(t, expectedRepoName, repo.RepoName)
	assert.Equal(t, samlUsername, repo.RepoUUID)
	t.Logf("✓ Repository verified: %s", repo.RepoPath)

	// 5. Verify folder exists on disk
	info, err := os.Stat(repo.RepoPath)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
	t.Log("✓ Repository folder exists on disk")

	// 6. Verify README contains SAML username
	readmeContent, err := os.ReadFile(filepath.Join(repo.RepoPath, "README.md"))
	require.NoError(t, err)
	assert.Contains(t, string(readmeContent), fmt.Sprintf("Owner: %s", samlUsername))
	t.Log("✓ README contains correct owner")
}

// TestRecoveryScenario tests the recovery when folder exists but not in database
// This can happen if the database is reset/recreated
func TestRecoveryScenario(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	storePath := filepath.Join(tempDir, "store")
	username := "recoveryuser"

	// 1. Initialize database
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

	// 2. Create user
	user := &database.MimirUser{
		Username: username,
		Email:    username + "@local",
	}
	db.Create(user)

	// 3. First setup - creates folder and adds to DB
	setupCfg := &git.SetupConfig{
		BaseStorePath: storePath,
		Username:      username,
		LocalOnly:     true,
	}

	result1, err := git.SetupUserRepository(setupCfg)
	require.NoError(t, err)
	t.Logf("✓ Initial repository created: %s", result1.RepoPath)

	// Store in DB
	dbRepo := &database.MimirGitRepo{
		UserID:   user.ID,
		RepoUUID: result1.RepoID,
		RepoName: result1.RepoName,
		RepoPath: result1.RepoPath,
	}
	db.Create(dbRepo)

	// 4. Simulate database reset (delete repo record)
	db.Unscoped().Delete(&database.MimirGitRepo{}, "user_id = ?", user.ID)

	// Verify DB is empty
	var count int64
	db.Model(&database.MimirGitRepo{}).Where("user_id = ?", user.ID).Count(&count)
	assert.Equal(t, int64(0), count)
	t.Log("✓ Simulated database reset (repo record deleted)")

	// 5. Verify folder still exists on disk
	_, err = os.Stat(result1.RepoPath)
	require.NoError(t, err)
	t.Log("✓ Repository folder still exists on disk")

	// 6. Attempt recovery - check for existing folder
	expectedRepoPath := git.GetUserRepositoryPath(storePath, username)

	var existingRepo database.MimirGitRepo
	err = db.Where("user_id = ?", user.ID).First(&existingRepo).Error
	if err != nil {
		// No repo in database, check if folder exists on disk (recovery scenario)
		if _, statErr := os.Stat(expectedRepoPath); statErr == nil {
			// Folder exists but not in DB - recover by adding to DB
			recoveredRepo := &database.MimirGitRepo{
				UserID:   user.ID,
				RepoUUID: username,
				RepoName: fmt.Sprintf("mimir-%s", username),
				RepoPath: expectedRepoPath,
			}
			db.Create(recoveredRepo)
			t.Log("✓ Recovery: Added existing folder to database")
		}
	}

	// 7. Verify recovery worked
	var recoveredRepo database.MimirGitRepo
	err = db.Where("user_id = ?", user.ID).First(&recoveredRepo).Error
	require.NoError(t, err)
	assert.Equal(t, expectedRepoPath, recoveredRepo.RepoPath)
	assert.Equal(t, username, recoveredRepo.RepoUUID)
	t.Logf("✓ Recovery verified: %s", recoveredRepo.RepoPath)
}

// TestNoNewFolderOnRestart tests that restarting the server doesn't create a new folder
func TestNoNewFolderOnRestart(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	storePath := filepath.Join(tempDir, "store")
	username := "persistentuser"

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

	// Create user
	user := &database.MimirUser{
		Username: username,
		Email:    username + "@local",
	}
	db.Create(user)

	// Simulate first "startup"
	var repo1Path string
	{
		setupCfg := &git.SetupConfig{
			BaseStorePath: storePath,
			Username:      username,
			LocalOnly:     true,
		}

		var existingRepo database.MimirGitRepo
		err = db.Where("user_id = ?", user.ID).First(&existingRepo).Error
		if err != nil {
			result, err := git.SetupUserRepository(setupCfg)
			require.NoError(t, err)
			repo1Path = result.RepoPath

			dbRepo := &database.MimirGitRepo{
				UserID:   user.ID,
				RepoUUID: result.RepoID,
				RepoName: result.RepoName,
				RepoPath: result.RepoPath,
			}
			db.Create(dbRepo)
			t.Logf("✓ First startup: Created %s", result.RepoPath)
		} else {
			repo1Path = existingRepo.RepoPath
		}
	}

	// Count folders after first startup
	entries1, err := os.ReadDir(storePath)
	require.NoError(t, err)
	folderCount1 := len(entries1)

	// Simulate second "startup" (restart)
	var repo2Path string
	{
		setupCfg := &git.SetupConfig{
			BaseStorePath: storePath,
			Username:      username,
			LocalOnly:     true,
		}

		var existingRepo database.MimirGitRepo
		err = db.Where("user_id = ?", user.ID).First(&existingRepo).Error
		if err != nil {
			// This should NOT happen - repo should exist
			t.Fatal("Expected repo to exist in database on restart")
		} else {
			repo2Path = existingRepo.RepoPath
			t.Logf("✓ Second startup: Reusing %s", existingRepo.RepoPath)
		}

		_ = setupCfg // Not used since repo already exists
	}

	// Count folders after second startup
	entries2, err := os.ReadDir(storePath)
	require.NoError(t, err)
	folderCount2 := len(entries2)

	// Verify no new folder was created
	assert.Equal(t, folderCount1, folderCount2, "New folder was created on restart!")
	assert.Equal(t, repo1Path, repo2Path, "Different repo paths on restart!")
	assert.Equal(t, 1, folderCount2, "Should only have one folder")
	t.Log("✓ No new folder created on restart")
}

// TestMultipleSAMLUsers tests that multiple SAML users get separate folders
func TestMultipleSAMLUsers(t *testing.T) {
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

	// Create multiple SAML users
	samlUsers := []struct {
		username string
		email    string
	}{
		{"alice.smith@company.com", "alice.smith@company.com"},
		{"bob.jones@company.com", "bob.jones@company.com"},
		{"charlie.brown@company.com", "charlie.brown@company.com"},
	}

	var createdPaths []string

	for _, samlUser := range samlUsers {
		// Create user
		user := &database.MimirUser{
			Username: samlUser.username,
			Email:    samlUser.email,
		}
		db.Create(user)

		// Setup repository
		setupCfg := &git.SetupConfig{
			BaseStorePath: storePath,
			Username:      user.Username,
			LocalOnly:     true,
		}

		result, err := git.SetupUserRepository(setupCfg)
		require.NoError(t, err)

		// Store in DB
		dbRepo := &database.MimirGitRepo{
			UserID:   user.ID,
			RepoUUID: result.RepoID,
			RepoName: result.RepoName,
			RepoPath: result.RepoPath,
		}
		db.Create(dbRepo)

		createdPaths = append(createdPaths, result.RepoPath)
		t.Logf("✓ Created repo for %s: %s", samlUser.username, result.RepoName)
	}

	// Verify all paths are unique
	pathSet := make(map[string]bool)
	for _, path := range createdPaths {
		assert.False(t, pathSet[path], "Duplicate path found: %s", path)
		pathSet[path] = true
	}

	// Verify correct number of folders
	entries, err := os.ReadDir(storePath)
	require.NoError(t, err)
	assert.Equal(t, len(samlUsers), len(entries))
	t.Logf("✓ Created %d unique folders for %d users", len(entries), len(samlUsers))
}

// TestMixedLocalAndSAMLUsers tests local and SAML users coexisting
func TestMixedLocalAndSAMLUsers(t *testing.T) {
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

	// Create users with different username styles
	users := []struct {
		username string
		email    string
		authType string
	}{
		{"localadmin", "localadmin@local", "local"},
		{"john.doe@company.com", "john.doe@company.com", "saml"},
		{"devuser", "devuser@local", "local"},
		{"jane.smith@company.com", "jane.smith@company.com", "saml"},
	}

	var repoPaths []string

	for _, u := range users {
		// Create user
		user := &database.MimirUser{
			Username: u.username,
			Email:    u.email,
		}
		db.Create(user)

		// Setup repository
		setupCfg := &git.SetupConfig{
			BaseStorePath: storePath,
			Username:      user.Username,
			LocalOnly:     true,
		}

		result, err := git.SetupUserRepository(setupCfg)
		require.NoError(t, err)

		// Verify naming
		expectedName := fmt.Sprintf("mimir-%s", u.username)
		assert.Equal(t, expectedName, result.RepoName)

		// Store in DB
		dbRepo := &database.MimirGitRepo{
			UserID:   user.ID,
			RepoUUID: result.RepoID,
			RepoName: result.RepoName,
			RepoPath: result.RepoPath,
		}
		db.Create(dbRepo)

		repoPaths = append(repoPaths, result.RepoPath)
		t.Logf("✓ [%s] Created repo for %s: %s", u.authType, u.username, result.RepoName)
	}

	// Verify all paths are unique
	pathSet := make(map[string]bool)
	for _, path := range repoPaths {
		assert.False(t, pathSet[path], "Duplicate path found: %s", path)
		pathSet[path] = true
	}

	// Verify correct number of folders
	entries, err := os.ReadDir(storePath)
	require.NoError(t, err)
	assert.Equal(t, len(users), len(entries))
	t.Logf("✓ Mixed auth: Created %d unique folders", len(entries))
}
