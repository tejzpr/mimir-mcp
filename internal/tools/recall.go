// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/tejzpr/medha-mcp/internal/database"
	"github.com/tejzpr/medha-mcp/internal/embeddings"
	"github.com/tejzpr/medha-mcp/internal/git"
	"github.com/tejzpr/medha-mcp/internal/memory"
)

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

// RecallResult represents a memory retrieval result with ranking score
type RecallResult struct {
	Memory      *database.UserMemory
	Content     *memory.Memory
	Score       float64
	MatchSource string // "title", "content", "tag", "grep", "association", "semantic"
}

// NewRecallTool creates the medha_recall tool definition
func NewRecallTool() mcp.Tool {
	return mcp.NewTool("medha_recall",
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

// RecallHandler handles the medha_recall tool
// Uses v2 architecture: UserDB for per-user memories
func RecallHandler(ctx *ToolContext, userID uint) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(c context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		topic := request.GetString("topic", "")
		exact := request.GetString("exact", "")
		listAll := request.GetBool("list_all", false)
		pathFilter := request.GetString("path", "")
		includeSuperseded := request.GetBool("include_superseded", false)
		includeArchived := request.GetBool("include_archived", false)
		limit := int(request.GetFloat("limit", 10.0))

		// Validate UserDB is available
		if ctx.UserDB == nil {
			return mcp.NewToolResultError("per-user database not available"), nil
		}

		// Get user's repo from system DB
		var repo database.MedhaGitRepo
		if err := ctx.DB.Where("user_id = ?", userID).First(&repo).Error; err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to get user repository: %v", err)), nil
		}

		var results []RecallResult

		if listAll {
			// List all memories
			results = listAllMemoriesV2(ctx, pathFilter, includeSuperseded, includeArchived)
		} else if exact != "" {
			// Exact text search using git grep
			results = searchExactV2(ctx, exact, pathFilter, repo.RepoPath)
		} else if topic != "" {
			// Topic-based search (combines multiple strategies)
			results = searchByTopicV2(ctx, topic, pathFilter, includeSuperseded, repo.RepoPath)
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
			updateAccessStatsV2(ctx, r.Memory)
		}

		// Format output
		output := formatRecallResultsV2(results)

		if len(results) == 0 {
			if topic != "" {
				return mcp.NewToolResultText(fmt.Sprintf("No memories found for topic: '%s'\n\nTry using 'exact' for literal text search, or 'list_all' to see what's stored.", topic)), nil
			}
			return mcp.NewToolResultText("No memories found."), nil
		}

		return mcp.NewToolResultText(output), nil
	}
}

// listAllMemoriesV2 returns all memories for browsing (v2 architecture)
func listAllMemoriesV2(ctx *ToolContext, pathFilter string, includeSuperseded, includeArchived bool) []RecallResult {
	query := ctx.UserDB.Model(&database.UserMemory{})

	if !includeSuperseded {
		query = query.Where("superseded_by IS NULL")
	}

	if includeArchived {
		query = query.Unscoped()
	}

	var memories []database.UserMemory
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
			Score:       calculateRecencyScoreV2(mem),
			MatchSource: "list",
		})
	}

	return results
}

