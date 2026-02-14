// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/tejzpr/medha-mcp/internal/database"
	"github.com/tejzpr/medha-mcp/internal/git"
	"github.com/tejzpr/medha-mcp/internal/locking"
	"github.com/tejzpr/medha-mcp/internal/memory"
	"gorm.io/gorm"
)

// NewRememberTool creates the medha_remember tool definition
func NewRememberTool() mcp.Tool {
	return mcp.NewTool("medha_remember",
		mcp.WithDescription("Store information in memory. Use for new information OR updating existing information. If updating, the old version is preserved in history. If this replaces/supersedes an old memory, specify 'replaces' to link them properly."),
		mcp.WithString("title",
			mcp.Required(),
			mcp.Description("Clear title for the memory"),
		),
		mcp.WithString("content",
			mcp.Required(),
			mcp.Description("The information to remember (markdown)"),
		),
		mcp.WithString("slug",
			mcp.Description("Short ID. If exists, updates that memory. If new, creates with this ID."),
		),
		mcp.WithString("replaces",
			mcp.Description("Slug of memory this supersedes. Old memory is marked as outdated."),
		),
		mcp.WithArray("tags",
			mcp.Description("Labels for organization"),
			mcp.WithStringItems(),
		),
		mcp.WithString("path",
			mcp.Description("Folder path. Example: 'projects/alpha/decisions'"),
		),
		mcp.WithString("note",
			mcp.Description("Add a note/annotation to the memory. Can be combined with content updates."),
		),
		mcp.WithArray("connections",
			mcp.Description("Link to related memories. Array of objects: [{\"to\": \"slug\", \"relationship\": \"related|part_of|references|person\"}]"),
			mcp.Items(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"to":           map[string]any{"type": "string", "description": "Slug of the related memory"},
					"relationship": map[string]any{"type": "string", "description": "Type of connection: related, part_of, references, or person"},
					"strength":     map[string]any{"type": "number", "description": "Relationship importance from 0.0 to 1.0"},
				},
				"required": []string{"to"},
			}),
		),
	)
}

// Connection represents a connection to create with a memory
type Connection struct {
	To           string  `json:"to"`
	Relationship string  `json:"relationship"`
	Strength     float64 `json:"strength,omitempty"`
}

// RememberHandler handles the medha_remember tool
// Uses v2 architecture: UserDB for per-user memories
func RememberHandler(ctx *ToolContext, userID uint) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(c context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Parse required fields
		title, err := request.RequireString("title")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		content, err := request.RequireString("content")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		// Parse optional fields
		slug := request.GetString("slug", "")
		replaces := request.GetString("replaces", "")
		tags := request.GetStringSlice("tags", []string{})
		pathFolder := request.GetString("path", "")
		note := request.GetString("note", "")
		connections := parseConnections(request)

		// Validate UserDB is available
		if ctx.UserDB == nil {
			return mcp.NewToolResultError("per-user database not available"), nil
		}

		// Get user's repo from system DB
		var repo database.MedhaGitRepo
		err = ctx.DB.Where("user_id = ?", userID).First(&repo).Error
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to get user repository: %v", err)), nil
		}

		// Sanitize title
		title = memory.SanitizeTitle(title)
		if title == "" {
			return mcp.NewToolResultError("title cannot be empty after sanitization"), nil
		}

		// Determine if this is an update or create
		if slug != "" {
			// Try to find existing memory in UserDB
			var existingMem database.UserMemory
			err = ctx.UserDB.Where("slug = ?", slug).First(&existingMem).Error
			if err == nil {
				// Memory exists - update it
				result, updateErr := handleUpdateV2(ctx, &existingMem, title, content, tags, repo.RepoPath)
				if updateErr != nil {
					return result, updateErr
				}
				// If note provided, also add annotation
				if note != "" {
					annotationResult, _ := handleAnnotationV2(ctx, slug, note, repo.RepoPath)
					// Append annotation result to update result
					if annotationResult != nil {
						return mcp.NewToolResultText(result.Content[0].(mcp.TextContent).Text + "\n" + annotationResult.Content[0].(mcp.TextContent).Text), nil
					}
				}
				return result, nil
			} else if err != gorm.ErrRecordNotFound {
				return mcp.NewToolResultError(fmt.Sprintf("database error: %v", err)), nil
			}
			// Memory doesn't exist - validate custom slug
			if err := memory.ValidateSlug(slug); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("invalid slug: %v", err)), nil
			}
		} else {
			// Generate slug from title
			slug = memory.GenerateSlug(title)
			if err := memory.ValidateSlug(slug); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("invalid slug: %v", err)), nil
			}

			// Check if generated slug already exists in UserDB
			var existingMem database.UserMemory
			err = ctx.UserDB.Where("slug = ?", slug).First(&existingMem).Error
			if err == nil {
				return mcp.NewToolResultError(fmt.Sprintf("memory with slug '%s' already exists. Provide a custom 'slug' parameter to update or create with different ID.", slug)), nil
			}
		}

		// Create new memory
		result, err := handleCreateV2(ctx, slug, title, content, tags, pathFolder, repo.RepoPath)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		// Handle supersession if specified
		if replaces != "" {
			err = handleSupersessionV2(ctx, slug, replaces, repo.RepoPath)
			if err != nil {
				// Log but don't fail - memory was created successfully
				result = result + fmt.Sprintf("\n\nWarning: Failed to mark '%s' as superseded: %v", replaces, err)
			} else {
				result = result + fmt.Sprintf("\n\nSupersedes: '%s' (marked as outdated)", replaces)
			}
		}

		// Handle connections if specified
		if len(connections) > 0 {
			connResults := handleConnectionsV2(ctx, slug, connections)
			if connResults != "" {
				result = result + "\n\n" + connResults
			}
		}

		return mcp.NewToolResultText(result), nil
	}
}

