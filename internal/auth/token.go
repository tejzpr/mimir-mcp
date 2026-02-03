// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package auth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/tejzpr/mimir-mcp/internal/database"
	"gorm.io/gorm"
)

// TokenManager handles authentication token operations
type TokenManager struct {
	db      *gorm.DB
	ttlHours int
}

// NewTokenManager creates a new token manager
func NewTokenManager(db *gorm.DB, ttlHours int) *TokenManager {
	return &TokenManager{
		db:      db,
		ttlHours: ttlHours,
	}
}

// GenerateToken creates a new access and refresh token for a user
func (tm *TokenManager) GenerateToken(userID uint) (*database.MimirAuthToken, error) {
	accessToken, err := generateRandomToken(32)
	if err != nil {
		return nil, fmt.Errorf("failed to generate access token: %w", err)
	}

	refreshToken, err := generateRandomToken(32)
	if err != nil {
		return nil, fmt.Errorf("failed to generate refresh token: %w", err)
	}

	token := &database.MimirAuthToken{
		UserID:       userID,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(tm.ttlHours) * time.Hour),
	}

	if err := tm.db.Create(token).Error; err != nil {
		return nil, fmt.Errorf("failed to store token: %w", err)
	}

	return token, nil
}

// ValidateToken checks if a token is valid and not expired
func (tm *TokenManager) ValidateToken(accessToken string) (*database.MimirAuthToken, error) {
	var token database.MimirAuthToken
	err := tm.db.Where("access_token = ?", accessToken).First(&token).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("token not found")
		}
		return nil, fmt.Errorf("failed to query token: %w", err)
	}

	// Check if token is expired
	if time.Now().After(token.ExpiresAt) {
		return nil, fmt.Errorf("token expired")
	}

	return &token, nil
}

// RefreshToken generates a new access token using a refresh token
func (tm *TokenManager) RefreshToken(refreshToken string) (*database.MimirAuthToken, error) {
	var oldToken database.MimirAuthToken
	err := tm.db.Where("refresh_token = ?", refreshToken).First(&oldToken).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("refresh token not found")
		}
		return nil, fmt.Errorf("failed to query refresh token: %w", err)
	}

	// Check if refresh token is expired (use 2x TTL for refresh tokens)
	refreshExpiry := oldToken.CreatedAt.Add(time.Duration(tm.ttlHours*2) * time.Hour)
	if time.Now().After(refreshExpiry) {
		return nil, fmt.Errorf("refresh token expired")
	}

	// Generate new access token
	accessToken, err := generateRandomToken(32)
	if err != nil {
		return nil, fmt.Errorf("failed to generate new access token: %w", err)
	}

	// Update the token
	oldToken.AccessToken = accessToken
	oldToken.ExpiresAt = time.Now().Add(time.Duration(tm.ttlHours) * time.Hour)
	oldToken.UpdatedAt = time.Now()

	if err := tm.db.Save(&oldToken).Error; err != nil {
		return nil, fmt.Errorf("failed to update token: %w", err)
	}

	return &oldToken, nil
}

// RevokeToken invalidates a token
func (tm *TokenManager) RevokeToken(accessToken string) error {
	result := tm.db.Where("access_token = ?", accessToken).Delete(&database.MimirAuthToken{})
	if result.Error != nil {
		return fmt.Errorf("failed to revoke token: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("token not found")
	}
	return nil
}

// RevokeAllUserTokens invalidates all tokens for a user
func (tm *TokenManager) RevokeAllUserTokens(userID uint) error {
	result := tm.db.Where("user_id = ?", userID).Delete(&database.MimirAuthToken{})
	if result.Error != nil {
		return fmt.Errorf("failed to revoke user tokens: %w", result.Error)
	}
	return nil
}

// CleanExpiredTokens removes expired tokens from the database
func (tm *TokenManager) CleanExpiredTokens() (int64, error) {
	result := tm.db.Where("expires_at < ?", time.Now()).Delete(&database.MimirAuthToken{})
	if result.Error != nil {
		return 0, fmt.Errorf("failed to clean expired tokens: %w", result.Error)
	}
	return result.RowsAffected, nil
}

// GetUserIDFromToken extracts the user ID from a valid token
func (tm *TokenManager) GetUserIDFromToken(accessToken string) (uint, error) {
	token, err := tm.ValidateToken(accessToken)
	if err != nil {
		return 0, err
	}
	return token.UserID, nil
}

// generateRandomToken creates a secure random token
func generateRandomToken(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(bytes), nil
}
