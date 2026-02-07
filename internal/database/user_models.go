// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package database

import (
	"time"

	"gorm.io/gorm"
)

// UserModels returns all models for the per-user database (memories, associations, tags)
// The per-user database is stored inside the git repository and syncs with git
func UserModels() []interface{} {
	return []interface{}{
		&UserMemory{},
		&UserMemoryAssociation{},
		&UserTag{},
		&UserMemoryTag{},
		&UserAnnotation{},
	}
}

// UserMemory represents a memory stored in the per-user git repository database
// This is the v2 model that lives in .medha/medha.db inside each user's git repo
type UserMemory struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	Slug      string         `gorm:"uniqueIndex;not null" json:"slug"`
	Title     string         `gorm:"not null" json:"title"`
	FilePath  string         `gorm:"not null" json:"file_path"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`

	// Human-aligned memory features
	SupersededBy   *string   `gorm:"column:superseded_by;index" json:"superseded_by,omitempty"`
	LastAccessedAt time.Time `gorm:"column:last_accessed_at" json:"last_accessed_at"`
	AccessCount    int       `gorm:"column:access_count;default:0" json:"access_count"`

	// Content hash for cache invalidation and embedding staleness detection
	ContentHash string `gorm:"column:content_hash" json:"content_hash,omitempty"`

	// Version for optimistic locking
	Version int64 `gorm:"column:version;default:1" json:"version"`
}

// TableName specifies the table name for UserMemory
func (UserMemory) TableName() string {
	return "memories"
}

// UserMemoryAssociation represents associations between memories in the per-user database
type UserMemoryAssociation struct {
	ID               uint      `gorm:"primaryKey" json:"id"`
	SourceSlug       string    `gorm:"index;not null" json:"source_slug"`
	TargetSlug       string    `gorm:"index;not null" json:"target_slug"`
	AssociationType  string    `gorm:"column:relationship;not null;default:related" json:"relationship"`
	Strength         float64   `gorm:"default:0.5" json:"strength"`
	CreatedAt        time.Time `json:"created_at"`
}

// TableName specifies the table name for UserMemoryAssociation
func (UserMemoryAssociation) TableName() string {
	return "associations"
}

// UserTag represents a tag in the per-user database
type UserTag struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"uniqueIndex;not null" json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// TableName specifies the table name for UserTag
func (UserTag) TableName() string {
	return "tags"
}

// UserMemoryTag represents the many-to-many relationship between memories and tags
type UserMemoryTag struct {
	MemorySlug string `gorm:"primaryKey;column:memory_slug" json:"memory_slug"`
	TagName    string `gorm:"primaryKey;column:tag_name" json:"tag_name"`
}

// TableName specifies the table name for UserMemoryTag
func (UserMemoryTag) TableName() string {
	return "memory_tags"
}

// UserAnnotation represents a note or correction on a memory
type UserAnnotation struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	MemorySlug string    `gorm:"index;not null;column:memory_slug" json:"memory_slug"`
	Type       string    `gorm:"not null" json:"type"`
	Content    string    `gorm:"type:text;not null" json:"content"`
	CreatedAt  time.Time `json:"created_at"`
	CreatedBy  string    `json:"created_by,omitempty"`
}

// TableName specifies the table name for UserAnnotation
func (UserAnnotation) TableName() string {
	return "annotations"
}

// MigrateUserDB runs migrations for a per-user database
func MigrateUserDB(db *gorm.DB) error {
	return db.AutoMigrate(UserModels()...)
}

// CreateUserIndexes creates indexes for the per-user database
func CreateUserIndexes(db *gorm.DB) error {
	indexes := []struct {
		table   string
		columns []string
		name    string
	}{
		{
			table:   "memories",
			columns: []string{"title"},
			name:    "idx_memories_title",
		},
		{
			table:   "memories",
			columns: []string{"updated_at"},
			name:    "idx_memories_updated",
		},
		{
			table:   "memories",
			columns: []string{"last_accessed_at"},
			name:    "idx_memories_accessed",
		},
		{
			table:   "memories",
			columns: []string{"superseded_by"},
			name:    "idx_memories_superseded",
		},
		{
			table:   "associations",
			columns: []string{"source_slug"},
			name:    "idx_assoc_source",
		},
		{
			table:   "associations",
			columns: []string{"target_slug"},
			name:    "idx_assoc_target",
		},
		{
			table:   "associations",
			columns: []string{"source_slug", "relationship"},
			name:    "idx_assoc_source_type",
		},
		{
			table:   "annotations",
			columns: []string{"memory_slug", "type"},
			name:    "idx_annotations_memory_type",
		},
	}

	for _, idx := range indexes {
		hasIndex := db.Migrator().HasIndex(idx.table, idx.name)
		if !hasIndex {
			sql := "CREATE INDEX IF NOT EXISTS " + idx.name + " ON " + idx.table + " (" + joinColumns(idx.columns) + ")"
			if err := db.Exec(sql).Error; err != nil {
				return err
			}
		}
	}

	return nil
}

// Note: AnnotationType and AssociationType constants are defined in models.go
// for backward compatibility. Use those constants with the UserMemory types as well.
