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

// NewForgetTool creates the medha_forget tool definition
func NewForgetTool() mcp.Tool {
	return mcp.NewTool("medha_forget",
		mcp.WithDescription("Archive a memory that's no longer relevant. The memory isn't deleted - it's moved to archive and can be restored later. Use when information is outdated or no longer needed."),
		mcp.WithString("slug",
			mcp.Required(),
			mcp.Description("Memory to archive"),
		),
	)
}

// ForgetHandler handles the medha_forget tool
// Uses v2 architecture: UserDB for per-user memories
func ForgetHandler(ctx *ToolContext, userID uint) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(c context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		slug, err := request.RequireString("slug")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		// Validate UserDB is available
		if ctx.UserDB == nil {
			return mcp.NewToolResultError("per-user database not available"), nil
		}

		// Get memory from UserDB
		var mem database.UserMemory
		if err := ctx.UserDB.Where("slug = ?", slug).First(&mem).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return mcp.NewToolResultError(fmt.Sprintf("memory not found: %s", slug)), nil
			}
			return mcp.NewToolResultError(fmt.Sprintf("database error: %v", err)), nil
		}

		// Capture original version for optimistic locking
		originalVersion := mem.Version

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

		oldFilePath := mem.FilePath
		if err := os.Rename(mem.FilePath, archivePath); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to move to archive: %v", err)), nil
		}

		// Git commit
		gitRepo, err := git.OpenRepository(ctx.RepoPath)
		if err == nil {
			msgFormat := git.CommitMessageFormats{}
			_ = gitRepo.CommitAll(msgFormat.ArchiveMemory(slug))
		}

		// Update database with optimistic locking (update file path and soft delete)
		now := time.Now()
		updates := map[string]interface{}{
			"file_path":  archivePath,
			"deleted_at": now,
			"updated_at": now,
		}

		err = locking.RetryWithBackoff(locking.MaxRetries, locking.RetryDelay, func() error {
			return locking.UpdateWithVersion(ctx.UserDB, "memories", slug, originalVersion, updates)
		})

		if err != nil {
			// Restore file to original location on conflict
			_ = os.Rename(archivePath, oldFilePath)
			if _, ok := err.(*locking.ConflictError); ok {
				return mcp.NewToolResultError("Memory was modified by another agent. Please retry."), nil
			}
			return mcp.NewToolResultError(fmt.Sprintf("failed to archive: %v", err)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("Memory '%s' archived (can be restored later)", slug)), nil
	}
}
