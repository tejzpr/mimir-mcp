// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package embeddings

import (
	"sort"

	"gorm.io/gorm"
)

// SearchResult represents a search result with similarity score
type SearchResult struct {
	Slug       string
	Similarity float32
}

// VectorSearch provides vector similarity search functionality
// Uses sqlite-vec for efficient KNN search when available
type VectorSearch struct {
	db         *gorm.DB
	service    *Service
	useVec     bool // Whether sqlite-vec is available
	dimensions int
}

// NewVectorSearch creates a new vector search instance
func NewVectorSearch(db *gorm.DB, service *Service) *VectorSearch {
	vs := &VectorSearch{
		db:         db,
		service:    service,
		useVec:     false,
		dimensions: DefaultEmbeddingDimensions,
	}

	// Check if sqlite-vec is available and initialize if so
	if IsVecTableAvailable(db) {
		vs.useVec = true
	}

	return vs
}

// NewVectorSearchWithVec creates a new vector search instance with sqlite-vec enabled
func NewVectorSearchWithVec(db *gorm.DB, service *Service, dimensions int) (*VectorSearch, error) {
	// Ensure vec_embeddings table exists
	if err := MigrateVecEmbeddings(db, dimensions); err != nil {
		return nil, err
	}

	return &VectorSearch{
		db:         db,
		service:    service,
		useVec:     true,
		dimensions: dimensions,
	}, nil
}

// Search finds the most similar vectors to the query
// Returns results sorted by similarity (highest first)
// Uses sqlite-vec KNN search when available, falls back to metadata table otherwise
func (v *VectorSearch) Search(query []float32, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 10
	}

	// Use sqlite-vec if available
	if v.useVec {
		return v.searchWithVec(query, limit)
	}

	// Fallback: search using embeddings metadata table (less efficient)
	return v.searchFallback(query, limit)
}

// searchWithVec performs KNN search using sqlite-vec
func (v *VectorSearch) searchWithVec(query []float32, limit int) ([]SearchResult, error) {
	vecResults, err := SearchVecEmbeddings(v.db, query, limit)
	if err != nil {
		return nil, err
	}

	// Convert VecSearchResult to SearchResult
	// sqlite-vec returns distance (lower is better), convert to similarity (higher is better)
	results := make([]SearchResult, len(vecResults))
	for i, vr := range vecResults {
		// Convert distance to similarity: similarity = 1 / (1 + distance)
		// This ensures similarity is in (0, 1] range
		similarity := float32(1.0 / (1.0 + vr.Distance))
		results[i] = SearchResult{
			Slug:       vr.Slug,
			Similarity: similarity,
		}
	}

	return results, nil
}

