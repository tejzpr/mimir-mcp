// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package auth

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tejzpr/mimir-mcp/internal/database"
	"gorm.io/gorm/logger"
)

func setupTestDB(t *testing.T) *database.Config {
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

	return cfg
}

func TestGenerateToken(t *testing.T) {
	dbCfg := setupTestDB(t)
	db, _ := database.Connect(dbCfg)
	defer database.Close(db)

	// Create a test user
	user := &database.MimirUser{
		Username: "testuser",
		Email:    "test@example.com",
	}
	db.Create(user)

	tm := NewTokenManager(db, 24)

	token, err := tm.GenerateToken(user.ID)
	require.NoError(t, err)
	assert.NotNil(t, token)
	assert.NotEmpty(t, token.AccessToken)
	assert.NotEmpty(t, token.RefreshToken)
	assert.Equal(t, user.ID, token.UserID)
	assert.True(t, token.ExpiresAt.After(time.Now()))
}

func TestValidateToken_Success(t *testing.T) {
	dbCfg := setupTestDB(t)
	db, _ := database.Connect(dbCfg)
	defer database.Close(db)

	user := &database.MimirUser{Username: "testuser"}
	db.Create(user)

	tm := NewTokenManager(db, 24)

	// Generate token
	token, err := tm.GenerateToken(user.ID)
	require.NoError(t, err)

	// Validate token
	validToken, err := tm.ValidateToken(token.AccessToken)
	require.NoError(t, err)
	assert.Equal(t, token.ID, validToken.ID)
	assert.Equal(t, user.ID, validToken.UserID)
}

func TestValidateToken_NotFound(t *testing.T) {
	dbCfg := setupTestDB(t)
	db, _ := database.Connect(dbCfg)
	defer database.Close(db)

	tm := NewTokenManager(db, 24)

	_, err := tm.ValidateToken("invalid-token")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "token not found")
}

func TestValidateToken_Expired(t *testing.T) {
	dbCfg := setupTestDB(t)
	db, _ := database.Connect(dbCfg)
	defer database.Close(db)

	user := &database.MimirUser{Username: "testuser"}
	db.Create(user)

	tm := NewTokenManager(db, 24)

	// Generate token
	token, err := tm.GenerateToken(user.ID)
	require.NoError(t, err)

	// Manually expire the token
	token.ExpiresAt = time.Now().Add(-1 * time.Hour)
	db.Save(token)

	// Validate token
	_, err = tm.ValidateToken(token.AccessToken)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "token expired")
}

func TestRefreshToken_Success(t *testing.T) {
	dbCfg := setupTestDB(t)
	db, _ := database.Connect(dbCfg)
	defer database.Close(db)

	user := &database.MimirUser{Username: "testuser"}
	db.Create(user)

	tm := NewTokenManager(db, 24)

	// Generate token
	originalToken, err := tm.GenerateToken(user.ID)
	require.NoError(t, err)
	originalAccessToken := originalToken.AccessToken

	// Refresh token
	newToken, err := tm.RefreshToken(originalToken.RefreshToken)
	require.NoError(t, err)
	assert.NotEqual(t, originalAccessToken, newToken.AccessToken)
	assert.Equal(t, originalToken.RefreshToken, newToken.RefreshToken)
	assert.True(t, newToken.ExpiresAt.After(time.Now()))
}

func TestRefreshToken_NotFound(t *testing.T) {
	dbCfg := setupTestDB(t)
	db, _ := database.Connect(dbCfg)
	defer database.Close(db)

	tm := NewTokenManager(db, 24)

	_, err := tm.RefreshToken("invalid-refresh-token")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "refresh token not found")
}

