// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package locking

import (
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// DefaultLockTTL is the default time-to-live for locks
const DefaultLockTTL = 5 * time.Minute

// MaxRetries is the default number of retries for optimistic locking
const MaxRetries = 3

// RetryDelay is the delay between retries
const RetryDelay = 100 * time.Millisecond

// Locker manages optimistic locking for memories
type Locker struct {
	db       *gorm.DB
	lockTTL  time.Duration
	retries  int
}

// NewLocker creates a new locker instance
func NewLocker(db *gorm.DB) *Locker {
	return &Locker{
		db:      db,
		lockTTL: DefaultLockTTL,
		retries: MaxRetries,
	}
}

// WithTTL sets a custom TTL for locks
func (l *Locker) WithTTL(ttl time.Duration) *Locker {
	l.lockTTL = ttl
	return l
}

// WithRetries sets a custom number of retries
func (l *Locker) WithRetries(retries int) *Locker {
	l.retries = retries
	return l
}

// Acquire attempts to acquire a lock for a memory
// Returns true if lock acquired, false if already locked by another agent
func (l *Locker) Acquire(slug, agentID string) (bool, error) {
	now := time.Now()
	expiresAt := now.Add(l.lockTTL)

	lock := MemoryLock{
		Slug:      slug,
		Version:   1,
		LockedBy:  agentID,
		LockedAt:  now,
		ExpiresAt: expiresAt,
	}

	// Try to insert or update if expired
	err := l.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "slug"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"locked_by":  clause.Column{Name: "CASE WHEN expires_at < ? THEN ? ELSE locked_by END", Table: now.Format(time.RFC3339), Raw: true},
			"locked_at":  clause.Column{Name: "CASE WHEN expires_at < ? THEN ? ELSE locked_at END", Table: now.Format(time.RFC3339), Raw: true},
			"expires_at": clause.Column{Name: "CASE WHEN expires_at < ? THEN ? ELSE expires_at END", Table: now.Format(time.RFC3339), Raw: true},
			"version":    gorm.Expr("version + 1"),
		}),
	}).Create(&lock).Error

	if err != nil {
		// Fallback: manual check and update
		return l.acquireFallback(slug, agentID, now, expiresAt)
	}

	// Verify the lock was acquired by us
	var current MemoryLock
	if err := l.db.Where("slug = ?", slug).First(&current).Error; err != nil {
		return false, err
	}

	return current.LockedBy == agentID, nil
}

// acquireFallback provides a fallback lock acquisition mechanism
func (l *Locker) acquireFallback(slug, agentID string, now, expiresAt time.Time) (bool, error) {
	var existing MemoryLock
	err := l.db.Where("slug = ?", slug).First(&existing).Error

	if err != nil {
		// No lock exists, create one
		lock := MemoryLock{
			Slug:      slug,
			Version:   1,
			LockedBy:  agentID,
			LockedAt:  now,
			ExpiresAt: expiresAt,
		}
		if err := l.db.Create(&lock).Error; err != nil {
			return false, err
		}
		return true, nil
	}

	// Lock exists - check if expired or owned by us
	if existing.IsExpired() || existing.LockedBy == agentID {
		// Take over the lock
		result := l.db.Model(&MemoryLock{}).
			Where("slug = ? AND version = ?", slug, existing.Version).
			Updates(map[string]interface{}{
				"locked_by":  agentID,
				"locked_at":  now,
				"expires_at": expiresAt,
				"version":    existing.Version + 1,
			})

		if result.Error != nil {
			return false, result.Error
		}
		return result.RowsAffected > 0, nil
	}

	// Locked by someone else and not expired
	return false, nil
}

// Release releases a lock held by the specified agent
func (l *Locker) Release(slug, agentID string) error {
	result := l.db.Where("slug = ? AND locked_by = ?", slug, agentID).
		Delete(&MemoryLock{})
	return result.Error
}

// ReleaseAll releases all locks held by an agent
func (l *Locker) ReleaseAll(agentID string) error {
	result := l.db.Where("locked_by = ?", agentID).Delete(&MemoryLock{})
	return result.Error
}

