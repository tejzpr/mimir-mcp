// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package database

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm/logger"
)

func TestDatabaseManager_OpenSystemDB_SQLite(t *testing.T) {
	// Create temp directory for test
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "system.db")

	cfg := &Config{
		Type:       "sqlite",
		SQLitePath: dbPath,
		LogLevel:   logger.Silent,
	}

	mgr, err := NewManager(cfg)
	require.NoError(t, err)
	defer mgr.Close()

	// Verify system DB is connected
	assert.NotNil(t, mgr.SystemDB())

	// Verify DB file was created
	_, err = os.Stat(dbPath)
	assert.NoError(t, err)

	// Verify can ping
	err = Ping(mgr.SystemDB())
	assert.NoError(t, err)
}

func TestDatabaseManager_OpenUserDB(t *testing.T) {
	// Create temp directories
	tmpDir := t.TempDir()
	systemDBPath := filepath.Join(tmpDir, "system.db")
	repoPath := filepath.Join(tmpDir, "test-repo")
	require.NoError(t, os.MkdirAll(repoPath, 0755))

	cfg := &Config{
		Type:       "sqlite",
		SQLitePath: systemDBPath,
		LogLevel:   logger.Silent,
	}

	mgr, err := NewManager(cfg)
	require.NoError(t, err)
	defer mgr.Close()

	// Open user DB
	userDB, err := mgr.GetUserDB(repoPath)
	require.NoError(t, err)
	assert.NotNil(t, userDB)

	// Verify .medha directory was created
	medhaDir := filepath.Join(repoPath, ".medha")
	_, err = os.Stat(medhaDir)
	assert.NoError(t, err)

	// Verify user DB file was created
	userDBPath := filepath.Join(repoPath, ".medha", "medha.db")
	_, err = os.Stat(userDBPath)
	assert.NoError(t, err)
}

func TestDatabaseManager_UserDB_JournalMode(t *testing.T) {
	// Create temp directories
	tmpDir := t.TempDir()
	systemDBPath := filepath.Join(tmpDir, "system.db")
	repoPath := filepath.Join(tmpDir, "test-repo")
	require.NoError(t, os.MkdirAll(repoPath, 0755))

	cfg := &Config{
		Type:       "sqlite",
		SQLitePath: systemDBPath,
		LogLevel:   logger.Silent,
	}

	mgr, err := NewManager(cfg)
	require.NoError(t, err)
	defer mgr.Close()

	userDB, err := mgr.GetUserDB(repoPath)
	require.NoError(t, err)

	// Check journal mode is DELETE (not WAL)
	journalMode, err := GetJournalMode(userDB)
	require.NoError(t, err)
	assert.Equal(t, "delete", strings.ToLower(journalMode))
}

func TestDatabaseManager_MultipleUserDBs(t *testing.T) {
	// Create temp directories
	tmpDir := t.TempDir()
	systemDBPath := filepath.Join(tmpDir, "system.db")
	repo1 := filepath.Join(tmpDir, "user1-repo")
	repo2 := filepath.Join(tmpDir, "user2-repo")
	require.NoError(t, os.MkdirAll(repo1, 0755))
	require.NoError(t, os.MkdirAll(repo2, 0755))

	cfg := &Config{
		Type:       "sqlite",
		SQLitePath: systemDBPath,
		LogLevel:   logger.Silent,
	}

	mgr, err := NewManager(cfg)
	require.NoError(t, err)
	defer mgr.Close()

	// Open two user DBs
	db1, err := mgr.GetUserDB(repo1)
	require.NoError(t, err)

	db2, err := mgr.GetUserDB(repo2)
	require.NoError(t, err)

	// They should be different connections
	assert.NotEqual(t, db1, db2)

	// Both should be functional
	err = Ping(db1)
	assert.NoError(t, err)

	err = Ping(db2)
	assert.NoError(t, err)
}

func TestDatabaseManager_UserDB_Caching(t *testing.T) {
	// Create temp directories
	tmpDir := t.TempDir()
	systemDBPath := filepath.Join(tmpDir, "system.db")
	repoPath := filepath.Join(tmpDir, "test-repo")
	require.NoError(t, os.MkdirAll(repoPath, 0755))

	cfg := &Config{
		Type:       "sqlite",
		SQLitePath: systemDBPath,
		LogLevel:   logger.Silent,
	}

	mgr, err := NewManager(cfg)
	require.NoError(t, err)
	defer mgr.Close()

	// Open user DB twice
	db1, err := mgr.GetUserDB(repoPath)
	require.NoError(t, err)

	db2, err := mgr.GetUserDB(repoPath)
	require.NoError(t, err)

	// Should return the same cached connection
	assert.Equal(t, db1, db2)
}

