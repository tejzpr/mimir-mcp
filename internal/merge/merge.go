// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package merge

import (
	"strings"
)

// Strategy defines the interface for merge strategies
type Strategy interface {
	// Merge performs a merge operation
	// base: common ancestor content
	// ours: local content
	// theirs: remote content
	Merge(base, ours, theirs string) (string, error)
}

// Result represents the result of a merge operation
type Result struct {
	Content   string
	HasConflict bool
	Conflicts []Conflict
}

// Conflict represents a merge conflict
type Conflict struct {
	StartLine int
	EndLine   int
	Ours      string
	Theirs    string
}

// ThreeWayMerge performs a three-way merge of content
// Returns merged content and whether there were conflicts
func ThreeWayMerge(base, ours, theirs string) (string, bool) {
	// Split into lines
	baseLines := strings.Split(base, "\n")
	ourLines := strings.Split(ours, "\n")
	theirLines := strings.Split(theirs, "\n")

	// Simple case: identical content
	if ours == theirs {
		return ours, false
	}

	// Simple case: only one side changed
	if ours == base {
		return theirs, false
	}
	if theirs == base {
		return ours, false
	}

	// Both changed - attempt line-by-line merge
	merged, hasConflict := mergeLines(baseLines, ourLines, theirLines)
	return strings.Join(merged, "\n"), hasConflict
}

// mergeLines merges lines using longest common subsequence
func mergeLines(base, ours, theirs []string) ([]string, bool) {
	// Find common parts
	hasConflict := false
	merged := make([]string, 0)

	// Simple approach: try to merge section by section
	// This is a basic implementation - production would use more sophisticated diff3

	maxLen := max(len(base), max(len(ours), len(theirs)))

	for i := 0; i < maxLen; i++ {
		baseLine := getLine(base, i)
		ourLine := getLine(ours, i)
		theirLine := getLine(theirs, i)

		if ourLine == theirLine {
			// Both agree
			merged = append(merged, ourLine)
		} else if ourLine == baseLine {
			// Only theirs changed
			merged = append(merged, theirLine)
		} else if theirLine == baseLine {
			// Only ours changed
			merged = append(merged, ourLine)
		} else {
			// Both changed differently - conflict
			hasConflict = true
			merged = append(merged, "<<<<<<< OURS")
			merged = append(merged, ourLine)
			merged = append(merged, "=======")
			merged = append(merged, theirLine)
			merged = append(merged, ">>>>>>> THEIRS")
		}
	}

	return merged, hasConflict
}

func getLine(lines []string, idx int) string {
	if idx < len(lines) {
		return lines[idx]
	}
	return ""
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// MergeTags merges two sets of tags (union)
func MergeTags(ours, theirs []string) []string {
	tagSet := make(map[string]bool)

	for _, tag := range ours {
		tagSet[tag] = true
	}
	for _, tag := range theirs {
		tagSet[tag] = true
	}

	result := make([]string, 0, len(tagSet))
	for tag := range tagSet {
		result = append(result, tag)
	}

	return result
}

// MergeStringSlice merges two string slices, removing duplicates
func MergeStringSlice(ours, theirs []string) []string {
	return MergeTags(ours, theirs) // Same logic
}

// HasConflictMarkers checks if content contains git conflict markers
func HasConflictMarkers(content string) bool {
	return strings.Contains(content, "<<<<<<<") ||
		strings.Contains(content, "=======") ||
		strings.Contains(content, ">>>>>>>")
}
