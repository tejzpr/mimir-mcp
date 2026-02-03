// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package database

import (
	"fmt"
	"os"
	"path/filepath"

	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Config holds database configuration
type Config struct {
	Type        string // "sqlite" or "postgres"
	SQLitePath  string
	PostgresDSN string
	LogLevel    logger.LogLevel
}

// Connect establishes a database connection based on the configuration
func Connect(cfg *Config) (*gorm.DB, error) {
	var db *gorm.DB
	var err error

	gormConfig := &gorm.Config{
		Logger: logger.Default.LogMode(cfg.LogLevel),
	}

	switch cfg.Type {
	case "sqlite":
		if err := ensureSQLiteDir(cfg.SQLitePath); err != nil {
			return nil, fmt.Errorf("failed to ensure sqlite directory: %w", err)
		}
		db, err = gorm.Open(sqlite.Open(cfg.SQLitePath), gormConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to sqlite: %w", err)
		}

	case "postgres":
		db, err = gorm.Open(postgres.Open(cfg.PostgresDSN), gormConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to postgres: %w", err)
		}

	default:
		return nil, fmt.Errorf("unsupported database type: %s", cfg.Type)
	}

	return db, nil
}

// ensureSQLiteDir creates the directory for the SQLite database if it doesn't exist
func ensureSQLiteDir(dbPath string) error {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create sqlite directory: %w", err)
	}
	return nil
}

// Close closes the database connection
func Close(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}
	return sqlDB.Close()
}

// Ping checks if the database connection is alive
func Ping(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}
	return sqlDB.Ping()
}
