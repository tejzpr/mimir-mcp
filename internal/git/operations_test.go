// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package git

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommitFile(t *testing.T) {
	tempDir := t.TempDir()
	repoPath := filepath.Join(tempDir, "test-repo")

	repo, err := InitRepository(repoPath)
	require.NoError(t, err)

	// Create a test file
	testFile := filepath.Join(repoPath, "test.txt")
	err = os.WriteFile(testFile, []byte("test content"), 0644)
	require.NoError(t, err)

	// Commit the file
	err = repo.CommitFile(testFile, "Add test file")
	require.NoError(t, err)

	// Verify commit exists
	commit, err := repo.GetLastCommit()
	require.NoError(t, err)
	assert.Equal(t, "Add test file", commit.Message)
}

func TestAddAndCommit(t *testing.T) {
	tempDir := t.TempDir()
	repoPath := filepath.Join(tempDir, "test-repo")

	repo, err := InitRepository(repoPath)
	require.NoError(t, err)

	// Create test files
	file1 := filepath.Join(repoPath, "file1.txt")
	file2 := filepath.Join(repoPath, "file2.txt")
	err = os.WriteFile(file1, []byte("content 1"), 0644)
	require.NoError(t, err)
	err = os.WriteFile(file2, []byte("content 2"), 0644)
	require.NoError(t, err)

	// Commit both files
	opts := DefaultCommitOptions()
	opts.Message = "Add two files"
	err = repo.AddAndCommit([]string{file1, file2}, opts)
	require.NoError(t, err)

	// Verify commit
	commit, err := repo.GetLastCommit()
	require.NoError(t, err)
	assert.Equal(t, "Add two files", commit.Message)
	assert.Equal(t, "Mimir", commit.Author.Name)
}

func TestCommitAll(t *testing.T) {
	tempDir := t.TempDir()
	repoPath := filepath.Join(tempDir, "test-repo")

	repo, err := InitRepository(repoPath)
	require.NoError(t, err)

	// Create multiple test files
	for i := 1; i <= 3; i++ {
		filename := filepath.Join(repoPath, filepath.Base(t.Name())+".txt")
		err = os.WriteFile(filename, []byte("content"), 0644)
		require.NoError(t, err)
	}

	// Commit all
	err = repo.CommitAll("Add all files")
	require.NoError(t, err)

	// Verify commit
	commit, err := repo.GetLastCommit()
	require.NoError(t, err)
	assert.Equal(t, "Add all files", commit.Message)
}

func TestCommitFile_NoChanges(t *testing.T) {
	tempDir := t.TempDir()
	repoPath := filepath.Join(tempDir, "test-repo")

	repo, err := InitRepository(repoPath)
	require.NoError(t, err)

	// Try to commit without any changes
	testFile := filepath.Join(repoPath, "nonexistent.txt")
	err = repo.CommitFile(testFile, "Should fail")
	assert.Error(t, err)
}

func TestGetCommitHistory(t *testing.T) {
	tempDir := t.TempDir()
	repoPath := filepath.Join(tempDir, "test-repo")

	repo, err := InitRepository(repoPath)
	require.NoError(t, err)

	// Create and commit multiple files
	for i := 1; i <= 3; i++ {
		testFile := filepath.Join(repoPath, fmt.Sprintf("file%d.txt", i))
		err = os.WriteFile(testFile, []byte("content"), 0644)
		require.NoError(t, err)
		err = repo.CommitFile(testFile, "Commit message")
		require.NoError(t, err)
	}

	// Get history
	commits, err := repo.GetCommitHistory(10)
	require.NoError(t, err)
	assert.Equal(t, 3, len(commits))
}

func TestGetCommitHistory_Limit(t *testing.T) {
	tempDir := t.TempDir()
	repoPath := filepath.Join(tempDir, "test-repo")

	repo, err := InitRepository(repoPath)
	require.NoError(t, err)

	// Create multiple commits
	for i := 1; i <= 5; i++ {
		testFile := filepath.Join(repoPath, fmt.Sprintf("file%d.txt", i))
		err = os.WriteFile(testFile, []byte("content"), 0644)
		require.NoError(t, err)
		err = repo.CommitFile(testFile, "Commit message")
		require.NoError(t, err)
	}

	// Get limited history
	commits, err := repo.GetCommitHistory(3)
	require.NoError(t, err)
	assert.Equal(t, 3, len(commits))
}

