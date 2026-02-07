// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_Defaults(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tempDir)
	defer os.Setenv("HOME", originalHome)

	// Ensure config directory exists
	err := EnsureConfigDir()
	require.NoError(t, err)

	cfg, err := Load()
	require.NoError(t, err)
	assert.NotNil(t, cfg)

	// Check defaults
	assert.Equal(t, "localhost", cfg.Server.Host)
	assert.Equal(t, 8080, cfg.Server.Port)
	assert.Equal(t, "sqlite", cfg.Database.Type)
	assert.Equal(t, "main", cfg.Git.DefaultBranch)
	assert.Equal(t, 60, cfg.Git.SyncInterval)
}

func TestLoadFromPath(t *testing.T) {
	tests := []struct {
		name        string
		configJSON  string
		expectError bool
		validate    func(*testing.T, *Config)
	}{
		{
			name: "valid sqlite config",
			configJSON: `{
				"server": {
					"host": "0.0.0.0",
					"port": 9000
				},
				"database": {
					"type": "sqlite",
					"sqlite_path": "/tmp/test.db"
				},
				"git": {
					"default_branch": "main",
					"sync_interval_minutes": 30
				},
				"security": {
					"token_ttl_hours": 12
				}
			}`,
			expectError: false,
			validate: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "0.0.0.0", cfg.Server.Host)
				assert.Equal(t, 9000, cfg.Server.Port)
				assert.Equal(t, "sqlite", cfg.Database.Type)
				assert.Equal(t, "/tmp/test.db", cfg.Database.SQLitePath)
				assert.Equal(t, 30, cfg.Git.SyncInterval)
				assert.Equal(t, 12, cfg.Security.TokenTTL)
			},
		},
		{
			name: "valid postgres config",
			configJSON: `{
				"database": {
					"type": "postgres",
					"postgres_dsn": "postgresql://user:pass@localhost/db"
				}
			}`,
			expectError: false,
			validate: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "postgres", cfg.Database.Type)
				assert.Equal(t, "postgresql://user:pass@localhost/db", cfg.Database.PostgresDSN)
			},
		},
		{
			name: "invalid database type",
			configJSON: `{
				"database": {
					"type": "mysql"
				}
			}`,
			expectError: true,
		},
		{
			name: "missing sqlite path",
			configJSON: `{
				"database": {
					"type": "sqlite",
					"sqlite_path": ""
				}
			}`,
			expectError: true,
		},
		{
			name: "invalid port",
			configJSON: `{
				"server": {
					"port": 100000
				},
				"database": {
					"type": "sqlite",
					"sqlite_path": "/tmp/test.db"
				}
			}`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempFile := filepath.Join(t.TempDir(), "config.json")
			err := os.WriteFile(tempFile, []byte(tt.configJSON), 0644)
			require.NoError(t, err)

			cfg, err := LoadFromPath(tempFile)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				if tt.validate != nil {
					tt.validate(t, cfg)
				}
			}
		})
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid config",
			config: &Config{
				Server: ServerConfig{
					Port: 8080,
				},
				Database: DatabaseConfig{
					Type:       "sqlite",
					SQLitePath: "/tmp/test.db",
				},
				Git: GitConfig{
					SyncInterval: 60,
				},
				Security: SecurityConfig{
					TokenTTL: 24,
				},
			},
			expectError: false,
		},
		{
			name: "invalid database type",
			config: &Config{
				Database: DatabaseConfig{
					Type: "mongodb",
				},
			},
			expectError: true,
			errorMsg:    "database.type must be 'sqlite' or 'postgres'",
		},
		{
			name: "invalid port - too low",
			config: &Config{
				Server: ServerConfig{
					Port: 0,
				},
				Database: DatabaseConfig{
					Type:       "sqlite",
					SQLitePath: "/tmp/test.db",
				},
			},
			expectError: true,
			errorMsg:    "server.port must be between 1 and 65535",
		},
		{
			name: "invalid port - too high",
			config: &Config{
				Server: ServerConfig{
					Port: 70000,
				},
				Database: DatabaseConfig{
					Type:       "sqlite",
					SQLitePath: "/tmp/test.db",
				},
			},
			expectError: true,
			errorMsg:    "server.port must be between 1 and 65535",
		},
		{
			name: "invalid sync interval",
			config: &Config{
				Server: ServerConfig{
					Port: 8080,
				},
				Database: DatabaseConfig{
					Type:       "sqlite",
					SQLitePath: "/tmp/test.db",
				},
				Git: GitConfig{
					SyncInterval: 0,
				},
			},
			expectError: true,
			errorMsg:    "git.sync_interval_minutes must be at least 1",
		},
		{
			name: "incomplete SAML config when auth type is saml",
			config: &Config{
				Server: ServerConfig{
					Port: 8080,
				},
				Database: DatabaseConfig{
					Type:       "sqlite",
					SQLitePath: "/tmp/test.db",
				},
				Auth: AuthConfig{
					Type: "saml",
				},
				SAML: SAMLConfig{
					EntityID: "test",
					// Missing ACSURL and IDPMetadata
				},
				Git: GitConfig{
					SyncInterval: 60,
				},
				Security: SecurityConfig{
					TokenTTL: 24,
				},
			},
			expectError: true,
			errorMsg:    "saml.acs_url is required",
		},
		{
			name: "valid local auth config",
			config: &Config{
				Server: ServerConfig{
					Port: 8080,
				},
				Database: DatabaseConfig{
					Type:       "sqlite",
					SQLitePath: "/tmp/test.db",
				},
				Auth: AuthConfig{
					Type: "local",
				},
				Git: GitConfig{
					SyncInterval: 60,
				},
				Security: SecurityConfig{
					TokenTTL: 24,
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validate(tt.config)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestEnsureConfigDir(t *testing.T) {
	tempDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tempDir)
	defer os.Setenv("HOME", originalHome)

	err := EnsureConfigDir()
	require.NoError(t, err)

	configPath := filepath.Join(tempDir, DefaultConfigDir)
	info, err := os.Stat(configPath)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestConfig_EmbeddingsDisabled_NoValidation(t *testing.T) {
	// When embeddings is disabled, no API key validation should occur
	cfg := &Config{
		Server: ServerConfig{
			Port: 8080,
		},
		Database: DatabaseConfig{
			Type:       "sqlite",
			SQLitePath: "/tmp/test.db",
		},
		Git: GitConfig{
			SyncInterval: 60,
		},
		Security: SecurityConfig{
			TokenTTL: 24,
		},
		Embeddings: EmbeddingConfig{
			Enabled:    false,
			Provider:   "openai",
			APIKeyEnv:  "NONEXISTENT_API_KEY",
			Dimensions: 1536,
			BatchSize:  100,
		},
	}

	err := validate(cfg)
	assert.NoError(t, err)
}

func TestConfig_EmbeddingsEnabled_RequiresAPIKey(t *testing.T) {
	// When embeddings is enabled, API key must be present in env
	cfg := &Config{
		Server: ServerConfig{
			Port: 8080,
		},
		Database: DatabaseConfig{
			Type:       "sqlite",
			SQLitePath: "/tmp/test.db",
		},
		Git: GitConfig{
			SyncInterval: 60,
		},
		Security: SecurityConfig{
			TokenTTL: 24,
		},
		Embeddings: EmbeddingConfig{
			Enabled:    true,
			Provider:   "openai",
			APIKeyEnv:  "NONEXISTENT_API_KEY_FOR_TEST",
			Dimensions: 1536,
			BatchSize:  100,
		},
	}

	// Ensure the env var doesn't exist
	os.Unsetenv("NONEXISTENT_API_KEY_FOR_TEST")

	err := validate(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "API key not found")
}

func TestConfig_EmbeddingsEnabled_ValidWithAPIKey(t *testing.T) {
	// Set a test API key
	os.Setenv("TEST_EMBEDDING_API_KEY", "test-key-123")
	defer os.Unsetenv("TEST_EMBEDDING_API_KEY")

	cfg := &Config{
		Server: ServerConfig{
			Port: 8080,
		},
		Database: DatabaseConfig{
			Type:       "sqlite",
			SQLitePath: "/tmp/test.db",
		},
		Git: GitConfig{
			SyncInterval: 60,
		},
		Security: SecurityConfig{
			TokenTTL: 24,
		},
		Embeddings: EmbeddingConfig{
			Enabled:    true,
			Provider:   "openai",
			APIKeyEnv:  "TEST_EMBEDDING_API_KEY",
			Dimensions: 1536,
			BatchSize:  100,
		},
	}

	err := validate(cfg)
	assert.NoError(t, err)
}

func TestConfig_EmbeddingsEnabled_InvalidProvider(t *testing.T) {
	os.Setenv("TEST_EMBEDDING_API_KEY", "test-key-123")
	defer os.Unsetenv("TEST_EMBEDDING_API_KEY")

	cfg := &Config{
		Server: ServerConfig{
			Port: 8080,
		},
		Database: DatabaseConfig{
			Type:       "sqlite",
			SQLitePath: "/tmp/test.db",
		},
		Git: GitConfig{
			SyncInterval: 60,
		},
		Security: SecurityConfig{
			TokenTTL: 24,
		},
		Embeddings: EmbeddingConfig{
			Enabled:    true,
			Provider:   "invalid-provider",
			APIKeyEnv:  "TEST_EMBEDDING_API_KEY",
			Dimensions: 1536,
			BatchSize:  100,
		},
	}

	err := validate(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "embeddings.provider must be one of")
}

func TestConfig_EmbeddingsEnabled_InvalidDimensions(t *testing.T) {
	os.Setenv("TEST_EMBEDDING_API_KEY", "test-key-123")
	defer os.Unsetenv("TEST_EMBEDDING_API_KEY")

	cfg := &Config{
		Server: ServerConfig{
			Port: 8080,
		},
		Database: DatabaseConfig{
			Type:       "sqlite",
			SQLitePath: "/tmp/test.db",
		},
		Git: GitConfig{
			SyncInterval: 60,
		},
		Security: SecurityConfig{
			TokenTTL: 24,
		},
		Embeddings: EmbeddingConfig{
			Enabled:    true,
			Provider:   "openai",
			APIKeyEnv:  "TEST_EMBEDDING_API_KEY",
			Dimensions: 0,
			BatchSize:  100,
		},
	}

	err := validate(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "embeddings.dimensions must be at least 1")
}

func TestConfig_EmbeddingsEnabled_LocalProvider_NoAPIKeyRequired(t *testing.T) {
	// Local provider doesn't require API key
	cfg := &Config{
		Server: ServerConfig{
			Port: 8080,
		},
		Database: DatabaseConfig{
			Type:       "sqlite",
			SQLitePath: "/tmp/test.db",
		},
		Git: GitConfig{
			SyncInterval: 60,
		},
		Security: SecurityConfig{
			TokenTTL: 24,
		},
		Embeddings: EmbeddingConfig{
			Enabled:    true,
			Provider:   "local",
			APIKeyEnv:  "NONEXISTENT_KEY",
			Dimensions: 384,
			BatchSize:  50,
		},
	}

	err := validate(cfg)
	assert.NoError(t, err)
}

func TestDefaultConfig_EmbeddingsDefaults(t *testing.T) {
	cfg := DefaultConfig()

	// Verify embedding defaults
	assert.False(t, cfg.Embeddings.Enabled)
	assert.Equal(t, "openai", cfg.Embeddings.Provider)
	assert.Equal(t, "https://api.openai.com/v1", cfg.Embeddings.BaseURL)
	assert.Equal(t, "text-embedding-3-small", cfg.Embeddings.Model)
	assert.Equal(t, "OPENAI_API_KEY", cfg.Embeddings.APIKeyEnv)
	assert.Equal(t, 1536, cfg.Embeddings.Dimensions)
	assert.True(t, cfg.Embeddings.LazyIndex)
	assert.Equal(t, 100, cfg.Embeddings.BatchSize)
}

func TestIsValidEmbeddingProvider(t *testing.T) {
	assert.True(t, IsValidEmbeddingProvider("openai"))
	assert.True(t, IsValidEmbeddingProvider("azure"))
	assert.True(t, IsValidEmbeddingProvider("local"))
	assert.False(t, IsValidEmbeddingProvider("invalid"))
	assert.False(t, IsValidEmbeddingProvider(""))
}

func TestValidEmbeddingProviders(t *testing.T) {
	providers := ValidEmbeddingProviders()
	assert.Contains(t, providers, "openai")
	assert.Contains(t, providers, "azure")
	assert.Contains(t, providers, "local")
	assert.Len(t, providers, 3)
}
