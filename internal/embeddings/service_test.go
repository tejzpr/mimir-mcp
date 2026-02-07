// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package embeddings

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupTestDB(t *testing.T) *gorm.DB {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)

	err = MigrateEmbeddings(db)
	require.NoError(t, err)

	return db
}

func TestEmbeddingService_GenerateAndCache(t *testing.T) {
	db := setupTestDB(t)

	mockClient := &MockClient{
		EmbedFunc: func(text string) ([]float32, error) {
			return make([]float32, 1536), nil
		},
	}

	svc := NewService(db, mockClient, "test-model", "v1", 1536)

	// First call - should generate
	vec1, err := svc.GetEmbedding("test-slug", "test content")
	require.NoError(t, err)
	assert.Len(t, vec1, 1536)
	assert.Equal(t, 1, mockClient.CallCount)

	// Second call with same content - should use cache
	vec2, err := svc.GetEmbedding("test-slug", "test content")
	require.NoError(t, err)
	assert.Equal(t, 1, mockClient.CallCount) // No new call
	assert.Equal(t, vec1, vec2)
}

func TestEmbeddingService_RegenerateOnContentChange(t *testing.T) {
	db := setupTestDB(t)

	mockClient := &MockClient{
		EmbedFunc: func(text string) ([]float32, error) {
			// Return different vectors for different content
			vec := make([]float32, 1536)
			vec[0] = float32(len(text))
			return vec, nil
		},
	}

	svc := NewService(db, mockClient, "test-model", "v1", 1536)

	// First content
	vec1, err := svc.GetEmbedding("test-slug", "short")
	require.NoError(t, err)

	// Modified content - should regenerate
	vec2, err := svc.GetEmbedding("test-slug", "much longer content")
	require.NoError(t, err)

	assert.Equal(t, 2, mockClient.CallCount)
	assert.NotEqual(t, vec1[0], vec2[0])
}

func TestEmbeddingService_RegenerateOnModelChange(t *testing.T) {
	db := setupTestDB(t)

	callCount := 0
	mockClient := &MockClient{
		EmbedFunc: func(text string) ([]float32, error) {
			callCount++
			return make([]float32, 1536), nil
		},
	}

	// v1 service
	svc1 := NewService(db, mockClient, "test-model", "v1", 1536)
	_, err := svc1.GetEmbedding("test-slug", "content")
	require.NoError(t, err)

	// v2 service (model upgrade)
	svc2 := NewService(db, mockClient, "test-model", "v2", 1536)
	_, err = svc2.GetEmbedding("test-slug", "content")
	require.NoError(t, err)

	// Should regenerate due to version change
	assert.Equal(t, 2, callCount)
}

func TestEmbeddingService_DisabledMode(t *testing.T) {
	db := setupTestDB(t)

	mockClient := &MockClient{}
	svc := NewService(db, mockClient, "test-model", "v1", 1536)
	svc.SetEnabled(false)

	vec, err := svc.GetEmbedding("test", "content")

	assert.NoError(t, err)
	assert.Nil(t, vec) // Returns nil when disabled
	assert.Equal(t, 0, mockClient.CallCount)
}

func TestCalculateContentHash(t *testing.T) {
	hash1 := CalculateContentHash("Hello World")
	hash2 := CalculateContentHash("Hello World")
	hash3 := CalculateContentHash("Hello World!")

	assert.Equal(t, hash1, hash2)
	assert.NotEqual(t, hash1, hash3)
}

func TestVectorConversion(t *testing.T) {
	original := []float32{1.0, 2.5, -3.14159, 0.0, 100.123}

	bytes := Float32SliceToBlob(original)
	restored := BlobToFloat32Slice(bytes)

	assert.Len(t, restored, len(original))
	for i := range original {
		assert.InDelta(t, original[i], restored[i], 0.00001)
	}
}

func TestCosineSimilarity(t *testing.T) {
	// Test cosine similarity through VectorSearch fallback
	db := setupTestDB(t)

	// Insert test vectors directly
	embeddings := []Embedding{
		{Slug: "identical", Vector: Float32SliceToBlob([]float32{1.0, 0.0, 0.0}), ContentHash: "h1", ModelName: "test", ModelVersion: "v1", Dimensions: 3},
		{Slug: "orthogonal", Vector: Float32SliceToBlob([]float32{0.0, 1.0, 0.0}), ContentHash: "h2", ModelName: "test", ModelVersion: "v1", Dimensions: 3},
		{Slug: "similar", Vector: Float32SliceToBlob([]float32{0.9, 0.1, 0.0}), ContentHash: "h3", ModelName: "test", ModelVersion: "v1", Dimensions: 3},
	}

	for _, emb := range embeddings {
		require.NoError(t, db.Create(&emb).Error)
	}

	search := NewVectorSearch(db, nil)

	// Query with [1,0,0] - identical should be first with sim ~1.0, similar should have sim > 0.9
	results, err := search.Search([]float32{1.0, 0.0, 0.0}, 3)
	require.NoError(t, err)

	// Find results
	var identicalSim, orthogonalSim, similarSim float32
	for _, r := range results {
		switch r.Slug {
		case "identical":
			identicalSim = r.Similarity
		case "orthogonal":
			orthogonalSim = r.Similarity
		case "similar":
			similarSim = r.Similarity
		}
	}

	// Identical should be ~1.0
	assert.InDelta(t, 1.0, identicalSim, 0.0001)
	// Orthogonal should be low (but similarity conversion makes it positive)
	assert.Less(t, orthogonalSim, float32(0.5))
	// Similar should be > orthogonal
	assert.Greater(t, similarSim, orthogonalSim)
}