// searchByTopicV2 performs multi-strategy topic search (v2 architecture)
// Includes semantic search when embeddings are available
func searchByTopicV2(ctx *ToolContext, topic string, pathFilter string, includeSuperseded bool, repoPath string) []RecallResult {
	resultMap := make(map[string]*RecallResult) // Keyed by slug

	// Strategy 1: Title match
	searchTitleV2(ctx, topic, resultMap, includeSuperseded)

	// Strategy 2: Tag match
	searchTagsV2(ctx, topic, resultMap, includeSuperseded)

	// Strategy 3: Content search (file-based)
	searchContentV2(ctx, topic, resultMap, repoPath, includeSuperseded)

	// Strategy 4: Semantic search (when embeddings are available)
	if ctx.HasEmbeddings() {
		searchSemanticV2(ctx, topic, resultMap, includeSuperseded)
	}

	// Strategy 5: Associated memories expansion
	expandAssociationsV2(ctx, resultMap)

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

// searchSemanticV2 performs semantic search using embeddings (v2 architecture)
func searchSemanticV2(ctx *ToolContext, topic string, resultMap map[string]*RecallResult, includeSuperseded bool) {
	if ctx.EmbeddingService == nil || !ctx.EmbeddingService.IsEnabled() {
		return
	}

	// Get vector search from embedding service
	vecSearch := ctx.EmbeddingService.GetVectorSearch()
	if vecSearch == nil {
		return
	}

	// Create semantic search instance
	semanticSearch := embeddings.NewSemanticSearch(ctx.EmbeddingService, vecSearch)

	// Perform semantic search
	searchResults, err := semanticSearch.Search(topic, 20)
	if err != nil || searchResults == nil {
		return
	}

	// Add results to resultMap
	for _, sr := range searchResults {
		// Skip if similarity is too low
		if sr.Similarity < 0.3 {
			continue
		}

		if _, exists := resultMap[sr.Slug]; exists {
			// Boost existing results that also have semantic match
			resultMap[sr.Slug].Score += float64(sr.Similarity) * 5.0
			if resultMap[sr.Slug].MatchSource != "semantic" {
				resultMap[sr.Slug].MatchSource += "+semantic"
			}
		} else {
			// Find memory in database
			var mem database.UserMemory
			if err := ctx.UserDB.Where("slug = ?", sr.Slug).First(&mem).Error; err != nil {
				continue
			}

			// Skip superseded if not included
			if !includeSuperseded && mem.SupersededBy != nil {
				continue
			}

			content := loadMemoryContent(mem.FilePath)
			resultMap[sr.Slug] = &RecallResult{
				Memory:      &mem,
				Content:     content,
				Score:       float64(sr.Similarity) * 8.0, // Semantic score
				MatchSource: "semantic",
			}
		}
	}
}

// searchTitleV2 searches by title match (v2 architecture)
func searchTitleV2(ctx *ToolContext, topic string, resultMap map[string]*RecallResult, includeSuperseded bool) {
	query := ctx.UserDB.Where("title LIKE ?", "%"+topic+"%")
	if !includeSuperseded {
		query = query.Where("superseded_by IS NULL")
	}

	var memories []database.UserMemory
	query.Find(&memories)

	for i := range memories {
		mem := &memories[i]
		if _, exists := resultMap[mem.Slug]; !exists {
			content := loadMemoryContent(mem.FilePath)
			resultMap[mem.Slug] = &RecallResult{
				Memory:      mem,
				Content:     content,
				Score:       10.0 + calculateRecencyScoreV2(mem), // High base score for title match
				MatchSource: "title",
			}
		} else {
			resultMap[mem.Slug].Score += 5.0 // Boost if matched multiple ways
		}
	}
}

// searchTagsV2 searches by tag match (v2 architecture)
func searchTagsV2(ctx *ToolContext, topic string, resultMap map[string]*RecallResult, includeSuperseded bool) {
	// Find memory slugs with matching tags
	var memorySlugs []string
	ctx.UserDB.Model(&database.UserMemoryTag{}).
		Where("tag_name LIKE ?", "%"+topic+"%").
		Distinct("memory_slug").
		Pluck("memory_slug", &memorySlugs)

	if len(memorySlugs) == 0 {
		return
	}

	query := ctx.UserDB.Where("slug IN ?", memorySlugs)
	if !includeSuperseded {
		query = query.Where("superseded_by IS NULL")
	}

	var memories []database.UserMemory
	query.Find(&memories)

	for i := range memories {
		mem := &memories[i]
		if _, exists := resultMap[mem.Slug]; !exists {
			content := loadMemoryContent(mem.FilePath)
			resultMap[mem.Slug] = &RecallResult{
				Memory:      mem,
				Content:     content,
				Score:       8.0 + calculateRecencyScoreV2(mem),
				MatchSource: "tag",
			}
		} else {
			resultMap[mem.Slug].Score += 4.0
		}
	}
}

// searchContentV2 searches file content (v2 architecture)
func searchContentV2(ctx *ToolContext, topic string, resultMap map[string]*RecallResult, repoPath string, includeSuperseded bool) {
	// Use git grep for content search
	gitRepo, err := git.OpenRepository(repoPath)
	if err != nil {
		return
	}

	grepResults, err := gitRepo.Grep(topic, "")
	if err != nil {
		return
	}

	// Map file paths to memory slugs
	query := ctx.UserDB.Model(&database.UserMemory{})
	if !includeSuperseded {
		query = query.Where("superseded_by IS NULL")
	}

	var memories []database.UserMemory
	query.Find(&memories)

	fileToMem := make(map[string]*database.UserMemory)
	for i := range memories {
		relPath := strings.TrimPrefix(memories[i].FilePath, repoPath+"/")
		fileToMem[relPath] = &memories[i]
	}

	for _, gr := range grepResults {
		mem, exists := fileToMem[gr.FilePath]
		if !exists {
			continue
		}

		if _, exists := resultMap[mem.Slug]; !exists {
			content := loadMemoryContent(mem.FilePath)
			resultMap[mem.Slug] = &RecallResult{
				Memory:      mem,
				Content:     content,
				Score:       6.0 + calculateRecencyScoreV2(mem),
				MatchSource: "content",
			}
		} else {
			resultMap[mem.Slug].Score += 3.0
		}
	}
}

// expandAssociationsV2 adds associated memories to results (v2 architecture)
func expandAssociationsV2(ctx *ToolContext, resultMap map[string]*RecallResult) {
	if len(resultMap) == 0 {
		return
	}

	// Get slugs of current results
	var currentSlugs []string
	for slug := range resultMap {
		currentSlugs = append(currentSlugs, slug)
	}

	// Get associated memories (1 hop) using slug-based associations
	var associations []database.UserMemoryAssociation
	ctx.UserDB.Where("source_slug IN ?", currentSlugs).Find(&associations)

	for _, assoc := range associations {
		if _, exists := resultMap[assoc.TargetSlug]; exists {
			continue // Skip already in results
		}

		var mem database.UserMemory
		if err := ctx.UserDB.Where("slug = ?", assoc.TargetSlug).First(&mem).Error; err != nil {
			continue
		}

		// Only add if not superseded
		if mem.SupersededBy != nil {
			continue
		}

		content := loadMemoryContent(mem.FilePath)
		resultMap[mem.Slug] = &RecallResult{
			Memory:      &mem,
			Content:     content,
			Score:       3.0 + calculateRecencyScoreV2(&mem), // Lower score for associations
			MatchSource: "association",
		}
	}
}

// searchExactV2 uses git grep for exact text search (v2 architecture)
func searchExactV2(ctx *ToolContext, exact, pathFilter, repoPath string) []RecallResult {
	gitRepo, err := git.OpenRepository(repoPath)
	if err != nil {
		return nil
	}

	grepResults, err := gitRepo.Grep(exact, pathFilter)
	if err != nil {
		return nil
	}

	// Build file path to memory mapping
	var memories []database.UserMemory
	ctx.UserDB.Find(&memories)

	fileToMem := make(map[string]*database.UserMemory)
	for i := range memories {
		relPath := strings.TrimPrefix(memories[i].FilePath, repoPath+"/")
		fileToMem[relPath] = &memories[i]
	}

	resultMap := make(map[string]*RecallResult) // Keyed by slug
	for _, gr := range grepResults {
		mem, exists := fileToMem[gr.FilePath]
		if !exists {
			continue
		}

		if _, exists := resultMap[mem.Slug]; !exists {
			content := loadMemoryContent(mem.FilePath)
			resultMap[mem.Slug] = &RecallResult{
				Memory:      mem,
				Content:     content,
				Score:       10.0 + calculateRecencyScoreV2(mem),
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

// calculateRecencyScoreV2 calculates a recency-based score boost (v2 architecture)
func calculateRecencyScoreV2(mem *database.UserMemory) float64 {
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

// updateAccessStatsV2 updates access statistics for a memory (v2 architecture)
func updateAccessStatsV2(ctx *ToolContext, mem *database.UserMemory) {
	ctx.UserDB.Model(mem).Updates(map[string]interface{}{
		"last_accessed_at": time.Now(),
		"access_count":     mem.AccessCount + 1,
	})
}

// formatRecallResultsV2 formats results for output (v2 architecture)
func formatRecallResultsV2(results []RecallResult) string {
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

// calculateContentHash computes a SHA256 hash of content for embedding staleness detection
func calculateContentHash(content string) string {
	hash := sha256.Sum256([]byte(content))
	return hex.EncodeToString(hash[:])
}
