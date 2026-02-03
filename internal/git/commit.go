// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package git

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// CommitOptions holds options for creating commits
type CommitOptions struct {
	Author    string
	Email     string
	Message   string
	AllowEmpty bool
}

// DefaultCommitOptions returns default commit options
func DefaultCommitOptions() *CommitOptions {
	return &CommitOptions{
		Author:    "Mimir",
		Email:     "memory@mimir.local",
		AllowEmpty: false,
	}
}

// CommitFile commits a single file to the repository
func (r *Repository) CommitFile(filePath, message string) error {
	opts := DefaultCommitOptions()
	opts.Message = message
	return r.AddAndCommit([]string{filePath}, opts)
}

// AddAndCommit adds files and commits them
func (r *Repository) AddAndCommit(files []string, opts *CommitOptions) error {
	if opts == nil {
		opts = DefaultCommitOptions()
	}

	worktree, err := r.repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	// Add files to staging
	for _, file := range files {
		// Convert to relative path
		relPath, err := filepath.Rel(r.Path, file)
		if err != nil {
			// If file is already relative or conversion fails, use as-is
			relPath = file
		}

		_, err = worktree.Add(relPath)
		if err != nil {
			return fmt.Errorf("failed to add file %s: %w", relPath, err)
		}
	}

	// Check if there are changes to commit
	status, err := worktree.Status()
	if err != nil {
		return fmt.Errorf("failed to get status: %w", err)
	}

	if status.IsClean() && !opts.AllowEmpty {
		return fmt.Errorf("no changes to commit")
	}

	// Create commit
	_, err = worktree.Commit(opts.Message, &git.CommitOptions{
		Author: &object.Signature{
			Name:  opts.Author,
			Email: opts.Email,
			When:  time.Now(),
		},
		AllowEmptyCommits: opts.AllowEmpty,
	})
	if err != nil {
		return fmt.Errorf("failed to commit: %w", err)
	}

	return nil
}

// CommitAll commits all changes in the repository
func (r *Repository) CommitAll(message string) error {
	opts := DefaultCommitOptions()
	opts.Message = message

	worktree, err := r.repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	// Add all changes
	err = worktree.AddWithOptions(&git.AddOptions{
		All: true,
	})
	if err != nil {
		return fmt.Errorf("failed to add all changes: %w", err)
	}

	// Create commit
	_, err = worktree.Commit(message, &git.CommitOptions{
		Author: &object.Signature{
			Name:  opts.Author,
			Email: opts.Email,
			When:  time.Now(),
		},
	})
	if err != nil {
		return fmt.Errorf("failed to commit: %w", err)
	}

	return nil
}

// GetCommitHistory returns the commit history
func (r *Repository) GetCommitHistory(maxCount int) ([]*object.Commit, error) {
	ref, err := r.repo.Head()
	if err != nil {
		return nil, fmt.Errorf("failed to get HEAD: %w", err)
	}

	commitIter, err := r.repo.Log(&git.LogOptions{
		From: ref.Hash(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get commit log: %w", err)
	}

	var commits []*object.Commit
	count := 0
	err = commitIter.ForEach(func(c *object.Commit) error {
		if maxCount > 0 && count >= maxCount {
			return fmt.Errorf("stop iteration")
		}
		commits = append(commits, c)
		count++
		return nil
	})

	if err != nil && err.Error() != "stop iteration" {
		return nil, fmt.Errorf("failed to iterate commits: %w", err)
	}

	return commits, nil
}

// GetLastCommit returns the most recent commit
func (r *Repository) GetLastCommit() (*object.Commit, error) {
	commits, err := r.GetCommitHistory(1)
	if err != nil {
		return nil, err
	}
	if len(commits) == 0 {
		return nil, fmt.Errorf("no commits found")
	}
	return commits[0], nil
}

// CommitMessageFormats provides standard commit message formats
type CommitMessageFormats struct{}

// CreateMemory returns a commit message for creating a memory
func (CommitMessageFormats) CreateMemory(slug string) string {
	return fmt.Sprintf("feat: Create memory '%s'", slug)
}

// UpdateMemory returns a commit message for updating a memory
func (CommitMessageFormats) UpdateMemory(slug string) string {
	return fmt.Sprintf("update: Modify memory '%s'", slug)
}

// Associate returns a commit message for creating an association
func (CommitMessageFormats) Associate(sourceSlug, targetSlug, associationType string) string {
	return fmt.Sprintf("associate: Link '%s' -> '%s' (%s)", sourceSlug, targetSlug, associationType)
}

// ArchiveMemory returns a commit message for soft deleting a memory
func (CommitMessageFormats) ArchiveMemory(slug string) string {
	return fmt.Sprintf("archive: Soft delete memory '%s'", slug)
}

// InitialCommit returns a commit message for repository initialization
func (CommitMessageFormats) InitialCommit() string {
	return "chore: Initialize Mimir repository"
}

// ClearSuperseded returns a commit message for clearing a superseded status
func (CommitMessageFormats) ClearSuperseded(slug string) string {
	return fmt.Sprintf("update: Clear superseded status from '%s'", slug)
}
