// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

const (
	// DefaultConfigDir is the default configuration directory
	DefaultConfigDir = ".mimir/configs"
	// DefaultConfigFile is the default configuration filename
	DefaultConfigFile = "config.json"
)

// Load reads configuration from ~/.mimir/configs/config.json
func Load() (*Config, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get user home directory: %w", err)
	}

	configPath := filepath.Join(homeDir, DefaultConfigDir)

	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("json")
	v.AddConfigPath(configPath)

	// Set defaults
	setDefaults(v)

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			// Config file not found, use defaults
			return loadFromDefaults(v)
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Validate configuration
	if err := validate(&cfg); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &cfg, nil
}

// LoadFromPath loads configuration from a specific path
func LoadFromPath(path string) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("json")

	setDefaults(v)

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if err := validate(&cfg); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &cfg, nil
}

// setDefaults sets default configuration values
func setDefaults(v *viper.Viper) {
	// Server defaults
	v.SetDefault("server.host", "localhost")
	v.SetDefault("server.port", 8080)
	v.SetDefault("server.tls.enabled", false)

	// Auth defaults
	v.SetDefault("auth.type", "local")

	// Database defaults
	v.SetDefault("database.type", "sqlite")
	homeDir, _ := os.UserHomeDir()
	v.SetDefault("database.sqlite_path", filepath.Join(homeDir, ".mimir/db/mimir.db"))

	// Git defaults
	v.SetDefault("git.default_branch", "main")
	v.SetDefault("git.sync_interval_minutes", 60)

	// Security defaults
	v.SetDefault("security.token_ttl_hours", 24)
}

// loadFromDefaults creates a config from default values
func loadFromDefaults(v *viper.Viper) (*Config, error) {
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal default config: %w", err)
	}
	return &cfg, nil
}

// validate checks if the configuration is valid
func validate(cfg *Config) error {
	// Validate auth type
	if cfg.Auth.Type != "" && cfg.Auth.Type != "saml" && cfg.Auth.Type != "local" {
		return fmt.Errorf("auth.type must be 'saml' or 'local', got '%s'", cfg.Auth.Type)
	}

	// Default to local if not specified
	if cfg.Auth.Type == "" {
		cfg.Auth.Type = "local"
	}

	// Validate database type
	if cfg.Database.Type != "sqlite" && cfg.Database.Type != "postgres" {
		return fmt.Errorf("database.type must be 'sqlite' or 'postgres', got '%s'", cfg.Database.Type)
	}

	// Validate database connection info
	if cfg.Database.Type == "sqlite" && cfg.Database.SQLitePath == "" {
		return fmt.Errorf("database.sqlite_path is required when type is 'sqlite'")
	}
	if cfg.Database.Type == "postgres" && cfg.Database.PostgresDSN == "" {
		return fmt.Errorf("database.postgres_dsn is required when type is 'postgres'")
	}

	// Validate SAML config only if auth type is saml
	if cfg.Auth.Type == "saml" {
		if cfg.SAML.EntityID == "" {
			return fmt.Errorf("saml.entity_id is required when auth.type='saml'")
		}
		if cfg.SAML.ACSURL == "" {
			return fmt.Errorf("saml.acs_url is required when auth.type='saml'")
		}
		if cfg.SAML.IDPMetadata == "" {
			return fmt.Errorf("saml.idp_metadata is required when auth.type='saml'")
		}
	}

	// Validate server port
	if cfg.Server.Port < 1 || cfg.Server.Port > 65535 {
		return fmt.Errorf("server.port must be between 1 and 65535, got %d", cfg.Server.Port)
	}

	// Validate git sync interval
	if cfg.Git.SyncInterval < 1 {
		return fmt.Errorf("git.sync_interval_minutes must be at least 1, got %d", cfg.Git.SyncInterval)
	}

	// Validate security settings
	if cfg.Security.TokenTTL < 1 {
		return fmt.Errorf("security.token_ttl_hours must be at least 1, got %d", cfg.Security.TokenTTL)
	}

	return nil
}

// EnsureConfigDir creates the configuration directory if it doesn't exist
func EnsureConfigDir() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get user home directory: %w", err)
	}

	configPath := filepath.Join(homeDir, DefaultConfigDir)
	if err := os.MkdirAll(configPath, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	return nil
}

// DefaultConfig returns a configuration with sensible defaults
func DefaultConfig() *Config {
	homeDir, _ := os.UserHomeDir()

	return &Config{
		Server: ServerConfig{
			Host: "localhost",
			Port: 8080,
			TLS: struct {
				Enabled  bool   `mapstructure:"enabled"`
				CertFile string `mapstructure:"cert_file"`
				KeyFile  string `mapstructure:"key_file"`
			}{
				Enabled: false,
			},
		},
		Database: DatabaseConfig{
			Type:       "sqlite",
			SQLitePath: filepath.Join(homeDir, ".mimir/db/mimir.db"),
		},
		Auth: AuthConfig{
			Type: "local",
		},
		Git: GitConfig{
			DefaultBranch: "main",
			SyncInterval:  60,
		},
		Security: SecurityConfig{
			TokenTTL: 24,
		},
	}
}
