// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/tejzpr/mimir-mcp/internal/database"
	"github.com/tejzpr/mimir-mcp/internal/git"
	"gorm.io/gorm"
)

// NewHistoryTool creates the mimir_history tool definition
func NewHistoryTool() mcp.Tool {
	return mcp.NewTool("mimir_history",
		mcp.WithDescription("Answer questions about when things happened and how they changed. Use when you need to know: when something was created, when it was last updated, what changed over time."),
		mcp.WithString("slug",
			mcp.Description("Memory to get history for"),
		),
		mcp.WithString("topic",
			mcp.Description("Find history related to a topic (if you don't know the slug)"),
		),
		mcp.WithBoolean("show_changes",
			mcp.Description("Show what actually changed (diff), not just when"),
		),
		mcp.WithString("since",
			mcp.Description("Only show history after this date (ISO 8601 or relative like '7d', '1w', '1m')"),
		),
		mcp.WithNumber("limit",
			mcp.Description("Maximum entries to return. Default: 10"),
		),
	)
}

// HistoryHandler handles the mimir_history tool
func HistoryHandler(ctx *ToolContext, userID uint) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(c context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		slug := request.GetString("slug", "")
		topic := request.GetString("topic", "")
		showChanges := request.GetBool("show_changes", false)
		sinceStr := request.GetString("since", "")
		limit := int(request.GetFloat("limit", 10.0))

		// Get user's repo
		var repo database.MimirGitRepo
		if err := ctx.DB.Where("user_id = ?", userID).First(&repo).Error; err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to get user repository: %v", err)), nil
		}

		// Parse since date
		sinceTime := parseSinceTime(sinceStr)

		// Open git repository
		gitRepo, err := git.OpenRepository(repo.RepoPath)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to open git repository: %v", err)), nil
		}

		var output strings.Builder

		if slug != "" {
			// History for specific memory
			result, err := getMemoryHistory(ctx, gitRepo, userID, slug, sinceTime, showChanges, limit)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			output.WriteString(result)
		} else if topic != "" {
			// Search for memories matching topic and show combined history
			result, err := getTopicHistory(ctx, gitRepo, userID, topic, sinceTime, showChanges, limit, repo.RepoPath)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			output.WriteString(result)
		} else {
			// Show recent activity across all memories
			result, err := getRecentActivity(gitRepo, sinceTime, limit, repo.RepoPath)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			output.WriteString(result)
		}

		return mcp.NewToolResultText(output.String()), nil
	}
}

// getMemoryHistory returns history for a specific memory
func getMemoryHistory(ctx *ToolContext, gitRepo *git.Repository, userID uint, slug string, since time.Time, showChanges bool, limit int) (string, error) {
	// Get memory
	var mem database.MimirMemory
	if err := ctx.DB.Unscoped().Where("slug = ? AND user_id = ?", slug, userID).First(&mem).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return "", fmt.Errorf("memory not found: %s", slug)
		}
		return "", fmt.Errorf("database error: %v", err)
	}

	// Get commit history for this file
	commits, err := gitRepo.SearchCommits("", mem.FilePath, since, time.Time{}, limit)
	if err != nil {
		return "", fmt.Errorf("failed to get history: %v", err)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# History for '%s'\n\n", mem.Title))
	sb.WriteString(fmt.Sprintf("**Slug**: `%s`\n", mem.Slug))
	sb.WriteString(fmt.Sprintf("**Created**: %s\n", mem.CreatedAt.Format("2006-01-02 15:04")))
	sb.WriteString(fmt.Sprintf("**Last Updated**: %s\n", mem.UpdatedAt.Format("2006-01-02 15:04")))
	sb.WriteString(fmt.Sprintf("**Access Count**: %d\n\n", mem.AccessCount))

	if mem.DeletedAt.Valid {
		sb.WriteString("⚠️ **Status**: Archived\n\n")
	}
	if mem.SupersededBy != nil {
		sb.WriteString(fmt.Sprintf("⚠️ **Superseded by**: `%s`\n\n", *mem.SupersededBy))
	}

	sb.WriteString("## Commit History\n\n")

	if len(commits) == 0 {
		sb.WriteString("No commits found in the specified time range.\n")
		return sb.String(), nil
	}

	for i, commit := range commits {
		sb.WriteString(fmt.Sprintf("### %d. %s\n", i+1, commit.Timestamp.Format("2006-01-02 15:04")))
		sb.WriteString(fmt.Sprintf("**Commit**: `%s`\n", commit.Hash[:8]))
		sb.WriteString(fmt.Sprintf("**Message**: %s\n", commit.Message))

		if showChanges && i < len(commits)-1 {
			// Show diff between this commit and the next (older) one
			diff, err := gitRepo.GetFileDiff(mem.FilePath, commits[i+1].Hash, commit.Hash)
			if err == nil && diff != nil {
				sb.WriteString("\n**Changes**:\n```diff\n")
				for _, hunk := range diff.Hunks {
					sb.WriteString(hunk + "\n")
				}
				sb.WriteString("```\n")
			}
		}
		sb.WriteString("\n")
	}

	return sb.String(), nil
}

