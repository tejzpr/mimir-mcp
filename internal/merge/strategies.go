// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package merge

import (
	"regexp"
	"strings"
	"time"
)

// Memory represents a memory for merging purposes
type Memory struct {
	Slug         string
	Title        string
	Content      string
	Tags         []string
	SupersededBy string
	UpdatedAt    time.Time
	Annotations  []Annotation
}

// Annotation represents a memory annotation
type Annotation struct {
	Type      string
	Content   string
	CreatedAt time.Time
	CreatedBy string
}

// LastWriteWinsStrategy implements Strategy with last-write-wins for metadata
type LastWriteWinsStrategy struct{}

// NewLastWriteWinsStrategy creates a new last-write-wins strategy
func NewLastWriteWinsStrategy() *LastWriteWinsStrategy {
	return &LastWriteWinsStrategy{}
}

// Merge implements Strategy.Merge with last-write-wins
func (s *LastWriteWinsStrategy) Merge(base, ours, theirs string) (string, error) {
	// For simple content, just use theirs (last write wins)
	return theirs, nil
}

// ContentBasedStrategy implements Strategy with intelligent content merging
type ContentBasedStrategy struct{}

// NewContentBasedStrategy creates a new content-based strategy
func NewContentBasedStrategy() *ContentBasedStrategy {
	return &ContentBasedStrategy{}
}

// Merge implements Strategy.Merge with three-way merge
func (s *ContentBasedStrategy) Merge(base, ours, theirs string) (string, error) {
	merged, _ := ThreeWayMerge(base, ours, theirs)
	return merged, nil
}

// MergeFrontmatter merges memory metadata (frontmatter)
// Uses last-write-wins for most fields, but special handling for some
func MergeFrontmatter(ours, theirs *Memory) *Memory {
	result := &Memory{}

	// Last-write-wins for title
	if theirs.UpdatedAt.After(ours.UpdatedAt) {
		result.Title = theirs.Title
		result.SupersededBy = theirs.SupersededBy
		result.UpdatedAt = theirs.UpdatedAt
	} else {
		result.Title = ours.Title
		result.SupersededBy = ours.SupersededBy
		result.UpdatedAt = ours.UpdatedAt
	}

	// Keep the slug from ours (should be the same anyway)
	result.Slug = ours.Slug

	// Union for tags
	result.Tags = MergeTags(ours.Tags, theirs.Tags)

	// Union for annotations
	result.Annotations = MergeAnnotations(ours.Annotations, theirs.Annotations)

	return result
}

// MergeAnnotations merges two sets of annotations
// Avoids duplicates based on content and type
func MergeAnnotations(ours, theirs []Annotation) []Annotation {
	// Create a map to track unique annotations
	seen := make(map[string]bool)
	result := make([]Annotation, 0)

	addAnnotation := func(a Annotation) {
		key := a.Type + "|" + a.Content
		if !seen[key] {
			seen[key] = true
			result = append(result, a)
		}
	}

	for _, a := range ours {
		addAnnotation(a)
	}
	for _, a := range theirs {
		addAnnotation(a)
	}

	return result
}

// SplitFrontmatterAndContent separates YAML frontmatter from markdown content
func SplitFrontmatterAndContent(content string) (frontmatter, body string) {
	if !strings.HasPrefix(content, "---") {
		return "", content
	}

	// Find the closing ---
	parts := strings.SplitN(content[3:], "---", 2)
	if len(parts) < 2 {
		return "", content
	}

	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}

// CombineFrontmatterAndContent combines frontmatter with content
func CombineFrontmatterAndContent(frontmatter, body string) string {
	if frontmatter == "" {
		return body
	}
	return "---\n" + frontmatter + "\n---\n\n" + body
}

// ExtractTags extracts tags from frontmatter
func ExtractTags(frontmatter string) []string {
	// Simple regex for tags
	re := regexp.MustCompile(`tags:\s*\[(.*?)\]`)
	match := re.FindStringSubmatch(frontmatter)
	if len(match) < 2 {
		return []string{}
	}

	tagStr := match[1]
	tags := strings.Split(tagStr, ",")
	result := make([]string, 0, len(tags))
	for _, tag := range tags {
		t := strings.TrimSpace(tag)
		t = strings.Trim(t, "\"'")
		if t != "" {
			result = append(result, t)
		}
	}

	return result
}

// UpdateTags updates tags in frontmatter
func UpdateTags(frontmatter string, tags []string) string {
	// Build new tags string
	tagStrs := make([]string, len(tags))
	for i, tag := range tags {
		tagStrs[i] = "\"" + tag + "\""
	}
	newTags := "tags: [" + strings.Join(tagStrs, ", ") + "]"

	// Replace existing tags or add new
	re := regexp.MustCompile(`tags:\s*\[.*?\]`)
	if re.MatchString(frontmatter) {
		return re.ReplaceAllString(frontmatter, newTags)
	}

	// Add tags at the end
	return frontmatter + "\n" + newTags
}

// ResolveConflict attempts to automatically resolve a conflict
// Returns resolved content or original if cannot resolve
func ResolveConflict(conflictedContent string) (string, bool) {
	if !HasConflictMarkers(conflictedContent) {
		return conflictedContent, true
	}

	// Try to resolve by keeping both changes
	re := regexp.MustCompile(`(?s)<<<<<<< OURS\n(.*?)\n=======\n(.*?)\n>>>>>>> THEIRS`)
	resolved := re.ReplaceAllString(conflictedContent, "$1\n$2")

	if resolved != conflictedContent {
		return resolved, true
	}

	return conflictedContent, false
}
