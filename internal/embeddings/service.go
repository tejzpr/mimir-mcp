// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package embeddings

import (
	"crypto/sha256"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Service handles embedding generation and caching with lazy regeneration
type Service struct {
	db           *gorm.DB
	client       Client
	modelName    string
	modelVersion string
	dimensions   int
	enabled      bool
	vecSearch    *VectorSearch // Vector search instance for sqlite-vec integration
}

// NewService creates a new embedding service
func NewService(db *gorm.DB, client Client, modelName, modelVersion string, dimensions int) *Service {
	return &Service{
		db:           db,
		client:       client,
		modelName:    modelName,
		modelVersion: modelVersion,
		dimensions:   dimensions,
		enabled:      true,
	}
}

// NewServiceWithVec creates a new embedding service with sqlite-vec enabled
func NewServiceWithVec(db *gorm.DB, client Client, modelName, modelVersion string, dimensions int) (*Service, error) {
	svc := &Service{
		db:           db,
		client:       client,
		modelName:    modelName,
		modelVersion: modelVersion,
		dimensions:   dimensions,
		enabled:      true,
	}

	// Initialize vector search with sqlite-vec
	vecSearch, err := NewVectorSearchWithVec(db, svc, dimensions)
	if err != nil {
		// Fallback to service without vec
		return svc, nil
	}
	svc.vecSearch = vecSearch

	return svc, nil
}

// SetEnabled enables or disables the embedding service
func (s *Service) SetEnabled(enabled bool) {
	s.enabled = enabled
}

// IsEnabled returns whether the service is enabled
func (s *Service) IsEnabled() bool {
	return s.enabled
}

// GetEmbedding retrieves or generates an embedding for the given content
// Implements lazy regeneration: returns cached if fresh, regenerates if stale
func (s *Service) GetEmbedding(slug, content string) ([]float32, error) {
	if !s.enabled {
		return nil, nil
	}

	contentHash := CalculateContentHash(content)

	// Check cache for fresh embedding
	var cached Embedding
	err := s.db.Where("slug = ? AND content_hash = ? AND model_version = ?",
		slug, contentHash, s.modelVersion).First(&cached).Error

	if err == nil {
		// Cache hit - embedding is fresh
		return BlobToFloat32Slice(cached.Vector), nil
	}

	// Cache miss or stale - regenerate
	vector, err := s.client.Embed(content)
	if err != nil {
		return nil, fmt.Errorf("failed to generate embedding: %w", err)
	}

	// Store for next time (upsert)
	embedding := Embedding{
		Slug:         slug,
		ContentHash:  contentHash,
		ModelName:    s.modelName,
		ModelVersion: s.modelVersion,
		Dimensions:   len(vector),
		Vector:       Float32SliceToBlob(vector),
		CreatedAt:    time.Now(),
	}

	err = s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "slug"}},
		DoUpdates: clause.AssignmentColumns([]string{"content_hash", "model_version", "vector", "created_at", "dimensions"}),
	}).Create(&embedding).Error

	if err != nil {
		return nil, fmt.Errorf("failed to cache embedding: %w", err)
	}

	// Also store in vec table if available
	if s.vecSearch != nil && s.vecSearch.IsVecEnabled() {
		_ = InsertVecEmbedding(s.db, slug, vector) // Best effort, don't fail
	}

	return vector, nil
}

// GetCachedEmbedding retrieves a cached embedding without regeneration
func (s *Service) GetCachedEmbedding(slug string) (*Embedding, error) {
	var embedding Embedding
	err := s.db.Where("slug = ?", slug).First(&embedding).Error
	if err != nil {
		return nil, err
	}
	return &embedding, nil
}

// DeleteEmbedding removes an embedding from the cache
func (s *Service) DeleteEmbedding(slug string) error {
	// Delete from metadata table
	if err := s.db.Where("slug = ?", slug).Delete(&Embedding{}).Error; err != nil {
		return err
	}

	// Also delete from vec table if available
	if s.vecSearch != nil && s.vecSearch.IsVecEnabled() {
		_ = DeleteVecEmbedding(s.db, slug)
	}

	return nil
}

// IndexAll generates embeddings for all provided memories
// This is useful for batch indexing after sync
func (s *Service) IndexAll(memories []MemoryContent) error {
	if !s.enabled {
		return nil
	}

	for _, mem := range memories {
		_, err := s.GetEmbedding(mem.Slug, mem.Content)
		if err != nil {
			// Log but continue with other memories
			continue
		}
	}

	return nil
}

// MemoryContent represents a memory with its content for embedding
type MemoryContent struct {
	Slug    string
	Content string
}

// IsStale checks if an embedding is stale (content changed or model changed)
func (s *Service) IsStale(slug, content string) (bool, error) {
	contentHash := CalculateContentHash(content)

	var embedding Embedding
	err := s.db.Where("slug = ?", slug).First(&embedding).Error
	if err != nil {
		// No embedding exists, considered stale
		return true, nil
	}

	// Check if content or model has changed
	if embedding.ContentHash != contentHash || embedding.ModelVersion != s.modelVersion {
		return true, nil
	}

	return false, nil
}

// GetContentHash returns the content hash for an embedding
func (s *Service) GetContentHash(slug string) (string, error) {
	var embedding Embedding
	err := s.db.Where("slug = ?", slug).First(&embedding).Error
	if err != nil {
		return "", err
	}
	return embedding.ContentHash, nil
}

// CountEmbeddings returns the total number of cached embeddings
func (s *Service) CountEmbeddings() (int64, error) {
	var count int64
	err := s.db.Model(&Embedding{}).Count(&count).Error
	return count, err
}

// CalculateContentHash computes a SHA256 hash of the content
func CalculateContentHash(content string) string {
	hash := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", hash[:16]) // Use first 16 bytes for shorter hash
}

// GetVectorSearch returns the vector search instance
// Used for direct vector search operations
func (s *Service) GetVectorSearch() *VectorSearch {
	if s.vecSearch == nil {
		// Create a basic vector search without vec if not already created
		s.vecSearch = NewVectorSearch(s.db, s)
	}
	return s.vecSearch
}

// IsVecEnabled returns whether sqlite-vec is enabled for this service
func (s *Service) IsVecEnabled() bool {
	return s.vecSearch != nil && s.vecSearch.IsVecEnabled()
}
