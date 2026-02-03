// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package git

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
)

// Repository wraps go-git repository operations
type Repository struct {
	Path string
	repo *git.Repository
}

// InitRepository initializes a new git repository
func InitRepository(path string) (*Repository, error) {
	// Ensure directory exists
	if err := os.MkdirAll(path, 0755); err != nil {
		return nil, fmt.Errorf("failed to create repository directory: %w", err)
	}

	// Initialize git repository
	repo, err := git.PlainInit(path, false)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize git repository: %w", err)
	}

	return &Repository{
		Path: path,
		repo: repo,
	}, nil
}

// OpenRepository opens an existing git repository
func OpenRepository(path string) (*Repository, error) {
	repo, err := git.PlainOpen(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open git repository: %w", err)
	}

	return &Repository{
		Path: path,
		repo: repo,
	}, nil
}

// Clone clones a repository from a URL with PAT authentication
func Clone(url, pat, path string) (*Repository, error) {
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("failed to create parent directory: %w", err)
	}

	// Set up authentication
	auth := &http.BasicAuth{
		Username: "git", // Can be anything except empty string
		Password: pat,
	}

	// Clone repository
	repo, err := git.PlainClone(path, false, &git.CloneOptions{
		URL:      url,
		Auth:     auth,
		Progress: os.Stdout,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to clone repository: %w", err)
	}

	return &Repository{
		Path: path,
		repo: repo,
	}, nil
}

// Status returns the status of the repository
func (r *Repository) Status() (git.Status, error) {
	worktree, err := r.repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("failed to get worktree: %w", err)
	}

	status, err := worktree.Status()
	if err != nil {
		return nil, fmt.Errorf("failed to get status: %w", err)
	}

	return status, nil
}

// IsClean returns true if the repository has no uncommitted changes
func (r *Repository) IsClean() (bool, error) {
	status, err := r.Status()
	if err != nil {
		return false, err
	}
	return status.IsClean(), nil
}

// HasChanges returns true if there are uncommitted changes
func (r *Repository) HasChanges() (bool, error) {
	clean, err := r.IsClean()
	if err != nil {
		return false, err
	}
	return !clean, nil
}

// GetHeadCommit returns the current HEAD commit
func (r *Repository) GetHeadCommit() (*plumbing.Reference, error) {
	ref, err := r.repo.Head()
	if err != nil {
		return nil, fmt.Errorf("failed to get HEAD: %w", err)
	}
	return ref, nil
}

// AddRemote adds a remote to the repository
func (r *Repository) AddRemote(name, url string) error {
	_, err := r.repo.CreateRemote(&config.RemoteConfig{
		Name: name,
		URLs: []string{url},
	})
	if err != nil {
		return fmt.Errorf("failed to add remote: %w", err)
	}
	return nil
}

// GetRemoteURL returns the URL of a remote
func (r *Repository) GetRemoteURL(name string) (string, error) {
	remote, err := r.repo.Remote(name)
	if err != nil {
		return "", fmt.Errorf("failed to get remote: %w", err)
	}

	cfg := remote.Config()
	if len(cfg.URLs) == 0 {
		return "", fmt.Errorf("remote has no URLs")
	}

	return cfg.URLs[0], nil
}

// HasRemote checks if a remote exists
func (r *Repository) HasRemote(name string) bool {
	_, err := r.repo.Remote(name)
	return err == nil
}

// GetRepo returns the underlying go-git repository
func (r *Repository) GetRepo() *git.Repository {
	return r.repo
}