// handleCreateV2 creates a new memory (v2 architecture)
func handleCreateV2(ctx *ToolContext, slug, title, content string, tags []string, pathFolder, repoPath string) (string, error) {
	now := time.Now()

	// Create memory object
	mem := &memory.Memory{
		ID:           slug,
		Title:        title,
		Tags:         tags,
		Created:      now,
		Updated:      now,
		Associations: []memory.Association{},
		Content:      content,
	}

	// Generate markdown
	markdown, err := mem.ToMarkdown()
	if err != nil {
		return "", fmt.Errorf("failed to generate markdown: %w", err)
	}

	// Determine file path
	organizer := memory.NewOrganizer(repoPath)
	var filePath string
	if pathFolder != "" {
		// Use custom path
		filePath = filepath.Join(repoPath, pathFolder, slug+".md")
	} else {
		// Use organizer's default path determination
		filePath = organizer.GetMemoryPath(slug, tags, "", now)
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return "", fmt.Errorf("failed to create directory: %w", err)
	}

	// Write file
	if err := os.WriteFile(filePath, []byte(markdown), 0644); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}

	// Git commit
	gitRepo, err := git.OpenRepository(repoPath)
	if err != nil {
		return "", fmt.Errorf("failed to open git repo: %w", err)
	}

	msgFormat := git.CommitMessageFormats{}
	if err := gitRepo.CommitFile(filePath, msgFormat.CreateMemory(slug)); err != nil {
		return "", fmt.Errorf("failed to commit: %w", err)
	}

	// Calculate content hash for embedding staleness detection
	contentHash := computeContentHash(content)

	// Store in UserDB (v2 architecture)
	dbMem := &database.UserMemory{
		Slug:           slug,
		Title:          title,
		FilePath:       filePath,
		LastAccessedAt: now,
		AccessCount:    0,
		ContentHash:    contentHash,
		Version:        1,
	}
	if err := ctx.UserDB.Create(dbMem).Error; err != nil {
		return "", fmt.Errorf("failed to store memory: %w", err)
	}

	// Store tags in UserDB
	storeTagsV2(ctx, slug, tags)

	return fmt.Sprintf("Memory created: %s\nSlug: %s\nPath: %s", title, slug, filePath), nil
}

