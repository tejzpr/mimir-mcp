// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tools

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/tejzpr/mimir-mcp/internal/database"
	"github.com/tejzpr/mimir-mcp/internal/git"
	"github.com/tejzpr/mimir-mcp/internal/graph"
	"github.com/tejzpr/mimir-mcp/internal/memory"
)

// RecallResult represents a memory retrieval result with ranking score
type RecallResult struct {
	Memory      *database.MimirMemory
	Content     *memory.Memory
	Score       float64
	MatchSource string // "title", "content", "tag", "grep", "association"
}

// NewRecallTool creates the mimir_recall tool definition
func NewRecallTool() mcp.Tool {
	return mcp.NewTool("mimir_recall",
		mcp.WithDescription("Find and retrieve information from memory. This is the primary tool for getting information - use it whenever you need to know something. It searches everything: titles, content, tags, associations. Returns full content, ranked by relevance."),
		mcp.WithString("topic",
			mcp.Description("What you want to know about. Can be a question, keywords, or topic. Examples: 'authentication approach', 'what did we decide about caching', 'TODO items'"),
		),
		mcp.WithString("exact",
			mcp.Description("Search for exact text (when topic search doesn't find something you know exists)"),
		),
		mcp.WithBoolean("list_all",
			mcp.Description("Just list what's stored, no search. Use when exploring."),
		),
		mcp.WithString("path",
			mcp.Description("Limit to a folder. Example: 'projects/alpha'"),
		),
		mcp.WithBoolean("include_superseded",
			mcp.Description("Include memories that have been superseded (default: false)"),
		),
		mcp.WithBoolean("include_archived",
			mcp.Description("Include archived memories (default: false)"),
		),
		mcp.WithNumber("limit",
			mcp.Description("Max results. Default: 10"),
		),
	)
}

// RecallHandler handles the mimir_recall tool
func RecallHandler(ctx *ToolContext, userID uint) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(c context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		topic := request.GetString("topic", "")
		exact := request.GetString("exact", "")
		listAll := request.GetBool("list_all", false)
		pathFilter := request.GetString("path", "")
		includeSuperseded := request.GetBool("include_superseded", false)
		includeArchived := request.GetBool("include_archived", false)
		limit := int(request.GetFloat("limit", 10.0))

		// Get user's repo
		var repo database.MimirGitRepo
		if err := ctx.DB.Where("user_id = ?", userID).First(&repo).Error; err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to get user repository: %v", err)), nil
		}

		var results []RecallResult

		if listAll {
			// List all memories
			results = listAllMemories(ctx, userID, pathFilter, includeSuperseded, includeArchived)
		} else if exact != "" {
			// Exact text search using git grep
			results = searchExact(ctx, userID, exact, pathFilter, repo.RepoPath)
		} else if topic != "" {
			// Topic-based search (combines multiple strategies)
			results = searchByTopic(ctx, userID, topic, pathFilter, includeSuperseded, repo.RepoPath)
		} else {
			return mcp.NewToolResultError("please provide 'topic', 'exact', or set 'list_all' to true"), nil
		}

		// Sort by score (descending)
		sort.Slice(results, func(i, j int) bool {
			return results[i].Score > results[j].Score
		})

		// Apply limit
		if len(results) > limit {
			results = results[:limit]
		}

		// Update access statistics
		for _, r := range results {
			updateAccessStats(ctx, r.Memory)
		}

		// Format output
		output := formatRecallResults(results)

		if len(results) == 0 {
			if topic != "" {
				return mcp.NewToolResultText(fmt.Sprintf("No memories found for topic: '%s'\n\nTry using 'exact' for literal text search, or 'list_all' to see what's stored.", topic)), nil
			}
			return mcp.NewToolResultText("No memories found."), nil
		}

		return mcp.NewToolResultText(output), nil
	}
}

// listAllMemories returns all memories for browsing
func listAllMemories(ctx *ToolContext, userID uint, pathFilter string, includeSuperseded, includeArchived bool) []RecallResult {
	query := ctx.DB.Where("user_id = ?", userID)

	if !includeSuperseded {
		query = query.Where("superseded_by IS NULL")
	}

	if includeArchived {
		query = query.Unscoped()
	}

	var memories []database.MimirMemory
	query.Order("updated_at DESC").Find(&memories)

	var results []RecallResult
	for i := range memories {
		mem := &memories[i]
		if pathFilter != "" && !strings.Contains(mem.FilePath, pathFilter) {
			continue
		}

		content := loadMemoryContent(mem.FilePath)
		results = append(results, RecallResult{
			Memory:      mem,
			Content:     content,
			Score:       calculateRecencyScore(mem),
			MatchSource: "list",
		})
	}

	return results
}

