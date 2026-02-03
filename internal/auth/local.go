// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package auth

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/tejzpr/mimir-mcp/internal/database"
	"gorm.io/gorm"
)

// LocalAuthenticator handles local system authentication
type LocalAuthenticator struct {
	tokenManager       *TokenManager
	useAccessingUser   bool // If true, use ACCESSING_USER env var instead of whoami
}

// NewLocalAuthenticator creates a new local authenticator
func NewLocalAuthenticator(tm *TokenManager) *LocalAuthenticator {
	return &LocalAuthenticator{
		tokenManager:     tm,
		useAccessingUser: false,
	}
}

// NewLocalAuthenticatorWithAccessingUser creates a local authenticator that uses ACCESSING_USER env var
func NewLocalAuthenticatorWithAccessingUser(tm *TokenManager) *LocalAuthenticator {
	return &LocalAuthenticator{
		tokenManager:     tm,
		useAccessingUser: true,
	}
}

// GetLocalUsername gets the username based on configuration:
// - If useAccessingUser is true: use ACCESSING_USER env var (for MCP servers called by authenticated systems)
// - Otherwise: use whoami (default for standalone usage)
func (l *LocalAuthenticator) GetLocalUsername() (string, error) {
	if l.useAccessingUser {
		username := os.Getenv("ACCESSING_USER")
		if username == "" {
			return "", fmt.Errorf("ACCESSING_USER environment variable is required but not set")
		}
		return strings.TrimSpace(username), nil
	}

	// Default: use system whoami
	cmd := exec.Command("whoami")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get username via whoami: %w", err)
	}
	username := strings.TrimSpace(string(output))
	if username == "" {
		return "", fmt.Errorf("whoami returned empty username")
	}
	return username, nil
}

// Authenticate creates or retrieves a user and generates a token for local mode
func (l *LocalAuthenticator) Authenticate(db *gorm.DB) (*database.MimirUser, *database.MimirAuthToken, error) {
	username, err := l.GetLocalUsername()
	if err != nil {
		return nil, nil, err
	}

	// Find or create user
	var user database.MimirUser
	result := db.Where("username = ?", username).FirstOrCreate(&user, database.MimirUser{
		Username: username,
		Email:    username + "@local",
	})
	if result.Error != nil {
		return nil, nil, fmt.Errorf("failed to create/find user: %w", result.Error)
	}

	// Generate token
	token, err := l.tokenManager.GenerateToken(user.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate token: %w", err)
	}

	return &user, token, nil
}
