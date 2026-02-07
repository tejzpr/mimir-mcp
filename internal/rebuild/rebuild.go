// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package rebuild

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tejzpr/medha-mcp/internal/database"
	"github.com/tejzpr/medha-mcp/internal/memory"
	"gorm.io/gorm"
)

// Database query constants
const (
	querySlugAndUserID = "slug = ? AND user_id = ?"
)

// Options configures rebuild behavior
type Options struct {
	Force bool // Clear existing data before rebuild
}

// Result contains statistics from the rebuild operation
type Result struct {
	MemoriesProcessed int
	MemoriesCreated   int
	MemoriesSkipped   int
	TagsCreated       int
	AssociationsCreated int
	Errors            []string
}

// RebuildIndex scans git repo and rebuilds database index
func RebuildIndex(db *gorm.DB, userID, repoID uint, repoPath string, opts Options) (*Result, error) {
	result := &Result{}

	// Handle existing data check and force clear
	if err := handleExistingData(db, userID, opts); err != nil {
		return nil, err
	}

	// Scan repository for markdown files
	files, err := scanRepository(repoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to scan repository: %w", err)
	}

	log.Printf("Found %d markdown files to process", len(files))

	// First pass: Process memory files and create records
	memoryAssociations := make(map[string][]memory.Association) // slug -> associations

	for _, filePath := range files {
		result.MemoriesProcessed++

		mem, isArchived, err := processMemoryFile(db, userID, repoID, repoPath, filePath)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", filePath, err))
			continue
		}

		if mem == nil {
			// Memory already exists, skipped
			result.MemoriesSkipped++
			continue
		}

		result.MemoriesCreated++

		// Track associations for second pass
		if len(mem.Associations) > 0 {
			memoryAssociations[mem.ID] = mem.Associations
		}

		// Handle archived files
		if isArchived {
			// Set deleted_at for archived memories
			db.Model(&database.MedhaMemory{}).Where(querySlugAndUserID, mem.ID, userID).
				Update("deleted_at", time.Now())
		}
	}

	// Second pass: Process associations
	assocCount, assocErrors := processAssociations(db, userID, memoryAssociations)
	result.AssociationsCreated = assocCount
	result.Errors = append(result.Errors, assocErrors...)

	return result, nil
}

// handleExistingData checks for existing data and clears if force is enabled
func handleExistingData(db *gorm.DB, userID uint, opts Options) error {
	var memoryCount int64
	if err := db.Model(&database.MedhaMemory{}).Where("user_id = ?", userID).Count(&memoryCount).Error; err != nil {
		return fmt.Errorf("failed to count existing memories: %w", err)
	}

	if memoryCount > 0 && !opts.Force {
		return fmt.Errorf("database contains %d existing memories for this user. Use --force to clear and rebuild", memoryCount)
	}

	if opts.Force && memoryCount > 0 {
		log.Printf("Force rebuild: clearing %d existing records...", memoryCount)
		if err := clearUserIndex(db, userID); err != nil {
			return fmt.Errorf("failed to clear user index: %w", err)
		}
	}

	return nil
}

// clearUserIndex removes all index data for a user (memories, tags, associations)
func clearUserIndex(db *gorm.DB, userID uint) error {
	// 1. Delete memory-tag links
	if err := db.Exec(`DELETE FROM medha_memory_tags 
		WHERE memory_id IN (SELECT id FROM medha_memories WHERE user_id = ?)`, userID).Error; err != nil {
		return fmt.Errorf("failed to delete memory-tag links: %w", err)
	}

	// 2. Delete associations (source direction)
	if err := db.Exec(`DELETE FROM medha_memory_associations 
		WHERE source_memory_id IN (SELECT id FROM medha_memories WHERE user_id = ?)`, userID).Error; err != nil {
		return fmt.Errorf("failed to delete associations: %w", err)
	}

	// 3. Delete memories (hard delete, bypass soft delete)
	if err := db.Unscoped().Where("user_id = ?", userID).Delete(&database.MedhaMemory{}).Error; err != nil {
		return fmt.Errorf("failed to delete memories: %w", err)
	}

	return nil
}

// scanRepository walks the repo and returns memory file paths
func scanRepository(repoPath string) ([]string, error) {
	var files []string

	err := filepath.Walk(repoPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			// Skip .git directory
			if info.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}

		// Only process .md files
		if !strings.HasSuffix(strings.ToLower(info.Name()), ".md") {
			return nil
		}

		// Skip README.md files
		if strings.ToLower(info.Name()) == "readme.md" {
			return nil
		}

		files = append(files, path)
		return nil
	})

	if err != nil {
		return nil, err
	}

	return files, nil
}