// searchByTopic performs multi-strategy topic search
func searchByTopic(ctx *ToolContext, userID uint, topic string, pathFilter string, includeSuperseded bool, repoPath string) []RecallResult {
	resultMap := make(map[uint]*RecallResult)

	// Strategy 1: Title match
	searchTitle(ctx, userID, topic, resultMap, includeSuperseded)

	// Strategy 2: Tag match
	searchTags(ctx, userID, topic, resultMap, includeSuperseded)

	// Strategy 3: Content search (file-based)
	searchContent(ctx, userID, topic, resultMap, repoPath, includeSuperseded)

	// Strategy 4: Associated memories expansion
	expandAssociations(ctx, resultMap)

	// Apply path filter and convert to slice
	var results []RecallResult
	for _, r := range resultMap {
		if pathFilter != "" && !strings.Contains(r.Memory.FilePath, pathFilter) {
			continue
		}
		results = append(results, *r)
	}

	return results
}

// searchTitle searches by title match
func searchTitle(ctx *ToolContext, userID uint, topic string, resultMap map[uint]*RecallResult, includeSuperseded bool) {
	query := ctx.DB.Where("user_id = ? AND title LIKE ?", userID, "%"+topic+"%")
	if !includeSuperseded {
		query = query.Where("superseded_by IS NULL")
	}

	var memories []database.MimirMemory
	query.Find(&memories)

	for i := range memories {
		mem := &memories[i]
		if _, exists := resultMap[mem.ID]; !exists {
			content := loadMemoryContent(mem.FilePath)
			resultMap[mem.ID] = &RecallResult{
				Memory:      mem,
				Content:     content,
				Score:       10.0 + calculateRecencyScore(mem), // High base score for title match
				MatchSource: "title",
			}
		} else {
			resultMap[mem.ID].Score += 5.0 // Boost if matched multiple ways
		}
	}
}

// searchTags searches by tag match
func searchTags(ctx *ToolContext, userID uint, topic string, resultMap map[uint]*RecallResult, includeSuperseded bool) {
	// Find tags matching topic
	var tagIDs []uint
	ctx.DB.Model(&database.MimirTag{}).Where("name LIKE ?", "%"+topic+"%").Pluck("id", &tagIDs)
	if len(tagIDs) == 0 {
		return
	}

	// Find memories with these tags
	var memoryIDs []uint
	ctx.DB.Model(&database.MimirMemoryTag{}).Where("tag_id IN ?", tagIDs).Distinct("memory_id").Pluck("memory_id", &memoryIDs)

	query := ctx.DB.Where("user_id = ? AND id IN ?", userID, memoryIDs)
	if !includeSuperseded {
		query = query.Where("superseded_by IS NULL")
	}

	var memories []database.MimirMemory
	query.Find(&memories)

	for i := range memories {
		mem := &memories[i]
		if _, exists := resultMap[mem.ID]; !exists {
			content := loadMemoryContent(mem.FilePath)
			resultMap[mem.ID] = &RecallResult{
				Memory:      mem,
				Content:     content,
				Score:       8.0 + calculateRecencyScore(mem),
				MatchSource: "tag",
			}
		} else {
			resultMap[mem.ID].Score += 4.0
		}
	}
}

// searchContent searches file content
func searchContent(ctx *ToolContext, userID uint, topic string, resultMap map[uint]*RecallResult, repoPath string, includeSuperseded bool) {
	// Use git grep for content search
	gitRepo, err := git.OpenRepository(repoPath)
	if err != nil {
		return
	}

	grepResults, err := gitRepo.Grep(topic, "")
	if err != nil {
		return
	}

	// Map file paths to memory IDs
	query := ctx.DB.Where("user_id = ?", userID)
	if !includeSuperseded {
		query = query.Where("superseded_by IS NULL")
	}

	var memories []database.MimirMemory
	query.Find(&memories)

	fileToMem := make(map[string]*database.MimirMemory)
	for i := range memories {
		relPath := strings.TrimPrefix(memories[i].FilePath, repoPath+"/")
		fileToMem[relPath] = &memories[i]
	}

	for _, gr := range grepResults {
		mem, exists := fileToMem[gr.FilePath]
		if !exists {
			continue
		}

		if _, exists := resultMap[mem.ID]; !exists {
			content := loadMemoryContent(mem.FilePath)
			resultMap[mem.ID] = &RecallResult{
				Memory:      mem,
				Content:     content,
				Score:       6.0 + calculateRecencyScore(mem),
				MatchSource: "content",
			}
		} else {
			resultMap[mem.ID].Score += 3.0
		}
	}
}

