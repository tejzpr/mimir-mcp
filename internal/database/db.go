// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3" // CGO-based SQLite driver for sqlite-vec support
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func init() {
	// Register sqlite-vec extension with mattn/go-sqlite3
	sqlite_vec.Auto()
}

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
		// Use CGO-based sqlite driver for sqlite-vec support
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

// ConnectSQLiteWithVec opens a SQLite database with sqlite-vec extension enabled
// This is the preferred method for opening databases that need vector search
func ConnectSQLiteWithVec(dbPath string, logLevel logger.LogLevel) (*gorm.DB, error) {
	if err := ensureSQLiteDir(dbPath); err != nil {
		return nil, fmt.Errorf("failed to ensure sqlite directory: %w", err)
	}

	gormConfig := &gorm.Config{
		Logger: logger.Default.LogMode(logLevel),
	}

	db, err := gorm.Open(sqlite.Open(dbPath), gormConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to sqlite: %w", err)
	}

	// Verify sqlite-vec is available
	var version string
	if err := db.Raw("SELECT vec_version()").Scan(&version).Error; err != nil {
		return nil, fmt.Errorf("sqlite-vec extension not available: %w", err)
	}

	return db, nil
}

// GetSQLiteVecVersion returns the version of the sqlite-vec extension
func GetSQLiteVecVersion(db *gorm.DB) (string, error) {
	var version string
	err := db.Raw("SELECT vec_version()").Scan(&version).Error
	return version, err
}

// IsVecAvailable checks if sqlite-vec extension is available
func IsVecAvailable(db *gorm.DB) bool {
	var version string
	err := db.Raw("SELECT vec_version()").Scan(&version).Error
	return err == nil && version != ""
}

// ensureSQLiteDir creates the directory for the SQLite database if it doesn't exist
func ensureSQLiteDir(dbPath string) error {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create sqlite directory: %w", err)
	}
	return nil
}

// withSqlDB is a helper that gets the underlying sql.DB and executes an operation on it
func withSqlDB(db *gorm.DB, operation func(*sql.DB) error) error {
	sqlDB, err := GetSqlDB(db)
	if err != nil {
		return fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}
	return operation(sqlDB)
}

// Close closes the database connection
func Close(db *gorm.DB) error {
	return withSqlDB(db, func(sqlDB *sql.DB) error {
		return sqlDB.Close()
	})
}

// Ping checks if the database connection is alive
func Ping(db *gorm.DB) error {
	return withSqlDB(db, func(sqlDB *sql.DB) error {
		return sqlDB.Ping()
	})
}

// GetSqlDB returns the underlying sql.DB for raw operations
func GetSqlDB(db *gorm.DB) (*sql.DB, error) {
	return db.DB()
}
