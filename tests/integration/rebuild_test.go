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
	"github.com/tejzpr/mimir-mcp/internal/rebuild"
	"gorm.io/gorm/logger"
)

// setupTestDB creates a test database
func setupTestDB(t *testing.T, dbPath string) *database.Config {
	return &database.Config{
		Type:       "sqlite",
		SQLitePath: dbPath,
		LogLevel:   logger.Silent,
	}
}

// createTestUser creates a test user in the database
func createTestUser(t *testing.T, db *database.Config, username string) (*database.MimirUser, func()) {
	conn, err := database.Connect(db)
	require.NoError(t, err)

	err = database.Migrate(conn)
	require.NoError(t, err)

	user := &database.MimirUser{
		Username: username,
		Email:    username + "@test.local",
	}
	conn.Create(user)

	return user, func() { database.Close(conn) }
}

// createTestRepo creates a test git repository and database record
func createTestRepo(t *testing.T, dbCfg *database.Config, userID uint, repoPath string) *database.MimirGitRepo {
	conn, err := database.Connect(dbCfg)
	require.NoError(t, err)
	defer database.Close(conn)

	// Initialize git repository
	_, err = git.InitRepository(repoPath)
	require.NoError(t, err)

	// Create database record
	repo := &database.MimirGitRepo{
		UserID:   userID,
		RepoUUID: "test-uuid",
		RepoName: "test-repo",
		RepoPath: repoPath,
	}
	conn.Create(repo)

	return repo
}

// writeMemoryFile writes a memory file to the repository
func writeMemoryFile(t *testing.T, repoPath string, mem *memory.Memory) string {
	markdown, err := mem.ToMarkdown()
	require.NoError(t, err)

	// Determine path based on tags or default
	var filePath string
	if len(mem.Tags) > 0 {
		filePath = filepath.Join(repoPath, "tags", mem.Tags[0], mem.ID+".md")
	} else {
		filePath = filepath.Join(repoPath, "2024", "01", mem.ID+".md")
	}

	err = os.MkdirAll(filepath.Dir(filePath), 0755)
	require.NoError(t, err)

	err = os.WriteFile(filePath, []byte(markdown), 0644)
	require.NoError(t, err)

	return filePath
}

// TestRebuildFromEmptyDatabase tests rebuilding when DB is completely empty
func TestRebuildFromEmptyDatabase(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	repoPath := filepath.Join(tempDir, "repo")

	// Setup database
	dbCfg := setupTestDB(t, dbPath)
	user, cleanup := createTestUser(t, dbCfg, "testuser")
	defer cleanup()

	// Create repo
	repo := createTestRepo(t, dbCfg, user.ID, repoPath)

	// Write memory files to repo
	mem1 := &memory.Memory{
		ID:      "memory-one",
		Title:   "Memory One",
		Tags:    []string{"tag1"},
		Created: time.Now(),
		Updated: time.Now(),
		Content: "Content of memory one",
	}
	mem2 := &memory.Memory{
		ID:      "memory-two",
		Title:   "Memory Two",
		Tags:    []string{"tag2", "tag3"},
		Created: time.Now(),
		Updated: time.Now(),
		Content: "Content of memory two",
	}

	writeMemoryFile(t, repoPath, mem1)
	writeMemoryFile(t, repoPath, mem2)

	// Connect and verify empty
	conn, err := database.Connect(dbCfg)
	require.NoError(t, err)
	defer database.Close(conn)

	var countBefore int64
	conn.Model(&database.MimirMemory{}).Count(&countBefore)
	assert.Equal(t, int64(0), countBefore)

	// Run rebuild
	result, err := rebuild.RebuildIndex(conn, user.ID, repo.ID, repoPath, rebuild.Options{Force: false})
	require.NoError(t, err)

	// Verify results
	assert.Equal(t, 2, result.MemoriesProcessed)
	assert.Equal(t, 2, result.MemoriesCreated)
	assert.Equal(t, 0, result.MemoriesSkipped)

	// Verify memories in DB
	var countAfter int64
	conn.Model(&database.MimirMemory{}).Count(&countAfter)
	assert.Equal(t, int64(2), countAfter)

	// Verify tags created
	var tagCount int64
	conn.Model(&database.MimirTag{}).Count(&tagCount)
	assert.Equal(t, int64(3), tagCount) // tag1, tag2, tag3
}