func TestCommitMessageFormats(t *testing.T) {
	msgFormat := CommitMessageFormats{}

	tests := []struct {
		name     string
		result   string
		expected string
	}{
		{
			name:     "create memory",
			result:   msgFormat.CreateMemory("test-memory"),
			expected: "feat: Create memory 'test-memory'",
		},
		{
			name:     "update memory",
			result:   msgFormat.UpdateMemory("test-memory"),
			expected: "update: Modify memory 'test-memory'",
		},
		{
			name:     "associate",
			result:   msgFormat.Associate("mem1", "mem2", "related_to"),
			expected: "associate: Link 'mem1' -> 'mem2' (related_to)",
		},
		{
			name:     "archive",
			result:   msgFormat.ArchiveMemory("test-memory"),
			expected: "archive: Soft delete memory 'test-memory'",
		},
		{
			name:     "initial commit",
			result:   msgFormat.InitialCommit(),
			expected: "chore: Initialize Mimir repository",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.result)
		})
	}
}

func TestSetupUserRepository(t *testing.T) {
	tempDir := t.TempDir()

	cfg := &SetupConfig{
		BaseStorePath: tempDir,
		Username:      "testuser",
		LocalOnly:     true,
	}

	result, err := SetupUserRepository(cfg)
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "testuser", result.RepoID)
	assert.Equal(t, "mimir-testuser", result.RepoName)
	assert.NotEmpty(t, result.RepoPath)
	assert.NotNil(t, result.Repository)

	// Verify repository exists
	info, err := os.Stat(result.RepoPath)
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	// Verify initial structure
	archivePath := filepath.Join(result.RepoPath, "archive")
	info, err = os.Stat(archivePath)
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	tagsPath := filepath.Join(result.RepoPath, "tags")
	info, err = os.Stat(tagsPath)
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	// Verify README exists
	readmePath := filepath.Join(result.RepoPath, "README.md")
	info, err = os.Stat(readmePath)
	require.NoError(t, err)
	assert.False(t, info.IsDir())

	// Verify initial commit
	commits, err := result.Repository.GetCommitHistory(10)
	require.NoError(t, err)
	assert.Equal(t, 1, len(commits))
	assert.Contains(t, commits[0].Message, "Initialize")
}

// TestSetupUserRepository_LocalUsername tests repository setup with local username (whoami style)
func TestSetupUserRepository_LocalUsername(t *testing.T) {
	tempDir := t.TempDir()

	// Simulate local username from whoami (simple, no special characters)
	localUsername := "localuser"

	cfg := &SetupConfig{
		BaseStorePath: tempDir,
		Username:      localUsername,
		LocalOnly:     true,
	}

	result, err := SetupUserRepository(cfg)
	require.NoError(t, err)

	// Verify deterministic naming
	assert.Equal(t, localUsername, result.RepoID)
	assert.Equal(t, "mimir-localuser", result.RepoName)
	expectedPath := filepath.Join(tempDir, "mimir-localuser")
	assert.Equal(t, expectedPath, result.RepoPath)

	// Verify folder exists
	info, err := os.Stat(result.RepoPath)
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	// Verify README contains owner info
	readmeContent, err := os.ReadFile(filepath.Join(result.RepoPath, "README.md"))
	require.NoError(t, err)
	assert.Contains(t, string(readmeContent), "Owner: localuser")
}

// TestSetupUserRepository_SAMLUsername tests repository setup with SAML username (email style)
func TestSetupUserRepository_SAMLUsername(t *testing.T) {
	tempDir := t.TempDir()

	// Simulate SAML username (email-style with @ and .)
	samlUsername := "john.doe@company.com"

	cfg := &SetupConfig{
		BaseStorePath: tempDir,
		Username:      samlUsername,
		LocalOnly:     true,
	}

	result, err := SetupUserRepository(cfg)
	require.NoError(t, err)

	// Verify deterministic naming with email
	assert.Equal(t, samlUsername, result.RepoID)
	assert.Equal(t, "mimir-john.doe@company.com", result.RepoName)
	expectedPath := filepath.Join(tempDir, "mimir-john.doe@company.com")
	assert.Equal(t, expectedPath, result.RepoPath)

	// Verify folder exists
	info, err := os.Stat(result.RepoPath)
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	// Verify README contains owner info
	readmeContent, err := os.ReadFile(filepath.Join(result.RepoPath, "README.md"))
	require.NoError(t, err)
	assert.Contains(t, string(readmeContent), "Owner: john.doe@company.com")
}

