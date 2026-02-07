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
	"github.com/tejzpr/medha-mcp/internal/tools"
	"gorm.io/gorm/logger"
)

func setupPerUserTestContext(t *testing.T) (*database.Manager, string, string, func()) {
	tmpDir := t.TempDir()
	systemDBPath := filepath.Join(tmpDir, "system.db")
	repoPath := filepath.Join(tmpDir, "test-repo")
	require.NoError(t, os.MkdirAll(repoPath, 0755))

	cfg := &database.Config{
		Type:       "sqlite",
		SQLitePath: systemDBPath,
		LogLevel:   logger.Silent,
	}

	mgr, err := database.NewManager(cfg)
	require.NoError(t, err)

	cleanup := func() {
		mgr.Close()
	}

	return mgr, repoPath, tmpDir, cleanup
}

func TestPerUserDB_DataIsolation(t *testing.T) {
	mgr, repoPath, tmpDir, cleanup := setupPerUserTestContext(t)
	defer cleanup()

	// Create two separate user repos
	repo1 := filepath.Join(tmpDir, "user1-repo")
	repo2 := filepath.Join(tmpDir, "user2-repo")
	require.NoError(t, os.MkdirAll(repo1, 0755))
	require.NoError(t, os.MkdirAll(repo2, 0755))

	// Get user DBs
	db1, err := mgr.GetUserDB(repo1)
	require.NoError(t, err)

	db2, err := mgr.GetUserDB(repo2)
	require.NoError(t, err)

	// Create memory in user1's DB
	mem1 := &database.UserMemory{
		Slug:     "user1-private",
		Title:    "User 1 Private Memory",
		FilePath: "2024/01/user1-private.md",
	}
	require.NoError(t, db1.Create(mem1).Error)

	// Create memory in user2's DB
	mem2 := &database.UserMemory{
		Slug:     "user2-private",
		Title:    "User 2 Private Memory",
		FilePath: "2024/01/user2-private.md",
	}
	require.NoError(t, db2.Create(mem2).Error)

	// User1 should only see their memory
	var memories1 []database.UserMemory
	db1.Find(&memories1)
	assert.Len(t, memories1, 1)
	assert.Equal(t, "user1-private", memories1[0].Slug)

	// User2 should only see their memory
	var memories2 []database.UserMemory
	db2.Find(&memories2)
	assert.Len(t, memories2, 1)
	assert.Equal(t, "user2-private", memories2[0].Slug)

	// Cross-check: user1's memory should not be in user2's DB
	var crossCheck database.UserMemory
	err = db2.Where("slug = ?", "user1-private").First(&crossCheck).Error
	assert.Error(t, err) // Should not find

	_ = repoPath // unused in this test
}

func TestPerUserDB_MigrationOnOpen(t *testing.T) {
	tmpDir := t.TempDir()
	repoPath := filepath.Join(tmpDir, "test-repo")
	require.NoError(t, os.MkdirAll(repoPath, 0755))

	// Open user DB - should trigger migration
	userDB, err := database.OpenUserDB(repoPath)
	require.NoError(t, err)
	defer func() {
		sqlDB, _ := userDB.DB()
		sqlDB.Close()
	}()

	// Verify all expected tables exist
	expectedTables := []string{
		"memories",
		"associations",
		"tags",
		"memory_tags",
		"annotations",
	}

	for _, table := range expectedTables {
		hasTable := userDB.Migrator().HasTable(table)
		assert.True(t, hasTable, "Table %s should exist after migration", table)
	}
}

func TestPerUserDB_ToolContext_V2(t *testing.T) {
	mgr, repoPath, _, cleanup := setupPerUserTestContext(t)
	defer cleanup()

	// Create tool context using v2 method
	ctx, err := tools.NewToolContextWithManager(mgr, repoPath)
	require.NoError(t, err)

	// Verify both databases are available
	assert.NotNil(t, ctx.SystemDB)
	assert.NotNil(t, ctx.UserDB)
	assert.True(t, ctx.HasUserDB())

	// Verify backward compatibility
	assert.NotNil(t, ctx.DB)
	assert.Equal(t, ctx.SystemDB, ctx.DB)
}

