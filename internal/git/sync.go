// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package git

import (
	"fmt"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
)

// SyncStatus represents the status of a git sync operation
type SyncStatus struct {
	LastSync        time.Time
	LocalCommits    int
	RemoteCommits   int
	HasConflicts    bool
	ConflictFiles   []string
	SyncSuccessful  bool
	Error           string
}

// Push pushes commits to the remote repository
func (r *Repository) Push(pat string) error {
	if pat == "" {
		return fmt.Errorf("PAT token is required for push")
	}

	auth := &http.BasicAuth{
		Username: "git",
		Password: pat,
	}

	err := r.repo.Push(&git.PushOptions{
		RemoteName: "origin",
		Auth:       auth,
	})

	if err != nil {
		if err == git.NoErrAlreadyUpToDate {
			return nil // Not an error, just nothing to push
		}
		return fmt.Errorf("failed to push: %w", err)
	}

	return nil
}

// Pull pulls changes from the remote repository
func (r *Repository) Pull(pat string) error {
	if pat == "" {
		return fmt.Errorf("PAT token is required for pull")
	}

	auth := &http.BasicAuth{
		Username: "git",
		Password: pat,
	}

	worktree, err := r.repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	err = worktree.Pull(&git.PullOptions{
		RemoteName: "origin",
		Auth:       auth,
	})

	if err != nil {
		if err == git.NoErrAlreadyUpToDate {
			return nil // Not an error, just already up to date
		}
		return fmt.Errorf("failed to pull: %w", err)
	}

	return nil
}

// Fetch fetches changes from the remote without merging
func (r *Repository) Fetch(pat string) error {
	if pat == "" {
		return fmt.Errorf("PAT token is required for fetch")
	}

	auth := &http.BasicAuth{
		Username: "git",
		Password: pat,
	}

	err := r.repo.Fetch(&git.FetchOptions{
		RemoteName: "origin",
		Auth:       auth,
	})

	if err != nil {
		if err == git.NoErrAlreadyUpToDate {
			return nil
		}
		return fmt.Errorf("failed to fetch: %w", err)
	}

	return nil
}

// SyncStatus returns the current sync status
func (r *Repository) SyncStatus() (*SyncStatus, error) {
	status := &SyncStatus{
		LastSync:       time.Now(),
		SyncSuccessful: true,
	}

	// Get local HEAD
	localRef, err := r.repo.Head()
	if err != nil {
		status.SyncSuccessful = false
		status.Error = fmt.Sprintf("failed to get local HEAD: %v", err)
		return status, nil
	}

	// Try to get remote HEAD
	remote, err := r.repo.Remote("origin")
	if err != nil {
		// No remote configured
		return status, nil
	}

	// Get remote refs
	refs, err := remote.List(&git.ListOptions{})
	if err != nil {
		status.SyncSuccessful = false
		status.Error = fmt.Sprintf("failed to list remote refs: %v", err)
		return status, nil
	}

	// Find origin/main or origin/master
	var remoteRef *plumbing.Reference
	for _, ref := range refs {
		if ref.Name().String() == "refs/heads/main" || ref.Name().String() == "refs/heads/master" {
			remoteRef = ref
			break
		}
	}

	if remoteRef == nil {
		return status, nil
	}

	// Count commits ahead/behind
	localCommit, err := r.repo.CommitObject(localRef.Hash())
	if err != nil {
		status.SyncSuccessful = false
		status.Error = fmt.Sprintf("failed to get local commit: %v", err)
		return status, nil
	}

	remoteCommit, err := r.repo.CommitObject(remoteRef.Hash())
	if err != nil {
		status.SyncSuccessful = false
		status.Error = fmt.Sprintf("failed to get remote commit: %v", err)
		return status, nil
	}

	// Check if local is ahead
	isAncestor, err := localCommit.IsAncestor(remoteCommit)
	if err == nil && !isAncestor {
		status.LocalCommits++ // Simplified count
	}

	// Check if remote is ahead
	isAncestor, err = remoteCommit.IsAncestor(localCommit)
	if err == nil && !isAncestor {
		status.RemoteCommits++ // Simplified count
	}

	return status, nil
}

// Sync performs a full sync (pull then push) with conflict resolution
func (r *Repository) Sync(pat string, forceLastWriteWins bool) (*SyncStatus, error) {
	status := &SyncStatus{
		LastSync:       time.Now(),
		SyncSuccessful: false,
	}

	// First, check if we have a remote
	if !r.HasRemote("origin") {
		status.SyncSuccessful = true
		status.Error = "No remote configured, skipping sync"
		return status, nil
	}

	// Fetch first to check for conflicts
	err := r.Fetch(pat)
	if err != nil {
		status.Error = fmt.Sprintf("fetch failed: %v", err)
		return status, fmt.Errorf("fetch failed: %w", err)
	}

	// Try to pull
	err = r.Pull(pat)
	if err != nil {
		// Check if it's a merge conflict
		if isConflictError(err) {
			status.HasConflicts = true
			
			if forceLastWriteWins {
				// Resolve conflicts by keeping our version
				err = r.resolveConflictsLastWriteWins()
				if err != nil {
					status.Error = fmt.Sprintf("conflict resolution failed: %v", err)
					return status, fmt.Errorf("conflict resolution failed: %w", err)
				}
			} else {
				status.Error = "merge conflicts detected, manual resolution required"
				return status, fmt.Errorf("merge conflicts detected")
			}
		} else {
			status.Error = fmt.Sprintf("pull failed: %v", err)
			return status, fmt.Errorf("pull failed: %w", err)
		}
	}

	// Push our changes
	err = r.Push(pat)
	if err != nil {
		status.Error = fmt.Sprintf("push failed: %v", err)
		return status, fmt.Errorf("push failed: %w", err)
	}

	status.SyncSuccessful = true
	return status, nil
}

