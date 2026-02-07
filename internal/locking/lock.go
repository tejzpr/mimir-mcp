// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package locking

import (
	"time"

	"gorm.io/gorm"
)

// MemoryLock represents an optimistic lock on a memory
type MemoryLock struct {
	Slug      string    `gorm:"primaryKey" json:"slug"`
	Version   int64     `gorm:"not null;default:1" json:"version"`
	LockedBy  string    `gorm:"not null" json:"locked_by"`
	LockedAt  time.Time `gorm:"not null" json:"locked_at"`
	ExpiresAt time.Time `gorm:"not null" json:"expires_at"`
}

// TableName specifies the table name for MemoryLock
func (MemoryLock) TableName() string {
	return "memory_locks"
}

// MigrateLocks runs migrations for the memory_locks table
func MigrateLocks(db *gorm.DB) error {
	return db.AutoMigrate(&MemoryLock{})
}

// IsExpired returns true if the lock has expired
func (l *MemoryLock) IsExpired() bool {
	return time.Now().After(l.ExpiresAt)
}

// ConflictError represents a version conflict during update
type ConflictError struct {
	Slug            string
	ExpectedVersion int64
	ActualVersion   int64
}

func (e *ConflictError) Error() string {
	return "version conflict: expected " + string(rune('0'+e.ExpectedVersion)) +
		", got " + string(rune('0'+e.ActualVersion))
}

// LockError represents a locking failure
type LockError struct {
	Slug     string
	LockedBy string
	Message  string
}

func (e *LockError) Error() string {
	return e.Message
}
