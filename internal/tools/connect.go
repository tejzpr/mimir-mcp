// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tools

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/tejzpr/mimir-mcp/internal/database"
	"github.com/tejzpr/mimir-mcp/internal/git"
	"github.com/tejzpr/mimir-mcp/internal/memory"
	"gorm.io/gorm"
)

// NewConnectTool creates the mimir_connect tool definition
func NewConnectTool() mcp.Tool {
	return mcp.NewTool("mimir_connect",
		mcp.WithDescription("Link or unlink related memories. Creates connections in the knowledge graph so related information can be found together."),
		mcp.WithString("from",
			mcp.Required(),
			mcp.Description("First memory slug"),
		),
		mcp.WithString("to",
			mcp.Required(),
			mcp.Description("Second memory slug"),
		),
		mcp.WithBoolean("disconnect",
			mcp.Description("Remove the link instead of creating it"),
		),
		mcp.WithString("relationship",
			mcp.Description("Type of connection: 'related', 'references', 'follows', 'supersedes', 'part_of'. Default: 'related'"),
		),
		mcp.WithNumber("strength",
			mcp.Description("Relationship importance from 0.0 (weak) to 1.0 (strong). Default: 0.5"),
		),
	)
}

// ConnectHandler handles the mimir_connect tool
func ConnectHandler(ctx *ToolContext, userID uint) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(c context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		fromSlug, err := request.RequireString("from")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		toSlug, err := request.RequireString("to")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		disconnect := request.GetBool("disconnect", false)
		relationship := request.GetString("relationship", "related")
		strength := request.GetFloat("strength", 0.5)

		// Map simplified relationship names to internal constants
		assocType := mapRelationshipType(relationship)
		if assocType == "" {
			return mcp.NewToolResultError(fmt.Sprintf("invalid relationship type: '%s'. Valid: related, references, follows, supersedes, part_of", relationship)), nil
		}

		// Get source memory
		var fromMem database.MimirMemory
		if err := ctx.DB.Where("slug = ? AND user_id = ?", fromSlug, userID).First(&fromMem).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return mcp.NewToolResultError(fmt.Sprintf("memory not found: %s", fromSlug)), nil
			}
			return mcp.NewToolResultError(fmt.Sprintf("database error: %v", err)), nil
		}

		// Get target memory
		var toMem database.MimirMemory
		if err := ctx.DB.Where("slug = ? AND user_id = ?", toSlug, userID).First(&toMem).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return mcp.NewToolResultError(fmt.Sprintf("memory not found: %s", toSlug)), nil
			}
			return mcp.NewToolResultError(fmt.Sprintf("database error: %v", err)), nil
		}

		if disconnect {
			return handleDisconnect(ctx, &fromMem, &toMem)
		}

		return handleConnect(ctx, userID, &fromMem, &toMem, assocType, strength)
	}
}