// getTopicHistory returns history for memories matching a topic
func getTopicHistory(ctx *ToolContext, gitRepo *git.Repository, userID uint, topic string, since time.Time, showChanges bool, limit int, repoPath string) (string, error) {
	// Find memories matching topic
	var memories []database.MimirMemory
	ctx.DB.Where("user_id = ? AND title LIKE ?", userID, "%"+topic+"%").Find(&memories)

	if len(memories) == 0 {
		return fmt.Sprintf("No memories found matching topic: '%s'", topic), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# History for topic: '%s'\n\n", topic))
	sb.WriteString(fmt.Sprintf("Found %d related memories:\n\n", len(memories)))

	for _, mem := range memories {
		sb.WriteString(fmt.Sprintf("## %s (`%s`)\n", mem.Title, mem.Slug))
		sb.WriteString(fmt.Sprintf("Created: %s | Updated: %s\n\n", 
			mem.CreatedAt.Format("2006-01-02"),
			mem.UpdatedAt.Format("2006-01-02")))

		// Get recent commits for this memory
		commits, err := gitRepo.SearchCommits("", mem.FilePath, since, time.Time{}, 3)
		if err != nil || len(commits) == 0 {
			sb.WriteString("No recent changes.\n\n")
			continue
		}

		sb.WriteString("Recent changes:\n")
		for _, commit := range commits {
			sb.WriteString(fmt.Sprintf("- %s: %s\n", 
				commit.Timestamp.Format("2006-01-02"),
				commit.Message))
		}
		sb.WriteString("\n")
	}

	return sb.String(), nil
}

// getRecentActivity returns recent activity across all memories
func getRecentActivity(gitRepo *git.Repository, since time.Time, limit int, repoPath string) (string, error) {
	// Get recent commits
	commits, err := gitRepo.SearchCommits("", "", since, time.Time{}, limit)
	if err != nil {
		return "", fmt.Errorf("failed to get recent activity: %v", err)
	}

	var sb strings.Builder
	sb.WriteString("# Recent Activity\n\n")

	if len(commits) == 0 {
		sb.WriteString("No recent activity found.\n")
		return sb.String(), nil
	}

	for _, commit := range commits {
		sb.WriteString(fmt.Sprintf("## %s\n", commit.Timestamp.Format("2006-01-02 15:04")))
		sb.WriteString(fmt.Sprintf("**%s**\n", commit.Message))
		if len(commit.Files) > 0 {
			sb.WriteString(fmt.Sprintf("Files: %s\n", strings.Join(commit.Files, ", ")))
		}
		sb.WriteString("\n")
	}

	return sb.String(), nil
}

// parseSinceTime parses a since string into a time.Time
func parseSinceTime(sinceStr string) time.Time {
	if sinceStr == "" {
		return time.Time{}
	}

	// Try ISO 8601 format
	if t, err := time.Parse(time.RFC3339, sinceStr); err == nil {
		return t
	}

	// Try date format
	if t, err := time.Parse("2006-01-02", sinceStr); err == nil {
		return t
	}

	// Try relative formats
	now := time.Now()
	sinceStr = strings.ToLower(sinceStr)

	if strings.HasSuffix(sinceStr, "d") {
		if days, err := parseNumber(sinceStr[:len(sinceStr)-1]); err == nil {
			return now.AddDate(0, 0, -days)
		}
	}
	if strings.HasSuffix(sinceStr, "w") {
		if weeks, err := parseNumber(sinceStr[:len(sinceStr)-1]); err == nil {
			return now.AddDate(0, 0, -weeks*7)
		}
	}
	if strings.HasSuffix(sinceStr, "m") {
		if months, err := parseNumber(sinceStr[:len(sinceStr)-1]); err == nil {
			return now.AddDate(0, -months, 0)
		}
	}

	return time.Time{}
}

// parseNumber parses a string to int
func parseNumber(s string) (int, error) {
	var n int
	_, err := fmt.Sscanf(s, "%d", &n)
	return n, err
}
