// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package integration

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tejzpr/medha-mcp/internal/database"
	"github.com/tejzpr/medha-mcp/internal/memory"
	"github.com/tejzpr/medha-mcp/internal/rebuild"
	"gorm.io/gorm/logger"
)

// TestRebuildUserDB_SinglePath tests rebuilding a per-user database by path
func TestRebuildUserDB_SinglePath(t *testing.T) {
	tempDir := t.TempDir()
	repoPath := filepath.Join(tempDir, "test-repo")
	require.NoError(t, os.MkdirAll(repoPath, 0755))

	// Create some markdown files
	createTestMemoryFile(t, repoPath, "memory-1", "Test Memory 1", []string{"tag1"})
	createTestMemoryFile(t, repoPath, "memory-2", "Test Memory 2", []string{"tag2"})

	// Open per-user database
	userDB, err := database.OpenUserDB(repoPath)
	require.NoError(t, err)
	defer func() {
		sqlDB, _ := userDB.DB()
		sqlDB.Close()
	}()

	// Run rebuild
	opts := rebuild.Options{Force: false}
	result, err := rebuild.RebuildUserIndex(userDB, repoPath, opts)
	require.NoError(t, err)

	assert.Equal(t, 2, result.MemoriesProcessed)
	assert.Equal(t, 2, result.MemoriesCreated)
	assert.Equal(t, 0, result.MemoriesSkipped)

	// Verify memories in database
	var memories []database.UserMemory
	userDB.Find(&memories)
	assert.Len(t, memories, 2)

	// Verify tags were created
	var tags []database.UserTag
	userDB.Find(&tags)
	assert.Len(t, tags, 2)
}

// TestRebuildUserDB_ForceOverwrite tests force rebuild overwrites existing data
func TestRebuildUserDB_ForceOverwrite(t *testing.T) {
	tempDir := t.TempDir()
	repoPath := filepath.Join(tempDir, "test-repo")
	require.NoError(t, os.MkdirAll(repoPath, 0755))

	// Create markdown files
	createTestMemoryFile(t, repoPath, "memory-1", "Test Memory 1", []string{"tag1"})

	// Open per-user database and create initial data
	userDB, err := database.OpenUserDB(repoPath)
	require.NoError(t, err)

	// First rebuild
	opts := rebuild.Options{Force: false}
	result1, err := rebuild.RebuildUserIndex(userDB, repoPath, opts)
	require.NoError(t, err)
	assert.Equal(t, 1, result1.MemoriesCreated)

	// Add another memory file
	createTestMemoryFile(t, repoPath, "memory-2", "Test Memory 2", []string{"tag2"})

	// Non-force rebuild should fail due to existing data
	_, err = rebuild.RebuildUserIndex(userDB, repoPath, rebuild.Options{Force: false})
	assert.Error(t, err) // Should fail because data exists

	// Force rebuild should succeed
	opts.Force = true
	result2, err := rebuild.RebuildUserIndex(userDB, repoPath, opts)
	require.NoError(t, err)
	assert.Equal(t, 2, result2.MemoriesProcessed)
	assert.Equal(t, 2, result2.MemoriesCreated)

	// Cleanup
	sqlDB, _ := userDB.DB()
	sqlDB.Close()
}

// TestRebuildUserDB_EmptyRepository tests rebuilding an empty repository
func TestRebuildUserDB_EmptyRepository(t *testing.T) {
	tempDir := t.TempDir()
	repoPath := filepath.Join(tempDir, "empty-repo")
	require.NoError(t, os.MkdirAll(repoPath, 0755))

	userDB, err := database.OpenUserDB(repoPath)
	require.NoError(t, err)
	defer func() {
		sqlDB, _ := userDB.DB()
		sqlDB.Close()
	}()

	opts := rebuild.Options{Force: false}
	result, err := rebuild.RebuildUserIndex(userDB, repoPath, opts)
	require.NoError(t, err)

	assert.Equal(t, 0, result.MemoriesProcessed)
	assert.Equal(t, 0, result.MemoriesCreated)
}

// TestRebuildUserDB_WithAssociations tests rebuilding memories with associations
func TestRebuildUserDB_WithAssociations(t *testing.T) {
	tempDir := t.TempDir()
	repoPath := filepath.Join(tempDir, "test-repo")
	require.NoError(t, os.MkdirAll(repoPath, 0755))

	// Create memory with association
	createTestMemoryFileWithAssociation(t, repoPath, "memory-1", "Memory 1", "memory-2")
	createTestMemoryFile(t, repoPath, "memory-2", "Memory 2", []string{})

	userDB, err := database.OpenUserDB(repoPath)
	require.NoError(t, err)
	defer func() {
		sqlDB, _ := userDB.DB()
		sqlDB.Close()
	}()

	opts := rebuild.Options{Force: false}
	result, err := rebuild.RebuildUserIndex(userDB, repoPath, opts)
	require.NoError(t, err)

	assert.Equal(t, 2, result.MemoriesCreated)
	assert.GreaterOrEqual(t, result.AssociationsCreated, 1)

	// Verify association in database
	var associations []database.UserMemoryAssociation
	userDB.Find(&associations)
	assert.GreaterOrEqual(t, len(associations), 1)
}

