// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/tejzpr/mimir-mcp/internal/database"
	"github.com/tejzpr/mimir-mcp/internal/git"
	"github.com/tejzpr/mimir-mcp/internal/memory"
	"gorm.io/gorm"
)

// NewForgetTool creates the mimir_forget tool definition
func NewForgetTool() mcp.Tool {
	return mcp.NewTool("mimir_forget",
		mcp.WithDescription("Archive a memory that's no longer relevant. The memory isn't deleted - it's moved to archive and can be restored later. Use when information is outdated or no longer needed."),
		mcp.WithString("slug",
			mcp.Required(),
			mcp.Description("Memory to archive"),
		),
	)
}

// ForgetHandler handles the mimir_forget tool
func ForgetHandler(ctx *ToolContext, userID uint) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(c context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		slug, err := request.RequireString("slug")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		// Get memory
		var mem database.MimirMemory
		if err := ctx.DB.Where("slug = ? AND user_id = ?", slug, userID).First(&mem).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return mcp.NewToolResultError(fmt.Sprintf("memory not found: %s", slug)), nil
			}
			return mcp.NewToolResultError(fmt.Sprintf("database error: %v", err)), nil
		}

		// Check if already archived
		if mem.DeletedAt.Valid {
			return mcp.NewToolResultError(fmt.Sprintf("memory '%s' is already archived", slug)), nil
		}

		// Get organizer and determine archive path
		organizer := memory.NewOrganizer(ctx.RepoPath)
		archivePath := organizer.GetArchivePath(slug)

		// Move file to archive
		if err := os.MkdirAll(filepath.Dir(archivePath), 0755); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to create archive dir: %v", err)), nil
		}

		if err := os.Rename(mem.FilePath, archivePath); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to move to archive: %v", err)), nil
		}

		// Update file path in database before soft delete
		oldFilePath := mem.FilePath
		mem.FilePath = archivePath
		ctx.DB.Save(&mem)

		// Git commit
		gitRepo, err := git.OpenRepository(ctx.RepoPath)
		if err == nil {
			msgFormat := git.CommitMessageFormats{}
			_ = gitRepo.CommitAll(msgFormat.ArchiveMemory(slug))
		}

		// Soft delete in database
		if err := ctx.DB.Delete(&mem).Error; err != nil {
			// Try to restore file if DB delete fails
			_ = os.Rename(archivePath, oldFilePath)
			return mcp.NewToolResultError(fmt.Sprintf("failed to archive: %v", err)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("Memory '%s' archived (can be restored later)", slug)), nil
	}
}