// handleUpdateV2 updates an existing memory (v2 architecture)
func handleUpdateV2(ctx *ToolContext, dbMem *database.UserMemory, title, content string, tags []string, repoPath string) (*mcp.CallToolResult, error) {
	// Check if memory is deleted
	if dbMem.DeletedAt.Valid {
		return mcp.NewToolResultError(fmt.Sprintf("memory '%s' has been archived. Use medha_restore first.", dbMem.Slug)), nil
	}

	// Capture original version for optimistic locking
	originalVersion := dbMem.Version

	// Read current memory
	markdownContent, err := os.ReadFile(dbMem.FilePath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to read file: %v", err)), nil
	}

	mem, err := memory.ParseMarkdown(string(markdownContent))
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to parse markdown: %v", err)), nil
	}

	// Update fields
	newTitle := dbMem.Title
	if title != "" {
		title = memory.SanitizeTitle(title)
		mem.Title = title
		newTitle = title
	}
	mem.Content = content
	mem.Updated = time.Now()

	// Update tags if provided
	if len(tags) > 0 {
		mem.Tags = tags
		ctx.UserDB.Where("memory_slug = ?", dbMem.Slug).Delete(&database.UserMemoryTag{})
		storeTagsV2(ctx, dbMem.Slug, tags)
	}

	// Generate updated markdown
	markdown, err := mem.ToMarkdown()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to generate markdown: %v", err)), nil
	}

	// Write file
	if err := os.WriteFile(dbMem.FilePath, []byte(markdown), 0644); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to write file: %v", err)), nil
	}

	// Git commit
	gitRepo, err := git.OpenRepository(repoPath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to open git repo: %v", err)), nil
	}

	msgFormat := git.CommitMessageFormats{}
	if err := gitRepo.CommitFile(dbMem.FilePath, msgFormat.UpdateMemory(dbMem.Slug)); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to commit: %v", err)), nil
	}

	// Update database with optimistic locking
	now := time.Now()
	contentHash := computeContentHash(content)
	updates := map[string]interface{}{
		"title":        newTitle,
		"updated_at":   now,
		"content_hash": contentHash,
	}

	err = locking.RetryWithBackoff(locking.MaxRetries, locking.RetryDelay, func() error {
		return locking.UpdateWithVersion(ctx.UserDB, "memories", dbMem.Slug, originalVersion, updates)
	})

	if err != nil {
		if _, ok := err.(*locking.ConflictError); ok {
			return mcp.NewToolResultError("Memory was modified by another agent. Please recall and retry."), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("failed to update database: %v", err)), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Memory updated: %s", dbMem.Slug)), nil
}

// handleAnnotationV2 adds an annotation to a memory (v2 architecture)
func handleAnnotationV2(ctx *ToolContext, slug, note, repoPath string) (*mcp.CallToolResult, error) {
	// Get memory from UserDB for file path
	var dbMem database.UserMemory
	if err := ctx.UserDB.Where("slug = ?", slug).First(&dbMem).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return mcp.NewToolResultError(fmt.Sprintf("memory not found: %s", slug)), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("database error: %v", err)), nil
	}

	// Determine annotation type based on content
	annotationType := "context"
	if containsWord(note, "wrong", "incorrect", "error", "mistake", "fix") {
		annotationType = "correction"
	} else if containsWord(note, "clarify", "clarification", "note", "actually") {
		annotationType = "clarification"
	} else if containsWord(note, "deprecated", "outdated", "old", "superseded") {
		annotationType = "deprecated"
	}

	// Read current memory file
	markdownContent, err := os.ReadFile(dbMem.FilePath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to read file: %v", err)), nil
	}

	mem, err := memory.ParseMarkdown(string(markdownContent))
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to parse markdown: %v", err)), nil
	}

	// Add annotation to frontmatter
	mem.Annotations = append(mem.Annotations, memory.Annotation{
		Type:      annotationType,
		Content:   note,
		CreatedAt: time.Now(),
	})
	mem.Updated = time.Now()

	// Generate updated markdown
	markdown, err := mem.ToMarkdown()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to generate markdown: %v", err)), nil
	}

	// Write file
	if err := os.WriteFile(dbMem.FilePath, []byte(markdown), 0644); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to write file: %v", err)), nil
	}

	// Git commit
	gitRepo, err := git.OpenRepository(repoPath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to open git repo: %v", err)), nil
	}

	msgFormat := git.CommitMessageFormats{}
	if err := gitRepo.CommitFile(dbMem.FilePath, msgFormat.AddAnnotation(slug, annotationType)); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to commit: %v", err)), nil
	}

	// Store annotation in UserDB
	annotation := &database.UserAnnotation{
		MemorySlug: slug,
		Type:       annotationType,
		Content:    note,
		CreatedAt:  time.Now(),
	}
	ctx.UserDB.Create(annotation)

	return mcp.NewToolResultText(fmt.Sprintf("Annotation added to '%s' (type: %s)", slug, annotationType)), nil
}

