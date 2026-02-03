// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package git

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// CommitInfo represents information about a commit
type CommitInfo struct {
	Hash      string    `json:"hash"`
	Message   string    `json:"message"`
	Author    string    `json:"author"`
	Email     string    `json:"email"`
	Timestamp time.Time `json:"timestamp"`
	Files     []string  `json:"files,omitempty"` // Files changed in this commit
}

// GrepResult represents a search match in a file
type GrepResult struct {
	FilePath   string `json:"file_path"`
	LineNumber int    `json:"line_number"`
	Line       string `json:"line"`
	MatchStart int    `json:"match_start"`
	MatchEnd   int    `json:"match_end"`
}

// DiffResult represents the diff between two versions of a file
type DiffResult struct {
	FilePath  string   `json:"file_path"`
	FromRef   string   `json:"from_ref"`
	ToRef     string   `json:"to_ref"`
	Additions []string `json:"additions"`
	Deletions []string `json:"deletions"`
	Hunks     []string `json:"hunks"` // Unified diff hunks
}

// SearchCommits searches commit history by message pattern, file path, and date range
func (r *Repository) SearchCommits(pattern string, filePath string, since time.Time, until time.Time, limit int) ([]CommitInfo, error) {
	ref, err := r.repo.Head()
	if err != nil {
		return nil, fmt.Errorf("failed to get HEAD: %w", err)
	}

	logOpts := &git.LogOptions{
		From: ref.Hash(),
	}

	// Add path filter if specified
	if filePath != "" {
		// Convert to relative path if absolute
		relPath := filePath
		if filepath.IsAbs(filePath) {
			relPath, err = filepath.Rel(r.Path, filePath)
			if err != nil {
				relPath = filePath
			}
		}
		logOpts.PathFilter = func(path string) bool {
			if strings.Contains(relPath, "*") {
				// Handle glob patterns
				matched, _ := filepath.Match(relPath, path)
				return matched
			}
			return strings.HasPrefix(path, relPath) || path == relPath
		}
	}

	// Add date filter
	if !since.IsZero() {
		logOpts.Since = &since
	}
	if !until.IsZero() {
		logOpts.Until = &until
	}

	commitIter, err := r.repo.Log(logOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to get commit log: %w", err)
	}

	var results []CommitInfo
	var patternRegex *regexp.Regexp
	if pattern != "" {
		patternRegex, err = regexp.Compile("(?i)" + pattern) // Case-insensitive
		if err != nil {
			return nil, fmt.Errorf("invalid search pattern: %w", err)
		}
	}

	count := 0
	err = commitIter.ForEach(func(c *object.Commit) error {
		if limit > 0 && count >= limit {
			return fmt.Errorf("limit reached")
		}

		// Filter by message pattern if specified
		if patternRegex != nil && !patternRegex.MatchString(c.Message) {
			return nil
		}

		info := CommitInfo{
			Hash:      c.Hash.String(),
			Message:   strings.TrimSpace(c.Message),
			Author:    c.Author.Name,
			Email:     c.Author.Email,
			Timestamp: c.Author.When,
		}

		// Get files changed in this commit
		files, err := r.getCommitFiles(c)
		if err == nil {
			info.Files = files
		}

		results = append(results, info)
		count++
		return nil
	})

	if err != nil && err.Error() != "limit reached" {
		return nil, fmt.Errorf("failed to iterate commits: %w", err)
	}

	return results, nil
}

// getCommitFiles returns the files changed in a commit
func (r *Repository) getCommitFiles(c *object.Commit) ([]string, error) {
	var files []string

	// Get parent commit for diff
	parent, err := c.Parent(0)
	if err != nil {
		// First commit - list all files in the tree
		tree, err := c.Tree()
		if err != nil {
			return nil, err
		}
		_ = tree.Files().ForEach(func(f *object.File) error {
			files = append(files, f.Name)
			return nil
		})
		return files, nil
	}

	// Get diff between parent and current
	parentTree, err := parent.Tree()
	if err != nil {
		return nil, err
	}

	currentTree, err := c.Tree()
	if err != nil {
		return nil, err
	}

	changes, err := parentTree.Diff(currentTree)
	if err != nil {
		return nil, err
	}

	for _, change := range changes {
		if change.To.Name != "" {
			files = append(files, change.To.Name)
		} else if change.From.Name != "" {
			files = append(files, change.From.Name)
		}
	}

	return files, nil
}