// TestRebuildUserDB_SkipsMedhaDirectory tests that .medha directory is skipped
func TestRebuildUserDB_SkipsMedhaDirectory(t *testing.T) {
	tempDir := t.TempDir()
	repoPath := filepath.Join(tempDir, "test-repo")
	require.NoError(t, os.MkdirAll(repoPath, 0755))

	// Create normal memory
	createTestMemoryFile(t, repoPath, "normal-memory", "Normal Memory", []string{})

	// Create a file in .medha directory (should be ignored)
	medhaDir := filepath.Join(repoPath, ".medha")
	require.NoError(t, os.MkdirAll(medhaDir, 0755))
	createTestMemoryFileInDir(t, medhaDir, "should-ignore", "Should Ignore", []string{})

	userDB, err := database.OpenUserDB(repoPath)
	require.NoError(t, err)
	defer func() {
		sqlDB, _ := userDB.DB()
		sqlDB.Close()
	}()

	opts := rebuild.Options{Force: false}
	result, err := rebuild.RebuildUserIndex(userDB, repoPath, opts)
	require.NoError(t, err)

	// Should only find the normal memory, not the one in .medha
	assert.Equal(t, 1, result.MemoriesProcessed)
	assert.Equal(t, 1, result.MemoriesCreated)
}

// TestRebuildUserDB_ContentHash tests that content hash is calculated
func TestRebuildUserDB_ContentHash(t *testing.T) {
	tempDir := t.TempDir()
	repoPath := filepath.Join(tempDir, "test-repo")
	require.NoError(t, os.MkdirAll(repoPath, 0755))

	createTestMemoryFile(t, repoPath, "hashed-memory", "Hashed Memory", []string{"test"})

	userDB, err := database.OpenUserDB(repoPath)
	require.NoError(t, err)
	defer func() {
		sqlDB, _ := userDB.DB()
		sqlDB.Close()
	}()

	opts := rebuild.Options{Force: false}
	_, err = rebuild.RebuildUserIndex(userDB, repoPath, opts)
	require.NoError(t, err)

	// Verify content hash was set
	var mem database.UserMemory
	err = userDB.Where("slug = ?", "hashed-memory").First(&mem).Error
	require.NoError(t, err)
	assert.NotEmpty(t, mem.ContentHash)
}

// TestRebuildUserDB_ArchivedMemories tests that archived memories are marked
func TestRebuildUserDB_ArchivedMemories(t *testing.T) {
	tempDir := t.TempDir()
	repoPath := filepath.Join(tempDir, "test-repo")
	archiveDir := filepath.Join(repoPath, "archive")
	require.NoError(t, os.MkdirAll(repoPath, 0755))
	require.NoError(t, os.MkdirAll(archiveDir, 0755))

	// Create normal memory
	createTestMemoryFile(t, repoPath, "active-memory", "Active Memory", []string{})

	// Create archived memory
	createTestMemoryFileInDir(t, archiveDir, "archived-memory", "Archived Memory", []string{})

	userDB, err := database.OpenUserDB(repoPath)
	require.NoError(t, err)
	defer func() {
		sqlDB, _ := userDB.DB()
		sqlDB.Close()
	}()

	opts := rebuild.Options{Force: false}
	_, err = rebuild.RebuildUserIndex(userDB, repoPath, opts)
	require.NoError(t, err)

	// Active memory should not be deleted
	var activeMem database.UserMemory
	err = userDB.Where("slug = ?", "active-memory").First(&activeMem).Error
	require.NoError(t, err)
	assert.False(t, activeMem.DeletedAt.Valid)

	// Archived memory should be soft deleted
	var archivedMem database.UserMemory
	err = userDB.Unscoped().Where("slug = ?", "archived-memory").First(&archivedMem).Error
	require.NoError(t, err)
	assert.True(t, archivedMem.DeletedAt.Valid)
}

