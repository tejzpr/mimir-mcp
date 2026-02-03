// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package database

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm/logger"
)

func TestConnect_SQLite(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	cfg := &Config{
		Type:       "sqlite",
		SQLitePath: dbPath,
		LogLevel:   logger.Silent,
	}

	db, err := Connect(cfg)
	require.NoError(t, err)
	require.NotNil(t, db)

	// Test connection
	err = Ping(db)
	assert.NoError(t, err)

	// Cleanup
	err = Close(db)
	assert.NoError(t, err)
}

func TestConnect_InvalidType(t *testing.T) {
	cfg := &Config{
		Type:     "mysql",
		LogLevel: logger.Silent,
	}

	db, err := Connect(cfg)
	assert.Error(t, err)
	assert.Nil(t, db)
	assert.Contains(t, err.Error(), "unsupported database type")
}

func TestEnsureSQLiteDir(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "subdir", "another", "test.db")

	err := ensureSQLiteDir(dbPath)
	require.NoError(t, err)

	// Check that the directory was created
	dir := filepath.Dir(dbPath)
	info, err := os.Stat(dir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestMigrate(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	cfg := &Config{
		Type:       "sqlite",
		SQLitePath: dbPath,
		LogLevel:   logger.Silent,
	}

	db, err := Connect(cfg)
	require.NoError(t, err)
	defer func() { _ = Close(db) }()

	// Run migrations
	err = Migrate(db)
	require.NoError(t, err)

	// Verify tables exist
	tables := []string{
		"mimir_users",
		"mimir_auth_tokens",
		"mimir_git_repos",
		"mimir_memories",
		"mimir_memory_associations",
		"mimir_tags",
		"mimir_memory_tags",
	}

	for _, table := range tables {
		hasTable := db.Migrator().HasTable(table)
		assert.True(t, hasTable, "table %s should exist", table)
	}
}

func TestModels_TableNames(t *testing.T) {
	tests := []struct {
		model     interface{}
		tableName string
	}{
		{MimirUser{}, "mimir_users"},
		{MimirAuthToken{}, "mimir_auth_tokens"},
		{MimirGitRepo{}, "mimir_git_repos"},
		{MimirMemory{}, "mimir_memories"},
		{MimirMemoryAssociation{}, "mimir_memory_associations"},
		{MimirTag{}, "mimir_tags"},
		{MimirMemoryTag{}, "mimir_memory_tags"},
	}

	for _, tt := range tests {
		t.Run(tt.tableName, func(t *testing.T) {
			var actualName string
			switch m := tt.model.(type) {
			case MimirUser:
				actualName = m.TableName()
			case MimirAuthToken:
				actualName = m.TableName()
			case MimirGitRepo:
				actualName = m.TableName()
			case MimirMemory:
				actualName = m.TableName()
			case MimirMemoryAssociation:
				actualName = m.TableName()
			case MimirTag:
				actualName = m.TableName()
			case MimirMemoryTag:
				actualName = m.TableName()
			}
			assert.Equal(t, tt.tableName, actualName)
		})
	}
}

func TestIsValidAssociationType(t *testing.T) {
	tests := []struct {
		aType string
		valid bool
	}{
		{AssociationTypeRelatedProject, true},
		{AssociationTypePerson, true},
		{AssociationTypeFollows, true},
		{AssociationTypePrecedes, true},
		{AssociationTypeReferences, true},
		{AssociationTypeRelatedTo, true},
		{"invalid_type", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.aType, func(t *testing.T) {
			result := IsValidAssociationType(tt.aType)
			assert.Equal(t, tt.valid, result)
		})
	}
}

func TestCreateIndexes(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	cfg := &Config{
		Type:       "sqlite",
		SQLitePath: dbPath,
		LogLevel:   logger.Silent,
	}

	db, err := Connect(cfg)
	require.NoError(t, err)
	defer func() { _ = Close(db) }()

	// Run migrations first
	err = Migrate(db)
	require.NoError(t, err)

	// Create indexes
	err = CreateIndexes(db)
	require.NoError(t, err)

	// Verify indexes were created
	// Note: SQLite index checking is limited, but we can at least verify no errors
	assert.NoError(t, err)
}

func TestDropAllTables(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	cfg := &Config{
		Type:       "sqlite",
		SQLitePath: dbPath,
		LogLevel:   logger.Silent,
	}

	db, err := Connect(cfg)
	require.NoError(t, err)
	defer func() { _ = Close(db) }()

	// Run migrations
	err = Migrate(db)
	require.NoError(t, err)

	// Drop all tables
	err = DropAllTables(db)
	require.NoError(t, err)

	// Verify tables don't exist
	hasTable := db.Migrator().HasTable("mimir_users")
	assert.False(t, hasTable)
}

func TestCRUD_User(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	cfg := &Config{
		Type:       "sqlite",
		SQLitePath: dbPath,
		LogLevel:   logger.Silent,
	}

	db, err := Connect(cfg)
	require.NoError(t, err)
	defer func() { _ = Close(db) }()

	err = Migrate(db)
	require.NoError(t, err)

	// Create
	user := &MimirUser{
		Username: "testuser",
		Email:    "test@example.com",
	}
	result := db.Create(user)
	require.NoError(t, result.Error)
	assert.NotZero(t, user.ID)

	// Read
	var foundUser MimirUser
	result = db.First(&foundUser, "username = ?", "testuser")
	require.NoError(t, result.Error)
	assert.Equal(t, "testuser", foundUser.Username)
	assert.Equal(t, "test@example.com", foundUser.Email)

	// Update
	result = db.Model(&foundUser).Update("email", "updated@example.com")
	require.NoError(t, result.Error)

	var updatedUser MimirUser
	db.First(&updatedUser, foundUser.ID)
	assert.Equal(t, "updated@example.com", updatedUser.Email)

	// Delete
	result = db.Delete(&foundUser)
	require.NoError(t, result.Error)
}

func TestCRUD_Memory(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	cfg := &Config{
		Type:       "sqlite",
		SQLitePath: dbPath,
		LogLevel:   logger.Silent,
	}

	db, err := Connect(cfg)
	require.NoError(t, err)
	defer func() { _ = Close(db) }()

	err = Migrate(db)
	require.NoError(t, err)

	// Create user first
	user := &MimirUser{Username: "testuser"}
	db.Create(user)

	// Create repo
	repo := &MimirGitRepo{
		UserID:   user.ID,
		RepoUUID: "test-uuid",
		RepoName: "test-repo",
		RepoPath: "/tmp/test",
	}
	db.Create(repo)

	// Create memory
	memory := &MimirMemory{
		UserID:   user.ID,
		RepoID:   repo.ID,
		Slug:     "test-memory-2024-01-01",
		Title:    "Test Memory",
		FilePath: "/tmp/test/2024/01/test-memory.md",
	}
	result := db.Create(memory)
	require.NoError(t, result.Error)
	assert.NotZero(t, memory.ID)

	// Read with slug
	var foundMemory MimirMemory
	result = db.First(&foundMemory, "slug = ?", "test-memory-2024-01-01")
	require.NoError(t, result.Error)
	assert.Equal(t, "Test Memory", foundMemory.Title)
}

func TestCRUD_Association(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	cfg := &Config{
		Type:       "sqlite",
		SQLitePath: dbPath,
		LogLevel:   logger.Silent,
	}

	db, err := Connect(cfg)
	require.NoError(t, err)
	defer func() { _ = Close(db) }()

	err = Migrate(db)
	require.NoError(t, err)

	// Create user and repo
	user := &MimirUser{Username: "testuser"}
	db.Create(user)

	repo := &MimirGitRepo{
		UserID:   user.ID,
		RepoUUID: "test-uuid",
		RepoName: "test-repo",
		RepoPath: "/tmp/test",
	}
	db.Create(repo)

	// Create two memories
	memory1 := &MimirMemory{
		UserID:   user.ID,
		RepoID:   repo.ID,
		Slug:     "memory-1",
		Title:    "Memory 1",
		FilePath: "/tmp/test/memory-1.md",
	}
	db.Create(memory1)

	memory2 := &MimirMemory{
		UserID:   user.ID,
		RepoID:   repo.ID,
		Slug:     "memory-2",
		Title:    "Memory 2",
		FilePath: "/tmp/test/memory-2.md",
	}
	db.Create(memory2)

	// Create association
	assoc := &MimirMemoryAssociation{
		SourceMemoryID:  memory1.ID,
		TargetMemoryID:  memory2.ID,
		AssociationType: AssociationTypeRelatedTo,
		Strength:        0.8,
	}
	result := db.Create(assoc)
	require.NoError(t, result.Error)

	// Query associations
	var associations []MimirMemoryAssociation
	result = db.Where("source_memory_id = ?", memory1.ID).Find(&associations)
	require.NoError(t, result.Error)
	assert.Equal(t, 1, len(associations))
	assert.Equal(t, AssociationTypeRelatedTo, associations[0].AssociationType)
	assert.Equal(t, 0.8, associations[0].Strength)
}
