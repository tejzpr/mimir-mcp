// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package memory

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

var (
	// slugRegex matches characters that should be kept in slugs
	slugRegex = regexp.MustCompile(`[^a-z0-9\s-]`)
	// multiSpaceRegex matches multiple spaces/dashes
	multiSpaceRegex = regexp.MustCompile(`[\s-]+`)
)

// GenerateSlug creates a human-readable slug from a title
func GenerateSlug(title string) string {
	// Convert to lowercase
	slug := strings.ToLower(title)

	// Remove special characters except spaces and dashes
	slug = slugRegex.ReplaceAllString(slug, "")

	// Replace spaces with dashes and collapse multiple dashes
	slug = multiSpaceRegex.ReplaceAllString(slug, "-")

	// Trim dashes from start and end
	slug = strings.Trim(slug, "-")

	// Add date suffix
	dateSuffix := time.Now().Format("2006-01-02")
	slug = fmt.Sprintf("%s-%s", slug, dateSuffix)

	return slug
}

// GenerateSlugWithDate creates a slug with a specific date
func GenerateSlugWithDate(title string, date time.Time) string {
	// Convert to lowercase
	slug := strings.ToLower(title)

	// Remove special characters except spaces and dashes
	slug = slugRegex.ReplaceAllString(slug, "")

	// Replace spaces with dashes and collapse multiple dashes
	slug = multiSpaceRegex.ReplaceAllString(slug, "-")

	// Trim dashes from start and end
	slug = strings.Trim(slug, "-")

	// Add date suffix
	dateSuffix := date.Format("2006-01-02")
	slug = fmt.Sprintf("%s-%s", slug, dateSuffix)

	return slug
}

// ValidateSlug checks if a slug is valid
func ValidateSlug(slug string) error {
	if slug == "" {
		return fmt.Errorf("slug cannot be empty")
	}

	// Check length (reasonable limits)
	if len(slug) < 3 {
		return fmt.Errorf("slug must be at least 3 characters")
	}

	if len(slug) > 200 {
		return fmt.Errorf("slug cannot exceed 200 characters")
	}

	// Check format (lowercase alphanumeric with dashes)
	validSlugRegex := regexp.MustCompile(`^[a-z0-9][a-z0-9-]*[a-z0-9]$`)
	if !validSlugRegex.MatchString(slug) {
		return fmt.Errorf("slug must contain only lowercase letters, numbers, and dashes")
	}

	return nil
}

// SanitizeTitle removes invalid characters from a title
func SanitizeTitle(title string) string {
	// Trim whitespace
	title = strings.TrimSpace(title)

	// Remove control characters
	controlRegex := regexp.MustCompile(`[\x00-\x1F\x7F]`)
	title = controlRegex.ReplaceAllString(title, "")

	return title
}
