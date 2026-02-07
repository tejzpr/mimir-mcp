// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package database

import (
	"gorm.io/gorm"
)

// SystemModels returns all models for the system database (auth, users, repos)
// The system database stores operational data that should NOT be synced to git
// These models are defined in models.go for backward compatibility
func SystemModels() []interface{} {
	return []interface{}{
		&MedhaUser{},
		&MedhaAuthToken{},
		&MedhaGitRepo{},
	}
}

// MigrateSystemDB runs migrations for the system database
func MigrateSystemDB(db *gorm.DB) error {
	return db.AutoMigrate(SystemModels()...)
}

// CreateSystemIndexes creates indexes for the system database
func CreateSystemIndexes(db *gorm.DB) error {
	indexes := []struct {
		table   string
		columns []string
		name    string
	}{
		{
			table:   "medha_auth_tokens",
			columns: []string{"user_id", "expires_at"},
			name:    "idx_tokens_user_expires",
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
