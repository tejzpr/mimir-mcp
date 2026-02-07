// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package embeddings

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client is the interface for embedding providers
type Client interface {
	// Embed generates an embedding vector for the given text
	Embed(text string) ([]float32, error)

	// EmbedBatch generates embedding vectors for multiple texts
	EmbedBatch(texts []string) ([][]float32, error)

	// GetModelInfo returns information about the embedding model
	GetModelInfo() ModelInfo
}

// ModelInfo contains metadata about the embedding model
type ModelInfo struct {
	Name       string
	Version    string
	Dimensions int
	Provider   string
}

// OpenAIClient implements the Client interface for OpenAI embeddings
type OpenAIClient struct {
	baseURL    string
	apiKey     string
	model      string
	dimensions int
	httpClient *http.Client
}

// OpenAIEmbeddingRequest represents the request body for OpenAI embeddings API
type OpenAIEmbeddingRequest struct {
	Input          interface{} `json:"input"` // string or []string
	Model          string      `json:"model"`
	EncodingFormat string      `json:"encoding_format,omitempty"`
	Dimensions     int         `json:"dimensions,omitempty"`
}

// OpenAIEmbeddingResponse represents the response from OpenAI embeddings API
type OpenAIEmbeddingResponse struct {
	Object string `json:"object"`
	Data   []struct {
		Object    string    `json:"object"`
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Model string `json:"model"`
	Usage struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
}

// OpenAIErrorResponse represents an error response from OpenAI
type OpenAIErrorResponse struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

// NewOpenAIClient creates a new OpenAI embedding client
func NewOpenAIClient(baseURL, apiKey, model string, dimensions int) *OpenAIClient {
	return &OpenAIClient{
		baseURL:    baseURL,
		apiKey:     apiKey,
		model:      model,
		dimensions: dimensions,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Embed generates an embedding vector for the given text
func (c *OpenAIClient) Embed(text string) ([]float32, error) {
	vectors, err := c.EmbedBatch([]string{text})
	if err != nil {
		return nil, err
	}
	if len(vectors) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}
	return vectors[0], nil
}

// EmbedBatch generates embedding vectors for multiple texts
func (c *OpenAIClient) EmbedBatch(texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}

	reqBody := OpenAIEmbeddingRequest{
		Input: texts,
		Model: c.model,
	}

	// Only include dimensions if explicitly set and supported by model
	if c.dimensions > 0 {
		reqBody.Dimensions = c.dimensions
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", c.baseURL+"/embeddings", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp OpenAIErrorResponse
		if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error.Message != "" {
			return nil, fmt.Errorf("OpenAI API error: %s", errResp.Error.Message)
		}
		return nil, fmt.Errorf("OpenAI API error: status %d", resp.StatusCode)
	}

	var embResp OpenAIEmbeddingResponse
	if err := json.Unmarshal(body, &embResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Sort by index to ensure correct order
	vectors := make([][]float32, len(texts))
	for _, data := range embResp.Data {
		if data.Index < len(vectors) {
			vectors[data.Index] = data.Embedding
		}
	}

	return vectors, nil
}

// GetModelInfo returns information about the embedding model
func (c *OpenAIClient) GetModelInfo() ModelInfo {
	return ModelInfo{
		Name:       c.model,
		Version:    "v1",
		Dimensions: c.dimensions,
		Provider:   "openai",
	}
}

// MockClient is a mock implementation for testing
type MockClient struct {
	EmbedFunc      func(text string) ([]float32, error)
	EmbedBatchFunc func(texts []string) ([][]float32, error)
	CallCount      int
	ModelInfo      ModelInfo
}

// Embed calls the mock function
func (m *MockClient) Embed(text string) ([]float32, error) {
	m.CallCount++
	if m.EmbedFunc != nil {
		return m.EmbedFunc(text)
	}
	// Default: return a zero vector
	return make([]float32, 1536), nil
}

// EmbedBatch calls the mock function
func (m *MockClient) EmbedBatch(texts []string) ([][]float32, error) {
	m.CallCount++
	if m.EmbedBatchFunc != nil {
		return m.EmbedBatchFunc(texts)
	}
	// Default: return zero vectors
	vectors := make([][]float32, len(texts))
	for i := range vectors {
		vectors[i] = make([]float32, 1536)
	}
	return vectors, nil
}

// GetModelInfo returns mock model info
func (m *MockClient) GetModelInfo() ModelInfo {
	if m.ModelInfo.Name != "" {
		return m.ModelInfo
	}
	return ModelInfo{
		Name:       "mock-model",
		Version:    "v1",
		Dimensions: 1536,
		Provider:   "mock",
	}
}