func TestEmbeddingService_IsStale(t *testing.T) {
	db := setupTestDB(t)

	mockClient := &MockClient{
		EmbedFunc: func(text string) ([]float32, error) {
			return make([]float32, 1536), nil
		},
	}

	svc := NewService(db, mockClient, "test-model", "v1", 1536)

	// No embedding exists - should be stale
	stale, err := svc.IsStale("nonexistent", "content")
	require.NoError(t, err)
	assert.True(t, stale)

	// Create embedding
	_, err = svc.GetEmbedding("test-slug", "original content")
	require.NoError(t, err)

	// Same content - not stale
	stale, err = svc.IsStale("test-slug", "original content")
	require.NoError(t, err)
	assert.False(t, stale)

	// Different content - stale
	stale, err = svc.IsStale("test-slug", "modified content")
	require.NoError(t, err)
	assert.True(t, stale)
}

func TestEmbeddingService_DeleteEmbedding(t *testing.T) {
	db := setupTestDB(t)

	mockClient := &MockClient{}
	svc := NewService(db, mockClient, "test-model", "v1", 1536)

	// Create embedding
	_, err := svc.GetEmbedding("to-delete", "content")
	require.NoError(t, err)

	// Verify it exists
	cached, err := svc.GetCachedEmbedding("to-delete")
	require.NoError(t, err)
	assert.NotNil(t, cached)

	// Delete
	err = svc.DeleteEmbedding("to-delete")
	assert.NoError(t, err)

	// Verify deleted
	_, err = svc.GetCachedEmbedding("to-delete")
	assert.Error(t, err)
}

func TestEmbeddingService_CountEmbeddings(t *testing.T) {
	db := setupTestDB(t)

	mockClient := &MockClient{}
	svc := NewService(db, mockClient, "test-model", "v1", 1536)

	// Initially empty
	count, err := svc.CountEmbeddings()
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)

	// Add embeddings
	for i := 0; i < 5; i++ {
		_, err := svc.GetEmbedding(
			"slug-"+string(rune('a'+i)),
			"content-"+string(rune('a'+i)),
		)
		require.NoError(t, err)
	}

	// Count should be 5
	count, err = svc.CountEmbeddings()
	require.NoError(t, err)
	assert.Equal(t, int64(5), count)
}

func TestEmbeddingService_GetContentHash(t *testing.T) {
	db := setupTestDB(t)

	mockClient := &MockClient{}
	svc := NewService(db, mockClient, "test-model", "v1", 1536)

	content := "test content"
	expectedHash := CalculateContentHash(content)

	_, err := svc.GetEmbedding("hash-test", content)
	require.NoError(t, err)

	hash, err := svc.GetContentHash("hash-test")
	require.NoError(t, err)
	assert.Equal(t, expectedHash, hash)
}

func TestMigrateEmbeddings(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)

	// Run migration
	err = MigrateEmbeddings(db)
	require.NoError(t, err)

	// Verify table exists
	hasTable := db.Migrator().HasTable(&Embedding{})
	assert.True(t, hasTable)

	// Create indexes
	err = CreateEmbeddingIndexes(db)
	require.NoError(t, err)
}

func TestMockClient(t *testing.T) {
	mock := &MockClient{
		EmbedFunc: func(text string) ([]float32, error) {
			vec := make([]float32, 384)
			vec[0] = float32(len(text))
			return vec, nil
		},
		ModelInfo: ModelInfo{
			Name:       "custom-model",
			Version:    "v2",
			Dimensions: 384,
			Provider:   "custom",
		},
	}

	vec, err := mock.Embed("test")
	require.NoError(t, err)
	assert.Len(t, vec, 384)
	assert.Equal(t, float32(4), vec[0]) // len("test") = 4

	info := mock.GetModelInfo()
	assert.Equal(t, "custom-model", info.Name)
	assert.Equal(t, 384, info.Dimensions)
}

// Integration test with real OpenAI API (skipped unless OPENAI_API_KEY is set)
func TestOpenAIClient_Integration(t *testing.T) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("OPENAI_API_KEY not set")
	}

	client := NewOpenAIClient(
		"https://api.openai.com/v1",
		apiKey,
		"text-embedding-3-small",
		1536,
	)

	vec, err := client.Embed("Hello, world!")
	require.NoError(t, err)
	assert.Len(t, vec, 1536)

	// Verify it's not all zeros
	hasNonZero := false
	for _, v := range vec {
		if v != 0 {
			hasNonZero = true
			break
		}
	}
	assert.True(t, hasNonZero)
}
