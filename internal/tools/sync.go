// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/tejzpr/mimir-mcp/internal/crypto"
	"github.com/tejzpr/mimir-mcp/internal/database"
	"github.com/tejzpr/mimir-mcp/internal/git"
	"github.com/tejzpr/mimir-mcp/internal/rebuild"
)

// NewSyncTool creates the mimir_sync tool definition
func NewSyncTool() mcp.Tool {
	return mcp.NewTool("mimir_sync",
		mcp.WithDescription("Manually trigger git push/pull sync"),
		mcp.WithBoolean("force", mcp.Description("Force last-write-wins for conflicts")),
	)
}

// SyncHandler handles the mimir_sync tool
func SyncHandler(ctx *ToolContext, userID uint, encryptionKey []byte) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(c context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		force := request.GetBool("force", false)

		// Get user's repo
		var repo database.MimirGitRepo
		if err := ctx.DB.Where("user_id = ?", userID).First(&repo).Error; err != nil {
			return mcp.NewToolResultError("repository not found"), nil
		}

		// Check if PAT is available
		if repo.PATTokenEncrypted == "" {
			return mcp.NewToolResultError("No PAT token configured. Sync requires remote repository access."), nil
		}

		// Decrypt PAT
		pat, err := crypto.DecryptPAT(repo.PATTokenEncrypted, encryptionKey)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to decrypt PAT: %v", err)), nil
		}

		// Open git repository
		gitRepo, err := git.OpenRepository(repo.RepoPath)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to open repository: %v", err)), nil
		}

		// Perform sync
		status, err := gitRepo.Sync(pat, force)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("sync failed: %v", err)), nil
		}

		// Format response
		result := fmt.Sprintf("Sync completed successfully\n\nStatus:\n- Last sync: %s\n- Successful: %v\n",
			status.LastSync.Format(time.RFC3339),
			status.SyncSuccessful)

		if status.HasConflicts {
			result += fmt.Sprintf("- Conflicts: %d (resolved: %v)\n", len(status.ConflictFiles), force)
		}
		if status.Error != "" {
			result += fmt.Sprintf("- Note: %s\n", status.Error)
		}

		// Auto-rebuild database index after sync to incorporate pulled changes
		if status.SyncSuccessful {
			rebuildResult, rebuildErr := rebuild.RebuildIndex(ctx.DB, userID, repo.ID, repo.RepoPath, rebuild.Options{Force: true})
			if rebuildErr != nil {
				result += fmt.Sprintf("\n- Index rebuild warning: %v\n", rebuildErr)
			} else {
				result += fmt.Sprintf("\n- Index rebuilt: %d memories processed, %d created, %d associations\n",
					rebuildResult.MemoriesProcessed,
					rebuildResult.MemoriesCreated,
					rebuildResult.AssociationsCreated)
			}
		}

		return mcp.NewToolResultText(result), nil
	}
}
