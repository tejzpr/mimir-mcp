// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package merge

import (
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestThreeWayMerge_NoConflict_Identical(t *testing.T) {
	base := "Hello World"
	ours := "Hello World"
	theirs := "Hello World"

	merged, hasConflict := ThreeWayMerge(base, ours, theirs)

	assert.False(t, hasConflict)
	assert.Equal(t, "Hello World", merged)
}

func TestThreeWayMerge_NoConflict_OnlyOursChanged(t *testing.T) {
	base := "Hello World"
	ours := "Hello New World"
	theirs := "Hello World"

	merged, hasConflict := ThreeWayMerge(base, ours, theirs)

	assert.False(t, hasConflict)
	assert.Equal(t, "Hello New World", merged)
}

func TestThreeWayMerge_NoConflict_OnlyTheirsChanged(t *testing.T) {
	base := "Hello World"
	ours := "Hello World"
	theirs := "Hello New World"

	merged, hasConflict := ThreeWayMerge(base, ours, theirs)

	assert.False(t, hasConflict)
	assert.Equal(t, "Hello New World", merged)
}

func TestThreeWayMerge_BothChanged_DifferentLines(t *testing.T) {
	base := "Line 1\nLine 2\nLine 3"
	ours := "Line 1 modified\nLine 2\nLine 3"
	theirs := "Line 1\nLine 2\nLine 3 modified"

	merged, hasConflict := ThreeWayMerge(base, ours, theirs)

	assert.False(t, hasConflict)
	assert.Contains(t, merged, "Line 1 modified")
	assert.Contains(t, merged, "Line 3 modified")
}

func TestThreeWayMerge_Conflict_SameLine(t *testing.T) {
	base := "Hello World"
	ours := "Hello Ours"
	theirs := "Hello Theirs"

	merged, hasConflict := ThreeWayMerge(base, ours, theirs)

	assert.True(t, hasConflict)
	assert.Contains(t, merged, "<<<<<<< OURS")
	assert.Contains(t, merged, "=======")
	assert.Contains(t, merged, ">>>>>>> THEIRS")
}

func TestMergeTags_Union(t *testing.T) {
	ours := []string{"go", "backend", "api"}
	theirs := []string{"go", "frontend", "api", "react"}

	result := MergeTags(ours, theirs)

	// Sort for deterministic comparison
	sort.Strings(result)
	expected := []string{"api", "backend", "frontend", "go", "react"}
	sort.Strings(expected)

	assert.ElementsMatch(t, expected, result)
}

func TestMergeTags_Empty(t *testing.T) {
	ours := []string{}
	theirs := []string{"tag1", "tag2"}

	result := MergeTags(ours, theirs)

	assert.ElementsMatch(t, []string{"tag1", "tag2"}, result)
}

func TestMergeFrontmatter_LastWriteWins(t *testing.T) {
	now := time.Now()

	ours := &Memory{
		Slug:      "test",
		Title:     "Old Title",
		Tags:      []string{"tag1"},
		UpdatedAt: now.Add(-time.Hour), // Older
	}

	theirs := &Memory{
		Slug:      "test",
		Title:     "New Title",
		Tags:      []string{"tag2"},
		UpdatedAt: now, // Newer
	}

	result := MergeFrontmatter(ours, theirs)

	// Title should be from theirs (newer)
	assert.Equal(t, "New Title", result.Title)
	// Tags should be union
	assert.ElementsMatch(t, []string{"tag1", "tag2"}, result.Tags)
}

func TestMergeAnnotations(t *testing.T) {
	ours := []Annotation{
		{Type: "correction", Content: "Fix typo"},
		{Type: "context", Content: "Related to project X"},
	}

	theirs := []Annotation{
		{Type: "correction", Content: "Fix typo"}, // Duplicate
		{Type: "clarification", Content: "This means..."},
	}

	result := MergeAnnotations(ours, theirs)

	// Should have 3 unique annotations
	assert.Len(t, result, 3)
}

func TestSplitFrontmatterAndContent(t *testing.T) {
	content := `---
title: Test
tags: [go, backend]
---

# Content

This is the body.`

	fm, body := SplitFrontmatterAndContent(content)

	assert.Contains(t, fm, "title: Test")
	assert.Contains(t, body, "# Content")
}

func TestSplitFrontmatterAndContent_NoFrontmatter(t *testing.T) {
	content := "Just content"

	fm, body := SplitFrontmatterAndContent(content)

	assert.Empty(t, fm)
	assert.Equal(t, "Just content", body)
}

func TestExtractTags(t *testing.T) {
	frontmatter := `title: Test
tags: ["go", "backend", "api"]
slug: test-slug`

	tags := ExtractTags(frontmatter)

	assert.ElementsMatch(t, []string{"go", "backend", "api"}, tags)
}

func TestUpdateTags(t *testing.T) {
	frontmatter := `title: Test
tags: ["old"]
slug: test-slug`

	updated := UpdateTags(frontmatter, []string{"new", "tags"})

	assert.Contains(t, updated, "tags: [\"new\", \"tags\"]")
	assert.NotContains(t, updated, "\"old\"")
}

func TestHasConflictMarkers(t *testing.T) {
	assert.True(t, HasConflictMarkers("<<<<<<< OURS\nsome\n=======\nother\n>>>>>>> THEIRS"))
	assert.False(t, HasConflictMarkers("Clean content"))
}

func TestResolveConflict(t *testing.T) {
	conflicted := `Line 1
<<<<<<< OURS
Our change
=======
Their change
>>>>>>> THEIRS
Line 3`

	resolved, success := ResolveConflict(conflicted)

	assert.True(t, success)
	assert.Contains(t, resolved, "Our change")
	assert.Contains(t, resolved, "Their change")
	assert.NotContains(t, resolved, "<<<<<<<")
}

func TestLastWriteWinsStrategy(t *testing.T) {
	strategy := NewLastWriteWinsStrategy()

	result, err := strategy.Merge("base", "ours", "theirs")

	assert.NoError(t, err)
	assert.Equal(t, "theirs", result)
}

func TestContentBasedStrategy(t *testing.T) {
	strategy := NewContentBasedStrategy()

	// Test with non-conflicting changes
	base := "Line 1\nLine 2"
	ours := "Line 1 modified\nLine 2"
	theirs := "Line 1\nLine 2 modified"

	result, err := strategy.Merge(base, ours, theirs)

	assert.NoError(t, err)
	assert.Contains(t, result, "Line 1 modified")
	assert.Contains(t, result, "Line 2 modified")
}

func TestCombineFrontmatterAndContent(t *testing.T) {
	frontmatter := "title: Test\ntags: [go]"
	body := "# Content\n\nBody text"

	combined := CombineFrontmatterAndContent(frontmatter, body)

	assert.True(t, len(combined) > 3 && combined[:4] == "---\n", "should start with ---")
	assert.Contains(t, combined, frontmatter)
	assert.Contains(t, combined, body)
}

func TestCombineFrontmatterAndContent_NoFrontmatter(t *testing.T) {
	body := "Just content"

	combined := CombineFrontmatterAndContent("", body)

	assert.Equal(t, body, combined)
}