// IsLocked checks if a memory is currently locked
func (l *Locker) IsLocked(slug string) (bool, string, error) {
	var lock MemoryLock
	err := l.db.Where("slug = ?", slug).First(&lock).Error

	if err != nil {
		return false, "", nil // Not locked
	}

	if lock.IsExpired() {
		return false, "", nil // Expired
	}

	return true, lock.LockedBy, nil
}

// Extend extends the TTL of an existing lock
func (l *Locker) Extend(slug, agentID string) error {
	expiresAt := time.Now().Add(l.lockTTL)

	result := l.db.Model(&MemoryLock{}).
		Where("slug = ? AND locked_by = ?", slug, agentID).
		Update("expires_at", expiresAt)

	if result.RowsAffected == 0 {
		return &LockError{
			Slug:     slug,
			LockedBy: agentID,
			Message:  "lock not found or owned by different agent",
		}
	}

	return result.Error
}

// CleanupExpired removes all expired locks
func (l *Locker) CleanupExpired() (int64, error) {
	result := l.db.Where("expires_at < ?", time.Now()).Delete(&MemoryLock{})
	return result.RowsAffected, result.Error
}

// WithLock executes a function while holding a lock
// Automatically releases the lock after execution
func (l *Locker) WithLock(slug, agentID string, fn func() error) error {
	acquired, err := l.Acquire(slug, agentID)
	if err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}
	if !acquired {
		return &LockError{
			Slug:    slug,
			Message: "failed to acquire lock",
		}
	}

	defer l.Release(slug, agentID) //nolint:errcheck

	return fn()
}

// UpdateWithVersion performs an optimistic locking update
// Returns ConflictError if versions don't match
func UpdateWithVersion(db *gorm.DB, table string, slug string, currentVersion int64, updates map[string]interface{}) error {
	// Add version increment to updates
	updates["version"] = gorm.Expr("version + 1")

	result := db.Table(table).
		Where("slug = ? AND version = ?", slug, currentVersion).
		Updates(updates)

	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected == 0 {
		// Check if record exists with different version
		var count int64
		db.Table(table).Where("slug = ?", slug).Count(&count)
		if count > 0 {
			return &ConflictError{
				Slug:            slug,
				ExpectedVersion: currentVersion,
				ActualVersion:   -1, // Unknown
			}
		}
		return fmt.Errorf("record not found: %s", slug)
	}

	return nil
}

// UpdateWithVersionUnscoped performs an optimistic locking update including soft-deleted records
// This is useful for restore operations where we need to update records with deleted_at set
// Returns ConflictError if versions don't match
func UpdateWithVersionUnscoped(db *gorm.DB, table string, slug string, currentVersion int64, updates map[string]interface{}) error {
	// Add version increment to updates
	updates["version"] = gorm.Expr("version + 1")

	// Use Unscoped to include soft-deleted records
	result := db.Unscoped().Table(table).
		Where("slug = ? AND version = ?", slug, currentVersion).
		Updates(updates)

	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected == 0 {
		// Check if record exists with different version (also unscoped)
		var count int64
		db.Unscoped().Table(table).Where("slug = ?", slug).Count(&count)
		if count > 0 {
			return &ConflictError{
				Slug:            slug,
				ExpectedVersion: currentVersion,
				ActualVersion:   -1, // Unknown
			}
		}
		return fmt.Errorf("record not found: %s", slug)
	}

	return nil
}

// RetryWithBackoff retries a function with exponential backoff
func RetryWithBackoff(maxRetries int, initialDelay time.Duration, fn func() error) error {
	var lastErr error
	delay := initialDelay

	for i := 0; i < maxRetries; i++ {
		if err := fn(); err != nil {
			lastErr = err
			// Only retry on conflict errors
			if _, ok := err.(*ConflictError); !ok {
				return err
			}
			time.Sleep(delay)
			delay *= 2 // Exponential backoff
		} else {
			return nil
		}
	}

	return fmt.Errorf("max retries exceeded: %w", lastErr)
}
