// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package database

import (
	"fmt"

	"gorm.io/gorm"
)

// AllModels returns all database models for migration (v1 backward compatibility)
// In v2, use SystemModels() and UserModels() separately
func AllModels() []interface{} {
	return []interface{}{
		&MedhaUser{},
		&MedhaAuthToken{},
		&MedhaGitRepo{},
		&MedhaMemory{},
		&MedhaMemoryAssociation{},
		&MedhaTag{},
		&MedhaMemoryTag{},
		&MedhaAnnotation{},
	}
}

// Migrate runs database migrations for all models (v1 backward compatibility)
// In v2, the system DB uses MigrateSystemDB and per-user DBs use MigrateUserDB
func Migrate(db *gorm.DB) error {
	if err := db.AutoMigrate(AllModels()...); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}
	return nil
}

// DropAllTables drops all tables (use with caution!)
func DropAllTables(db *gorm.DB) error {
	// Drop in reverse order to avoid foreign key constraints
	models := []interface{}{
		&MedhaAnnotation{},
		&MedhaMemoryTag{},
		&MedhaTag{},
		&MedhaMemoryAssociation{},
		&MedhaMemory{},
		&MedhaGitRepo{},
		&MedhaAuthToken{},
		&MedhaUser{},
	}

	for _, model := range models {
		if err := db.Migrator().DropTable(model); err != nil {
			return fmt.Errorf("failed to drop table: %w", err)
		}
	}

	return nil
}

// CreateIndexes creates additional indexes for better query performance
func CreateIndexes(db *gorm.DB) error {
	// Add composite indexes for frequently queried combinations
	indexes := []struct {
		table   string
		columns []string
		name    string
	}{
		{
			table:   "medha_memories",
			columns: []string{"user_id", "created_at"},
			name:    "idx_memories_user_created",
		},
		{
			table:   "medha_memories",
			columns: []string{"user_id", "updated_at"},
			name:    "idx_memories_user_updated",
		},
		{
			table:   "medha_memory_associations",
			columns: []string{"source_memory_id", "association_type"},
			name:    "idx_associations_source_type",
		},
		{
			table:   "medha_memory_associations",
			columns: []string{"target_memory_id", "association_type"},
			name:    "idx_associations_target_type",
		},
		{
			table:   "medha_auth_tokens",
			columns: []string{"user_id", "expires_at"},
			name:    "idx_tokens_user_expires",
		},
		// Human-aligned memory indexes
		{
			table:   "medha_memories",
			columns: []string{"user_id", "last_accessed_at"},
			name:    "idx_memories_user_accessed",
		},
		{
			table:   "medha_memories",
			columns: []string{"user_id", "access_count"},
			name:    "idx_memories_user_access_count",
		},
		{
			table:   "medha_memories",
			columns: []string{"user_id", "superseded_by"},
			name:    "idx_memories_user_superseded",
		},
		{
			table:   "medha_annotations",
			columns: []string{"memory_id", "type"},
			name:    "idx_annotations_memory_type",
		},
	}

	for _, idx := range indexes {
		// Check if index exists
		hasIndex := db.Migrator().HasIndex(idx.table, idx.name)
		if !hasIndex {
			// Create the index using raw SQL (GORM doesn't support composite indexes well)
			sql := fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s (%s)",
				idx.name,
				idx.table,
				joinColumns(idx.columns))

			if err := db.Exec(sql).Error; err != nil {
				return fmt.Errorf("failed to create index %s: %w", idx.name, err)
			}
		}
	}

	return nil
}

// joinColumns joins column names with commas
func joinColumns(columns []string) string {
	result := ""
	for i, col := range columns {
		if i > 0 {
			result += ", "
		}
		result += col
	}
	return result
}
