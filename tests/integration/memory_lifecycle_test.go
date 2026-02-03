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
	"github.com/tejzpr/mimir-mcp/internal/database"
	"github.com/tejzpr/mimir-mcp/internal/git"
	"github.com/tejzpr/mimir-mcp/internal/memory"
	"gorm.io/gorm/logger"
)

// TestMemoryLifecycle tests the complete lifecycle of a memory
func TestMemoryLifecycle(t *testing.T) {
	// Setup
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	repoPath := filepath.Join(tempDir, "repo")

	// Connect to database
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
		Username: "testuser",
		Email:    "test@example.com",
	}
	db.Create(user)

	// Initialize git repository
	repo, err := git.InitRepository(repoPath)
	require.NoError(t, err)

	// Create initial structure
	_ = os.MkdirAll(filepath.Join(repoPath, "2024/01"), 0755)

	// Store repo in database
	dbRepo := &database.MimirGitRepo{
		UserID:   user.ID,
		RepoUUID: "test-uuid",
		RepoName: "test-repo",
		RepoPath: repoPath,
	}
	db.Create(dbRepo)

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

	// Commit
	msgFormat := git.CommitMessageFormats{}
	err = repo.CommitFile(filePath, msgFormat.CreateMemory(slug))
	require.NoError(t, err)

	// Store in database
	dbMem := &database.MimirMemory{
		UserID:   user.ID,
		RepoID:   dbRepo.ID,
		Slug:     slug,
		Title:    "Test Memory",
		FilePath: filePath,
	}
	db.Create(dbMem)

	// 2. READ: Verify memory can be read
	var foundMem database.MimirMemory
	err = db.Where("slug = ?", slug).First(&foundMem).Error
	require.NoError(t, err)
	assert.Equal(t, "Test Memory", foundMem.Title)

	content, err := os.ReadFile(foundMem.FilePath)
	require.NoError(t, err)

	parsedMem, err := memory.ParseMarkdown(string(content))
	require.NoError(t, err)
	assert.Equal(t, "Test Memory", parsedMem.Title)
	assert.Contains(t, parsedMem.Content, "This is a test memory")

	// 3. UPDATE: Modify the memory
	parsedMem.Content = "# Updated Content\n\nThis has been updated."
	parsedMem.Updated = time.Now()

	updatedMarkdown, err := parsedMem.ToMarkdown()
	require.NoError(t, err)

	err = os.WriteFile(filePath, []byte(updatedMarkdown), 0644)
	require.NoError(t, err)

	err = repo.CommitFile(filePath, msgFormat.UpdateMemory(slug))
	require.NoError(t, err)

	// Update database
	foundMem.UpdatedAt = time.Now()
	db.Save(&foundMem)

	// Verify update
	content, _ = os.ReadFile(filePath)
	parsedMem, _ = memory.ParseMarkdown(string(content))
	assert.Contains(t, parsedMem.Content, "This has been updated")

	// 4. DELETE: Soft delete the memory
	archivePath := organizer.GetArchivePath(slug)
	_ = os.MkdirAll(filepath.Dir(archivePath), 0755)
	err = os.Rename(filePath, archivePath)
	require.NoError(t, err)

	err = repo.CommitAll(msgFormat.ArchiveMemory(slug))
	require.NoError(t, err)

	// Soft delete in database
	db.Delete(&foundMem)

	// 5. VERIFY: Memory is soft deleted but history preserved
	var deletedMem database.MimirMemory
	err = db.Unscoped().Where("slug = ?", slug).First(&deletedMem).Error
	require.NoError(t, err)
	assert.True(t, deletedMem.DeletedAt.Valid)

	// Verify file is in archive
	_, err = os.Stat(archivePath)
	assert.NoError(t, err)

	// Verify git history preserved
	commits, err := repo.GetCommitHistory(10)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(commits), 3) // Create, Update, Delete
}