// handleConnect creates a connection between memories
func handleConnect(ctx *ToolContext, userID uint, fromMem, toMem *database.MimirMemory, assocType string, strength float64) (*mcp.CallToolResult, error) {
	// Check if association already exists
	var existing database.MimirMemoryAssociation
	err := ctx.DB.Where("source_memory_id = ? AND target_memory_id = ?", fromMem.ID, toMem.ID).First(&existing).Error
	if err == nil {
		// Association exists - update it
		existing.AssociationType = assocType
		existing.Strength = strength
		if err := ctx.DB.Save(&existing).Error; err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to update association: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Updated connection: '%s' -%s-> '%s' (strength: %.2f)", fromMem.Slug, assocType, toMem.Slug, strength)), nil
	}

	// Create new association
	association := &database.MimirMemoryAssociation{
		SourceMemoryID:  fromMem.ID,
		TargetMemoryID:  toMem.ID,
		AssociationType: assocType,
		Strength:        strength,
	}
	if err := ctx.DB.Create(association).Error; err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to create association: %v", err)), nil
	}

	// Create reverse association (bidirectional) for non-directional types
	if !isDirectionalType(assocType) {
		reverseAssoc := &database.MimirMemoryAssociation{
			SourceMemoryID:  toMem.ID,
			TargetMemoryID:  fromMem.ID,
			AssociationType: assocType,
			Strength:        strength,
		}
		ctx.DB.Create(reverseAssoc) // Ignore errors for reverse
	}

	// Handle supersedes type specially - mark target as superseded
	if assocType == database.AssociationTypeSupersedes {
		// Update the markdown file with superseded_by in frontmatter
		markdownContent, err := os.ReadFile(toMem.FilePath)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to read target memory file: %v", err)), nil
		}

		mem, err := memory.ParseMarkdown(string(markdownContent))
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to parse target memory markdown: %v", err)), nil
		}

		// Update frontmatter with superseded_by
		mem.SupersededBy = fromMem.Slug
		mem.Updated = time.Now()

		// Generate updated markdown
		markdown, err := mem.ToMarkdown()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to generate updated markdown: %v", err)), nil
		}

		// Write updated file
		if err := os.WriteFile(toMem.FilePath, []byte(markdown), 0644); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to write updated file: %v", err)), nil
		}

		// Update database
		toMem.SupersededBy = &fromMem.Slug
		ctx.DB.Save(toMem)

		// Git commit
		gitRepo, err := git.OpenRepository(ctx.RepoPath)
		if err == nil {
			msgFormat := git.CommitMessageFormats{}
			_ = gitRepo.CommitFile(toMem.FilePath, msgFormat.SupersedeMemory(toMem.Slug, fromMem.Slug))
		}

		return mcp.NewToolResultText(fmt.Sprintf("'%s' now supersedes '%s' (marked as outdated)", fromMem.Slug, toMem.Slug)), nil
	}

	// Git commit
	gitRepo, err := git.OpenRepository(ctx.RepoPath)
	if err == nil {
		msgFormat := git.CommitMessageFormats{}
		_ = gitRepo.CommitAll(msgFormat.Associate(fromMem.Slug, toMem.Slug, assocType))
	}

	return mcp.NewToolResultText(fmt.Sprintf("Connected: '%s' -%s-> '%s' (strength: %.2f)", fromMem.Slug, assocType, toMem.Slug, strength)), nil
}

// handleDisconnect removes a connection between memories
func handleDisconnect(ctx *ToolContext, fromMem, toMem *database.MimirMemory) (*mcp.CallToolResult, error) {
	// Delete forward association
	result := ctx.DB.Where("source_memory_id = ? AND target_memory_id = ?", fromMem.ID, toMem.ID).Delete(&database.MimirMemoryAssociation{})
	
	// Delete reverse association
	ctx.DB.Where("source_memory_id = ? AND target_memory_id = ?", toMem.ID, fromMem.ID).Delete(&database.MimirMemoryAssociation{})

	if result.RowsAffected == 0 {
		return mcp.NewToolResultError(fmt.Sprintf("no connection found between '%s' and '%s'", fromMem.Slug, toMem.Slug)), nil
	}

	// If target was superseded by source, clear the superseded_by field from both DB and file
	if toMem.SupersededBy != nil && *toMem.SupersededBy == fromMem.Slug {
		// Update the markdown file to remove superseded_by from frontmatter
		markdownContent, err := os.ReadFile(toMem.FilePath)
		if err == nil {
			mem, err := memory.ParseMarkdown(string(markdownContent))
			if err == nil {
				// Clear superseded_by from frontmatter
				mem.SupersededBy = ""
				mem.Updated = time.Now()

				// Generate updated markdown
				markdown, err := mem.ToMarkdown()
				if err == nil {
					_ = os.WriteFile(toMem.FilePath, []byte(markdown), 0644)

					// Git commit
					gitRepo, err := git.OpenRepository(ctx.RepoPath)
					if err == nil {
						msgFormat := git.CommitMessageFormats{}
						_ = gitRepo.CommitFile(toMem.FilePath, msgFormat.ClearSuperseded(toMem.Slug))
					}
				}
			}
		}

		// Update database
		toMem.SupersededBy = nil
		ctx.DB.Save(toMem)
	}

	return mcp.NewToolResultText(fmt.Sprintf("Disconnected: '%s' and '%s'", fromMem.Slug, toMem.Slug)), nil
}

// mapRelationshipType maps user-friendly names to internal constants
func mapRelationshipType(relationship string) string {
	switch relationship {
	case "related", "related_to":
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
		return ""
	}
}

// isDirectionalType returns true for relationship types that should not be bidirectional
func isDirectionalType(assocType string) bool {
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
