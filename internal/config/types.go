// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package config

// Config represents the complete application configuration
type Config struct {
	Server     ServerConfig     `mapstructure:"server"`
	Database   DatabaseConfig   `mapstructure:"database"`
	Auth       AuthConfig       `mapstructure:"auth"`
	SAML       SAMLConfig       `mapstructure:"saml"`
	Git        GitConfig        `mapstructure:"git"`
	Security   SecurityConfig   `mapstructure:"security"`
	Embeddings EmbeddingConfig  `mapstructure:"embeddings"`
}

// ServerConfig holds HTTP server configuration
type ServerConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
	TLS  struct {
		Enabled  bool   `mapstructure:"enabled"`
		CertFile string `mapstructure:"cert_file"`
		KeyFile  string `mapstructure:"key_file"`
	} `mapstructure:"tls"`
}

// DatabaseConfig holds database connection settings
type DatabaseConfig struct {
	Type        string `mapstructure:"type"` // "sqlite" or "postgres"
	SQLitePath  string `mapstructure:"sqlite_path"`
	PostgresDSN string `mapstructure:"postgres_dsn"`
}

// AuthConfig holds authentication type configuration
type AuthConfig struct {
	Type string `mapstructure:"type"` // "saml" or "local"
}

// SAMLConfig holds SAML authentication configuration
type SAMLConfig struct {
	EntityID     string `mapstructure:"entity_id"`
	ACSURL       string `mapstructure:"acs_url"`
	MetadataURL  string `mapstructure:"metadata_url"`
	IDPMetadata  string `mapstructure:"idp_metadata"`
	Certificate  string `mapstructure:"certificate"`
	PrivateKey   string `mapstructure:"private_key"`
	Provider     string `mapstructure:"provider"` // "duo" or "okta"
}

// GitConfig holds git-related configuration
type GitConfig struct {
	DefaultBranch string `mapstructure:"default_branch"`
	SyncInterval  int    `mapstructure:"sync_interval_minutes"` // Hourly sync interval
}

// SecurityConfig holds security-related settings
type SecurityConfig struct {
	EncryptionKey string `mapstructure:"encryption_key"` // For PAT encryption
	TokenTTL      int    `mapstructure:"token_ttl_hours"`
}

// EmbeddingConfig holds configuration for semantic search embeddings
type EmbeddingConfig struct {
	Enabled    bool   `mapstructure:"enabled"`              // Feature flag for embeddings
	Provider   string `mapstructure:"provider"`             // "openai", "azure", "local"
	BaseURL    string `mapstructure:"base_url"`             // API base URL
	Model      string `mapstructure:"model"`                // Model name (e.g., "text-embedding-3-small")
	APIKeyEnv  string `mapstructure:"api_key_env"`          // Environment variable name for API key
	Dimensions int    `mapstructure:"dimensions"`           // Vector dimensions (e.g., 1536)
	LazyIndex  bool   `mapstructure:"lazy_index"`           // Enable lazy indexing on first access
	BatchSize  int    `mapstructure:"batch_size"`           // Batch size for bulk embedding operations
}

// EmbeddingProviders defines valid embedding providers
const (
	EmbeddingProviderOpenAI = "openai"
	EmbeddingProviderAzure  = "azure"
	EmbeddingProviderLocal  = "local"
)

// ValidEmbeddingProviders returns all valid embedding provider values
func ValidEmbeddingProviders() []string {
	return []string{
		EmbeddingProviderOpenAI,
		EmbeddingProviderAzure,
		EmbeddingProviderLocal,
	}
}

// isValidType is a generic helper to check if a type is in a list of valid types
func isValidType(aType string, validTypes []string) bool {
	for _, valid := range validTypes {
		if aType == valid {
			return true
		}
	}
	return false
}

// IsValidEmbeddingProvider checks if a provider is valid
func IsValidEmbeddingProvider(provider string) bool {
	return isValidType(provider, ValidEmbeddingProviders())
}
