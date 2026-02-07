// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package embeddings

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVectorSearch_SimilarityRanking(t *testing.T) {
	db := setupTestDB(t)

	// Insert test vectors directly
	embeddings := []Embedding{
		{Slug: "doc1", Vector: Float32SliceToBlob([]float32{1.0, 0.0, 0.0}), ContentHash: "h1", ModelName: "test", ModelVersion: "v1", Dimensions: 3, CreatedAt: time.Now()},
		{Slug: "doc2", Vector: Float32SliceToBlob([]float32{0.9, 0.1, 0.0}), ContentHash: "h2", ModelName: "test", ModelVersion: "v1", Dimensions: 3, CreatedAt: time.Now()},  // Similar to doc1
		{Slug: "doc3", Vector: Float32SliceToBlob([]float32{0.0, 1.0, 0.0}), ContentHash: "h3", ModelName: "test", ModelVersion: "v1", Dimensions: 3, CreatedAt: time.Now()},  // Different
	}

	for _, emb := range embeddings {
		require.NoError(t, db.Create(&emb).Error)
	}

	search := NewVectorSearch(db, nil)

	// Search for vector similar to doc1
	results, err := search.Search([]float32{1.0, 0.0, 0.0}, 3)

	require.NoError(t, err)
	assert.Len(t, results, 3)
	assert.Equal(t, "doc1", results[0].Slug) // Exact match first
	assert.Equal(t, "doc2", results[1].Slug) // Similar second
	assert.InDelta(t, 1.0, results[0].Similarity, 0.0001)
}

func TestVectorSearch_LimitResults(t *testing.T) {
	db := setupTestDB(t)

	// Insert 100 vectors
	for i := 0; i < 100; i++ {
		vec := make([]float32, 10)
		vec[i%10] = 1.0
		emb := Embedding{
			Slug:         string(rune('a' + i%26)) + string(rune('0'+i/26)),
			Vector:       Float32SliceToBlob(vec),
			ContentHash:  "h",
			ModelName:    "test",
			ModelVersion: "v1",
			Dimensions:   10,
			CreatedAt:    time.Now(),
		}
		require.NoError(t, db.Create(&emb).Error)
	}

	search := NewVectorSearch(db, nil)

	query := make([]float32, 10)
	query[0] = 1.0

	results, err := search.Search(query, 10)

	require.NoError(t, err)
	assert.Len(t, results, 10)
}

func TestVectorSearch_EmptyIndex(t *testing.T) {
	db := setupTestDB(t)
	search := NewVectorSearch(db, nil)

	results, err := search.Search([]float32{1.0, 0.0, 0.0}, 10)

	require.NoError(t, err)
	assert.Len(t, results, 0)
}

func TestVectorSearch_Delete(t *testing.T) {
	db := setupTestDB(t)

	emb := Embedding{
		Slug:         "to-delete",
		Vector:       Float32SliceToBlob([]float32{1.0, 0.0, 0.0}),
		ContentHash:  "h",
		ModelName:    "test",
		ModelVersion: "v1",
		Dimensions:   3,
		CreatedAt:    time.Now(),
	}
	require.NoError(t, db.Create(&emb).Error)

	search := NewVectorSearch(db, nil)

	// Delete
	err := search.Delete("to-delete")
	assert.NoError(t, err)

	// Should not find
	results, err := search.Search([]float32{1.0, 0.0, 0.0}, 10)
	require.NoError(t, err)
	assert.Len(t, results, 0)
}

func TestVectorSearch_Count(t *testing.T) {
	db := setupTestDB(t)

	// Insert some embeddings
	for i := 0; i < 5; i++ {
		emb := Embedding{
			Slug:         string(rune('a' + i)),
			Vector:       Float32SliceToBlob([]float32{1.0, 0.0, 0.0}),
			ContentHash:  "h",
			ModelName:    "test",
			ModelVersion: "v1",
			Dimensions:   3,
			CreatedAt:    time.Now(),
		}
		require.NoError(t, db.Create(&emb).Error)
	}

	search := NewVectorSearch(db, nil)

	count, err := search.Count()
	require.NoError(t, err)
	assert.Equal(t, int64(5), count)
}

