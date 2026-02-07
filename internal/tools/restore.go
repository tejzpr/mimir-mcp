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
	"github.com/tejzpr/medha-mcp/internal/database"
	"github.com/tejzpr/medha-mcp/internal/git"
	"github.com/tejzpr/medha-mcp/internal/locking"
	"github.com/tejzpr/medha-mcp/internal/memory"
	"gorm.io/gorm"
)

// NewRestoreTool creates the medha_restore tool definition
func NewRestoreTool() mcp.Tool {
	return mcp.NewTool("medha_restore",
		mcp.WithDescription("Restore an archived memory. Brings an archived memory back to active status. Use medha_recall with list_all to find archived memories and their slugs."),
		mcp.WithString("slug",
			mcp.Required(),
			mcp.Description("Slug of the archived memory to restore"),
		),
	)
}

// RestoreHandler handles the medha_restore tool
// Uses v2 architecture: UserDB for per-user memories
func RestoreHandler(ctx *ToolContext, userID uint) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(c context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		slug, err := request.RequireString("slug")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		// Validate UserDB is available
		if ctx.UserDB == nil {
			return mcp.NewToolResultError("per-user database not available"), nil
		}

		// Get memory including soft-deleted ones from UserDB
		var mem database.UserMemory
		if err := ctx.UserDB.Unscoped().Where("slug = ?", slug).First(&mem).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return mcp.NewToolResultError(fmt.Sprintf("memory not found: %s", slug)), nil
			}
			return mcp.NewToolResultError(fmt.Sprintf("database error: %v", err)), nil
		}

		// Capture original version for optimistic locking
		originalVersion := mem.Version

		// Check if memory is actually archived
		if !mem.DeletedAt.Valid {
			return mcp.NewToolResultError(fmt.Sprintf("memory '%s' is not archived", slug)), nil
		}

		// Read current memory content to get tags for path determination
		var memContent *memory.Memory
		if content, err := os.ReadFile(mem.FilePath); err == nil {
			memContent, _ = memory.ParseMarkdown(string(content))
		}

		// Determine new path (out of archive)
		organizer := memory.NewOrganizer(ctx.RepoPath)
		var newFilePath string
		if memContent != nil {
			newFilePath = organizer.GetMemoryPath(slug, memContent.Tags, "", memContent.Created)
		} else {
			// Fallback to date-based path
			newFilePath = organizer.GetMemoryPath(slug, []string{}, "", time.Now())
		}

		// Ensure directory exists
		if err := os.MkdirAll(filepath.Dir(newFilePath), 0755); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to create directory: %v", err)), nil
		}

		// Move file from archive
		oldFilePath := mem.FilePath
		if err := os.Rename(mem.FilePath, newFilePath); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to restore file: %v", err)), nil
		}

		// Git commit
		gitRepo, err := git.OpenRepository(ctx.RepoPath)
		if err == nil {
			msgFormat := git.CommitMessageFormats{}
			_ = gitRepo.CommitAll(msgFormat.RestoreMemory(slug))
		}

		// Update database with optimistic locking (restore from soft delete)
		now := time.Now()
		err = locking.RetryWithBackoff(locking.MaxRetries, locking.RetryDelay, func() error {
			return locking.UpdateWithVersionUnscoped(ctx.UserDB, "memories", slug, originalVersion, map[string]interface{}{
				"file_path":  newFilePath,
				"deleted_at": nil, // Remove soft delete
				"updated_at": now,
			})
		})

		if err != nil {
			// Restore file to archive on conflict
			_ = os.Rename(newFilePath, oldFilePath)
			if _, ok := err.(*locking.ConflictError); ok {
				return mcp.NewToolResultError("Memory was modified by another agent. Please retry."), nil
			}
			return mcp.NewToolResultError(fmt.Sprintf("failed to restore in database: %v", err)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("Memory '%s' restored to: %s", slug, newFilePath)), nil
	}
}