// TestSetupUserRepository_MissingUsername tests that setup fails without username
func TestSetupUserRepository_MissingUsername(t *testing.T) {
	tempDir := t.TempDir()

	cfg := &SetupConfig{
		BaseStorePath: tempDir,
		Username:      "", // Empty username
		LocalOnly:     true,
	}

	result, err := SetupUserRepository(cfg)
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "username is required")
}

// TestSetupUserRepository_AlreadyExists tests that setup fails if repo already exists
func TestSetupUserRepository_AlreadyExists(t *testing.T) {
	tempDir := t.TempDir()
	username := "existinguser"

	// First setup should succeed
	cfg := &SetupConfig{
		BaseStorePath: tempDir,
		Username:      username,
		LocalOnly:     true,
	}

	result1, err := SetupUserRepository(cfg)
	require.NoError(t, err)
	assert.NotNil(t, result1)

	// Second setup with same username should fail
	result2, err := SetupUserRepository(cfg)
	assert.Error(t, err)
	assert.Nil(t, result2)
	assert.Contains(t, err.Error(), "repository already exists")
}

// TestSetupUserRepository_DeterministicNaming tests that same username always gets same folder
func TestSetupUserRepository_DeterministicNaming(t *testing.T) {
	// Test multiple usernames to ensure deterministic naming
	testCases := []struct {
		username     string
		expectedName string
	}{
		{"alice", "mimir-alice"},
		{"bob.smith", "mimir-bob.smith"},
		{"charlie@example.com", "mimir-charlie@example.com"},
		{"user_with_underscore", "mimir-user_with_underscore"},
		{"User123", "mimir-User123"},
	}

	for _, tc := range testCases {
		t.Run(tc.username, func(t *testing.T) {
			tempDir := t.TempDir()

			cfg := &SetupConfig{
				BaseStorePath: tempDir,
				Username:      tc.username,
				LocalOnly:     true,
			}

			result, err := SetupUserRepository(cfg)
			require.NoError(t, err)

			assert.Equal(t, tc.expectedName, result.RepoName)
			assert.Equal(t, tc.username, result.RepoID)

			// Verify GetUserRepositoryPath returns the same path
			expectedPath := GetUserRepositoryPath(tempDir, tc.username)
			assert.Equal(t, expectedPath, result.RepoPath)
		})
	}
}

// TestSetupUserRepository_MultipleUsers tests that different users get different folders
func TestSetupUserRepository_MultipleUsers(t *testing.T) {
	tempDir := t.TempDir()

	users := []string{"user1", "user2@company.com", "admin"}

	var results []*SetupResult
	for _, username := range users {
		cfg := &SetupConfig{
			BaseStorePath: tempDir,
			Username:      username,
			LocalOnly:     true,
		}

		result, err := SetupUserRepository(cfg)
		require.NoError(t, err)
		results = append(results, result)
	}

	// Verify all repos are unique
	paths := make(map[string]bool)
	for _, result := range results {
		assert.False(t, paths[result.RepoPath], "Duplicate path found: %s", result.RepoPath)
		paths[result.RepoPath] = true
	}

	// Verify all folders exist
	entries, err := os.ReadDir(tempDir)
	require.NoError(t, err)
	assert.Equal(t, len(users), len(entries))
}

func TestEnsureStorePath(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "nested", "store", "path")

	err := EnsureStorePath(storePath)
	require.NoError(t, err)

	// Verify path exists
	info, err := os.Stat(storePath)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestGetUserRepositoryPath(t *testing.T) {
	testCases := []struct {
		basePath string
		username string
		expected string
	}{
		{"/home/user/.mimir/store", "localuser", "/home/user/.mimir/store/mimir-localuser"},
		{"/home/user/.mimir/store", "john.doe@company.com", "/home/user/.mimir/store/mimir-john.doe@company.com"},
		{"/var/mimir", "admin", "/var/mimir/mimir-admin"},
	}

	for _, tc := range testCases {
		t.Run(tc.username, func(t *testing.T) {
			path := GetUserRepositoryPath(tc.basePath, tc.username)
			assert.Equal(t, tc.expected, path)
		})
	}
}

// Tests for new human-aligned git operations

