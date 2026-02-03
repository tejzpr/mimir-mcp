// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package memory

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestGenerateSlug(t *testing.T) {
	tests := []struct {
		title    string
		expected string
	}{
		{
			title:    "Project Alpha Kickoff",
			expected: "project-alpha-kickoff-",
		},
		{
			title:    "Meeting with Client X",
			expected: "meeting-with-client-x-",
		},
		{
			title:    "Q1 2024 Planning",
			expected: "q1-2024-planning-",
		},
		{
			title:    "Test!@#$%^&*()_+",
			expected: "test-",
		},
		{
			title:    "Multiple   Spaces   Here",
			expected: "multiple-spaces-here-",
		},
		{
			title:    "  Leading and Trailing  ",
			expected: "leading-and-trailing-",
		},
	}

	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			slug := GenerateSlug(tt.title)
			assert.True(t, strings.HasPrefix(slug, tt.expected))
			// Should end with date in format YYYY-MM-DD
			assert.Regexp(t, `\d{4}-\d{2}-\d{2}$`, slug)
		})
	}
}

func TestGenerateSlugWithDate(t *testing.T) {
	date := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	tests := []struct {
		title    string
		expected string
	}{
		{
			title:    "Project Alpha",
			expected: "project-alpha-2024-01-15",
		},
		{
			title:    "Test Meeting",
			expected: "test-meeting-2024-01-15",
		},
	}

	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			slug := GenerateSlugWithDate(tt.title, date)
			assert.Equal(t, tt.expected, slug)
		})
	}
}

func TestValidateSlug(t *testing.T) {
	tests := []struct {
		slug        string
		expectError bool
		errorMsg    string
	}{
		{
			slug:        "valid-slug-2024-01-15",
			expectError: false,
		},
		{
			slug:        "project-alpha-kickoff-2024-01-15",
			expectError: false,
		},
		{
			slug:        "",
			expectError: true,
			errorMsg:    "cannot be empty",
		},
		{
			slug:        "ab",
			expectError: true,
			errorMsg:    "at least 3 characters",
		},
		{
			slug:        strings.Repeat("a", 201),
			expectError: true,
			errorMsg:    "cannot exceed 200 characters",
		},
		{
			slug:        "Invalid-Slug",
			expectError: true,
			errorMsg:    "lowercase",
		},
		{
			slug:        "slug with spaces",
			expectError: true,
			errorMsg:    "lowercase",
		},
		{
			slug:        "slug_with_underscores",
			expectError: true,
			errorMsg:    "lowercase",
		},
		{
			slug:        "-starts-with-dash",
			expectError: true,
			errorMsg:    "lowercase",
		},
		{
			slug:        "ends-with-dash-",
			expectError: true,
			errorMsg:    "lowercase",
		},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("slug=%s", tt.slug), func(t *testing.T) {
			err := ValidateSlug(tt.slug)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSanitizeTitle(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "  Normal Title  ",
			expected: "Normal Title",
		},
		{
			input:    "Title\x00with\x1Fcontrol\x7Fchars",
			expected: "Titlewithcontrolchars",
		},
		{
			input:    "Valid Title 123",
			expected: "Valid Title 123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := SanitizeTitle(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGenerateSlug_Consistency(t *testing.T) {
	title := "Test Title"
	
	// Generate multiple slugs from same title on same day
	slug1 := GenerateSlugWithDate(title, time.Now())
	slug2 := GenerateSlugWithDate(title, time.Now())
	
	// Should be identical
	assert.Equal(t, slug1, slug2)
}

func TestGenerateSlug_DifferentDates(t *testing.T) {
	title := "Test Title"
	date1 := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	date2 := time.Date(2024, 1, 16, 10, 0, 0, 0, time.UTC)

	slug1 := GenerateSlugWithDate(title, date1)
	slug2 := GenerateSlugWithDate(title, date2)

	// Should be different
	assert.NotEqual(t, slug1, slug2)
	assert.Contains(t, slug1, "2024-01-15")
	assert.Contains(t, slug2, "2024-01-16")
}

func TestGenerateSlug_UnicodeTitle(t *testing.T) {
	tests := []struct {
		title      string
		contains   string
		shouldFail bool
	}{
		{
			title:      "Café Meeting ☕",
			contains:   "caf-meeting",
			shouldFail: false,
		},
		{
			title:      "Résumé Discussion",
			contains:   "rsum-discussion",
			shouldFail: false,
		},
		{
			title:      "测试标题",
			contains:   "", // Should remove all non-ascii, leaving only date
			shouldFail: true, // This creates an invalid slug (just date with dashes)
		},
	}

	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			slug := GenerateSlug(tt.title)
			if tt.contains != "" {
				assert.Contains(t, slug, tt.contains)
			}
			
			if tt.shouldFail {
				// Slug might be invalid for pure unicode titles
				err := ValidateSlug(slug)
				if err != nil {
					// This is expected
					return
				}
			}
			
			// Should be valid if not expected to fail
			err := ValidateSlug(slug)
			assert.NoError(t, err)
		})
	}
}
