// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package locking

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupTestDB(t *testing.T) *gorm.DB {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)

	err = MigrateLocks(db)
	require.NoError(t, err)

	return db
}

func TestOptimisticLock_Acquire_Success(t *testing.T) {
	db := setupTestDB(t)
	locker := NewLocker(db)

	acquired, err := locker.Acquire("test-memory", "agent-1")

	require.NoError(t, err)
	assert.True(t, acquired)

	// Verify lock is stored
	isLocked, lockedBy, err := locker.IsLocked("test-memory")
	require.NoError(t, err)
	assert.True(t, isLocked)
	assert.Equal(t, "agent-1", lockedBy)
}

func TestOptimisticLock_Acquire_AlreadyLocked(t *testing.T) {
	db := setupTestDB(t)
	locker := NewLocker(db)

	// First agent acquires
	acquired1, err := locker.Acquire("test-memory", "agent-1")
	require.NoError(t, err)
	assert.True(t, acquired1)

	// Second agent tries to acquire
	acquired2, err := locker.Acquire("test-memory", "agent-2")
	require.NoError(t, err)
	assert.False(t, acquired2)
}

func TestOptimisticLock_Acquire_SameAgent(t *testing.T) {
	db := setupTestDB(t)
	locker := NewLocker(db)

	// Same agent acquires twice
	acquired1, err := locker.Acquire("test-memory", "agent-1")
	require.NoError(t, err)
	assert.True(t, acquired1)

	acquired2, err := locker.Acquire("test-memory", "agent-1")
	require.NoError(t, err)
	assert.True(t, acquired2) // Same agent can reacquire
}

func TestOptimisticLock_Acquire_Expired(t *testing.T) {
	db := setupTestDB(t)
	locker := NewLocker(db).WithTTL(100 * time.Millisecond)

	// First agent acquires
	acquired1, err := locker.Acquire("test-memory", "agent-1")
	require.NoError(t, err)
	assert.True(t, acquired1)

	// Wait for lock to expire
	time.Sleep(150 * time.Millisecond)

	// Second agent should be able to acquire
	acquired2, err := locker.Acquire("test-memory", "agent-2")
	require.NoError(t, err)
	assert.True(t, acquired2)
}

func TestOptimisticLock_Release(t *testing.T) {
	db := setupTestDB(t)
	locker := NewLocker(db)

	// Acquire
	_, _ = locker.Acquire("test-memory", "agent-1")

	// Release
	err := locker.Release("test-memory", "agent-1")
	require.NoError(t, err)

	// Should be able to acquire again
	isLocked, _, _ := locker.IsLocked("test-memory")
	assert.False(t, isLocked)
}

func TestOptimisticLock_Extend(t *testing.T) {
	db := setupTestDB(t)
	locker := NewLocker(db).WithTTL(100 * time.Millisecond)

	// Acquire
	_, _ = locker.Acquire("test-memory", "agent-1")

	// Wait a bit
	time.Sleep(50 * time.Millisecond)

	// Extend
	err := locker.Extend("test-memory", "agent-1")
	require.NoError(t, err)

	// Wait for original TTL to expire
	time.Sleep(60 * time.Millisecond)

	// Lock should still be held
	isLocked, _, _ := locker.IsLocked("test-memory")
	assert.True(t, isLocked)
}

func TestOptimisticLock_VersionIncrement(t *testing.T) {
	db := setupTestDB(t)

	// Create a test table for version testing
	type VersionedRecord struct {
		Slug    string `gorm:"primaryKey"`
		Data    string
		Version int64 `gorm:"default:1"`
	}

	err := db.AutoMigrate(&VersionedRecord{})
	require.NoError(t, err)

	// Create initial record
	record := VersionedRecord{Slug: "test", Data: "initial", Version: 1}
	require.NoError(t, db.Create(&record).Error)

	// Update with correct version
	err = UpdateWithVersion(db, "versioned_records", "test", 1, map[string]interface{}{
		"data": "updated",
	})
	assert.NoError(t, err)

	// Verify version incremented
	var updated VersionedRecord
	db.Where("slug = ?", "test").First(&updated)
	assert.Equal(t, int64(2), updated.Version)
	assert.Equal(t, "updated", updated.Data)
}