// processMemoryFile parses a markdown file and creates DB records
// Returns the parsed memory, whether it's archived, and any error
func processMemoryFile(db *gorm.DB, userID, repoID uint, repoPath, filePath string) (*memory.Memory, bool, error) {
	// Read file content
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, false, fmt.Errorf("failed to read file: %w", err)
	}

	// Parse markdown with frontmatter
	mem, err := memory.ParseMarkdown(string(content))
	if err != nil {
		return nil, false, fmt.Errorf("failed to parse markdown: %w", err)
	}

	// Validate slug
	if mem.ID == "" {
		// Generate slug from filename if not in frontmatter
		baseName := filepath.Base(filePath)
		mem.ID = strings.TrimSuffix(baseName, ".md")
	}

	// Check if memory with this slug already exists
	var existingMem database.MedhaMemory
	err = db.Where(querySlugAndUserID, mem.ID, userID).First(&existingMem).Error
	if err == nil {
		// Memory already exists, skip
		log.Printf("Skipping existing memory: %s", mem.ID)
		return nil, false, nil
	}

	// Check if file is in archive directory
	relPath, _ := filepath.Rel(repoPath, filePath)
	isArchived := strings.HasPrefix(relPath, "archive"+string(filepath.Separator)) ||
		strings.HasPrefix(relPath, "archive/")

	// Create memory record
	dbMem := &database.MedhaMemory{
		UserID:   userID,
		RepoID:   repoID,
		Slug:     mem.ID,
		Title:    mem.Title,
		FilePath: filePath,
	}

	// Handle superseded_by from frontmatter
	if mem.SupersededBy != "" {
		dbMem.SupersededBy = &mem.SupersededBy
	}

	if err := db.Create(dbMem).Error; err != nil {
		return nil, false, fmt.Errorf("failed to create memory record: %w", err)
	}

	// Create tags
	for _, tagName := range mem.Tags {
		if tagName == "" {
			continue
		}

		var tag database.MedhaTag
		err := db.Where("name = ?", tagName).FirstOrCreate(&tag, database.MedhaTag{
			Name: tagName,
		}).Error
		if err != nil {
			log.Printf("Warning: failed to create tag %s: %v", tagName, err)
			continue
		}

		// Create memory-tag association
		memTag := &database.MedhaMemoryTag{
			MemoryID: dbMem.ID,
			TagID:    tag.ID,
		}
		db.Create(memTag)
	}

	return mem, isArchived, nil
}

// processAssociations creates association records for all memories
func processAssociations(db *gorm.DB, userID uint, memoryAssociations map[string][]memory.Association) (int, []string) {
	var created int
	var errors []string

	for sourceSlug, associations := range memoryAssociations {
		// Find source memory
		var sourceMem database.MedhaMemory
		if err := db.Where(querySlugAndUserID, sourceSlug, userID).First(&sourceMem).Error; err != nil {
			errors = append(errors, fmt.Sprintf("source memory not found for association: %s", sourceSlug))
			continue
		}

		for _, assoc := range associations {
			// Find target memory
			var targetMem database.MedhaMemory
			if err := db.Where(querySlugAndUserID, assoc.Target, userID).First(&targetMem).Error; err != nil {
				log.Printf("Warning: target memory not found for association: %s -> %s", sourceSlug, assoc.Target)
				continue
			}

			// Check if association already exists
			var existingAssoc database.MedhaMemoryAssociation
			err := db.Where("source_memory_id = ? AND target_memory_id = ?", sourceMem.ID, targetMem.ID).
				First(&existingAssoc).Error
			if err == nil {
				// Association already exists, skip
				continue
			}

			// Create association
			dbAssoc := &database.MedhaMemoryAssociation{
				SourceMemoryID:  sourceMem.ID,
				TargetMemoryID:  targetMem.ID,
				AssociationType: assoc.Type,
				Strength:        assoc.Strength,
			}

			if err := db.Create(dbAssoc).Error; err != nil {
				errors = append(errors, fmt.Sprintf("failed to create association %s -> %s: %v", sourceSlug, assoc.Target, err))
				continue
			}

			created++
		}
	}

	return created, errors
}

// =============================================================================
// V2 Per-User Database Functions
// =============================================================================

// Query constant for v2
const querySlugEqualsV2 = "slug = ?"