// TestRebuildNoDuplicates tests that rebuild with --force doesn't create duplicates
// when git files match existing DB records
func TestRebuildNoDuplicates(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	repoPath := filepath.Join(tempDir, "repo")

	// Setup
	dbCfg := setupTestDB(t, dbPath)
	user, cleanup := createTestUser(t, dbCfg, "testuser")
	defer cleanup()

	repo := createTestRepo(t, dbCfg, user.ID, repoPath)

	// Write TWO memory files to git
	mem1 := &memory.Memory{
		ID:      "existing-memory",
		Title:   "Existing Memory",
		Tags:    []string{"test"},
		Created: time.Now(),
		Updated: time.Now(),
		Content: "Content",
	}
	mem2 := &memory.Memory{
		ID:      "new-memory",
		Title:   "New Memory",
		Tags:    []string{"test"},
		Created: time.Now(),
		Updated: time.Now(),
		Content: "New Content",
	}
	writeMemoryFile(t, repoPath, mem1)
	writeMemoryFile(t, repoPath, mem2)

	// Connect - DB is empty at this point
	conn, err := database.Connect(dbCfg)
	require.NoError(t, err)
	defer database.Close(conn)

	// First rebuild populates DB
	result1, err := rebuild.RebuildIndex(conn, user.ID, repo.ID, repoPath, rebuild.Options{Force: false})
	require.NoError(t, err)
	assert.Equal(t, 2, result1.MemoriesCreated)

	// Second rebuild with --force should clear and recreate (no duplicates)
	result2, err := rebuild.RebuildIndex(conn, user.ID, repo.ID, repoPath, rebuild.Options{Force: true})
	require.NoError(t, err)

	// Should still have 2 memories (cleared and recreated)
	assert.Equal(t, 2, result2.MemoriesCreated)
	assert.Equal(t, 0, result2.MemoriesSkipped)

	// Verify exactly 2 memories in DB
	var countAfter int64
	conn.Model(&database.MimirMemory{}).Count(&countAfter)
	assert.Equal(t, int64(2), countAfter)
}

// TestRebuildRequiresForceWithData tests error when data exists without --force
func TestRebuildRequiresForceWithData(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	repoPath := filepath.Join(tempDir, "repo")

	// Setup
	dbCfg := setupTestDB(t, dbPath)
	user, cleanup := createTestUser(t, dbCfg, "testuser")
	defer cleanup()

	repo := createTestRepo(t, dbCfg, user.ID, repoPath)

	// Connect and create a memory in DB
	conn, err := database.Connect(dbCfg)
	require.NoError(t, err)
	defer database.Close(conn)

	dbMem := &database.MimirMemory{
		UserID:   user.ID,
		RepoID:   repo.ID,
		Slug:     "existing-memory",
		Title:    "Existing Memory",
		FilePath: filepath.Join(repoPath, "test.md"),
	}
	conn.Create(dbMem)

	// Attempt rebuild without force
	_, err = rebuild.RebuildIndex(conn, user.ID, repo.ID, repoPath, rebuild.Options{Force: false})

	// Should return error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "existing memories")
}

// TestRebuildForceOverwrites tests that --force clears and rebuilds
func TestRebuildForceOverwrites(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	repoPath := filepath.Join(tempDir, "repo")

	// Setup
	dbCfg := setupTestDB(t, dbPath)
	user, cleanup := createTestUser(t, dbCfg, "testuser")
	defer cleanup()

	repo := createTestRepo(t, dbCfg, user.ID, repoPath)

	// Connect and create memories in DB (that don't exist in git)
	conn, err := database.Connect(dbCfg)
	require.NoError(t, err)
	defer database.Close(conn)

	for i := 0; i < 5; i++ {
		dbMem := &database.MimirMemory{
			UserID:   user.ID,
			RepoID:   repo.ID,
			Slug:     "old-memory-" + string(rune('a'+i)),
			Title:    "Old Memory",
			FilePath: filepath.Join(repoPath, "old.md"),
		}
		conn.Create(dbMem)
	}

	var countBefore int64
	conn.Model(&database.MimirMemory{}).Count(&countBefore)
	assert.Equal(t, int64(5), countBefore)

	// Write different memories to git
	for i := 0; i < 3; i++ {
		mem := &memory.Memory{
			ID:      "new-memory-" + string(rune('x'+i)),
			Title:   "New Memory",
			Tags:    []string{"new"},
			Created: time.Now(),
			Updated: time.Now(),
			Content: "New content",
		}
		writeMemoryFile(t, repoPath, mem)
	}

	// Force rebuild
	result, err := rebuild.RebuildIndex(conn, user.ID, repo.ID, repoPath, rebuild.Options{Force: true})
	require.NoError(t, err)

	// Should now have 3 (from git), not 5 or 8
	var countAfter int64
	conn.Model(&database.MimirMemory{}).Count(&countAfter)
	assert.Equal(t, int64(3), countAfter)
	assert.Equal(t, 3, result.MemoriesCreated)
}

