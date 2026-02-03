// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package memory

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestGetMemoryPath_CategoryBased(t *testing.T) {
	organizer := NewOrganizer("/test/repo")
	date := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	path := organizer.GetMemoryPath("test-memory", []string{"tag1"}, "projects", date)
	
	expected := filepath.Join("/test/repo", "2024", "01", "projects", "test-memory.md")
	assert.Equal(t, expected, path)
}

func TestGetMemoryPath_TagBased(t *testing.T) {
	organizer := NewOrganizer("/test/repo")
	date := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	// No category, should use first tag
	path := organizer.GetMemoryPath("test-memory", []string{"meetings", "important"}, "", date)
	
	expected := filepath.Join("/test/repo", "tags", "meetings", "test-memory.md")
	assert.Equal(t, expected, path)
}

func TestGetMemoryPath_DateBased(t *testing.T) {
	organizer := NewOrganizer("/test/repo")
	date := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	// No category, no tags - use date
	path := organizer.GetMemoryPath("test-memory", []string{}, "", date)
	
	expected := filepath.Join("/test/repo", "2024", "01", "test-memory.md")
	assert.Equal(t, expected, path)
}

func TestGetArchivePath(t *testing.T) {
	organizer := NewOrganizer("/test/repo")

	path := organizer.GetArchivePath("archived-memory")
	
	expected := filepath.Join("/test/repo", "archive", "archived-memory.md")
	assert.Equal(t, expected, path)
}

func TestParsePathForMetadata(t *testing.T) {
	tests := []struct {
		name           string
		repoPath       string
		filePath       string
		expectedCat    string
		expectedArch   bool
		expectError    bool
	}{
		{
			name:         "category path",
			repoPath:     "/test/repo",
			filePath:     "/test/repo/2024/01/projects/test.md",
			expectedCat:  "projects",
			expectedArch: false,
			expectError:  false,
		},
		{
			name:         "tag path",
			repoPath:     "/test/repo",
			filePath:     "/test/repo/tags/meetings/test.md",
			expectedCat:  "meetings",
			expectedArch: false,
			expectError:  false,
		},
		{
			name:         "archive path",
			repoPath:     "/test/repo",
			filePath:     "/test/repo/archive/test.md",
			expectedCat:  "",
			expectedArch: true,
			expectError:  false,
		},
		{
			name:         "date-only path",
			repoPath:     "/test/repo",
			filePath:     "/test/repo/2024/01/test.md",
			expectedCat:  "test.md", // Third part is considered category
			expectedArch: false,
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			category, isArchived, err := ParsePathForMetadata(tt.repoPath, tt.filePath)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedCat, category)
				assert.Equal(t, tt.expectedArch, isArchived)
			}
		})
	}
}

func TestGetCategoryPath(t *testing.T) {
	organizer := NewOrganizer("/test/repo")
	date := time.Date(2024, 3, 25, 10, 30, 0, 0, time.UTC)

	path := organizer.getCategoryPath("Meeting Notes", date)
	expected := filepath.Join("2024", "03", "meeting-notes")
	assert.Equal(t, expected, path)
}

func TestGetTagPath(t *testing.T) {
	organizer := NewOrganizer("/test/repo")
	date := time.Date(2024, 3, 25, 10, 30, 0, 0, time.UTC)

	path := organizer.getTagPath("Important Items", date)
	expected := filepath.Join("tags", "important-items")
	assert.Equal(t, expected, path)
}

func TestGetDatePath(t *testing.T) {
	organizer := NewOrganizer("/test/repo")
	
	tests := []struct {
		date     time.Time
		expected string
	}{
		{
			date:     time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
			expected: filepath.Join("2024", "01"),
		},
		{
			date:     time.Date(2024, 12, 31, 23, 59, 0, 0, time.UTC),
			expected: filepath.Join("2024", "12"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.date.String(), func(t *testing.T) {
			path := organizer.getDatePath(tt.date)
			assert.Equal(t, tt.expected, path)
		})
	}
}

func TestOrganizer_MultipleStrategies(t *testing.T) {
	organizer := NewOrganizer("/test/repo")
	date := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	slug := "test-memory"

	// Test priority: category > tags > date
	
	// With category (highest priority)
	path1 := organizer.GetMemoryPath(slug, []string{"tag1"}, "projects", date)
	assert.Contains(t, path1, "projects")
	assert.NotContains(t, path1, "tags")

	// With tags but no category
	path2 := organizer.GetMemoryPath(slug, []string{"meetings"}, "", date)
	assert.Contains(t, path2, filepath.Join("tags", "meetings"))

	// With neither (date-based)
	path3 := organizer.GetMemoryPath(slug, []string{}, "", date)
	assert.Contains(t, path3, filepath.Join("2024", "01"))
	assert.NotContains(t, path3, "tags")
	assert.NotContains(t, path3, "projects")
}