func TestRefreshToken_Expired(t *testing.T) {
	dbCfg := setupTestDB(t)
	db, _ := database.Connect(dbCfg)
	defer database.Close(db)

	user := &database.MimirUser{Username: "testuser"}
	db.Create(user)

	tm := NewTokenManager(db, 1) // 1 hour TTL

	// Generate token
	token, err := tm.GenerateToken(user.ID)
	require.NoError(t, err)

	// Manually set creation time to past (beyond 2x TTL)
	token.CreatedAt = time.Now().Add(-3 * time.Hour)
	db.Save(token)

	// Try to refresh
	_, err = tm.RefreshToken(token.RefreshToken)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "refresh token expired")
}

func TestRevokeToken(t *testing.T) {
	dbCfg := setupTestDB(t)
	db, _ := database.Connect(dbCfg)
	defer database.Close(db)

	user := &database.MimirUser{Username: "testuser"}
	db.Create(user)

	tm := NewTokenManager(db, 24)

	// Generate token
	token, err := tm.GenerateToken(user.ID)
	require.NoError(t, err)

	// Revoke token
	err = tm.RevokeToken(token.AccessToken)
	require.NoError(t, err)

	// Try to validate revoked token
	_, err = tm.ValidateToken(token.AccessToken)
	assert.Error(t, err)
}

func TestRevokeToken_NotFound(t *testing.T) {
	dbCfg := setupTestDB(t)
	db, _ := database.Connect(dbCfg)
	defer database.Close(db)

	tm := NewTokenManager(db, 24)

	err := tm.RevokeToken("invalid-token")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "token not found")
}

func TestRevokeAllUserTokens(t *testing.T) {
	dbCfg := setupTestDB(t)
	db, _ := database.Connect(dbCfg)
	defer database.Close(db)

	user := &database.MimirUser{Username: "testuser"}
	db.Create(user)

	tm := NewTokenManager(db, 24)

	// Generate multiple tokens
	token1, _ := tm.GenerateToken(user.ID)
	token2, _ := tm.GenerateToken(user.ID)

	// Revoke all tokens
	err := tm.RevokeAllUserTokens(user.ID)
	require.NoError(t, err)

	// Verify all tokens are revoked
	_, err = tm.ValidateToken(token1.AccessToken)
	assert.Error(t, err)

	_, err = tm.ValidateToken(token2.AccessToken)
	assert.Error(t, err)
}

func TestCleanExpiredTokens(t *testing.T) {
	dbCfg := setupTestDB(t)
	db, _ := database.Connect(dbCfg)
	defer database.Close(db)

	user := &database.MimirUser{Username: "testuser"}
	db.Create(user)

	tm := NewTokenManager(db, 24)

	// Generate tokens
	token1, _ := tm.GenerateToken(user.ID)
	token2, _ := tm.GenerateToken(user.ID)

	// Manually expire one token
	token1.ExpiresAt = time.Now().Add(-1 * time.Hour)
	db.Save(token1)

	// Clean expired tokens
	count, err := tm.CleanExpiredTokens()
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)

	// Verify expired token is gone
	_, err = tm.ValidateToken(token1.AccessToken)
	assert.Error(t, err)

	// Verify valid token still exists
	_, err = tm.ValidateToken(token2.AccessToken)
	assert.NoError(t, err)
}

func TestGetUserIDFromToken(t *testing.T) {
	dbCfg := setupTestDB(t)
	db, _ := database.Connect(dbCfg)
	defer database.Close(db)

	user := &database.MimirUser{Username: "testuser"}
	db.Create(user)

	tm := NewTokenManager(db, 24)

	token, err := tm.GenerateToken(user.ID)
	require.NoError(t, err)

	userID, err := tm.GetUserIDFromToken(token.AccessToken)
	require.NoError(t, err)
	assert.Equal(t, user.ID, userID)
}

func TestGenerateRandomToken(t *testing.T) {
	token1, err := generateRandomToken(32)
	require.NoError(t, err)
	assert.NotEmpty(t, token1)

	token2, err := generateRandomToken(32)
	require.NoError(t, err)
	assert.NotEmpty(t, token2)

	// Tokens should be different
	assert.NotEqual(t, token1, token2)
}
