// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package memory

import (
	"bytes"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseMarkdown parses markdown content with YAML frontmatter
func ParseMarkdown(content string) (*Memory, error) {
	// Split frontmatter from content
	frontmatter, body, err := splitFrontmatter(content)
	if err != nil {
		return nil, fmt.Errorf("failed to split frontmatter: %w", err)
	}

	// Parse frontmatter
	var memory Memory
	if frontmatter != "" {
		if err := yaml.Unmarshal([]byte(frontmatter), &memory); err != nil {
			return nil, fmt.Errorf("failed to parse frontmatter: %w", err)
		}
	}

	// Set content
	memory.Content = strings.TrimSpace(body)

	return &memory, nil
}

// ToMarkdown converts a Memory to markdown with frontmatter
func (m *Memory) ToMarkdown() (string, error) {
	var buf bytes.Buffer

	// Write frontmatter
	buf.WriteString("---\n")

	frontmatterData, err := yaml.Marshal(m)
	if err != nil {
		return "", fmt.Errorf("failed to marshal frontmatter: %w", err)
	}

	buf.Write(frontmatterData)
	buf.WriteString("---\n\n")

	// Write content
	buf.WriteString(m.Content)
	buf.WriteString("\n")

	return buf.String(), nil
}

// splitFrontmatter splits markdown content into frontmatter and body
func splitFrontmatter(content string) (string, string, error) {
	content = strings.TrimSpace(content)

	// Check if content starts with ---
	if !strings.HasPrefix(content, "---") {
		// No frontmatter
		return "", content, nil
	}

	// Find the closing ---
	lines := strings.Split(content, "\n")
	if len(lines) < 3 {
		return "", content, nil
	}

	// Find closing delimiter
	closingIndex := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			closingIndex = i
			break
		}
	}

	if closingIndex == -1 {
		return "", content, fmt.Errorf("frontmatter not properly closed")
	}

	// Extract frontmatter (lines between first and second ---)
	frontmatter := strings.Join(lines[1:closingIndex], "\n")

	// Extract body (everything after closing ---)
	body := ""
	if closingIndex+1 < len(lines) {
		body = strings.Join(lines[closingIndex+1:], "\n")
	}

	return frontmatter, body, nil
}

// AddAssociation adds an association to the memory
func (m *Memory) AddAssociation(target, associationType string, strength float64) {
	// Check if association already exists
	for i, assoc := range m.Associations {
		if assoc.Target == target {
			// Update existing association
			m.Associations[i].Type = associationType
			m.Associations[i].Strength = strength
			return
		}
	}

	// Add new association
	m.Associations = append(m.Associations, Association{
		Target:   target,
		Type:     associationType,
		Strength: strength,
	})
}

// RemoveAssociation removes an association from the memory
func (m *Memory) RemoveAssociation(target string) bool {
	for i, assoc := range m.Associations {
		if assoc.Target == target {
			// Remove association
			m.Associations = append(m.Associations[:i], m.Associations[i+1:]...)
			return true
		}
	}
	return false
}

// GetAssociation retrieves an association by target
func (m *Memory) GetAssociation(target string) (*Association, bool) {
	for _, assoc := range m.Associations {
		if assoc.Target == target {
			return &assoc, true
		}
	}
	return nil, false
}