// RebuildUserIndex scans git repo and rebuilds the per-user database index
// This is the v2 function that works with UserMemory models in .medha/medha.db
func RebuildUserIndex(userDB *gorm.DB, repoPath string, opts Options) (*Result, error) {
	result := &Result{}

	// Handle existing data check and force clear
	if err := handleExistingUserData(userDB, opts); err != nil {
		return nil, err
	}

	// Scan repository for markdown files (skip .medha directory)
	files, err := scanRepositoryV2(repoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to scan repository: %w", err)
	}

	log.Printf("Found %d markdown files to process", len(files))

	// First pass: Process memory files and create records
	memoryAssociations := make(map[string][]memory.Association) // slug -> associations

	for _, filePath := range files {
		result.MemoriesProcessed++

		mem, isArchived, err := processUserMemoryFile(userDB, repoPath, filePath)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", filePath, err))
			continue
		}

		if mem == nil {
			// Memory already exists, skipped
			result.MemoriesSkipped++
			continue
		}

		result.MemoriesCreated++

		// Track associations for second pass
		if len(mem.Associations) > 0 {
			memoryAssociations[mem.ID] = mem.Associations
		}

		// Handle archived files
		if isArchived {
			// Set deleted_at for archived memories
			userDB.Model(&database.UserMemory{}).Where(querySlugEqualsV2, mem.ID).
				Update("deleted_at", time.Now())
		}
	}

	// Second pass: Process associations
	assocCount, assocErrors := processUserAssociations(userDB, memoryAssociations)
	result.AssociationsCreated = assocCount
	result.Errors = append(result.Errors, assocErrors...)

	return result, nil
}

// handleExistingUserData checks for existing data and clears if force is enabled
func handleExistingUserData(userDB *gorm.DB, opts Options) error {
	var memoryCount int64
	if err := userDB.Model(&database.UserMemory{}).Count(&memoryCount).Error; err != nil {
		return fmt.Errorf("failed to count existing memories: %w", err)
	}

	if memoryCount > 0 && !opts.Force {
		return fmt.Errorf("database contains %d existing memories. Use --force to clear and rebuild", memoryCount)
	}

	if opts.Force && memoryCount > 0 {
		log.Printf("Force rebuild: clearing %d existing records...", memoryCount)
		if err := clearUserDBIndex(userDB); err != nil {
			return fmt.Errorf("failed to clear user index: %w", err)
		}
	}

	return nil
}

// clearUserDBIndex removes all index data from the per-user database
func clearUserDBIndex(userDB *gorm.DB) error {
	// 1. Delete memory-tag links
	if err := userDB.Exec("DELETE FROM memory_tags").Error; err != nil {
		return fmt.Errorf("failed to delete memory-tag links: %w", err)
	}

	// 2. Delete associations
	if err := userDB.Exec("DELETE FROM associations").Error; err != nil {
		return fmt.Errorf("failed to delete associations: %w", err)
	}

	// 3. Delete annotations
	if err := userDB.Exec("DELETE FROM annotations").Error; err != nil {
		return fmt.Errorf("failed to delete annotations: %w", err)
	}

	// 4. Delete memories (hard delete)
	if err := userDB.Unscoped().Where("1 = 1").Delete(&database.UserMemory{}).Error; err != nil {
		return fmt.Errorf("failed to delete memories: %w", err)
	}

	// 5. Delete tags
	if err := userDB.Exec("DELETE FROM tags").Error; err != nil {
		return fmt.Errorf("failed to delete tags: %w", err)
	}

	return nil
}

// scanRepositoryV2 walks the repo and returns memory file paths (v2 version)
// Skips .git and .medha directories
func scanRepositoryV2(repoPath string) ([]string, error) {
	var files []string

	err := filepath.Walk(repoPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			// Skip .git and .medha directories
			if info.Name() == ".git" || info.Name() == ".medha" {
				return filepath.SkipDir
			}
			return nil
		}

		// Only process .md files
		if !strings.HasSuffix(strings.ToLower(info.Name()), ".md") {
			return nil
		}

		// Skip README.md files
		if strings.ToLower(info.Name()) == "readme.md" {
			return nil
		}

		files = append(files, path)
		return nil
	})

	if err != nil {
		return nil, err
	}

	return files, nil
}

