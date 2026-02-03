// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/tejzpr/mimir-mcp/internal/database"
	"github.com/tejzpr/mimir-mcp/internal/git"
	"github.com/tejzpr/mimir-mcp/internal/memory"
	"gorm.io/gorm"
)

// NewRememberTool creates the mimir_remember tool definition
func NewRememberTool() mcp.Tool {
	return mcp.NewTool("mimir_remember",
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
		),
		mcp.WithString("path",
			mcp.Description("Folder path. Example: 'projects/alpha/decisions'"),
		),
		mcp.WithString("note",
			mcp.Description("Add a note/annotation to the memory. Can be combined with content updates."),
		),
		mcp.WithArray("connections",
			mcp.Description("Link to related memories. Array of objects: [{\"to\": \"slug\", \"relationship\": \"related|part_of|references|person\"}]"),
		),
	)
}

// Connection represents a connection to create with a memory
type Connection struct {
	To           string  `json:"to"`
	Relationship string  `json:"relationship"`
	Strength     float64 `json:"strength,omitempty"`
}

// RememberHandler handles the mimir_remember tool
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

		// Get user's repo
		var repo database.MimirGitRepo
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
			// Try to find existing memory
			var existingMem database.MimirMemory
			err = ctx.DB.Where("slug = ? AND user_id = ?", slug, userID).First(&existingMem).Error
			if err == nil {
				// Memory exists - update it
				result, updateErr := handleUpdate(ctx, userID, &existingMem, title, content, tags, repo.RepoPath)
				if updateErr != nil {
					return result, updateErr
				}
				// If note provided, also add annotation
				if note != "" {
					annotationResult, _ := handleAnnotation(ctx, userID, slug, note, repo.RepoPath)
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

			// Check if generated slug already exists
			var existingMem database.MimirMemory
			err = ctx.DB.Where("slug = ?", slug).First(&existingMem).Error
			if err == nil {
				return mcp.NewToolResultError(fmt.Sprintf("memory with slug '%s' already exists. Provide a custom 'slug' parameter to update or create with different ID.", slug)), nil
			}
		}

		// Create new memory
		result, err := handleCreate(ctx, userID, repo.ID, slug, title, content, tags, pathFolder, repo.RepoPath)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		// Handle supersession if specified
		if replaces != "" {
			err = handleSupersession(ctx, userID, slug, replaces, repo.RepoPath)
			if err != nil {
				// Log but don't fail - memory was created successfully
				result = result + fmt.Sprintf("\n\nWarning: Failed to mark '%s' as superseded: %v", replaces, err)
			} else {
				result = result + fmt.Sprintf("\n\nSupersedes: '%s' (marked as outdated)", replaces)
			}
		}

		// Handle connections if specified
		if len(connections) > 0 {
			connResults := handleConnections(ctx, userID, slug, connections)
			if connResults != "" {
				result = result + "\n\n" + connResults
			}
		}

		return mcp.NewToolResultText(result), nil
	}
}

// handleCreate creates a new memory
func handleCreate(ctx *ToolContext, userID uint, repoID uint, slug, title, content string, tags []string, pathFolder, repoPath string) (string, error) {
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

	// Store in database
	dbMem := &database.MimirMemory{
		UserID:         userID,
		RepoID:         repoID,
		Slug:           slug,
		Title:          title,
		FilePath:       filePath,
		LastAccessedAt: now,
		AccessCount:    0,
	}
	if err := ctx.DB.Create(dbMem).Error; err != nil {
		return "", fmt.Errorf("failed to store memory: %w", err)
	}

	// Store tags
	storeTags(ctx, dbMem.ID, tags)

	return fmt.Sprintf("Memory created: %s\nSlug: %s\nPath: %s", title, slug, filePath), nil
}

// handleUpdate updates an existing memory
func handleUpdate(ctx *ToolContext, userID uint, dbMem *database.MimirMemory, title, content string, tags []string, repoPath string) (*mcp.CallToolResult, error) {
	// Check if memory is deleted
	if dbMem.DeletedAt.Valid {
		return mcp.NewToolResultError(fmt.Sprintf("memory '%s' has been archived. Use mimir_restore first.", dbMem.Slug)), nil
	}

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
	if title != "" {
		title = memory.SanitizeTitle(title)
		mem.Title = title
		dbMem.Title = title
	}
	mem.Content = content
	mem.Updated = time.Now()

	// Update tags if provided
	if len(tags) > 0 {
		mem.Tags = tags
		ctx.DB.Where("memory_id = ?", dbMem.ID).Delete(&database.MimirMemoryTag{})
		storeTags(ctx, dbMem.ID, tags)
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

	// Update database
	dbMem.UpdatedAt = time.Now()
	if err := ctx.DB.Save(dbMem).Error; err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to update database: %v", err)), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Memory updated: %s", dbMem.Slug)), nil
}