func TestSearchCommits(t *testing.T) {
	tempDir := t.TempDir()
	repoPath := filepath.Join(tempDir, "test-repo")

	repo, err := InitRepository(repoPath)
	require.NoError(t, err)

	// Create multiple commits with different messages
	messages := []string{
		"feat: Add authentication",
		"fix: Resolve bug in login",
		"feat: Add user profile",
	}

	for i, msg := range messages {
		testFile := filepath.Join(repoPath, fmt.Sprintf("file%d.md", i))
		err = os.WriteFile(testFile, []byte(fmt.Sprintf("content %d", i)), 0644)
		require.NoError(t, err)
		err = repo.CommitFile(testFile, msg)
		require.NoError(t, err)
	}

	// Search for "feat" commits
	results, err := repo.SearchCommits("feat", "", time.Time{}, time.Time{}, 10)
	require.NoError(t, err)
	assert.Equal(t, 2, len(results))

	// Search for "authentication"
	results, err = repo.SearchCommits("authentication", "", time.Time{}, time.Time{}, 10)
	require.NoError(t, err)
	assert.Equal(t, 1, len(results))
	assert.Contains(t, results[0].Message, "authentication")

	// Search all commits with limit
	results, err = repo.SearchCommits("", "", time.Time{}, time.Time{}, 2)
	require.NoError(t, err)
	assert.Equal(t, 2, len(results))
}

func TestSearchCommits_ByFilePath(t *testing.T) {
	tempDir := t.TempDir()
	repoPath := filepath.Join(tempDir, "test-repo")

	repo, err := InitRepository(repoPath)
	require.NoError(t, err)

	// Create files in different directories
	err = os.MkdirAll(filepath.Join(repoPath, "subdir"), 0755)
	require.NoError(t, err)

	file1 := filepath.Join(repoPath, "root.md")
	file2 := filepath.Join(repoPath, "subdir", "nested.md")

	err = os.WriteFile(file1, []byte("root content"), 0644)
	require.NoError(t, err)
	err = repo.CommitFile(file1, "Add root file")
	require.NoError(t, err)

	err = os.WriteFile(file2, []byte("nested content"), 0644)
	require.NoError(t, err)
	err = repo.CommitFile(file2, "Add nested file")
	require.NoError(t, err)

	// Search for commits affecting subdir
	results, err := repo.SearchCommits("", "subdir", time.Time{}, time.Time{}, 10)
	require.NoError(t, err)
	assert.Equal(t, 1, len(results))
	assert.Contains(t, results[0].Message, "nested")
}

func TestGetFileAtRevision(t *testing.T) {
	tempDir := t.TempDir()
	repoPath := filepath.Join(tempDir, "test-repo")

	repo, err := InitRepository(repoPath)
	require.NoError(t, err)

	// Create and commit first version
	testFile := filepath.Join(repoPath, "test.md")
	err = os.WriteFile(testFile, []byte("version 1"), 0644)
	require.NoError(t, err)
	err = repo.CommitFile(testFile, "Add file v1")
	require.NoError(t, err)

	// Update and commit second version
	err = os.WriteFile(testFile, []byte("version 2"), 0644)
	require.NoError(t, err)
	err = repo.CommitFile(testFile, "Update file v2")
	require.NoError(t, err)

	// Get current version (HEAD)
	content, err := repo.GetFileAtRevision(testFile, "HEAD")
	require.NoError(t, err)
	assert.Equal(t, "version 2", string(content))

	// Get previous version (HEAD~1)
	content, err = repo.GetFileAtRevision(testFile, "HEAD~1")
	require.NoError(t, err)
	assert.Equal(t, "version 1", string(content))
}

func TestGetFileDiff(t *testing.T) {
	tempDir := t.TempDir()
	repoPath := filepath.Join(tempDir, "test-repo")

	repo, err := InitRepository(repoPath)
	require.NoError(t, err)

	// Create and commit first version
	testFile := filepath.Join(repoPath, "test.md")
	err = os.WriteFile(testFile, []byte("line1\nline2\nline3"), 0644)
	require.NoError(t, err)
	err = repo.CommitFile(testFile, "Add file")
	require.NoError(t, err)

	// Update and commit second version
	err = os.WriteFile(testFile, []byte("line1\nline2 modified\nline4"), 0644)
	require.NoError(t, err)
	err = repo.CommitFile(testFile, "Modify file")
	require.NoError(t, err)

	// Get diff
	diff, err := repo.GetFileDiff(testFile, "HEAD~1", "HEAD")
	require.NoError(t, err)

	assert.NotEmpty(t, diff.FilePath)
	assert.Contains(t, diff.Additions, "line2 modified")
	assert.Contains(t, diff.Additions, "line4")
	assert.Contains(t, diff.Deletions, "line2")
	assert.Contains(t, diff.Deletions, "line3")
}