// GetFileAtRevision returns the content of a file at a specific revision
func (r *Repository) GetFileAtRevision(filePath string, ref string) ([]byte, error) {
	// Convert to relative path if absolute
	relPath := filePath
	if filepath.IsAbs(filePath) {
		var err error
		relPath, err = filepath.Rel(r.Path, filePath)
		if err != nil {
			relPath = filePath
		}
	}

	// Resolve the reference
	hash, err := r.resolveRef(ref)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve ref '%s': %w", ref, err)
	}

	// Get the commit
	commit, err := r.repo.CommitObject(hash)
	if err != nil {
		return nil, fmt.Errorf("failed to get commit: %w", err)
	}

	// Get the tree
	tree, err := commit.Tree()
	if err != nil {
		return nil, fmt.Errorf("failed to get tree: %w", err)
	}

	// Get the file
	file, err := tree.File(relPath)
	if err != nil {
		return nil, fmt.Errorf("file not found at revision: %w", err)
	}

	// Get content
	content, err := file.Contents()
	if err != nil {
		return nil, fmt.Errorf("failed to read file content: %w", err)
	}

	return []byte(content), nil
}

// resolveRef resolves a reference string to a hash
// Supports: HEAD, HEAD~N, branch names, tag names, and commit hashes
func (r *Repository) resolveRef(ref string) (plumbing.Hash, error) {
	// Handle HEAD~N notation
	if strings.HasPrefix(ref, "HEAD") {
		headRef, err := r.repo.Head()
		if err != nil {
			return plumbing.ZeroHash, err
		}

		if ref == "HEAD" {
			return headRef.Hash(), nil
		}

		// Parse HEAD~N
		if strings.HasPrefix(ref, "HEAD~") {
			nStr := strings.TrimPrefix(ref, "HEAD~")
			var n int
			_, err := fmt.Sscanf(nStr, "%d", &n)
			if err != nil {
				return plumbing.ZeroHash, fmt.Errorf("invalid ref format: %s", ref)
			}

			// Walk back N commits
			commit, err := r.repo.CommitObject(headRef.Hash())
			if err != nil {
				return plumbing.ZeroHash, err
			}

			for i := 0; i < n; i++ {
				parent, err := commit.Parent(0)
				if err != nil {
					return plumbing.ZeroHash, fmt.Errorf("cannot go back %d commits: %w", n, err)
				}
				commit = parent
			}
			return commit.Hash, nil
		}
	}

	// Try as a commit hash
	if len(ref) >= 7 {
		hash := plumbing.NewHash(ref)
		if _, err := r.repo.CommitObject(hash); err == nil {
			return hash, nil
		}
	}

	// Try as a branch/tag reference
	refObj, err := r.repo.Reference(plumbing.ReferenceName("refs/heads/"+ref), true)
	if err == nil {
		return refObj.Hash(), nil
	}

	refObj, err = r.repo.Reference(plumbing.ReferenceName("refs/tags/"+ref), true)
	if err == nil {
		return refObj.Hash(), nil
	}

	return plumbing.ZeroHash, fmt.Errorf("cannot resolve reference: %s", ref)
}

// GetFileDiff returns the diff of a file between two revisions
func (r *Repository) GetFileDiff(filePath string, fromRef string, toRef string) (*DiffResult, error) {
	// Default to HEAD if toRef is empty
	if toRef == "" {
		toRef = "HEAD"
	}

	// Default to HEAD~1 if fromRef is empty
	if fromRef == "" {
		fromRef = "HEAD~1"
	}

	// Convert to relative path if absolute
	relPath := filePath
	if filepath.IsAbs(filePath) {
		var err error
		relPath, err = filepath.Rel(r.Path, filePath)
		if err != nil {
			relPath = filePath
		}
	}

	// Get content at both revisions
	fromContent, err := r.GetFileAtRevision(relPath, fromRef)
	if err != nil {
		// File might not exist at fromRef (new file)
		fromContent = []byte{}
	}

	toContent, err := r.GetFileAtRevision(relPath, toRef)
	if err != nil {
		// File might not exist at toRef (deleted file)
		toContent = []byte{}
	}

	// Compute diff
	result := &DiffResult{
		FilePath: relPath,
		FromRef:  fromRef,
		ToRef:    toRef,
	}

	fromLines := strings.Split(string(fromContent), "\n")
	toLines := strings.Split(string(toContent), "\n")

	// Simple line-by-line diff
	fromSet := make(map[string]bool)
	toSet := make(map[string]bool)

	for _, line := range fromLines {
		if strings.TrimSpace(line) != "" {
			fromSet[line] = true
		}
	}
	for _, line := range toLines {
		if strings.TrimSpace(line) != "" {
			toSet[line] = true
		}
	}

	// Find additions (in toLines but not in fromLines)
	for _, line := range toLines {
		if strings.TrimSpace(line) != "" && !fromSet[line] {
			result.Additions = append(result.Additions, line)
		}
	}

	// Find deletions (in fromLines but not in toLines)
	for _, line := range fromLines {
		if strings.TrimSpace(line) != "" && !toSet[line] {
			result.Deletions = append(result.Deletions, line)
		}
	}

	// Create unified diff hunks
	result.Hunks = r.createUnifiedDiff(fromLines, toLines, relPath, fromRef, toRef)

	return result, nil
}

