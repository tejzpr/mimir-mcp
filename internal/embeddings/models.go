// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package embeddings

import (
	"encoding/binary"
	"fmt"
	"math"
	"time"

	"gorm.io/gorm"
)

// Default dimensions for text-embedding-3-small
const DefaultEmbeddingDimensions = 1536

// Embedding represents a stored embedding vector for a memory
// This is used as a metadata table that tracks embedding info
type Embedding struct {
	Slug         string    `gorm:"primaryKey" json:"slug"`
	ContentHash  string    `gorm:"not null" json:"content_hash"`
	ModelName    string    `gorm:"not null" json:"model_name"`
	ModelVersion string    `gorm:"not null" json:"model_version"`
	Dimensions   int       `gorm:"not null" json:"dimensions"`
	Vector       []byte    `gorm:"type:blob;not null" json:"-"` // Stored as binary for fallback
	CreatedAt    time.Time `gorm:"not null" json:"created_at"`
}

// TableName specifies the table name for Embedding
func (Embedding) TableName() string {
	return "embeddings"
}

// MigrateEmbeddings runs migrations for the embeddings table
func MigrateEmbeddings(db *gorm.DB) error {
	return db.AutoMigrate(&Embedding{})
}

// MigrateVecEmbeddings creates the sqlite-vec virtual table for vector search
// This requires sqlite-vec extension to be loaded
func MigrateVecEmbeddings(db *gorm.DB, dimensions int) error {
	if dimensions <= 0 {
		dimensions = DefaultEmbeddingDimensions
	}

	// Create the vec_embeddings virtual table using vec0 module
	// The vec0 module provides efficient KNN search
	sql := fmt.Sprintf(`
		CREATE VIRTUAL TABLE IF NOT EXISTS vec_embeddings USING vec0(
			slug TEXT PRIMARY KEY,
			embedding FLOAT[%d]
		)
	`, dimensions)

	if err := db.Exec(sql).Error; err != nil {
		return fmt.Errorf("failed to create vec_embeddings virtual table: %w", err)
	}

	return nil
}

// IsVecTableAvailable checks if the vec_embeddings virtual table exists
func IsVecTableAvailable(db *gorm.DB) bool {
	var count int64
	err := db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='vec_embeddings'").Scan(&count).Error
	return err == nil && count > 0
}

// InsertVecEmbedding inserts or updates a vector in the vec_embeddings table
func InsertVecEmbedding(db *gorm.DB, slug string, vector []float32) error {
	// Delete existing first (vec0 doesn't support ON CONFLICT)
	if err := db.Exec("DELETE FROM vec_embeddings WHERE slug = ?", slug).Error; err != nil {
		return fmt.Errorf("failed to delete existing embedding: %w", err)
	}

	// Insert new embedding
	if err := db.Exec("INSERT INTO vec_embeddings (slug, embedding) VALUES (?, ?)",
		slug, Float32SliceToBlob(vector)).Error; err != nil {
		return fmt.Errorf("failed to insert embedding: %w", err)
	}

	return nil
}

// DeleteVecEmbedding removes a vector from the vec_embeddings table
func DeleteVecEmbedding(db *gorm.DB, slug string) error {
	return db.Exec("DELETE FROM vec_embeddings WHERE slug = ?", slug).Error
}

// SearchVecEmbeddings performs KNN search using sqlite-vec
// Returns slugs and distances sorted by similarity (closest first)
func SearchVecEmbeddings(db *gorm.DB, queryVector []float32, limit int) ([]VecSearchResult, error) {
	if limit <= 0 {
		limit = 10
	}

	var results []VecSearchResult
	err := db.Raw(`
		SELECT slug, distance
		FROM vec_embeddings
		WHERE embedding MATCH ?
		ORDER BY distance
		LIMIT ?
	`, Float32SliceToBlob(queryVector), limit).Scan(&results).Error

	if err != nil {
		return nil, fmt.Errorf("vector search failed: %w", err)
	}

	return results, nil
}

// VecSearchResult represents a result from vector search
type VecSearchResult struct {
	Slug     string  `json:"slug"`
	Distance float64 `json:"distance"`
}

// CreateEmbeddingIndexes creates indexes for the embeddings table
func CreateEmbeddingIndexes(db *gorm.DB) error {
	indexes := []struct {
		name    string
		columns string
	}{
		{"idx_embeddings_content_hash", "slug, content_hash"},
		{"idx_embeddings_model", "model_name, model_version"},
	}

	for _, idx := range indexes {
		hasIndex := db.Migrator().HasIndex("embeddings", idx.name)
		if !hasIndex {
			sql := "CREATE INDEX IF NOT EXISTS " + idx.name + " ON embeddings (" + idx.columns + ")"
			if err := db.Exec(sql).Error; err != nil {
				return err
			}
		}
	}

	return nil
}

// Float32SliceToBlob converts a float32 slice to a byte slice for sqlite-vec
// sqlite-vec expects vectors as BLOBs in little-endian format
func Float32SliceToBlob(v []float32) []byte {
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		bits := math.Float32bits(f)
		binary.LittleEndian.PutUint32(buf[i*4:], bits)
	}
	return buf
}

// BlobToFloat32Slice converts a byte slice back to float32 slice
func BlobToFloat32Slice(b []byte) []float32 {
	if len(b)%4 != 0 {
		return nil
	}
	v := make([]float32, len(b)/4)
	for i := range v {
		bits := binary.LittleEndian.Uint32(b[i*4:])
		v[i] = math.Float32frombits(bits)
	}
	return v
}