func TestOptimisticLock_VersionConflict(t *testing.T) {
	db := setupTestDB(t)

	type VersionedRecord struct {
		Slug    string `gorm:"primaryKey"`
		Data    string
		Version int64 `gorm:"default:1"`
	}

	err := db.AutoMigrate(&VersionedRecord{})
	require.NoError(t, err)

	// Create initial record
	record := VersionedRecord{Slug: "test", Data: "initial", Version: 1}
	require.NoError(t, db.Create(&record).Error)

	// Update with wrong version
	err = UpdateWithVersion(db, "versioned_records", "test", 99, map[string]interface{}{
		"data": "updated",
	})

	assert.Error(t, err)
	_, isConflict := err.(*ConflictError)
	assert.True(t, isConflict)
}

func TestOptimisticLock_WithLock(t *testing.T) {
	db := setupTestDB(t)
	locker := NewLocker(db)

	executed := false

	err := locker.WithLock("test-memory", "agent-1", func() error {
		executed = true
		// Verify lock is held during execution
		isLocked, lockedBy, _ := locker.IsLocked("test-memory")
		assert.True(t, isLocked)
		assert.Equal(t, "agent-1", lockedBy)
		return nil
	})

	require.NoError(t, err)
	assert.True(t, executed)

	// Lock should be released after
	isLocked, _, _ := locker.IsLocked("test-memory")
	assert.False(t, isLocked)
}

func TestOptimisticLock_CleanupExpired(t *testing.T) {
	db := setupTestDB(t)
	locker := NewLocker(db).WithTTL(50 * time.Millisecond)

	// Create multiple locks
	_, _ = locker.Acquire("mem1", "agent-1")
	_, _ = locker.Acquire("mem2", "agent-1")
	_, _ = locker.Acquire("mem3", "agent-1")

	// Wait for expiry
	time.Sleep(100 * time.Millisecond)

	// Cleanup
	count, err := locker.CleanupExpired()
	require.NoError(t, err)
	assert.Equal(t, int64(3), count)
}

func TestRetryWithBackoff(t *testing.T) {
	attempts := 0

	err := RetryWithBackoff(3, 10*time.Millisecond, func() error {
		attempts++
		if attempts < 3 {
			return &ConflictError{Slug: "test"}
		}
		return nil
	})

	assert.NoError(t, err)
	assert.Equal(t, 3, attempts)
}

func TestRetryWithBackoff_MaxRetries(t *testing.T) {
	attempts := 0

	err := RetryWithBackoff(3, 1*time.Millisecond, func() error {
		attempts++
		return &ConflictError{Slug: "test"}
	})

	assert.Error(t, err)
	assert.Equal(t, 3, attempts)
}

func TestConcurrentLocking(t *testing.T) {
	db := setupTestDB(t)
	locker := NewLocker(db)

	const numAgents = 10
	results := make([]bool, numAgents)
	var wg sync.WaitGroup

	// Multiple agents try to acquire the same lock simultaneously
	for i := 0; i < numAgents; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			acquired, _ := locker.Acquire("contested-memory", "agent-"+string(rune('0'+idx)))
			results[idx] = acquired
		}(i)
	}

	wg.Wait()

	// Only one should have succeeded
	successCount := 0
	for _, r := range results {
		if r {
			successCount++
		}
	}
	assert.Equal(t, 1, successCount)
}

func TestMemoryLock_IsExpired(t *testing.T) {
	// Not expired
	lock := MemoryLock{
		ExpiresAt: time.Now().Add(time.Hour),
	}
	assert.False(t, lock.IsExpired())

	// Expired
	lock.ExpiresAt = time.Now().Add(-time.Hour)
	assert.True(t, lock.IsExpired())
}

func TestLocker_ReleaseAll(t *testing.T) {
	db := setupTestDB(t)
	locker := NewLocker(db)

	// Agent-1 acquires multiple locks
	_, _ = locker.Acquire("mem1", "agent-1")
	_, _ = locker.Acquire("mem2", "agent-1")
	_, _ = locker.Acquire("mem3", "agent-2") // Different agent

	// Release all for agent-1
	err := locker.ReleaseAll("agent-1")
	require.NoError(t, err)

	// Agent-1's locks should be gone
	isLocked1, _, _ := locker.IsLocked("mem1")
	isLocked2, _, _ := locker.IsLocked("mem2")
	assert.False(t, isLocked1)
	assert.False(t, isLocked2)

	// Agent-2's lock should remain
	isLocked3, lockedBy, _ := locker.IsLocked("mem3")
	assert.True(t, isLocked3)
	assert.Equal(t, "agent-2", lockedBy)
}