// searchFallback loads embeddings from metadata table and searches in memory
// This is used when sqlite-vec is not available
func (v *VectorSearch) searchFallback(query []float32, limit int) ([]SearchResult, error) {
	// Load all embeddings from database
	var embeddings []Embedding
	if err := v.db.Find(&embeddings).Error; err != nil {
		return nil, err
	}

	if len(embeddings) == 0 {
		return []SearchResult{}, nil
	}

	// Calculate similarity for each embedding using cosine similarity
	results := make([]SearchResult, 0, len(embeddings))
	for _, emb := range embeddings {
		vector := BlobToFloat32Slice(emb.Vector)
		if vector == nil {
			continue
		}
		similarity := cosineSimilarity(query, vector)

		results = append(results, SearchResult{
			Slug:       emb.Slug,
			Similarity: similarity,
		})
	}

	// Sort by similarity (descending)
	sort.Slice(results, func(i, j int) bool {
		return results[i].Similarity > results[j].Similarity
	})

	// Limit results
	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

// SearchWithThreshold finds vectors with similarity above the threshold
func (v *VectorSearch) SearchWithThreshold(query []float32, threshold float32, limit int) ([]SearchResult, error) {
	results, err := v.Search(query, limit*2) // Get more to filter
	if err != nil {
		return nil, err
	}

	// Filter by threshold
	filtered := make([]SearchResult, 0, len(results))
	for _, r := range results {
		if r.Similarity >= threshold {
			filtered = append(filtered, r)
		}
	}

	if len(filtered) > limit {
		filtered = filtered[:limit]
	}

	return filtered, nil
}

// Store stores a vector for a slug in both metadata table and vec table
func (v *VectorSearch) Store(slug string, vector []float32, contentHash, modelName, modelVersion string) error {
	// Store in metadata table
	emb := Embedding{
		Slug:         slug,
		ContentHash:  contentHash,
		ModelName:    modelName,
		ModelVersion: modelVersion,
		Dimensions:   len(vector),
		Vector:       Float32SliceToBlob(vector),
	}

	if err := v.db.Save(&emb).Error; err != nil {
		return err
	}

	// Also store in vec table if available
	if v.useVec {
		if err := InsertVecEmbedding(v.db, slug, vector); err != nil {
			// Log but don't fail - metadata table is still updated
			// Vector search will work via fallback
			return nil
		}
	}

	return nil
}

// Delete removes a vector for a slug from both tables
func (v *VectorSearch) Delete(slug string) error {
	// Delete from metadata table
	if err := v.db.Where("slug = ?", slug).Delete(&Embedding{}).Error; err != nil {
		return err
	}

	// Delete from vec table if available
	if v.useVec {
		_ = DeleteVecEmbedding(v.db, slug)
	}

	return nil
}

// Count returns the number of indexed vectors
func (v *VectorSearch) Count() (int64, error) {
	var count int64
	err := v.db.Model(&Embedding{}).Count(&count).Error
	return count, err
}

// IsVecEnabled returns whether sqlite-vec is being used
func (v *VectorSearch) IsVecEnabled() bool {
	return v.useVec
}

// SemanticSearch performs semantic search using the embedding service
// This is the main entry point for semantic search functionality
type SemanticSearch struct {
	service *Service
	search  *VectorSearch
}

// NewSemanticSearch creates a new semantic search instance
func NewSemanticSearch(service *Service, search *VectorSearch) *SemanticSearch {
	return &SemanticSearch{
		service: service,
		search:  search,
	}
}

// Search performs a semantic search for the query text
func (s *SemanticSearch) Search(query string, limit int) ([]SearchResult, error) {
	if !s.service.IsEnabled() {
		return nil, nil
	}

	// Generate embedding for the query
	queryVector, err := s.service.client.Embed(query)
	if err != nil {
		return nil, err
	}

	// Search for similar vectors
	return s.search.Search(queryVector, limit)
}

// SearchWithThreshold performs semantic search with a minimum similarity threshold
func (s *SemanticSearch) SearchWithThreshold(query string, threshold float32, limit int) ([]SearchResult, error) {
	if !s.service.IsEnabled() {
		return nil, nil
	}

	queryVector, err := s.service.client.Embed(query)
	if err != nil {
		return nil, err
	}

	return s.search.SearchWithThreshold(queryVector, threshold, limit)
}

// HybridSearch combines keyword and semantic search results
// Returns results that match both keyword and semantic criteria
func (s *SemanticSearch) HybridSearch(query string, keywordMatches []string, limit int) ([]SearchResult, error) {
	semanticResults, err := s.Search(query, limit*2)
	if err != nil {
		return nil, err
	}

	// If no semantic results, just return keyword matches as results with default similarity
	if semanticResults == nil || len(semanticResults) == 0 {
		results := make([]SearchResult, 0, len(keywordMatches))
		for _, slug := range keywordMatches {
			results = append(results, SearchResult{
				Slug:       slug,
				Similarity: 0.5, // Default similarity for keyword matches
			})
		}
		if len(results) > limit {
			results = results[:limit]
		}
		return results, nil
	}

	// Create a set of keyword matches for fast lookup
	keywordSet := make(map[string]bool)
	for _, slug := range keywordMatches {
		keywordSet[slug] = true
	}

	// Score results: boost semantic results that also have keyword matches
	boostedResults := make([]SearchResult, 0, len(semanticResults))
	for _, r := range semanticResults {
		if keywordSet[r.Slug] {
			// Boost similarity for keyword matches
			r.Similarity = r.Similarity * 1.2
			if r.Similarity > 1.0 {
				r.Similarity = 1.0
			}
		}
		boostedResults = append(boostedResults, r)
	}

	// Add keyword matches that weren't in semantic results
	for slug := range keywordSet {
		found := false
		for _, r := range boostedResults {
			if r.Slug == slug {
				found = true
				break
			}
		}
		if !found {
			boostedResults = append(boostedResults, SearchResult{
				Slug:       slug,
				Similarity: 0.4, // Lower similarity for keyword-only matches
			})
		}
	}

	// Re-sort after boosting
	sort.Slice(boostedResults, func(i, j int) bool {
		return boostedResults[i].Similarity > boostedResults[j].Similarity
	})

	if len(boostedResults) > limit {
		boostedResults = boostedResults[:limit]
	}

	return boostedResults, nil
}

// cosineSimilarity calculates cosine similarity between two vectors
// Used as fallback when sqlite-vec is not available
func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dotProduct, normA, normB float32
	for i := range a {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (sqrt32(normA) * sqrt32(normB))
}

// sqrt32 calculates square root for float32
func sqrt32(x float32) float32 {
	if x <= 0 {
		return 0
	}
	// Newton-Raphson method
	z := x / 2
	for i := 0; i < 10; i++ {
		z = (z + x/z) / 2
	}
	return z
}