func TestDatabaseManager_CloseUserDB(t *testing.T) {
	// Create temp directories
	tmpDir := t.TempDir()
	systemDBPath := filepath.Join(tmpDir, "system.db")
	repoPath := filepath.Join(tmpDir, "test-repo")
	require.NoError(t, os.MkdirAll(repoPath, 0755))

	cfg := &Config{
		Type:       "sqlite",
		SQLitePath: systemDBPath,
		LogLevel:   logger.Silent,
	}

	mgr, err := NewManager(cfg)
	require.NoError(t, err)
	defer mgr.Close()

	// Open user DB
	_, err = mgr.GetUserDB(repoPath)
	require.NoError(t, err)

	// Close user DB
	err = mgr.CloseUserDB(repoPath)
	assert.NoError(t, err)

	// Should be able to reopen
	db, err := mgr.GetUserDB(repoPath)
	require.NoError(t, err)
	assert.NotNil(t, db)
}

func TestDatabaseManager_ReopenUserDB(t *testing.T) {
	// Create temp directories
	tmpDir := t.TempDir()
	systemDBPath := filepath.Join(tmpDir, "system.db")
	repoPath := filepath.Join(tmpDir, "test-repo")
	require.NoError(t, os.MkdirAll(repoPath, 0755))

	cfg := &Config{
		Type:       "sqlite",
		SQLitePath: systemDBPath,
		LogLevel:   logger.Silent,
	}

	mgr, err := NewManager(cfg)
	require.NoError(t, err)
	defer mgr.Close()

	// Open user DB
	db1, err := mgr.GetUserDB(repoPath)
	require.NoError(t, err)

	// Reopen user DB
	db2, err := mgr.ReopenUserDB(repoPath)
	require.NoError(t, err)

	// Should be a different connection
	assert.NotEqual(t, db1, db2)
}

func TestUserDBPath(t *testing.T) {
	repoPath := "/home/user/.medha/store/medha-testuser"
	expected := "/home/user/.medha/store/medha-testuser/.medha/medha.db"

	actual := GetUserDBPath(repoPath)
	assert.Equal(t, expected, actual)
}

func TestUserDBExists(t *testing.T) {
	tmpDir := t.TempDir()

	// Should return false for non-existent DB
	repoPath := filepath.Join(tmpDir, "nonexistent")
	assert.False(t, UserDBExists(repoPath))

	// Create the DB
	dbPath := GetUserDBPath(tmpDir)
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0755))
	f, err := os.Create(dbPath)
	require.NoError(t, err)
	f.Close()

	// Should return true now
	assert.True(t, UserDBExists(tmpDir))
}

func TestMigrateUserDB(t *testing.T) {
	tmpDir := t.TempDir()
	repoPath := filepath.Join(tmpDir, "test-repo")
	require.NoError(t, os.MkdirAll(repoPath, 0755))

	// Open user DB (this triggers migration)
	userDB, err := OpenUserDB(repoPath)
	require.NoError(t, err)
	defer func() {
		sqlDB, _ := userDB.DB()
		sqlDB.Close()
	}()

	// Verify tables exist
	tables := []string{"memories", "associations", "tags", "memory_tags", "annotations"}
	for _, table := range tables {
		hasTable := userDB.Migrator().HasTable(table)
		assert.True(t, hasTable, "Table %s should exist", table)
	}
}

func TestMigrateSystemDB(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "system.db")

	cfg := &Config{
		Type:       "sqlite",
		SQLitePath: dbPath,
		LogLevel:   logger.Silent,
	}

	mgr, err := NewManager(cfg)
	require.NoError(t, err)
	defer mgr.Close()

	// Verify system tables exist
	tables := []string{"medha_users", "medha_auth_tokens", "medha_git_repos"}
	for _, table := range tables {
		hasTable := mgr.SystemDB().Migrator().HasTable(table)
		assert.True(t, hasTable, "Table %s should exist", table)
	}
}