// expandAssociations adds associated memories to results
func expandAssociations(ctx *ToolContext, resultMap map[uint]*RecallResult) {
	if len(resultMap) == 0 {
		return
	}

	// Get IDs of current results
	var currentIDs []uint
	for id := range resultMap {
		currentIDs = append(currentIDs, id)
	}

	// Get associated memories (1 hop)
	graphMgr := graph.NewManager(ctx.DB)
	for _, id := range currentIDs {
		g, err := graphMgr.TraverseGraph(id, 1, true)
		if err != nil {
			continue
		}

		for _, node := range g.Nodes {
			if node.Depth == 0 {
				continue // Skip the source node
			}

			if _, exists := resultMap[node.MemoryID]; !exists {
				var mem database.MimirMemory
				if err := ctx.DB.First(&mem, node.MemoryID).Error; err != nil {
					continue
				}
				
				// Only add if not superseded
				if mem.SupersededBy != nil {
					continue
				}

				content := loadMemoryContent(mem.FilePath)
				resultMap[mem.ID] = &RecallResult{
					Memory:      &mem,
					Content:     content,
					Score:       3.0 + calculateRecencyScore(&mem), // Lower score for associations
					MatchSource: "association",
				}
			}
		}
	}
}

// searchExact uses git grep for exact text search
func searchExact(ctx *ToolContext, userID uint, exact, pathFilter, repoPath string) []RecallResult {
	gitRepo, err := git.OpenRepository(repoPath)
	if err != nil {
		return nil
	}

	grepResults, err := gitRepo.Grep(exact, pathFilter)
	if err != nil {
		return nil
	}

	// Build file path to memory mapping
	var memories []database.MimirMemory
	ctx.DB.Where("user_id = ?", userID).Find(&memories)

	fileToMem := make(map[string]*database.MimirMemory)
	for i := range memories {
		relPath := strings.TrimPrefix(memories[i].FilePath, repoPath+"/")
		fileToMem[relPath] = &memories[i]
	}

	resultMap := make(map[uint]*RecallResult)
	for _, gr := range grepResults {
		mem, exists := fileToMem[gr.FilePath]
		if !exists {
			continue
		}

		if _, exists := resultMap[mem.ID]; !exists {
			content := loadMemoryContent(mem.FilePath)
			resultMap[mem.ID] = &RecallResult{
				Memory:      mem,
				Content:     content,
				Score:       10.0 + calculateRecencyScore(mem),
				MatchSource: "grep",
			}
		}
	}

	var results []RecallResult
	for _, r := range resultMap {
		results = append(results, *r)
	}
	return results
}

// loadMemoryContent reads and parses a memory file
func loadMemoryContent(filePath string) *memory.Memory {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil
	}
	parsed, err := memory.ParseMarkdown(string(content))
	if err != nil {
		return nil
	}
	return parsed
}

// calculateRecencyScore calculates a recency-based score boost
func calculateRecencyScore(mem *database.MimirMemory) float64 {
	daysSinceUpdate := time.Since(mem.UpdatedAt).Hours() / 24
	if daysSinceUpdate < 1 {
		return 2.0 // Very recent
	} else if daysSinceUpdate < 7 {
		return 1.5 // This week
	} else if daysSinceUpdate < 30 {
		return 1.0 // This month
	}
	return 0.5 // Older
}

// updateAccessStats updates access statistics for a memory
func updateAccessStats(ctx *ToolContext, mem *database.MimirMemory) {
	ctx.DB.Model(mem).Updates(map[string]interface{}{
		"last_accessed_at": time.Now(),
		"access_count":     mem.AccessCount + 1,
	})
}

// formatRecallResults formats results for output
func formatRecallResults(results []RecallResult) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d memories:\n\n", len(results)))

	for i, r := range results {
		sb.WriteString(fmt.Sprintf("## %d. %s\n", i+1, r.Memory.Title))
		sb.WriteString(fmt.Sprintf("**Slug**: `%s` | **Match**: %s | **Updated**: %s\n\n",
			r.Memory.Slug,
			r.MatchSource,
			r.Memory.UpdatedAt.Format("2006-01-02")))

		// Show superseded warning
		if r.Memory.SupersededBy != nil {
			sb.WriteString(fmt.Sprintf("⚠️ **Superseded by**: `%s`\n\n", *r.Memory.SupersededBy))
		}

		// Show annotations from file frontmatter
		if r.Content != nil && len(r.Content.Annotations) > 0 {
			sb.WriteString("**Annotations**:\n")
			for _, a := range r.Content.Annotations {
				sb.WriteString(fmt.Sprintf("- [%s] %s\n", a.Type, a.Content))
			}
			sb.WriteString("\n")
		}

		// Show tags
		if r.Content != nil && len(r.Content.Tags) > 0 {
			sb.WriteString(fmt.Sprintf("**Tags**: %s\n\n", strings.Join(r.Content.Tags, ", ")))
		}

		// Show content
		if r.Content != nil {
			content := r.Content.Content
			if len(content) > 1000 {
				content = content[:1000] + "\n\n... (content truncated)"
			}
			sb.WriteString(content)
		}

		sb.WriteString("\n\n---\n\n")
	}

	return sb.String()
}