// handleAnnotation adds an annotation to a memory - stored in git frontmatter
func handleAnnotation(ctx *ToolContext, userID uint, slug, note, repoPath string) (*mcp.CallToolResult, error) {
	// Get memory from DB for file path
	var dbMem database.MimirMemory
	if err := ctx.DB.Where("slug = ? AND user_id = ?", slug, userID).First(&dbMem).Error; err != nil {
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

	return mcp.NewToolResultText(fmt.Sprintf("Annotation added to '%s' (type: %s)", slug, annotationType)), nil
}

// handleSupersession marks an old memory as superseded by a new one
func handleSupersession(ctx *ToolContext, userID uint, newSlug, oldSlug, repoPath string) error {
	// Get old memory
	var oldMem database.MimirMemory
	if err := ctx.DB.Where("slug = ? AND user_id = ?", oldSlug, userID).First(&oldMem).Error; err != nil {
		return fmt.Errorf("old memory not found: %s", oldSlug)
	}

	// Get new memory
	var newMem database.MimirMemory
	if err := ctx.DB.Where("slug = ? AND user_id = ?", newSlug, userID).First(&newMem).Error; err != nil {
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

	// Mark old memory as superseded in database
	oldMem.SupersededBy = &newSlug
	if err := ctx.DB.Save(&oldMem).Error; err != nil {
		return fmt.Errorf("failed to update old memory: %w", err)
	}

	// Create association
	association := &database.MimirMemoryAssociation{
		SourceMemoryID:  newMem.ID,
		TargetMemoryID:  oldMem.ID,
		AssociationType: database.AssociationTypeSupersedes,
		Strength:        1.0,
	}
	if err := ctx.DB.Create(association).Error; err != nil {
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

// storeTags stores tags for a memory
func storeTags(ctx *ToolContext, memoryID uint, tags []string) {
	for _, tagName := range tags {
		var tag database.MimirTag
		ctx.DB.Where("name = ?", tagName).FirstOrCreate(&tag, database.MimirTag{
			Name: tagName,
		})

		memTag := &database.MimirMemoryTag{
			MemoryID: memoryID,
			TagID:    tag.ID,
		}
		ctx.DB.Create(memTag)
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

// handleConnections creates associations for the newly created memory
func handleConnections(ctx *ToolContext, userID uint, sourceSlug string, connections []Connection) string {
	var results []string

	// Get source memory
	var sourceMem database.MimirMemory
	if err := ctx.DB.Where("slug = ? AND user_id = ?", sourceSlug, userID).First(&sourceMem).Error; err != nil {
		return fmt.Sprintf("Warning: Could not find source memory for connections: %v", err)
	}

	for _, conn := range connections {
		// Get target memory
		var targetMem database.MimirMemory
		if err := ctx.DB.Where("slug = ? AND user_id = ?", conn.To, userID).First(&targetMem).Error; err != nil {
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

		// Create association
		association := &database.MimirMemoryAssociation{
			SourceMemoryID:  sourceMem.ID,
			TargetMemoryID:  targetMem.ID,
			AssociationType: assocType,
			Strength:        strength,
		}
		if err := ctx.DB.Create(association).Error; err != nil {
			results = append(results, fmt.Sprintf("- Connection to '%s' failed: %v", conn.To, err))
			continue
		}

		// Create reverse for bidirectional types
		if !isDirectionalTypeForRemember(assocType) {
			reverseAssoc := &database.MimirMemoryAssociation{
				SourceMemoryID:  targetMem.ID,
				TargetMemoryID:  sourceMem.ID,
				AssociationType: assocType,
				Strength:        strength,
			}
			ctx.DB.Create(reverseAssoc)
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

// isDirectionalTypeForRemember returns true for relationship types that should not be bidirectional
func isDirectionalTypeForRemember(assocType string) bool {
	switch assocType {
	case database.AssociationTypeFollows,
		database.AssociationTypePrecedes,
		database.AssociationTypeSupersedes,
		database.AssociationTypePartOf:
		return true
	default:
		return false
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