// processUserMemoryFile parses a markdown file and creates UserMemory records
// Returns the parsed memory, whether it's archived, and any error
func processUserMemoryFile(userDB *gorm.DB, repoPath, filePath string) (*memory.Memory, bool, error) {
	// Read file content
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, false, fmt.Errorf("failed to read file: %w", err)
	}

	// Parse markdown with frontmatter
	mem, err := memory.ParseMarkdown(string(content))
	if err != nil {
		return nil, false, fmt.Errorf("failed to parse markdown: %w", err)
	}

	// Validate slug
	if mem.ID == "" {
		// Generate slug from filename if not in frontmatter
		baseName := filepath.Base(filePath)
		mem.ID = strings.TrimSuffix(baseName, ".md")
	}

	// Check if memory with this slug already exists
	var existingMem database.UserMemory
	err = userDB.Where(querySlugEqualsV2, mem.ID).First(&existingMem).Error
	if err == nil {
		// Memory already exists, skip
		log.Printf("Skipping existing memory: %s", mem.ID)
		return nil, false, nil
	}

	// Check if file is in archive directory
	relPath, _ := filepath.Rel(repoPath, filePath)
	isArchived := strings.HasPrefix(relPath, "archive"+string(filepath.Separator)) ||
		strings.HasPrefix(relPath, "archive/")

	// Calculate content hash
	contentHash := CalculateContentHash(string(content))

	// Create memory record
	dbMem := &database.UserMemory{
		Slug:        mem.ID,
		Title:       mem.Title,
		FilePath:    relPath, // Store relative path for portability
		ContentHash: contentHash,
	}

	// Handle superseded_by from frontmatter
	if mem.SupersededBy != "" {
		dbMem.SupersededBy = &mem.SupersededBy
	}

	if err := userDB.Create(dbMem).Error; err != nil {
		return nil, false, fmt.Errorf("failed to create memory record: %w", err)
	}

	// Create tags
	for _, tagName := range mem.Tags {
		if tagName == "" {
			continue
		}

		var tag database.UserTag
		err := userDB.Where("name = ?", tagName).FirstOrCreate(&tag, database.UserTag{
			Name: tagName,
		}).Error
		if err != nil {
			log.Printf("Warning: failed to create tag %s: %v", tagName, err)
			continue
		}

		// Create memory-tag association
		memTag := &database.UserMemoryTag{
			MemorySlug: mem.ID,
			TagName:    tagName,
		}
		userDB.Create(memTag)
	}

	return mem, isArchived, nil
}

// processUserAssociations creates association records for all memories in per-user DB
func processUserAssociations(userDB *gorm.DB, memoryAssociations map[string][]memory.Association) (int, []string) {
	var created int
	var errors []string

	for sourceSlug, associations := range memoryAssociations {
		// Verify source memory exists
		var sourceMem database.UserMemory
		if err := userDB.Where(querySlugEqualsV2, sourceSlug).First(&sourceMem).Error; err != nil {
			errors = append(errors, fmt.Sprintf("source memory not found for association: %s", sourceSlug))
			continue
		}

		for _, assoc := range associations {
			// Verify target memory exists
			var targetMem database.UserMemory
			if err := userDB.Where(querySlugEqualsV2, assoc.Target).First(&targetMem).Error; err != nil {
				log.Printf("Warning: target memory not found for association: %s -> %s", sourceSlug, assoc.Target)
				continue
			}

			// Check if association already exists
			var existingAssoc database.UserMemoryAssociation
			err := userDB.Where("source_slug = ? AND target_slug = ?", sourceSlug, assoc.Target).
				First(&existingAssoc).Error
			if err == nil {
				// Association already exists, skip
				continue
			}

			// Create association
			dbAssoc := &database.UserMemoryAssociation{
				SourceSlug:      sourceSlug,
				TargetSlug:      assoc.Target,
				AssociationType: assoc.Type,
				Strength:        assoc.Strength,
			}

			if err := userDB.Create(dbAssoc).Error; err != nil {
				errors = append(errors, fmt.Sprintf("failed to create association %s -> %s: %v", sourceSlug, assoc.Target, err))
				continue
			}

			created++
		}
	}

	return created, errors
}

// CalculateContentHash computes a hash of the content for cache invalidation
// Exported for use in tests and other packages
func CalculateContentHash(content string) string {
	// Simple hash using FNV-1a
	const offset64 = 14695981039346656037
	const prime64 = 1099511628211

	hash := uint64(offset64)
	for i := 0; i < len(content); i++ {
		hash ^= uint64(content[i])
		hash *= prime64
	}

	return fmt.Sprintf("%x", hash)
}
