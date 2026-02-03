// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tejzpr/mimir-mcp/internal/database"
	"gorm.io/gorm/logger"
)

func setupMiddlewareTest(t *testing.T) (*Middleware, *TokenManager, uint) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	cfg := &database.Config{
		Type:       "sqlite",
		SQLitePath: dbPath,
		LogLevel:   logger.Silent,
	}

	db, err := database.Connect(cfg)
	require.NoError(t, err)

	err = database.Migrate(db)
	require.NoError(t, err)

	// Create test user
	user := &database.MimirUser{
		Username: "testuser",
		Email:    "test@example.com",
	}
	db.Create(user)

	tm := NewTokenManager(db, 24)
	middleware := NewMiddleware(tm)

	return middleware, tm, user.ID
}

func TestRequireAuth_ValidToken(t *testing.T) {
	middleware, tm, userID := setupMiddlewareTest(t)

	// Generate valid token
	token, err := tm.GenerateToken(userID)
	require.NoError(t, err)

	// Create test handler
	handler := middleware.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if user ID is in context
		extractedUserID, ok := GetUserIDFromContext(r.Context())
		assert.True(t, ok)
		assert.Equal(t, userID, extractedUserID)

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("authenticated"))
	}))

	// Create request with Authorization header
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "authenticated", rec.Body.String())
}

func TestRequireAuth_MissingToken(t *testing.T) {
	middleware, _, _ := setupMiddlewareTest(t)

	handler := middleware.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "missing token")
}

func TestRequireAuth_InvalidToken(t *testing.T) {
	middleware, _, _ := setupMiddlewareTest(t)

	handler := middleware.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRequireAuth_ExpiredToken(t *testing.T) {
	middleware, tm, userID := setupMiddlewareTest(t)

	// Generate token
	token, err := tm.GenerateToken(userID)
	require.NoError(t, err)

	// Revoke token
	err = tm.RevokeToken(token.AccessToken)
	require.NoError(t, err)

	handler := middleware.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestOptionalAuth_WithToken(t *testing.T) {
	middleware, tm, userID := setupMiddlewareTest(t)

	token, err := tm.GenerateToken(userID)
	require.NoError(t, err)

	handler := middleware.OptionalAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		extractedUserID, ok := GetUserIDFromContext(r.Context())
		assert.True(t, ok)
		assert.Equal(t, userID, extractedUserID)

		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestOptionalAuth_WithoutToken(t *testing.T) {
	middleware, _, _ := setupMiddlewareTest(t)

	handler := middleware.OptionalAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, ok := GetUserIDFromContext(r.Context())
		assert.False(t, ok)

		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestExtractToken_FromHeader(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer test-token-123")

	token := extractToken(req)
	assert.Equal(t, "test-token-123", token)
}

func TestExtractToken_FromQuery(t *testing.T) {
	req := httptest.NewRequest("GET", "/test?access_token=test-token-456", nil)

	token := extractToken(req)
	assert.Equal(t, "test-token-456", token)
}

func TestExtractToken_InvalidFormat(t *testing.T) {
	tests := []struct {
		name   string
		header string
	}{
		{"no bearer prefix", "test-token"},
		{"empty", ""},
		{"wrong prefix", "Basic dGVzdDp0ZXN0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}

			token := extractToken(req)
			assert.Empty(t, token)
		})
	}
}

func TestGetUserIDFromContext(t *testing.T) {
	ctx := context.Background()

	// Test with no user ID
	_, ok := GetUserIDFromContext(ctx)
	assert.False(t, ok)

	// Test with user ID
	ctx = WithUserID(ctx, 123)
	userID, ok := GetUserIDFromContext(ctx)
	assert.True(t, ok)
	assert.Equal(t, uint(123), userID)
}

func TestGetTokenFromContext(t *testing.T) {
	ctx := context.Background()

	// Test with no token
	_, ok := GetTokenFromContext(ctx)
	assert.False(t, ok)

	// Test with token
	ctx = context.WithValue(ctx, TokenKey, "test-token")
	token, ok := GetTokenFromContext(ctx)
	assert.True(t, ok)
	assert.Equal(t, "test-token", token)
}

func TestRequireAuth_QueryParameter(t *testing.T) {
	middleware, tm, userID := setupMiddlewareTest(t)

	token, err := tm.GenerateToken(userID)
	require.NoError(t, err)

	handler := middleware.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test?access_token="+token.AccessToken, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestRequireAuth_CaseInsensitiveBearer(t *testing.T) {
	middleware, tm, userID := setupMiddlewareTest(t)

	token, err := tm.GenerateToken(userID)
	require.NoError(t, err)

	tests := []string{
		"Bearer " + token.AccessToken,
		"bearer " + token.AccessToken,
		"BEARER " + token.AccessToken,
	}

	for _, authHeader := range tests {
		t.Run(authHeader, func(t *testing.T) {
			handler := middleware.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest("GET", "/test", nil)
			req.Header.Set("Authorization", authHeader)

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusOK, rec.Code)
		})
	}
}