// TestRebuildUserDB_AllRepos tests rebuilding all repositories
func TestRebuildUserDB_AllRepos(t *testing.T) {
	tempDir := t.TempDir()
	systemDBPath := filepath.Join(tempDir, "system.db")

	// Create database manager
	dbCfg := &database.Config{
		Type:       "sqlite",
		SQLitePath: systemDBPath,
		LogLevel:   logger.Silent,
	}

	mgr, err := database.NewManager(dbCfg)
	require.NoError(t, err)
	defer mgr.Close()

	// Run v1 migrations
	err = database.Migrate(mgr.SystemDB())
	require.NoError(t, err)

	// Create two users with repos
	user1 := &database.MedhaUser{Username: "user1@test.com", Email: "user1@test.com"}
	user2 := &database.MedhaUser{Username: "user2@test.com", Email: "user2@test.com"}
	mgr.SystemDB().Create(user1)
	mgr.SystemDB().Create(user2)

	repo1Path := filepath.Join(tempDir, "repo1")
	repo2Path := filepath.Join(tempDir, "repo2")
	require.NoError(t, os.MkdirAll(repo1Path, 0755))
	require.NoError(t, os.MkdirAll(repo2Path, 0755))

	repo1 := &database.MedhaGitRepo{UserID: user1.ID, RepoUUID: "repo1", RepoName: "repo1", RepoPath: repo1Path}
	repo2 := &database.MedhaGitRepo{UserID: user2.ID, RepoUUID: "repo2", RepoName: "repo2", RepoPath: repo2Path}
	mgr.SystemDB().Create(repo1)
	mgr.SystemDB().Create(repo2)

	// Create memory files in each repo
	createTestMemoryFile(t, repo1Path, "user1-memory", "User 1 Memory", []string{})
	createTestMemoryFile(t, repo2Path, "user2-memory", "User 2 Memory", []string{})

	// Query all repos
	var repos []database.MedhaGitRepo
	err = mgr.SystemDB().Find(&repos).Error
	require.NoError(t, err)
	assert.Len(t, repos, 2)

	// Rebuild each repo's per-user database
	opts := rebuild.Options{Force: false}
	for _, repo := range repos {
		userDB, err := database.OpenUserDB(repo.RepoPath)
		require.NoError(t, err)

		result, err := rebuild.RebuildUserIndex(userDB, repo.RepoPath, opts)
		require.NoError(t, err)
		assert.Equal(t, 1, result.MemoriesCreated)

		sqlDB, _ := userDB.DB()
		sqlDB.Close()
	}

	// Verify each repo has its own data
	userDB1, _ := database.OpenUserDB(repo1Path)
	var mems1 []database.UserMemory
	userDB1.Find(&mems1)
	assert.Len(t, mems1, 1)
	assert.Equal(t, "user1-memory", mems1[0].Slug)
	sqlDB1, _ := userDB1.DB()
	sqlDB1.Close()

	userDB2, _ := database.OpenUserDB(repo2Path)
	var mems2 []database.UserMemory
	userDB2.Find(&mems2)
	assert.Len(t, mems2, 1)
	assert.Equal(t, "user2-memory", mems2[0].Slug)
	sqlDB2, _ := userDB2.DB()
	sqlDB2.Close()
}

// TestRebuildUserDB_FindByUsername tests finding repo by username
func TestRebuildUserDB_FindByUsername(t *testing.T) {
	tempDir := t.TempDir()
	systemDBPath := filepath.Join(tempDir, "system.db")

	dbCfg := &database.Config{
		Type:       "sqlite",
		SQLitePath: systemDBPath,
		LogLevel:   logger.Silent,
	}

	mgr, err := database.NewManager(dbCfg)
	require.NoError(t, err)
	defer mgr.Close()

	err = database.Migrate(mgr.SystemDB())
	require.NoError(t, err)

	// Create user
	user := &database.MedhaUser{Username: "testuser@example.com", Email: "testuser@example.com"}
	mgr.SystemDB().Create(user)

	repoPath := filepath.Join(tempDir, "user-repo")
	require.NoError(t, os.MkdirAll(repoPath, 0755))

	repo := &database.MedhaGitRepo{UserID: user.ID, RepoUUID: "user-repo", RepoName: "user-repo", RepoPath: repoPath}
	mgr.SystemDB().Create(repo)

	// Find repo by username (simulating CLI lookup)
	var foundRepo database.MedhaGitRepo
	err = mgr.SystemDB().Joins("JOIN medha_users ON medha_users.id = medha_git_repos.user_id").
		Where("medha_users.username = ?", "testuser@example.com").
		First(&foundRepo).Error
	require.NoError(t, err)
	assert.Equal(t, repoPath, foundRepo.RepoPath)
}

// Helper functions

func createTestMemoryFile(t *testing.T, repoPath, slug, title string, tags []string) {
	t.Helper()
	createTestMemoryFileInDir(t, repoPath, slug, title, tags)
}

func createTestMemoryFileInDir(t *testing.T, dir, slug, title string, tags []string) {
	t.Helper()

	mem := &memory.Memory{
		ID:      slug,
		Title:   title,
		Tags:    tags,
		Created: time.Now(),
		Updated: time.Now(),
		Content: "# " + title + "\n\nTest content for " + slug,
	}

	markdown, err := mem.ToMarkdown()
	require.NoError(t, err)

	filePath := filepath.Join(dir, slug+".md")
	require.NoError(t, os.WriteFile(filePath, []byte(markdown), 0644))
}

func createTestMemoryFileWithAssociation(t *testing.T, repoPath, slug, title, targetSlug string) {
	t.Helper()

	mem := &memory.Memory{
		ID:      slug,
		Title:   title,
		Tags:    []string{},
		Created: time.Now(),
		Updated: time.Now(),
		Content: "# " + title + "\n\nTest content with association.",
		Associations: []memory.Association{
			{Target: targetSlug, Type: "related_to", Strength: 0.8},
		},
	}

	markdown, err := mem.ToMarkdown()
	require.NoError(t, err)

	filePath := filepath.Join(repoPath, slug+".md")
	require.NoError(t, os.WriteFile(filePath, []byte(markdown), 0644))
}