func TestGrep(t *testing.T) {
	tempDir := t.TempDir()
	repoPath := filepath.Join(tempDir, "test-repo")

	repo, err := InitRepository(repoPath)
	require.NoError(t, err)

	// Create test files with different content
	file1 := filepath.Join(repoPath, "auth.md")
	file2 := filepath.Join(repoPath, "profile.md")
	file3 := filepath.Join(repoPath, "notes.md")

	err = os.WriteFile(file1, []byte("# Authentication\n\nUser authentication flow"), 0644)
	require.NoError(t, err)
	err = os.WriteFile(file2, []byte("# User Profile\n\nProfile settings"), 0644)
	require.NoError(t, err)
	err = os.WriteFile(file3, []byte("# Notes\n\nRandom notes"), 0644)
	require.NoError(t, err)

	// Commit files
	err = repo.CommitAll("Add test files")
	require.NoError(t, err)

	// Search for "user" (case insensitive)
	results, err := repo.Grep("user", "")
	require.NoError(t, err)
	assert.True(t, len(results) >= 2, "Expected at least 2 matches for 'user'")

	// Search for "authentication"
	results, err = repo.Grep("authentication", "")
	require.NoError(t, err)
	assert.True(t, len(results) >= 1, "Expected at least 1 match for 'authentication'")

	// Verify file path in results
	var foundAuthFile bool
	for _, r := range results {
		if r.FilePath == "auth.md" {
			foundAuthFile = true
			break
		}
	}
	assert.True(t, foundAuthFile, "Expected to find match in auth.md")
}

func TestGrep_WithPathFilter(t *testing.T) {
	tempDir := t.TempDir()
	repoPath := filepath.Join(tempDir, "test-repo")

	repo, err := InitRepository(repoPath)
	require.NoError(t, err)

	// Create test files in different directories
	err = os.MkdirAll(filepath.Join(repoPath, "projects"), 0755)
	require.NoError(t, err)

	file1 := filepath.Join(repoPath, "root.md")
	file2 := filepath.Join(repoPath, "projects", "project.md")

	err = os.WriteFile(file1, []byte("ROOT: keyword here"), 0644)
	require.NoError(t, err)
	err = os.WriteFile(file2, []byte("PROJECT: keyword here"), 0644)
	require.NoError(t, err)
	err = repo.CommitAll("Add files")
	require.NoError(t, err)

	// Search with path filter
	results, err := repo.Grep("keyword", "projects")
	require.NoError(t, err)
	assert.Equal(t, 1, len(results))
	assert.Equal(t, "projects/project.md", results[0].FilePath)
}

func TestGetFileHistory(t *testing.T) {
	tempDir := t.TempDir()
	repoPath := filepath.Join(tempDir, "test-repo")

	repo, err := InitRepository(repoPath)
	require.NoError(t, err)

	testFile := filepath.Join(repoPath, "test.md")

	// Create multiple versions
	for i := 1; i <= 3; i++ {
		err = os.WriteFile(testFile, []byte(fmt.Sprintf("version %d", i)), 0644)
		require.NoError(t, err)
		err = repo.CommitFile(testFile, fmt.Sprintf("Update to v%d", i))
		require.NoError(t, err)
	}

	// Get file history
	history, err := repo.GetFileHistory(testFile, 10)
	require.NoError(t, err)
	assert.Equal(t, 3, len(history))
}

func TestCommitMessageFormats_HumanAligned(t *testing.T) {
	msgFormat := CommitMessageFormats{}

	tests := []struct {
		name     string
		result   string
		expected string
	}{
		{
			name:     "restore memory",
			result:   msgFormat.RestoreMemory("test-memory"),
			expected: "restore: Unarchive memory 'test-memory'",
		},
		{
			name:     "supersede memory",
			result:   msgFormat.SupersedeMemory("old-memory", "new-memory"),
			expected: "supersede: 'old-memory' replaced by 'new-memory'",
		},
		{
			name:     "add annotation",
			result:   msgFormat.AddAnnotation("test-memory", "correction"),
			expected: "annotate: Add correction to 'test-memory'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.result)
		})
	}
}