func TestVectorSearch_SearchWithThreshold(t *testing.T) {
	db := setupTestDB(t)

	// Insert test vectors
	embeddings := []Embedding{
		{Slug: "high-sim", Vector: Float32SliceToBlob([]float32{1.0, 0.0, 0.0}), ContentHash: "h1", ModelName: "test", ModelVersion: "v1", Dimensions: 3, CreatedAt: time.Now()},
		{Slug: "med-sim", Vector: Float32SliceToBlob([]float32{0.7, 0.7, 0.0}), ContentHash: "h2", ModelName: "test", ModelVersion: "v1", Dimensions: 3, CreatedAt: time.Now()},
		{Slug: "low-sim", Vector: Float32SliceToBlob([]float32{0.0, 0.0, 1.0}), ContentHash: "h3", ModelName: "test", ModelVersion: "v1", Dimensions: 3, CreatedAt: time.Now()},
	}

	for _, emb := range embeddings {
		require.NoError(t, db.Create(&emb).Error)
	}

	search := NewVectorSearch(db, nil)

	// Search with high threshold - should only return high-sim
	results, err := search.SearchWithThreshold([]float32{1.0, 0.0, 0.0}, 0.9, 10)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "high-sim", results[0].Slug)
}

func TestSemanticSearch_Search(t *testing.T) {
	db := setupTestDB(t)

	mockClient := &MockClient{
		EmbedFunc: func(text string) ([]float32, error) {
			// Return deterministic vectors based on content
			vec := make([]float32, 3)
			if text == "query" {
				vec[0] = 1.0
			} else if text == "similar" {
				vec[0] = 0.9
				vec[1] = 0.1
			}
			return vec, nil
		},
	}

	svc := NewService(db, mockClient, "test-model", "v1", 3)

	// Index some content
	_, _ = svc.GetEmbedding("similar-doc", "similar")

	search := NewVectorSearch(db, svc)
	semanticSearch := NewSemanticSearch(svc, search)

	results, err := semanticSearch.Search("query", 5)
	require.NoError(t, err)
	assert.Greater(t, len(results), 0)
}

func TestSemanticSearch_DisabledService(t *testing.T) {
	db := setupTestDB(t)

	mockClient := &MockClient{}
	svc := NewService(db, mockClient, "test-model", "v1", 3)
	svc.SetEnabled(false)

	search := NewVectorSearch(db, svc)
	semanticSearch := NewSemanticSearch(svc, search)

	results, err := semanticSearch.Search("query", 5)
	require.NoError(t, err)
	assert.Nil(t, results)
}

func TestSemanticSearch_HybridSearch(t *testing.T) {
	db := setupTestDB(t)

	mockClient := &MockClient{
		EmbedFunc: func(text string) ([]float32, error) {
			return []float32{1.0, 0.0, 0.0}, nil
		},
	}

	svc := NewService(db, mockClient, "test-model", "v1", 3)

	// Insert embeddings
	embeddings := []Embedding{
		{Slug: "both-match", Vector: Float32SliceToBlob([]float32{0.9, 0.1, 0.0}), ContentHash: "h1", ModelName: "test", ModelVersion: "v1", Dimensions: 3, CreatedAt: time.Now()},
		{Slug: "semantic-only", Vector: Float32SliceToBlob([]float32{0.8, 0.2, 0.0}), ContentHash: "h2", ModelName: "test", ModelVersion: "v1", Dimensions: 3, CreatedAt: time.Now()},
		{Slug: "keyword-only", Vector: Float32SliceToBlob([]float32{0.1, 0.9, 0.0}), ContentHash: "h3", ModelName: "test", ModelVersion: "v1", Dimensions: 3, CreatedAt: time.Now()},
	}
	for _, emb := range embeddings {
		require.NoError(t, db.Create(&emb).Error)
	}

	search := NewVectorSearch(db, svc)
	semanticSearch := NewSemanticSearch(svc, search)

	// Keyword matches include "both-match" and "keyword-only"
	keywordMatches := []string{"both-match", "keyword-only"}

	results, err := semanticSearch.HybridSearch("query", keywordMatches, 5)
	require.NoError(t, err)

	// "both-match" should be boosted to the top
	assert.Equal(t, "both-match", results[0].Slug)
}
