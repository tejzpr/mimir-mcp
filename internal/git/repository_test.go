// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package git

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitRepository(t *testing.T) {
	tempDir := t.TempDir()
	repoPath := filepath.Join(tempDir, "test-repo")

	repo, err := InitRepository(repoPath)
	require.NoError(t, err)
	assert.NotNil(t, repo)
	assert.Equal(t, repoPath, repo.Path)

	// Verify .git directory exists
	gitDir := filepath.Join(repoPath, ".git")
	info, err := os.Stat(gitDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestOpenRepository(t *testing.T) {
	tempDir := t.TempDir()
	repoPath := filepath.Join(tempDir, "test-repo")

	// Initialize first
	_, err := InitRepository(repoPath)
	require.NoError(t, err)

	// Then open
	repo, err := OpenRepository(repoPath)
	require.NoError(t, err)
	assert.NotNil(t, repo)
	assert.Equal(t, repoPath, repo.Path)
}

func TestOpenRepository_NotExist(t *testing.T) {
	tempDir := t.TempDir()
	repoPath := filepath.Join(tempDir, "nonexistent")

	_, err := OpenRepository(repoPath)
	assert.Error(t, err)
}

func TestStatus(t *testing.T) {
	tempDir := t.TempDir()
	repoPath := filepath.Join(tempDir, "test-repo")

	repo, err := InitRepository(repoPath)
	require.NoError(t, err)

	status, err := repo.Status()
	require.NoError(t, err)
	assert.NotNil(t, status)
}

func TestIsClean(t *testing.T) {
	tempDir := t.TempDir()
	repoPath := filepath.Join(tempDir, "test-repo")

	repo, err := InitRepository(repoPath)
	require.NoError(t, err)

	// Repository should be clean initially
	clean, err := repo.IsClean()
	require.NoError(t, err)
	assert.True(t, clean)

	// Add a file
	testFile := filepath.Join(repoPath, "test.txt")
	err = os.WriteFile(testFile, []byte("test content"), 0644)
	require.NoError(t, err)

	// Now it should not be clean
	clean, err = repo.IsClean()
	require.NoError(t, err)
	assert.False(t, clean)
}

func TestHasChanges(t *testing.T) {
	tempDir := t.TempDir()
	repoPath := filepath.Join(tempDir, "test-repo")

	repo, err := InitRepository(repoPath)
	require.NoError(t, err)

	// Initially no changes
	hasChanges, err := repo.HasChanges()
	require.NoError(t, err)
	assert.False(t, hasChanges)

	// Add a file
	testFile := filepath.Join(repoPath, "test.txt")
	err = os.WriteFile(testFile, []byte("test content"), 0644)
	require.NoError(t, err)

	// Now has changes
	hasChanges, err = repo.HasChanges()
	require.NoError(t, err)
	assert.True(t, hasChanges)
}

func TestAddRemote(t *testing.T) {
	tempDir := t.TempDir()
	repoPath := filepath.Join(tempDir, "test-repo")

	repo, err := InitRepository(repoPath)
	require.NoError(t, err)

	// Add remote
	err = repo.AddRemote("origin", "https://github.com/test/repo.git")
	require.NoError(t, err)

	// Verify remote exists
	hasRemote := repo.HasRemote("origin")
	assert.True(t, hasRemote)

	// Get remote URL
	url, err := repo.GetRemoteURL("origin")
	require.NoError(t, err)
	assert.Equal(t, "https://github.com/test/repo.git", url)
}

func TestHasRemote(t *testing.T) {
	tempDir := t.TempDir()
	repoPath := filepath.Join(tempDir, "test-repo")

	repo, err := InitRepository(repoPath)
	require.NoError(t, err)

	// Initially no remote
	assert.False(t, repo.HasRemote("origin"))

	// Add remote
	err = repo.AddRemote("origin", "https://github.com/test/repo.git")
	require.NoError(t, err)

	// Now has remote
	assert.True(t, repo.HasRemote("origin"))
	assert.False(t, repo.HasRemote("upstream"))
}

func TestGetRemoteURL_NotExist(t *testing.T) {
	tempDir := t.TempDir()
	repoPath := filepath.Join(tempDir, "test-repo")

	repo, err := InitRepository(repoPath)
	require.NoError(t, err)

	_, err = repo.GetRemoteURL("origin")
	assert.Error(t, err)
}