// handleSupersessionV2 marks an old memory as superseded by a new one (v2 architecture)
func handleSupersessionV2(ctx *ToolContext, newSlug, oldSlug, repoPath string) error {
	// Get old memory from UserDB
	var oldMem database.UserMemory
	if err := ctx.UserDB.Where("slug = ?", oldSlug).First(&oldMem).Error; err != nil {
		return fmt.Errorf("old memory not found: %s", oldSlug)
	}

	// Get new memory from UserDB
	var newMem database.UserMemory
	if err := ctx.UserDB.Where("slug = ?", newSlug).First(&newMem).Error; err != nil {
		return fmt.Errorf("new memory not found: %s", newSlug)
	}

	// Read old memory file to update frontmatter
	markdownContent, err := os.ReadFile(oldMem.FilePath)
	if err != nil {
		return fmt.Errorf("failed to read old memory file: %w", err)
	}

	mem, err := memory.ParseMarkdown(string(markdownContent))
	if err != nil {
		return fmt.Errorf("failed to parse old memory markdown: %w", err)
	}

	// Update frontmatter with superseded_by
	mem.SupersededBy = newSlug
	mem.Updated = time.Now()

	// Generate updated markdown
	markdown, err := mem.ToMarkdown()
	if err != nil {
		return fmt.Errorf("failed to generate updated markdown: %w", err)
	}

	// Write updated file
	if err := os.WriteFile(oldMem.FilePath, []byte(markdown), 0644); err != nil {
		return fmt.Errorf("failed to write updated file: %w", err)
	}

	// Mark old memory as superseded in UserDB
	oldMem.SupersededBy = &newSlug
	if err := ctx.UserDB.Save(&oldMem).Error; err != nil {
		return fmt.Errorf("failed to update old memory: %w", err)
	}

	// Create association in UserDB (slug-based)
	association := &database.UserMemoryAssociation{
		SourceSlug:      newSlug,
		TargetSlug:      oldSlug,
		AssociationType: database.AssociationTypeSupersedes,
		Strength:        1.0,
	}
	if err := ctx.UserDB.Create(association).Error; err != nil {
		// Not a critical error - memory is still marked as superseded
		return nil
	}

	// Git commit
	gitRepo, err := git.OpenRepository(repoPath)
	if err == nil {
		msgFormat := git.CommitMessageFormats{}
		_ = gitRepo.CommitFile(oldMem.FilePath, msgFormat.SupersedeMemory(oldSlug, newSlug))
	}

	return nil
}

// storeTagsV2 stores tags for a memory (v2 architecture using slugs)
func storeTagsV2(ctx *ToolContext, memorySlug string, tags []string) {
	for _, tagName := range tags {
		// Create tag if not exists
		var tag database.UserTag
		ctx.UserDB.Where("name = ?", tagName).FirstOrCreate(&tag, database.UserTag{
			Name: tagName,
		})

		// Create memory-tag relationship
		memTag := &database.UserMemoryTag{
			MemorySlug: memorySlug,
			TagName:    tagName,
		}
		ctx.UserDB.Create(memTag)
	}
}

// containsWord checks if text contains any of the given words (case-insensitive)
func containsWord(text string, words ...string) bool {
	lowText := text
	for _, w := range words {
		if len(w) > 0 && len(lowText) >= len(w) {
			for i := 0; i <= len(lowText)-len(w); i++ {
				if lowText[i:i+len(w)] == w {
					return true
				}
			}
		}
	}
	return false
}

