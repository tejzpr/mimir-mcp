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
	"github.com/tejzpr/medha-mcp/internal/git"
	"github.com/tejzpr/medha-mcp/internal/memory"
	"github.com/tejzpr/medha-mcp/internal/rebuild"
	"gorm.io/gorm/logger"
)

// TestMemoryLifecycle tests the complete lifecycle of a memory
// Updated to use v2 architecture with per-user database
func TestMemoryLifecycle(t *testing.T) {
	// Setup
	tempDir := t.TempDir()
	systemDBPath := filepath.Join(tempDir, "system.db")
	repoPath := filepath.Join(tempDir, "repo")
	require.NoError(t, os.MkdirAll(repoPath, 0755))

	// Create database manager (v2 architecture)
	dbCfg := &database.Config{
		Type:       "sqlite",
		SQLitePath: systemDBPath,
		LogLevel:   logger.Silent,
	}

	mgr, err := database.NewManager(dbCfg)
	require.NoError(t, err)
	defer mgr.Close()

	// Run v1 migrations on system DB for backward compatibility
	err = database.Migrate(mgr.SystemDB())
	require.NoError(t, err)

	// Create test user in system DB
	user := &database.MedhaUser{
		Username: "testuser",
		Email:    "test@example.com",
	}
	mgr.SystemDB().Create(user)

	// Initialize git repository (skip if sandboxed)
	repo, gitErr := git.InitRepository(repoPath)
	gitAvailable := gitErr == nil

	// Create initial structure
	_ = os.MkdirAll(filepath.Join(repoPath, "2024/01"), 0755)

	// Store repo in system database
	dbRepo := &database.MedhaGitRepo{
		UserID:   user.ID,
		RepoUUID: "test-uuid",
		RepoName: "test-repo",
		RepoPath: repoPath,
	}
	mgr.SystemDB().Create(dbRepo)

	// Get per-user database (v2)
	userDB, err := mgr.GetUserDB(repoPath)
	require.NoError(t, err)

	// 1. CREATE: Write a new memory
	slug := memory.GenerateSlugWithDate("Test Memory", time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC))
	mem := &memory.Memory{
		ID:      slug,
		Title:   "Test Memory",
		Tags:    []string{"test", "integration"},
		Created: time.Now(),
		Updated: time.Now(),
		Content: "# Test Content\n\nThis is a test memory.",
	}

	markdown, err := mem.ToMarkdown()
	require.NoError(t, err)

	organizer := memory.NewOrganizer(repoPath)
	filePath := organizer.GetMemoryPath(slug, mem.Tags, "", mem.Created)

	_ = os.MkdirAll(filepath.Dir(filePath), 0755)
	err = os.WriteFile(filePath, []byte(markdown), 0644)
	require.NoError(t, err)

	// Commit (if git available)
	msgFormat := git.CommitMessageFormats{}
	if gitAvailable && repo != nil {
		err = repo.CommitFile(filePath, msgFormat.CreateMemory(slug))
		require.NoError(t, err)
	}

	// Store in per-user database (v2) with content hash
	contentHash := rebuild.CalculateContentHash(markdown)
	dbMem := &database.UserMemory{
		Slug:        slug,
		Title:       "Test Memory",
		FilePath:    filePath,
		ContentHash: contentHash,
		Version:     1,
	}
	userDB.Create(dbMem)

	// 2. READ: Verify memory can be read from per-user DB
	var foundMem database.UserMemory
	err = userDB.Where("slug = ?", slug).First(&foundMem).Error
	require.NoError(t, err)
	assert.Equal(t, "Test Memory", foundMem.Title)
	assert.Equal(t, contentHash, foundMem.ContentHash)
	assert.Equal(t, int64(1), foundMem.Version)

	content, err := os.ReadFile(foundMem.FilePath)
	require.NoError(t, err)

	parsedMem, err := memory.ParseMarkdown(string(content))
	require.NoError(t, err)
	assert.Equal(t, "Test Memory", parsedMem.Title)
	assert.Contains(t, parsedMem.Content, "This is a test memory")

	// 3. UPDATE: Modify the memory with version increment
	parsedMem.Content = "# Updated Content\n\nThis has been updated."
	parsedMem.Updated = time.Now()

	updatedMarkdown, err := parsedMem.ToMarkdown()
	require.NoError(t, err)

	err = os.WriteFile(filePath, []byte(updatedMarkdown), 0644)
	require.NoError(t, err)

	if gitAvailable && repo != nil {
		err = repo.CommitFile(filePath, msgFormat.UpdateMemory(slug))
		require.NoError(t, err)
	}

	// Update in per-user database with new version and content hash
	newContentHash := rebuild.CalculateContentHash(updatedMarkdown)
	err = userDB.Model(&foundMem).Updates(map[string]interface{}{
		"content_hash": newContentHash,
		"version":      foundMem.Version + 1,
	}).Error
	require.NoError(t, err)

	// Verify version was incremented
	var updatedMem database.UserMemory
	userDB.Where("slug = ?", slug).First(&updatedMem)
	assert.Equal(t, int64(2), updatedMem.Version)
	assert.Equal(t, newContentHash, updatedMem.ContentHash)

	// Verify file content
	content, _ = os.ReadFile(filePath)
	parsedMem, _ = memory.ParseMarkdown(string(content))
	assert.Contains(t, parsedMem.Content, "This has been updated")

	// 4. DELETE: Soft delete the memory
	archivePath := organizer.GetArchivePath(slug)
	_ = os.MkdirAll(filepath.Dir(archivePath), 0755)
	err = os.Rename(filePath, archivePath)
	require.NoError(t, err)

	if gitAvailable && repo != nil {
		err = repo.CommitAll(msgFormat.ArchiveMemory(slug))
		require.NoError(t, err)
	}

	// Soft delete in per-user database
	userDB.Delete(&updatedMem)

	// 5. VERIFY: Memory is soft deleted but history preserved
	var deletedMem database.UserMemory
	err = userDB.Unscoped().Where("slug = ?", slug).First(&deletedMem).Error
	require.NoError(t, err)
	assert.True(t, deletedMem.DeletedAt.Valid)

	// Verify file is in archive
	_, err = os.Stat(archivePath)
	assert.NoError(t, err)

	// Verify git history preserved (if available)
	if gitAvailable && repo != nil {
		commits, err := repo.GetCommitHistory(10)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(commits), 3) // Create, Update, Delete
	}
}

