// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package integration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tejzpr/medha-mcp/internal/database"
	"github.com/tejzpr/medha-mcp/internal/git"
)

// Note: These tests require a local git setup without remote
// Remote tests would require PAT and are skipped in CI

func TestSync_PerUserDB_Creation(t *testing.T) {
	tmpDir := t.TempDir()

	// Initialize a git repo
	repo, err := git.InitRepository(tmpDir)
	if err != nil {
		t.Skipf("Skipping test: git init failed (likely sandboxed): %v", err)
	}

	// Create the .medha directory and database
	medhaDir := filepath.Join(tmpDir, ".medha")
	require.NoError(t, os.MkdirAll(medhaDir, 0755))

	userDB, err := database.OpenUserDB(tmpDir)
	require.NoError(t, err)

	// Create a memory
	mem := &database.UserMemory{
		Slug:     "test-memory",
		Title:    "Test Memory",
		FilePath: "test.md",
	}
	require.NoError(t, userDB.Create(mem).Error)

	// Close DB
	sqlDB, _ := userDB.DB()
	sqlDB.Close()

	// Verify .medha/medha.db exists
	dbPath := database.GetUserDBPath(tmpDir)
	_, err = os.Stat(dbPath)
	assert.NoError(t, err)

	// Verify repo can stage the DB
	assert.True(t, repo.HasPerUserDB())
}

func TestSync_PerUserDB_DataPersistence(t *testing.T) {
	tmpDir := t.TempDir()

	// Initialize git repo
	_, err := git.InitRepository(tmpDir)
	if err != nil {
		t.Skipf("Skipping test: git init failed (likely sandboxed): %v", err)
	}

	// First session: create and write data
	userDB1, err := database.OpenUserDB(tmpDir)
	require.NoError(t, err)

	mem := &database.UserMemory{
		Slug:     "persistent",
		Title:    "Persistent Memory",
		FilePath: "persistent.md",
		Version:  1,
	}
	require.NoError(t, userDB1.Create(mem).Error)

	sqlDB1, _ := userDB1.DB()
	sqlDB1.Close()

	// Second session: verify data persists
	userDB2, err := database.OpenUserDB(tmpDir)
	require.NoError(t, err)
	defer func() {
		sqlDB2, _ := userDB2.DB()
		sqlDB2.Close()
	}()

	var retrieved database.UserMemory
	err = userDB2.Where("slug = ?", "persistent").First(&retrieved).Error
	require.NoError(t, err)

	assert.Equal(t, "Persistent Memory", retrieved.Title)
	assert.Equal(t, int64(1), retrieved.Version)
}

func TestSync_SyncV2Options_Hooks(t *testing.T) {
	tmpDir := t.TempDir()

	repo, err := git.InitRepository(tmpDir)
	if err != nil {
		t.Skipf("Skipping test: git init failed (likely sandboxed): %v", err)
	}

	// Track hook calls
	beforeCalled := false
	afterCalled := false

	opts := git.SyncV2Options{
		PAT:               "",
		ForceLastWriteWins: true,
		IncludePerUserDB:  true,
		OnBeforeSync: func() error {
			beforeCalled = true
			return nil
		},
		OnAfterSync: func() error {
			afterCalled = true
			return nil
		},
	}

	// Sync (will skip due to no remote, but hooks should be called)
	status, err := repo.SyncV2(opts)
	assert.NoError(t, err)

	// Should succeed (no remote = skip)
	assert.True(t, status.SyncSuccessful)
	assert.Contains(t, status.Error, "No remote configured")

	// Hooks should NOT be called when no remote
	// (hooks are for actual sync operations)
	assert.False(t, beforeCalled)
	assert.False(t, afterCalled)
}

func TestSync_StagePerUserDB(t *testing.T) {
	tmpDir := t.TempDir()

	repo, err := git.InitRepository(tmpDir)
	if err != nil {
		t.Skipf("Skipping test: git init failed (likely sandboxed): %v", err)
	}

	// Create the database
	_, err = database.OpenUserDB(tmpDir)
	require.NoError(t, err)

	// Should have the DB
	assert.True(t, repo.HasPerUserDB())
}

func TestSync_HasPerUserDB_NoDatabase(t *testing.T) {
	tmpDir := t.TempDir()

	repo, err := git.InitRepository(tmpDir)
	if err != nil {
		t.Skipf("Skipping test: git init failed (likely sandboxed): %v", err)
	}

	// Should not have DB yet
	assert.False(t, repo.HasPerUserDB())
}

func TestSync_DBClosedDuringOperation(t *testing.T) {
	tmpDir := t.TempDir()

	// Create database and write data
	userDB, err := database.OpenUserDB(tmpDir)
	require.NoError(t, err)

	mem := &database.UserMemory{
		Slug:  "test",
		Title: "Test",
	}
	require.NoError(t, userDB.Create(mem).Error)

	// Close the database
	sqlDB, _ := userDB.DB()
	err = sqlDB.Close()
	assert.NoError(t, err)

	// Verify DB file exists
	dbPath := database.GetUserDBPath(tmpDir)
	_, err = os.Stat(dbPath)
	assert.NoError(t, err)

	// Reopen database
	userDB2, err := database.OpenUserDB(tmpDir)
	require.NoError(t, err)
	defer func() {
		sqlDB2, _ := userDB2.DB()
		sqlDB2.Close()
	}()

	// Data should still be there
	var retrieved database.UserMemory
	err = userDB2.Where("slug = ?", "test").First(&retrieved).Error
	assert.NoError(t, err)
}

func TestSync_JournalModeDelete(t *testing.T) {
	tmpDir := t.TempDir()

	userDB, err := database.OpenUserDB(tmpDir)
	require.NoError(t, err)
	defer func() {
		sqlDB, _ := userDB.DB()
		sqlDB.Close()
	}()

	// Verify journal mode is DELETE (not WAL)
	mode, err := database.GetJournalMode(userDB)
	require.NoError(t, err)
	assert.Equal(t, "delete", mode)

	// Verify no -wal or -shm files exist
	dbDir := filepath.Dir(database.GetUserDBPath(tmpDir))
	entries, _ := os.ReadDir(dbDir)
	for _, entry := range entries {
		assert.NotContains(t, entry.Name(), "-wal")
		assert.NotContains(t, entry.Name(), "-shm")
	}
}