// parseConnections extracts connection objects from the request
func parseConnections(request mcp.CallToolRequest) []Connection {
	var connections []Connection

	// Try to get the connections array from arguments
	if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
		if connArray, ok := args["connections"].([]interface{}); ok {
			for _, item := range connArray {
				if connMap, ok := item.(map[string]interface{}); ok {
					conn := Connection{
						Strength: 0.5, // default
					}
					if to, ok := connMap["to"].(string); ok {
						conn.To = to
					}
					if rel, ok := connMap["relationship"].(string); ok {
						conn.Relationship = rel
					}
					if strength, ok := connMap["strength"].(float64); ok {
						conn.Strength = strength
					}
					if conn.To != "" {
						connections = append(connections, conn)
					}
				}
			}
		}
	}

	return connections
}

// handleConnectionsV2 creates associations for the newly created memory (v2 architecture)
func handleConnectionsV2(ctx *ToolContext, sourceSlug string, connections []Connection) string {
	var results []string

	// Verify source memory exists
	var sourceMem database.UserMemory
	if err := ctx.UserDB.Where("slug = ?", sourceSlug).First(&sourceMem).Error; err != nil {
		return fmt.Sprintf("Warning: Could not find source memory for connections: %v", err)
	}

	for _, conn := range connections {
		// Verify target memory exists
		var targetMem database.UserMemory
		if err := ctx.UserDB.Where("slug = ?", conn.To).First(&targetMem).Error; err != nil {
			results = append(results, fmt.Sprintf("- Connection to '%s' failed: memory not found", conn.To))
			continue
		}

		// Map relationship type
		assocType := mapRelationshipTypeForRemember(conn.Relationship)
		if assocType == "" {
			assocType = database.AssociationTypeRelatedTo
		}

		// Set default strength if not specified
		strength := conn.Strength
		if strength <= 0 {
			strength = 0.5
		}

		// Create association in UserDB (slug-based)
		association := &database.UserMemoryAssociation{
			SourceSlug:      sourceSlug,
			TargetSlug:      conn.To,
			AssociationType: assocType,
			Strength:        strength,
		}
		if err := ctx.UserDB.Create(association).Error; err != nil {
			results = append(results, fmt.Sprintf("- Connection to '%s' failed: %v", conn.To, err))
			continue
		}

		// Create reverse for bidirectional types
		if !database.IsDirectionalType(assocType) {
			reverseAssoc := &database.UserMemoryAssociation{
				SourceSlug:      conn.To,
				TargetSlug:      sourceSlug,
				AssociationType: assocType,
				Strength:        strength,
			}
			ctx.UserDB.Create(reverseAssoc)
		}

		results = append(results, fmt.Sprintf("- Connected to '%s' (%s)", conn.To, assocType))
	}

	if len(results) > 0 {
		return "Connections:\n" + joinStrings(results, "\n")
	}
	return ""
}

// mapRelationshipTypeForRemember maps user-friendly names to internal constants
func mapRelationshipTypeForRemember(relationship string) string {
	switch relationship {
	case "related", "related_to", "":
		return database.AssociationTypeRelatedTo
	case "references", "ref":
		return database.AssociationTypeReferences
	case "follows", "after":
		return database.AssociationTypeFollows
	case "precedes", "before":
		return database.AssociationTypePrecedes
	case "supersedes", "replaces":
		return database.AssociationTypeSupersedes
	case "part_of", "part", "belongs_to":
		return database.AssociationTypePartOf
	case "project", "related_project":
		return database.AssociationTypeRelatedProject
	case "person":
		return database.AssociationTypePerson
	default:
		return database.AssociationTypeRelatedTo
	}
}


// joinStrings joins strings with a separator
func joinStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	result := strs[0]
	for i := 1; i < len(strs); i++ {
		result += sep + strs[i]
	}
	return result
}

// computeContentHash computes a SHA256 hash of content for embedding staleness detection
func computeContentHash(content string) string {
	hash := sha256.Sum256([]byte(content))
	return hex.EncodeToString(hash[:])
}