func TestPerUserDB_MemoryOperations(t *testing.T) {
	mgr, repoPath, _, cleanup := setupPerUserTestContext(t)
	defer cleanup()

	userDB, err := mgr.GetUserDB(repoPath)
	require.NoError(t, err)

	// Create a memory
	mem := &database.UserMemory{
		Slug:        "test-memory",
		Title:       "Test Memory",
		FilePath:    "2024/01/test-memory.md",
		ContentHash: "abc123",
		Version:     1,
	}
	require.NoError(t, userDB.Create(mem).Error)

	// Retrieve it
	var retrieved database.UserMemory
	err = userDB.Where("slug = ?", "test-memory").First(&retrieved).Error
	require.NoError(t, err)

	assert.Equal(t, "Test Memory", retrieved.Title)
	assert.Equal(t, "abc123", retrieved.ContentHash)
	assert.Equal(t, int64(1), retrieved.Version)
}

func TestPerUserDB_Associations(t *testing.T) {
	mgr, repoPath, _, cleanup := setupPerUserTestContext(t)
	defer cleanup()

	userDB, err := mgr.GetUserDB(repoPath)
	require.NoError(t, err)

	// Create two memories
	mem1 := &database.UserMemory{Slug: "memory-1", Title: "Memory 1", FilePath: "1.md"}
	mem2 := &database.UserMemory{Slug: "memory-2", Title: "Memory 2", FilePath: "2.md"}
	require.NoError(t, userDB.Create(mem1).Error)
	require.NoError(t, userDB.Create(mem2).Error)

	// Create an association
	assoc := &database.UserMemoryAssociation{
		SourceSlug:      "memory-1",
		TargetSlug:      "memory-2",
		AssociationType: "related_to",
		Strength:        0.8,
	}
	require.NoError(t, userDB.Create(assoc).Error)

	// Query associations
	var associations []database.UserMemoryAssociation
	err = userDB.Where("source_slug = ?", "memory-1").Find(&associations).Error
	require.NoError(t, err)

	assert.Len(t, associations, 1)
	assert.Equal(t, "memory-2", associations[0].TargetSlug)
	assert.Equal(t, "related_to", associations[0].AssociationType)
}

func TestPerUserDB_Tags(t *testing.T) {
	mgr, repoPath, _, cleanup := setupPerUserTestContext(t)
	defer cleanup()

	userDB, err := mgr.GetUserDB(repoPath)
	require.NoError(t, err)

	// Create a memory
	mem := &database.UserMemory{Slug: "tagged-memory", Title: "Tagged Memory", FilePath: "1.md"}
	require.NoError(t, userDB.Create(mem).Error)

	// Create tags
	tag1 := &database.UserTag{Name: "golang"}
	tag2 := &database.UserTag{Name: "backend"}
	require.NoError(t, userDB.Create(tag1).Error)
	require.NoError(t, userDB.Create(tag2).Error)

	// Create memory-tag links
	memTag1 := &database.UserMemoryTag{MemorySlug: "tagged-memory", TagName: "golang"}
	memTag2 := &database.UserMemoryTag{MemorySlug: "tagged-memory", TagName: "backend"}
	require.NoError(t, userDB.Create(memTag1).Error)
	require.NoError(t, userDB.Create(memTag2).Error)

	// Query memory tags
	var memTags []database.UserMemoryTag
	err = userDB.Where("memory_slug = ?", "tagged-memory").Find(&memTags).Error
	require.NoError(t, err)

	assert.Len(t, memTags, 2)
}

func TestPerUserDB_SoftDelete(t *testing.T) {
	mgr, repoPath, _, cleanup := setupPerUserTestContext(t)
	defer cleanup()

	userDB, err := mgr.GetUserDB(repoPath)
	require.NoError(t, err)

	// Create a memory
	mem := &database.UserMemory{Slug: "to-delete", Title: "To Delete", FilePath: "1.md"}
	require.NoError(t, userDB.Create(mem).Error)

	// Soft delete it
	err = userDB.Delete(mem).Error
	require.NoError(t, err)

	// Should not find with normal query
	var found database.UserMemory
	err = userDB.Where("slug = ?", "to-delete").First(&found).Error
	assert.Error(t, err)

	// Should find with Unscoped
	err = userDB.Unscoped().Where("slug = ?", "to-delete").First(&found).Error
	assert.NoError(t, err)
	assert.NotNil(t, found.DeletedAt.Time)
}

