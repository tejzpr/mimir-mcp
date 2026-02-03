// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package memory

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseMarkdown_WithFrontmatter(t *testing.T) {
	content := `---
id: test-memory-2024-01-15
title: Test Memory
tags: [project, meeting]
created: 2024-01-15T10:30:00Z
updated: 2024-01-15T14:00:00Z
associations:
  - target: other-memory
    type: related_to
    strength: 0.8
---

# Test Memory

This is the content of the memory.

## Details

More content here.
`

	memory, err := ParseMarkdown(content)
	require.NoError(t, err)
	assert.Equal(t, "test-memory-2024-01-15", memory.ID)
	assert.Equal(t, "Test Memory", memory.Title)
	assert.Equal(t, []string{"project", "meeting"}, memory.Tags)
	assert.Len(t, memory.Associations, 1)
	assert.Equal(t, "other-memory", memory.Associations[0].Target)
	assert.Equal(t, "related_to", memory.Associations[0].Type)
	assert.Equal(t, 0.8, memory.Associations[0].Strength)
	assert.Contains(t, memory.Content, "# Test Memory")
	assert.Contains(t, memory.Content, "## Details")
}

func TestParseMarkdown_NoFrontmatter(t *testing.T) {
	content := `# Just Content

No frontmatter here.
`

	memory, err := ParseMarkdown(content)
	require.NoError(t, err)
	assert.Empty(t, memory.ID)
	assert.Empty(t, memory.Title)
	assert.Contains(t, memory.Content, "# Just Content")
}

func TestParseMarkdown_InvalidFrontmatter(t *testing.T) {
	content := `---
id: test
title: [this is invalid yaml: missing quote
---

Content
`

	_, err := ParseMarkdown(content)
	assert.Error(t, err)
}

func TestToMarkdown(t *testing.T) {
	memory := &Memory{
		ID:      "test-memory-2024-01-15",
		Title:   "Test Memory",
		Tags:    []string{"project", "meeting"},
		Created: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		Updated: time.Date(2024, 1, 15, 14, 0, 0, 0, time.UTC),
		Associations: []Association{
			{
				Target:   "other-memory",
				Type:     "related_to",
				Strength: 0.8,
			},
		},
		Content: "# Test Memory\n\nThis is the content.",
	}

	markdown, err := memory.ToMarkdown()
	require.NoError(t, err)
	assert.Contains(t, markdown, "---")
	assert.Contains(t, markdown, "id: test-memory-2024-01-15")
	assert.Contains(t, markdown, "title: Test Memory")
	assert.Contains(t, markdown, "# Test Memory")
	assert.Contains(t, markdown, "This is the content")
}

func TestParseMarkdown_RoundTrip(t *testing.T) {
	original := &Memory{
		ID:      "test-memory",
		Title:   "Test",
		Tags:    []string{"tag1", "tag2"},
		Created: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		Updated: time.Date(2024, 1, 15, 14, 0, 0, 0, time.UTC),
		Content: "Test content",
	}

	// Convert to markdown
	markdown, err := original.ToMarkdown()
	require.NoError(t, err)

	// Parse back
	parsed, err := ParseMarkdown(markdown)
	require.NoError(t, err)

	assert.Equal(t, original.ID, parsed.ID)
	assert.Equal(t, original.Title, parsed.Title)
	assert.Equal(t, original.Tags, parsed.Tags)
	assert.Equal(t, original.Content, parsed.Content)
}

func TestAddAssociation(t *testing.T) {
	memory := &Memory{
		ID:           "test-memory",
		Associations: []Association{},
	}

	// Add first association
	memory.AddAssociation("target1", "related_to", 0.8)
	assert.Len(t, memory.Associations, 1)
	assert.Equal(t, "target1", memory.Associations[0].Target)

	// Add second association
	memory.AddAssociation("target2", "person", 0.9)
	assert.Len(t, memory.Associations, 2)

	// Update existing association
	memory.AddAssociation("target1", "references", 0.7)
	assert.Len(t, memory.Associations, 2)
	assoc, ok := memory.GetAssociation("target1")
	require.True(t, ok)
	assert.Equal(t, "references", assoc.Type)
	assert.Equal(t, 0.7, assoc.Strength)
}

func TestRemoveAssociation(t *testing.T) {
	memory := &Memory{
		ID: "test-memory",
		Associations: []Association{
			{Target: "target1", Type: "related_to", Strength: 0.8},
			{Target: "target2", Type: "person", Strength: 0.9},
		},
	}

	// Remove existing association
	removed := memory.RemoveAssociation("target1")
	assert.True(t, removed)
	assert.Len(t, memory.Associations, 1)
	assert.Equal(t, "target2", memory.Associations[0].Target)

	// Try to remove non-existent association
	removed = memory.RemoveAssociation("target3")
	assert.False(t, removed)
	assert.Len(t, memory.Associations, 1)
}

func TestGetAssociation(t *testing.T) {
	memory := &Memory{
		ID: "test-memory",
		Associations: []Association{
			{Target: "target1", Type: "related_to", Strength: 0.8},
		},
	}

	// Get existing association
	assoc, ok := memory.GetAssociation("target1")
	require.True(t, ok)
	assert.Equal(t, "related_to", assoc.Type)

	// Try to get non-existent association
	_, ok = memory.GetAssociation("target2")
	assert.False(t, ok)
}

func TestSplitFrontmatter(t *testing.T) {
	tests := []struct {
		name              string
		content           string
		expectedFM        string
		expectedBody      string
		shouldError       bool
	}{
		{
			name: "valid frontmatter",
			content: `---
title: Test
---

Body content`,
			expectedFM:   "title: Test",
			expectedBody: "Body content",
			shouldError:  false,
		},
		{
			name: "no frontmatter",
			content: `Just body content`,
			expectedFM:   "",
			expectedBody: "Just body content",
			shouldError:  false,
		},
		{
			name: "unclosed frontmatter",
			content: `---
title: Test

Body without closing`,
			expectedFM:   "",
			expectedBody: "",
			shouldError:  true,
		},
		{
			name: "empty frontmatter",
			content: `---
---

Body`,
			expectedFM:   "",
			expectedBody: "Body",
			shouldError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, body, err := splitFrontmatter(tt.content)

			if tt.shouldError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedFM, fm)
				assert.Contains(t, body, strings.TrimSpace(tt.expectedBody))
			}
		})
	}
}
