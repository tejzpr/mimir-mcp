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

// NewRestoreTool creates the mimir_restore tool definition
func NewRestoreTool() mcp.Tool {
	return mcp.NewTool("mimir_restore",
		mcp.WithDescription("Restore an archived memory. Brings an archived memory back to active status. Use mimir_recall with list_all to find archived memories and their slugs."),
		mcp.WithString("slug",
			mcp.Required(),
			mcp.Description("Slug of the archived memory to restore"),
		),
	)
}

// RestoreHandler handles the mimir_restore tool
func RestoreHandler(ctx *ToolContext, userID uint) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(c context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		slug, err := request.RequireString("slug")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		// Get memory including soft-deleted ones
		var mem database.MimirMemory
		if err := ctx.DB.Unscoped().Where("slug = ? AND user_id = ?", slug, userID).First(&mem).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return mcp.NewToolResultError(fmt.Sprintf("memory not found: %s", slug)), nil
			}
			return mcp.NewToolResultError(fmt.Sprintf("database error: %v", err)), nil
		}

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
		if err := os.Rename(mem.FilePath, newFilePath); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to restore file: %v", err)), nil
		}

		// Update file path
		oldFilePath := mem.FilePath
		mem.FilePath = newFilePath

		// Git commit
		gitRepo, err := git.OpenRepository(ctx.RepoPath)
		if err == nil {
			msgFormat := git.CommitMessageFormats{}
			_ = gitRepo.CommitAll(msgFormat.RestoreMemory(slug))
		}

		// Restore in database (remove soft delete)
		mem.DeletedAt = gorm.DeletedAt{}
		mem.UpdatedAt = time.Now()
		if err := ctx.DB.Unscoped().Save(&mem).Error; err != nil {
			// Try to restore file to archive if DB update fails
			_ = os.Rename(newFilePath, oldFilePath)
			return mcp.NewToolResultError(fmt.Sprintf("failed to restore in database: %v", err)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("Memory '%s' restored to: %s", slug, newFilePath)), nil
	}
}
