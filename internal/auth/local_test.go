// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package auth

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tejzpr/mimir-mcp/internal/database"
	"gorm.io/gorm/logger"
)

func TestGetLocalUsername(t *testing.T) {
	tm := NewTokenManager(nil, 24)
	localAuth := NewLocalAuthenticator(tm)

	username, err := localAuth.GetLocalUsername()
	require.NoError(t, err)
	assert.NotEmpty(t, username)
	t.Logf("Current username: %s", username)
}

func TestLocalAuthenticate(t *testing.T) {
	// Setup test database
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	dbCfg := &database.Config{
		Type:       "sqlite",
		SQLitePath: dbPath,
		LogLevel:   logger.Silent,
	}

	db, err := database.Connect(dbCfg)
	require.NoError(t, err)
	defer database.Close(db)

	err = database.Migrate(db)
	require.NoError(t, err)

	// Create token manager and local auth
	tm := NewTokenManager(db, 24)
	localAuth := NewLocalAuthenticator(tm)

	// Authenticate
	user, token, err := localAuth.Authenticate(db)
	require.NoError(t, err)
	assert.NotNil(t, user)
	assert.NotNil(t, token)
	assert.NotEmpty(t, user.Username)
	assert.NotEmpty(t, token.AccessToken)
	assert.Equal(t, user.ID, token.UserID)

	t.Logf("User created: %s (ID: %d)", user.Username, user.ID)
	t.Logf("Token generated: %s", token.AccessToken[:20]+"...")
}

func TestLocalAuthenticate_ExistingUser(t *testing.T) {
	// Setup test database
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	dbCfg := &database.Config{
		Type:       "sqlite",
		SQLitePath: dbPath,
		LogLevel:   logger.Silent,
	}

	db, err := database.Connect(dbCfg)
	require.NoError(t, err)
	defer database.Close(db)

	err = database.Migrate(db)
	require.NoError(t, err)

	tm := NewTokenManager(db, 24)
	localAuth := NewLocalAuthenticator(tm)

	// First authentication
	user1, token1, err := localAuth.Authenticate(db)
	require.NoError(t, err)

	// Second authentication (should reuse same user)
	user2, token2, err := localAuth.Authenticate(db)
	require.NoError(t, err)

	assert.Equal(t, user1.ID, user2.ID)
	assert.Equal(t, user1.Username, user2.Username)
	assert.NotEqual(t, token1.AccessToken, token2.AccessToken) // Different tokens
}