func TestPerUserDB_Version(t *testing.T) {
	mgr, repoPath, _, cleanup := setupPerUserTestContext(t)
	defer cleanup()

	userDB, err := mgr.GetUserDB(repoPath)
	require.NoError(t, err)

	// Create a memory with version
	mem := &database.UserMemory{
		Slug:     "versioned",
		Title:    "Versioned Memory",
		FilePath: "1.md",
		Version:  1,
	}
	require.NoError(t, userDB.Create(mem).Error)

	// Update version
	err = userDB.Model(mem).Update("version", 2).Error
	require.NoError(t, err)

	// Verify version updated
	var updated database.UserMemory
	userDB.Where("slug = ?", "versioned").First(&updated)
	assert.Equal(t, int64(2), updated.Version)
}

func TestPerUserDB_ContentHash(t *testing.T) {
	mgr, repoPath, _, cleanup := setupPerUserTestContext(t)
	defer cleanup()

	userDB, err := mgr.GetUserDB(repoPath)
	require.NoError(t, err)

	// Create a memory with content hash
	mem := &database.UserMemory{
		Slug:        "hashed",
		Title:       "Hashed Memory",
		FilePath:    "1.md",
		ContentHash: "initial-hash-123",
	}
	require.NoError(t, userDB.Create(mem).Error)

	// Update content hash
	newHash := "updated-hash-456"
	err = userDB.Model(mem).Update("content_hash", newHash).Error
	require.NoError(t, err)

	// Verify hash updated
	var updated database.UserMemory
	userDB.Where("slug = ?", "hashed").First(&updated)
	assert.Equal(t, newHash, updated.ContentHash)
}

func TestPerUserDB_Annotations(t *testing.T) {
	mgr, repoPath, _, cleanup := setupPerUserTestContext(t)
	defer cleanup()

	userDB, err := mgr.GetUserDB(repoPath)
	require.NoError(t, err)

	// Create a memory
	mem := &database.UserMemory{Slug: "annotated", Title: "Annotated Memory", FilePath: "1.md"}
	require.NoError(t, userDB.Create(mem).Error)

	// Create annotation
	annotation := &database.UserAnnotation{
		MemorySlug: "annotated",
		Type:       database.AnnotationTypeCorrection,
		Content:    "This is a correction note",
		CreatedBy:  "testuser",
		CreatedAt:  time.Now(),
	}
	require.NoError(t, userDB.Create(annotation).Error)

	// Query annotations
	var annotations []database.UserAnnotation
	err = userDB.Where("memory_slug = ?", "annotated").Find(&annotations).Error
	require.NoError(t, err)

	assert.Len(t, annotations, 1)
	assert.Equal(t, database.AnnotationTypeCorrection, annotations[0].Type)
	assert.Equal(t, "This is a correction note", annotations[0].Content)
}

func TestPerUserDB_SupersededBy(t *testing.T) {
	mgr, repoPath, _, cleanup := setupPerUserTestContext(t)
	defer cleanup()

	userDB, err := mgr.GetUserDB(repoPath)
	require.NoError(t, err)

	// Create old memory
	supersededBySlug := "new-memory"
	oldMem := &database.UserMemory{
		Slug:         "old-memory",
		Title:        "Old Memory",
		FilePath:     "1.md",
		SupersededBy: &supersededBySlug,
	}
	require.NoError(t, userDB.Create(oldMem).Error)

	// Create new memory
	newMem := &database.UserMemory{
		Slug:     "new-memory",
		Title:    "New Memory",
		FilePath: "2.md",
	}
	require.NoError(t, userDB.Create(newMem).Error)

	// Query for non-superseded memories
	var activeMemories []database.UserMemory
	err = userDB.Where("superseded_by IS NULL").Find(&activeMemories).Error
	require.NoError(t, err)

	assert.Len(t, activeMemories, 1)
	assert.Equal(t, "new-memory", activeMemories[0].Slug)

	// Query including superseded
	var allMemories []database.UserMemory
	err = userDB.Find(&allMemories).Error
	require.NoError(t, err)

	assert.Len(t, allMemories, 2)
}