// TestMemoryLifecycle_UserDB_Only tests memory lifecycle using only per-user database
func TestMemoryLifecycle_UserDB_Only(t *testing.T) {
	tempDir := t.TempDir()
	repoPath := filepath.Join(tempDir, "repo")
	require.NoError(t, os.MkdirAll(repoPath, 0755))

	// Open per-user database directly
	userDB, err := database.OpenUserDB(repoPath)
	require.NoError(t, err)
	defer func() {
		sqlDB, _ := userDB.DB()
		sqlDB.Close()
	}()

	// CREATE
	mem := &database.UserMemory{
		Slug:        "lifecycle-test",
		Title:       "Lifecycle Test Memory",
		FilePath:    "2024/01/lifecycle-test.md",
		ContentHash: "hash-v1",
		Version:     1,
	}
	require.NoError(t, userDB.Create(mem).Error)

	// READ
	var found database.UserMemory
	err = userDB.Where("slug = ?", "lifecycle-test").First(&found).Error
	require.NoError(t, err)
	assert.Equal(t, "Lifecycle Test Memory", found.Title)
	assert.Equal(t, int64(1), found.Version)

	// UPDATE with version
	err = userDB.Model(&found).Updates(map[string]interface{}{
		"title":        "Updated Title",
		"content_hash": "hash-v2",
		"version":      2,
	}).Error
	require.NoError(t, err)

	var updated database.UserMemory
	userDB.Where("slug = ?", "lifecycle-test").First(&updated)
	assert.Equal(t, "Updated Title", updated.Title)
	assert.Equal(t, int64(2), updated.Version)

	// SOFT DELETE
	err = userDB.Delete(&updated).Error
	require.NoError(t, err)

	// Should not find with normal query
	var notFound database.UserMemory
	err = userDB.Where("slug = ?", "lifecycle-test").First(&notFound).Error
	assert.Error(t, err)

	// Should find with Unscoped
	var deleted database.UserMemory
	err = userDB.Unscoped().Where("slug = ?", "lifecycle-test").First(&deleted).Error
	require.NoError(t, err)
	assert.True(t, deleted.DeletedAt.Valid)
	assert.Equal(t, int64(2), deleted.Version)

	// RESTORE (undelete)
	err = userDB.Unscoped().Model(&deleted).Update("deleted_at", nil).Error
	require.NoError(t, err)

	var restored database.UserMemory
	err = userDB.Where("slug = ?", "lifecycle-test").First(&restored).Error
	require.NoError(t, err)
	assert.Equal(t, "Updated Title", restored.Title)
}
