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
	"github.com/tejzpr/medha-mcp/internal/database"
	"github.com/tejzpr/medha-mcp/internal/git"
	"github.com/tejzpr/medha-mcp/internal/locking"
	"github.com/tejzpr/medha-mcp/internal/memory"
	"gorm.io/gorm"
)

// NewConnectTool creates the medha_connect tool definition
func NewConnectTool() mcp.Tool {
	return mcp.NewTool("medha_connect",
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

// ConnectHandler handles the medha_connect tool
// Uses v2 architecture: UserDB for per-user memories with slug-based associations
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

		// Validate UserDB is available
		if ctx.UserDB == nil {
			return mcp.NewToolResultError("per-user database not available"), nil
		}

		// Map simplified relationship names to internal constants
		assocType := mapRelationshipType(relationship)
		if assocType == "" {
			return mcp.NewToolResultError(fmt.Sprintf("invalid relationship type: '%s'. Valid: related, references, follows, supersedes, part_of", relationship)), nil
		}

		// Get source memory from UserDB
		var fromMem database.UserMemory
		if err := ctx.UserDB.Where("slug = ?", fromSlug).First(&fromMem).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return mcp.NewToolResultError(fmt.Sprintf("memory not found: %s", fromSlug)), nil
			}
			return mcp.NewToolResultError(fmt.Sprintf("database error: %v", err)), nil
		}

		// Get target memory from UserDB
		var toMem database.UserMemory
		if err := ctx.UserDB.Where("slug = ?", toSlug).First(&toMem).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return mcp.NewToolResultError(fmt.Sprintf("memory not found: %s", toSlug)), nil
			}
			return mcp.NewToolResultError(fmt.Sprintf("database error: %v", err)), nil
		}

		if disconnect {
			return handleDisconnectV2(ctx, &fromMem, &toMem)
		}

		return handleConnectV2(ctx, &fromMem, &toMem, assocType, strength)
	}
}

// handleConnectV2 creates a connection between memories (v2 architecture with slug-based associations)
func handleConnectV2(ctx *ToolContext, fromMem, toMem *database.UserMemory, assocType string, strength float64) (*mcp.CallToolResult, error) {
	// Check if association already exists
	var existing database.UserMemoryAssociation
	err := ctx.UserDB.Where("source_slug = ? AND target_slug = ?", fromMem.Slug, toMem.Slug).First(&existing).Error
	if err == nil {
		// Association exists - update it
		existing.AssociationType = assocType
		existing.Strength = strength
		if err := ctx.UserDB.Save(&existing).Error; err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to update association: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Updated connection: '%s' -%s-> '%s' (strength: %.2f)", fromMem.Slug, assocType, toMem.Slug, strength)), nil
	}

	// Create new association
	association := &database.UserMemoryAssociation{
		SourceSlug:      fromMem.Slug,
		TargetSlug:      toMem.Slug,
		AssociationType: assocType,
		Strength:        strength,
	}
	if err := ctx.UserDB.Create(association).Error; err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to create association: %v", err)), nil
	}

	// Create reverse association (bidirectional) for non-directional types
	if !isDirectionalType(assocType) {
		reverseAssoc := &database.UserMemoryAssociation{
			SourceSlug:      toMem.Slug,
			TargetSlug:      fromMem.Slug,
			AssociationType: assocType,
			Strength:        strength,
		}
		ctx.UserDB.Create(reverseAssoc) // Ignore errors for reverse
	}

	// Handle supersedes type specially - mark target as superseded
	if assocType == database.AssociationTypeSupersedes {
		// Capture version for optimistic locking
		originalVersion := toMem.Version

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

		// Git commit
		gitRepo, err := git.OpenRepository(ctx.RepoPath)
		if err == nil {
			msgFormat := git.CommitMessageFormats{}
			_ = gitRepo.CommitFile(toMem.FilePath, msgFormat.SupersedeMemory(toMem.Slug, fromMem.Slug))
		}

		// Update database with optimistic locking
		now := time.Now()
		err = locking.RetryWithBackoff(locking.MaxRetries, locking.RetryDelay, func() error {
			return locking.UpdateWithVersion(ctx.UserDB, "memories", toMem.Slug, originalVersion, map[string]interface{}{
				"superseded_by": fromMem.Slug,
				"updated_at":    now,
			})
		})

		if err != nil {
			if _, ok := err.(*locking.ConflictError); ok {
				return mcp.NewToolResultError("Target memory was modified by another agent. Please retry."), nil
			}
			return mcp.NewToolResultError(fmt.Sprintf("failed to update database: %v", err)), nil
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

// handleDisconnectV2 removes a connection between memories (v2 architecture)
func handleDisconnectV2(ctx *ToolContext, fromMem, toMem *database.UserMemory) (*mcp.CallToolResult, error) {
	// Delete forward association
	result := ctx.UserDB.Where("source_slug = ? AND target_slug = ?", fromMem.Slug, toMem.Slug).Delete(&database.UserMemoryAssociation{})
	
	// Delete reverse association
	ctx.UserDB.Where("source_slug = ? AND target_slug = ?", toMem.Slug, fromMem.Slug).Delete(&database.UserMemoryAssociation{})

	if result.RowsAffected == 0 {
		return mcp.NewToolResultError(fmt.Sprintf("no connection found between '%s' and '%s'", fromMem.Slug, toMem.Slug)), nil
	}

	// If target was superseded by source, clear the superseded_by field from both DB and file
	if toMem.SupersededBy != nil && *toMem.SupersededBy == fromMem.Slug {
		// Capture version for optimistic locking
		originalVersion := toMem.Version

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

		// Update database with optimistic locking
		now := time.Now()
		_ = locking.RetryWithBackoff(locking.MaxRetries, locking.RetryDelay, func() error {
			return locking.UpdateWithVersion(ctx.UserDB, "memories", toMem.Slug, originalVersion, map[string]interface{}{
				"superseded_by": nil,
				"updated_at":    now,
			})
		})
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