// isConflictError checks if the error is a merge conflict
func isConflictError(err error) bool {
	if err == nil {
		return false
	}
	// go-git returns specific errors for conflicts
	return err.Error() == "merge conflict"
}

// resolveConflictsLastWriteWins resolves conflicts by keeping the local version
func (r *Repository) resolveConflictsLastWriteWins() error {
	worktree, err := r.repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	// Get status to find conflicted files
	status, err := worktree.Status()
	if err != nil {
		return fmt.Errorf("failed to get status: %w", err)
	}

	// Add all files (choosing ours)
	for file := range status {
		_, err := worktree.Add(file)
		if err != nil {
			return fmt.Errorf("failed to add file %s: %w", file, err)
		}
	}

	// Commit the resolution
	opts := DefaultCommitOptions()
	opts.Message = "chore: Resolve merge conflicts (last-write-wins)"
	opts.AllowEmpty = true

	return r.AddAndCommit([]string{"."}, opts)
}

// SyncV2Options configures the v2 sync operation
type SyncV2Options struct {
	PAT               string
	ForceLastWriteWins bool
	IncludePerUserDB  bool
	OnBeforeSync      func() error // Called before sync (close DB)
	OnAfterSync       func() error // Called after sync (reopen DB)
}

// SyncV2 performs a full sync with support for per-user database
// This is the v2 sync that includes the .medha/medha.db in git operations
func (r *Repository) SyncV2(opts SyncV2Options) (*SyncStatus, error) {
	status := &SyncStatus{
		LastSync:       time.Now(),
		SyncSuccessful: false,
	}

	// First, check if we have a remote
	if !r.HasRemote("origin") {
		status.SyncSuccessful = true
		status.Error = "No remote configured, skipping sync"
		return status, nil
	}

	// Call before sync hook (close DB)
	if opts.OnBeforeSync != nil {
		if err := opts.OnBeforeSync(); err != nil {
			status.Error = fmt.Sprintf("pre-sync hook failed: %v", err)
			return status, fmt.Errorf("pre-sync hook failed: %w", err)
		}
	}

	// Stage the per-user database if requested
	if opts.IncludePerUserDB {
		if err := r.StagePerUserDB(); err != nil {
			// Not a fatal error if file doesn't exist
			if err.Error() != "no changes to stage" {
				status.Error = fmt.Sprintf("failed to stage per-user DB: %v", err)
			}
		}
	}

	// Fetch first to check for conflicts
	err := r.Fetch(opts.PAT)
	if err != nil {
		status.Error = fmt.Sprintf("fetch failed: %v", err)
		if opts.OnAfterSync != nil {
			opts.OnAfterSync() //nolint:errcheck
		}
		return status, fmt.Errorf("fetch failed: %w", err)
	}

	// Try to pull
	err = r.Pull(opts.PAT)
	if err != nil {
		if isConflictError(err) {
			status.HasConflicts = true
			
			if opts.ForceLastWriteWins {
				err = r.resolveConflictsLastWriteWins()
				if err != nil {
					status.Error = fmt.Sprintf("conflict resolution failed: %v", err)
					if opts.OnAfterSync != nil {
						opts.OnAfterSync() //nolint:errcheck
					}
					return status, fmt.Errorf("conflict resolution failed: %w", err)
				}
			} else {
				status.Error = "merge conflicts detected, manual resolution required"
				if opts.OnAfterSync != nil {
					opts.OnAfterSync() //nolint:errcheck
				}
				return status, fmt.Errorf("merge conflicts detected")
			}
		} else {
			status.Error = fmt.Sprintf("pull failed: %v", err)
			if opts.OnAfterSync != nil {
				opts.OnAfterSync() //nolint:errcheck
			}
			return status, fmt.Errorf("pull failed: %w", err)
		}
	}

	// Push our changes
	err = r.Push(opts.PAT)
	if err != nil {
		status.Error = fmt.Sprintf("push failed: %v", err)
		if opts.OnAfterSync != nil {
			opts.OnAfterSync() //nolint:errcheck
		}
		return status, fmt.Errorf("push failed: %w", err)
	}

	// Call after sync hook (reopen DB)
	if opts.OnAfterSync != nil {
		if err := opts.OnAfterSync(); err != nil {
			status.SyncSuccessful = true
			status.Error = fmt.Sprintf("post-sync hook failed (sync was successful): %v", err)
			return status, nil
		}
	}

	status.SyncSuccessful = true
	return status, nil
}

// StagePerUserDB stages the per-user database file for commit
func (r *Repository) StagePerUserDB() error {
	worktree, err := r.repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	dbPath := ".medha/medha.db"
	_, err = worktree.Add(dbPath)
	if err != nil {
		// Check if it's just because the file doesn't exist
		return fmt.Errorf("failed to stage per-user DB: %w", err)
	}

	return nil
}

// HasPerUserDB checks if the per-user database exists in the repository
func (r *Repository) HasPerUserDB() bool {
	worktree, err := r.repo.Worktree()
	if err != nil {
		return false
	}

	fs := worktree.Filesystem
	_, err = fs.Stat(".medha/medha.db")
	return err == nil
}
