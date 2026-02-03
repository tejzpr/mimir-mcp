// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package git

import (
	"fmt"
	"os"
	"path/filepath"
)

// SetupConfig holds configuration for repository setup
type SetupConfig struct {
	BaseStorePath string // Base path for all repositories (e.g., ~/.mimir/store)
	Username      string // Username for deterministic repo naming (whoami for local, SAML username for remote)
	RepoURL       string // Optional: URL of existing repository
	PAT           string // Optional: Personal Access Token for remote operations
	LocalOnly     bool   // If true, no remote configuration
}

// SetupResult contains the result of repository setup
type SetupResult struct {
	RepoID     string // Username-based identifier (deterministic)
	RepoName   string
	RepoPath   string
	RepoURL    string
	Repository *Repository
}

// SetupUserRepository creates and initializes a user's git repository
func SetupUserRepository(cfg *SetupConfig) (*SetupResult, error) {
	// Use username for deterministic repository naming
	// This ensures the same folder is used across restarts
	if cfg.Username == "" {
		return nil, fmt.Errorf("username is required for repository setup")
	}
	repoName := fmt.Sprintf("mimir-%s", cfg.Username)
	repoPath := filepath.Join(cfg.BaseStorePath, repoName)

	// Check if repo already exists
	if _, err := os.Stat(repoPath); err == nil {
		return nil, fmt.Errorf("repository already exists at %s", repoPath)
	}

	var repo *Repository
	var err error

	// If RepoURL is provided and PAT is available, clone from remote
	if cfg.RepoURL != "" && cfg.PAT != "" && !cfg.LocalOnly {
		repo, err = Clone(cfg.RepoURL, cfg.PAT, repoPath)
		if err != nil {
			return nil, fmt.Errorf("failed to clone repository: %w", err)
		}
	} else {
		// Initialize new local repository
		repo, err = InitRepository(repoPath)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize repository: %w", err)
		}
	}

	// Create initial folder structure
	err = createInitialStructure(repoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create initial structure: %w", err)
	}

	// Create initial commit if this is a new repository
	if cfg.RepoURL == "" || cfg.LocalOnly {
		readme := filepath.Join(repoPath, "README.md")
		readmeContent := fmt.Sprintf("# Mimir\n\nOwner: %s\n\nThis repository contains your personal memory storage for Mimir MCP.\n", cfg.Username)
		err = os.WriteFile(readme, []byte(readmeContent), 0644)
		if err != nil {
			return nil, fmt.Errorf("failed to create README: %w", err)
		}

		msgFormat := CommitMessageFormats{}
		err = repo.CommitFile(readme, msgFormat.InitialCommit())
		if err != nil {
			return nil, fmt.Errorf("failed to create initial commit: %w", err)
		}
	}

	// Add remote if URL is provided and not local-only
	if cfg.RepoURL != "" && !cfg.LocalOnly && !repo.HasRemote("origin") {
		err = repo.AddRemote("origin", cfg.RepoURL)
		if err != nil {
			return nil, fmt.Errorf("failed to add remote: %w", err)
		}
	}

	result := &SetupResult{
		RepoID:     cfg.Username,
		RepoName:   repoName,
		RepoPath:   repoPath,
		RepoURL:    cfg.RepoURL,
		Repository: repo,
	}

	return result, nil
}

// createInitialStructure creates the initial folder structure
func createInitialStructure(repoPath string) error {
	// Create directory structure based on current year/month
	// This will be used for organizing memories
	folders := []string{
		"archive",                  // For soft-deleted memories
		"tags",                     // For tag-based organization
		filepath.Join("tags", "meetings"),
		filepath.Join("tags", "projects"),
		filepath.Join("tags", "notes"),
		filepath.Join("tags", "research"),
	}

	for _, folder := range folders {
		fullPath := filepath.Join(repoPath, folder)
		err := os.MkdirAll(fullPath, 0755)
		if err != nil {
			return fmt.Errorf("failed to create folder %s: %w", folder, err)
		}

		// Create .gitkeep to ensure empty directories are tracked
		gitkeep := filepath.Join(fullPath, ".gitkeep")
		err = os.WriteFile(gitkeep, []byte(""), 0644)
		if err != nil {
			return fmt.Errorf("failed to create .gitkeep in %s: %w", folder, err)
		}
	}

	return nil
}

// EnsureStorePath ensures the base store path exists
func EnsureStorePath(basePath string) error {
	if err := os.MkdirAll(basePath, 0755); err != nil {
		return fmt.Errorf("failed to create store path: %w", err)
	}
	return nil
}

// GetUserRepositoryPath returns the expected path for a user's repository
func GetUserRepositoryPath(basePath, username string) string {
	return filepath.Join(basePath, fmt.Sprintf("mimir-%s", username))
}