// TestRebuildWithAssociations tests that associations are recreated
func TestRebuildWithAssociations(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	repoPath := filepath.Join(tempDir, "repo")

	// Setup
	dbCfg := setupTestDB(t, dbPath)
	user, cleanup := createTestUser(t, dbCfg, "testuser")
	defer cleanup()

	repo := createTestRepo(t, dbCfg, user.ID, repoPath)

	// Create memories with associations
	mem1 := &memory.Memory{
		ID:      "memory-one",
		Title:   "Memory One",
		Tags:    []string{"test"},
		Created: time.Now(),
		Updated: time.Now(),
		Content: "Content 1",
		Associations: []memory.Association{
			{Target: "memory-two", Type: "related_to", Strength: 0.8},
		},
	}
	mem2 := &memory.Memory{
		ID:      "memory-two",
		Title:   "Memory Two",
		Tags:    []string{"test"},
		Created: time.Now(),
		Updated: time.Now(),
		Content: "Content 2",
	}

	writeMemoryFile(t, repoPath, mem1)
	writeMemoryFile(t, repoPath, mem2)

	// Connect and run rebuild
	conn, err := database.Connect(dbCfg)
	require.NoError(t, err)
	defer database.Close(conn)

	result, err := rebuild.RebuildIndex(conn, user.ID, repo.ID, repoPath, rebuild.Options{Force: false})
	require.NoError(t, err)

	// Verify associations created
	assert.Equal(t, 1, result.AssociationsCreated)

	var assocCount int64
	conn.Model(&database.MimirMemoryAssociation{}).Count(&assocCount)
	assert.Equal(t, int64(1), assocCount)

	// Verify association details
	var assoc database.MimirMemoryAssociation
	conn.First(&assoc)
	assert.Equal(t, "related_to", assoc.AssociationType)
	assert.Equal(t, 0.8, assoc.Strength)
}

// TestRebuildHandlesArchived tests that archive files get deleted_at set
func TestRebuildHandlesArchived(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	repoPath := filepath.Join(tempDir, "repo")

	// Setup
	dbCfg := setupTestDB(t, dbPath)
	user, cleanup := createTestUser(t, dbCfg, "testuser")
	defer cleanup()

	repo := createTestRepo(t, dbCfg, user.ID, repoPath)

	// Create active memory
	activeMem := &memory.Memory{
		ID:      "active-memory",
		Title:   "Active",
		Tags:    []string{"active"},
		Created: time.Now(),
		Updated: time.Now(),
		Content: "Active content",
	}
	writeMemoryFile(t, repoPath, activeMem)

	// Create archived memory (in archive/ folder)
	archivedMem := &memory.Memory{
		ID:      "archived-memory",
		Title:   "Archived",
		Created: time.Now(),
		Updated: time.Now(),
		Content: "Archived content",
	}
	archivePath := filepath.Join(repoPath, "archive", "archived-memory.md")
	_ = os.MkdirAll(filepath.Dir(archivePath), 0755)
	markdown, _ := archivedMem.ToMarkdown()
	_ = os.WriteFile(archivePath, []byte(markdown), 0644)

	// Connect and run rebuild
	conn, err := database.Connect(dbCfg)
	require.NoError(t, err)
	defer database.Close(conn)

	result, err := rebuild.RebuildIndex(conn, user.ID, repo.ID, repoPath, rebuild.Options{Force: false})
	require.NoError(t, err)

	assert.Equal(t, 2, result.MemoriesCreated)

	// Verify active memory is not deleted
	var activeMem2 database.MimirMemory
	err = conn.Where("slug = ?", "active-memory").First(&activeMem2).Error
	require.NoError(t, err)
	assert.False(t, activeMem2.DeletedAt.Valid)

	// Verify archived memory has deleted_at set
	var archivedMem2 database.MimirMemory
	err = conn.Unscoped().Where("slug = ?", "archived-memory").First(&archivedMem2).Error
	require.NoError(t, err)
	assert.True(t, archivedMem2.DeletedAt.Valid)
}

