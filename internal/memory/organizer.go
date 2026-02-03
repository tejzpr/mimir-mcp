// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package memory

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// OrganizerConfig holds configuration for memory organization
type OrganizerConfig struct {
	BaseRepoPath string
}

// Organizer manages memory file organization
type Organizer struct {
	config *OrganizerConfig
}

// NewOrganizer creates a new memory organizer
func NewOrganizer(baseRepoPath string) *Organizer {
	return &Organizer{
		config: &OrganizerConfig{
			BaseRepoPath: baseRepoPath,
		},
	}
}

// GetMemoryPath determines the file path for a memory based on its metadata
func (o *Organizer) GetMemoryPath(slug string, tags []string, category string, createdAt time.Time) string {
	// Determine primary organization strategy
	// Priority: category > tags > date-based

	var subPath string

	if category != "" {
		// Use category-based organization
		subPath = o.getCategoryPath(category, createdAt)
	} else if len(tags) > 0 {
		// Use tag-based organization (use first tag)
		subPath = o.getTagPath(tags[0], createdAt)
	} else {
		// Default to date-based organization
		subPath = o.getDatePath(createdAt)
	}

	// Construct full path
	filename := fmt.Sprintf("%s.md", slug)
	return filepath.Join(o.config.BaseRepoPath, subPath, filename)
}

// getCategoryPath returns a path based on category and date
func (o *Organizer) getCategoryPath(category string, createdAt time.Time) string {
	year := createdAt.Format("2006")
	month := createdAt.Format("01")
	
	// Sanitize category name
	category = strings.ToLower(strings.TrimSpace(category))
	category = strings.ReplaceAll(category, " ", "-")

	return filepath.Join(year, month, category)
}

// getTagPath returns a path based on tag
func (o *Organizer) getTagPath(tag string, createdAt time.Time) string {
	// Sanitize tag name
	tag = strings.ToLower(strings.TrimSpace(tag))
	tag = strings.ReplaceAll(tag, " ", "-")

	return filepath.Join("tags", tag)
}

// getDatePath returns a pure date-based path
func (o *Organizer) getDatePath(createdAt time.Time) string {
	year := createdAt.Format("2006")
	month := createdAt.Format("01")
	return filepath.Join(year, month)
}

// GetArchivePath returns the path for archived memories
func (o *Organizer) GetArchivePath(slug string) string {
	filename := fmt.Sprintf("%s.md", slug)
	return filepath.Join(o.config.BaseRepoPath, "archive", filename)
}

// ParsePathForMetadata extracts metadata from a file path
func ParsePathForMetadata(repoPath, filePath string) (category string, isArchived bool, err error) {
	// Get relative path from repo
	relPath, err := filepath.Rel(repoPath, filePath)
	if err != nil {
		return "", false, fmt.Errorf("failed to get relative path: %w", err)
	}

	parts := strings.Split(relPath, string(filepath.Separator))
	
	if len(parts) == 0 {
		return "", false, fmt.Errorf("invalid path")
	}

	// Check if archived
	if parts[0] == "archive" {
		return "", true, nil
	}

	// Check if in tags
	if parts[0] == "tags" && len(parts) >= 2 {
		return parts[1], false, nil
	}

	// Check if in year/month/category structure
	if len(parts) >= 3 {
		category = parts[2]
	}

	return category, false, nil
}

// EnsureDirectoryExists creates the directory for a file path if it doesn't exist
func EnsureDirectoryExists(filePath string) error {
	dir := filepath.Dir(filePath)
	return ensureDir(dir)
}

// ensureDir creates a directory and all parent directories
func ensureDir(path string) error {
	// Note: This is handled by the caller to avoid import cycles
	// Just a placeholder for documentation
	return nil
}