// createUnifiedDiff creates a unified diff format output
func (r *Repository) createUnifiedDiff(fromLines, toLines []string, filePath, fromRef, toRef string) []string {
	var hunks []string

	// Header
	hunks = append(hunks, fmt.Sprintf("--- a/%s (%s)", filePath, fromRef))
	hunks = append(hunks, fmt.Sprintf("+++ b/%s (%s)", filePath, toRef))

	// Simple diff output
	fromMap := make(map[string]int)
	for i, line := range fromLines {
		fromMap[line] = i + 1
	}

	toMap := make(map[string]int)
	for i, line := range toLines {
		toMap[line] = i + 1
	}

	// Show removed lines
	for _, line := range fromLines {
		if _, exists := toMap[line]; !exists && strings.TrimSpace(line) != "" {
			hunks = append(hunks, fmt.Sprintf("- %s", line))
		}
	}

	// Show added lines
	for _, line := range toLines {
		if _, exists := fromMap[line]; !exists && strings.TrimSpace(line) != "" {
			hunks = append(hunks, fmt.Sprintf("+ %s", line))
		}
	}

	return hunks
}

// Grep searches for a pattern across all files in the repository
func (r *Repository) Grep(pattern string, pathFilter string) ([]GrepResult, error) {
	regex, err := regexp.Compile("(?i)" + pattern) // Case-insensitive by default
	if err != nil {
		return nil, fmt.Errorf("invalid search pattern: %w", err)
	}

	var results []GrepResult

	// Walk the repository directory
	err = filepath.Walk(r.Path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}

		// Skip .git directory
		if info.IsDir() {
			if info.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}

		// Get relative path
		relPath, err := filepath.Rel(r.Path, path)
		if err != nil {
			return nil
		}

		// Apply path filter if specified
		if pathFilter != "" {
			if strings.Contains(pathFilter, "*") {
				matched, _ := filepath.Match(pathFilter, relPath)
				if !matched {
					return nil
				}
			} else if !strings.HasPrefix(relPath, pathFilter) {
				return nil
			}
		}

		// Only search text files (markdown, etc.)
		if !isTextFile(path) {
			return nil
		}

		// Search file content
		fileResults, err := r.searchFile(path, relPath, regex)
		if err != nil {
			return nil // Skip files we can't read
		}

		results = append(results, fileResults...)
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to search repository: %w", err)
	}

	return results, nil
}

// searchFile searches a single file for pattern matches
func (r *Repository) searchFile(absPath, relPath string, regex *regexp.Regexp) ([]GrepResult, error) {
	file, err := os.Open(absPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var results []GrepResult
	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		// Find all matches in the line
		matches := regex.FindStringIndex(line)
		if matches != nil {
			results = append(results, GrepResult{
				FilePath:   relPath,
				LineNumber: lineNum,
				Line:       line,
				MatchStart: matches[0],
				MatchEnd:   matches[1],
			})
		}
	}

	return results, scanner.Err()
}

// isTextFile checks if a file is likely a text file based on extension
func isTextFile(path string) bool {
	textExtensions := map[string]bool{
		".md":       true,
		".txt":      true,
		".json":     true,
		".yaml":     true,
		".yml":      true,
		".xml":      true,
		".html":     true,
		".css":      true,
		".js":       true,
		".ts":       true,
		".go":       true,
		".py":       true,
		".rb":       true,
		".java":     true,
		".c":        true,
		".h":        true,
		".cpp":      true,
		".rs":       true,
		".sh":       true,
		".bash":     true,
		".zsh":      true,
		".toml":     true,
		".ini":      true,
		".cfg":      true,
		".conf":     true,
		".markdown": true,
	}

	ext := strings.ToLower(filepath.Ext(path))
	return textExtensions[ext]
}

// GetFileHistory returns the commit history for a specific file
func (r *Repository) GetFileHistory(filePath string, limit int) ([]CommitInfo, error) {
	return r.SearchCommits("", filePath, time.Time{}, time.Time{}, limit)
}

// RestoreMemory commit message format
func (CommitMessageFormats) RestoreMemory(slug string) string {
	return fmt.Sprintf("restore: Unarchive memory '%s'", slug)
}

// SupersedeMemory commit message format
func (CommitMessageFormats) SupersedeMemory(oldSlug, newSlug string) string {
	return fmt.Sprintf("supersede: '%s' replaced by '%s'", oldSlug, newSlug)
}

// AddAnnotation commit message format
func (CommitMessageFormats) AddAnnotation(slug, annotationType string) string {
	return fmt.Sprintf("annotate: Add %s to '%s'", annotationType, slug)
}