// TestRebuildSupersededStatus tests that superseded_by frontmatter is restored after rebuild
// This is a regression test for: https://github.com/tejzpr/mimir-mcp/issues/X
// When a memory is superseded, the superseded_by field should be in the markdown frontmatter
// and should be restored when the database is rebuilt
func TestRebuildSupersededStatus(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	repoPath := filepath.Join(tempDir, "repo")

	// Setup
	dbCfg := setupTestDB(t, dbPath)
	user, cleanup := createTestUser(t, dbCfg, "testuser")
	defer cleanup()

	repo := createTestRepo(t, dbCfg, user.ID, repoPath)

	// Create original memory with superseded_by in frontmatter
	oldMem := &memory.Memory{
		ID:           "memory-v1",
		Title:        "Original Decision",
		Tags:         []string{"decisions"},
		Created:      time.Now(),
		Updated:      time.Now(),
		Content:      "Original content",
		SupersededBy: "memory-v2", // This should be restored after rebuild
	}
	writeMemoryFile(t, repoPath, oldMem)

	// Create superseding memory
	newMem := &memory.Memory{
		ID:      "memory-v2",
		Title:   "Updated Decision",
		Tags:    []string{"decisions"},
		Created: time.Now(),
		Updated: time.Now(),
		Content: "Updated content",
	}
	writeMemoryFile(t, repoPath, newMem)

	// Connect and run first rebuild
	conn, err := database.Connect(dbCfg)
	require.NoError(t, err)
	defer database.Close(conn)

	result, err := rebuild.RebuildIndex(conn, user.ID, repo.ID, repoPath, rebuild.Options{Force: false})
	require.NoError(t, err)
	assert.Equal(t, 2, result.MemoriesCreated)

	// Verify superseded_by was restored in database
	var oldMemDB database.MimirMemory
	err = conn.Where("slug = ?", "memory-v1").First(&oldMemDB).Error
	require.NoError(t, err)
	require.NotNil(t, oldMemDB.SupersededBy, "superseded_by should not be nil")
	assert.Equal(t, "memory-v2", *oldMemDB.SupersededBy)

	// Verify new memory is not superseded
	var newMemDB database.MimirMemory
	err = conn.Where("slug = ?", "memory-v2").First(&newMemDB).Error
	require.NoError(t, err)
	assert.Nil(t, newMemDB.SupersededBy)

	// Now simulate database deletion and rebuild - this is the bug scenario
	// Clear all memories from DB
	conn.Unscoped().Where("user_id = ?", user.ID).Delete(&database.MimirMemory{})

	// Verify memories are gone
	var countBefore int64
	conn.Model(&database.MimirMemory{}).Count(&countBefore)
	assert.Equal(t, int64(0), countBefore)

	// Rebuild from git - superseded_by should be restored from frontmatter
	result2, err := rebuild.RebuildIndex(conn, user.ID, repo.ID, repoPath, rebuild.Options{Force: false})
	require.NoError(t, err)
	assert.Equal(t, 2, result2.MemoriesCreated)

	// Verify superseded_by is STILL present after rebuild
	var oldMemDBAfter database.MimirMemory
	err = conn.Where("slug = ?", "memory-v1").First(&oldMemDBAfter).Error
	require.NoError(t, err)
	require.NotNil(t, oldMemDBAfter.SupersededBy, "superseded_by should be restored from frontmatter")
	assert.Equal(t, "memory-v2", *oldMemDBAfter.SupersededBy)
}

// TestRebuildIdempotent tests that running rebuild with --force twice produces same result
func TestRebuildIdempotent(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	repoPath := filepath.Join(tempDir, "repo")

	// Setup
	dbCfg := setupTestDB(t, dbPath)
	user, cleanup := createTestUser(t, dbCfg, "testuser")
	defer cleanup()

	repo := createTestRepo(t, dbCfg, user.ID, repoPath)

	// Write memory file
	mem := &memory.Memory{
		ID:      "test-memory",
		Title:   "Test Memory",
		Tags:    []string{"test"},
		Created: time.Now(),
		Updated: time.Now(),
		Content: "Content",
	}
	writeMemoryFile(t, repoPath, mem)

	// Connect
	conn, err := database.Connect(dbCfg)
	require.NoError(t, err)
	defer database.Close(conn)

	// First rebuild (empty DB, no force needed)
	result1, err := rebuild.RebuildIndex(conn, user.ID, repo.ID, repoPath, rebuild.Options{Force: false})
	require.NoError(t, err)
	assert.Equal(t, 1, result1.MemoriesCreated)

	// Second rebuild (has data, requires --force)
	result2, err := rebuild.RebuildIndex(conn, user.ID, repo.ID, repoPath, rebuild.Options{Force: true})
	require.NoError(t, err)
	assert.Equal(t, 1, result2.MemoriesCreated) // Cleared and recreated

	// Third rebuild (has data, requires --force)
	result3, err := rebuild.RebuildIndex(conn, user.ID, repo.ID, repoPath, rebuild.Options{Force: true})
	require.NoError(t, err)
	assert.Equal(t, 1, result3.MemoriesCreated) // Cleared and recreated again

	// Verify still only 1 memory
	var count int64
	conn.Model(&database.MimirMemory{}).Count(&count)
	assert.Equal(t, int64(1), count)
}
