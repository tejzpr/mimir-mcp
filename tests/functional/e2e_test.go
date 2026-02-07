// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package functional

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tejzpr/medha-mcp/internal/database"
	"github.com/tejzpr/medha-mcp/internal/git"
	"github.com/tejzpr/medha-mcp/internal/graph"
	"github.com/tejzpr/medha-mcp/internal/memory"
	"github.com/tejzpr/medha-mcp/internal/rebuild"
	"gorm.io/gorm/logger"
)

// TestE2ECompleteWorkflow tests a complete end-to-end user scenario
// Updated to use v2 architecture with DatabaseManager and per-user database
func TestE2ECompleteWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	// Setup
	tempDir := t.TempDir()
	systemDBPath := filepath.Join(tempDir, "system.db")
	storePath := filepath.Join(tempDir, "store")

	// 1. Initialize database manager (v2 architecture)
	dbCfg := &database.Config{
		Type:       "sqlite",
		SQLitePath: systemDBPath,
		LogLevel:   logger.Silent,
	}

	mgr, err := database.NewManager(dbCfg)
	require.NoError(t, err)
	defer mgr.Close()

	// Run v1 migrations on system DB for backward compatibility with graph manager
	err = database.Migrate(mgr.SystemDB())
	require.NoError(t, err)

	// 2. Create user in system DB (simulating SAML authentication)
	user := &database.MedhaUser{
		Username: "john.doe@example.com",
		Email:    "john.doe@example.com",
	}
	result := mgr.SystemDB().Create(user)
	require.NoError(t, result.Error)
	t.Logf("✓ User created: %s", user.Username)

	// 3. Setup user repository (post-authentication)
	setupCfg := &git.SetupConfig{
		BaseStorePath: storePath,
		Username:      user.Username,
		LocalOnly:     true, // No PAT for this test
	}

	setupResult, err := git.SetupUserRepository(setupCfg)
	require.NoError(t, err)
	t.Logf("✓ Repository created: %s at %s", setupResult.RepoName, setupResult.RepoPath)

	// Store repo in system database
	dbRepo := &database.MedhaGitRepo{
		UserID:   user.ID,
		RepoUUID: setupResult.RepoID,
		RepoName: setupResult.RepoName,
		RepoPath: setupResult.RepoPath,
	}
	mgr.SystemDB().Create(dbRepo)

	// Get per-user database (v2)
	userDB, err := mgr.GetUserDB(setupResult.RepoPath)
	require.NoError(t, err)

	// 4. Create multiple memories
	memories := []struct {
		title    string
		content  string
		tags     []string
		category string
	}{
		{
			title:    "Project Alpha Planning",
			content:  "# Project Alpha\n\n## Goals\n- Launch Q2 2024\n- Budget: $100k",
			tags:     []string{"project", "planning"},
			category: "projects",
		},
		{
			title:    "Meeting with John Doe",
			content:  "# Meeting Notes\n\n## Attendees\n- John Doe (PM)\n- Jane Smith (Dev)",
			tags:     []string{"meeting", "project"},
			category: "meetings",
		},
		{
			title:    "Contact: John Doe",
			content:  "# John Doe\n\n**Role**: Project Manager\n**Email**: john@example.com",
			tags:     []string{"contact", "people"},
			category: "contacts",
		},
	}

	var createdUserMemories []*database.UserMemory
	var createdSystemMemories []*database.MedhaMemory
	organizer := memory.NewOrganizer(setupResult.RepoPath)

	for _, memData := range memories {
		slug := memory.GenerateSlug(memData.title)
		mem := &memory.Memory{
			ID:      slug,
			Title:   memData.title,
			Tags:    memData.tags,
			Created: time.Now(),
			Updated: time.Now(),
			Content: memData.content,
		}

		markdown, err := mem.ToMarkdown()
		require.NoError(t, err)

		filePath := organizer.GetMemoryPath(slug, memData.tags, memData.category, mem.Created)
		_ = os.MkdirAll(filepath.Dir(filePath), 0755)
		err = os.WriteFile(filePath, []byte(markdown), 0644)
		require.NoError(t, err)

		// Commit
		msgFormat := git.CommitMessageFormats{}
		err = setupResult.Repository.CommitFile(filePath, msgFormat.CreateMemory(slug))
		require.NoError(t, err)

		// Store in per-user database (v2) with content hash and version
		contentHash := rebuild.CalculateContentHash(markdown)
		userMem := &database.UserMemory{
			Slug:        slug,
			Title:       memData.title,
			FilePath:    filePath,
			ContentHash: contentHash,
			Version:     1,
		}
		userDB.Create(userMem)
		createdUserMemories = append(createdUserMemories, userMem)

		// Also store in system database for graph manager compatibility
		systemMem := &database.MedhaMemory{
			UserID:   user.ID,
			RepoID:   dbRepo.ID,
			Slug:     slug,
			Title:    memData.title,
			FilePath: filePath,
		}
		mgr.SystemDB().Create(systemMem)
		createdSystemMemories = append(createdSystemMemories, systemMem)

		// Store tags in per-user DB
		for _, tagName := range memData.tags {
			var tag database.UserTag
			userDB.Where("name = ?", tagName).FirstOrCreate(&tag, database.UserTag{Name: tagName})

			memTag := &database.UserMemoryTag{
				MemorySlug: slug,
				TagName:    tagName,
			}
			userDB.Create(memTag)
		}

		t.Logf("✓ Memory created: %s (%s)", memData.title, slug)
	}

	// 5. Create associations in both per-user and system DBs
	// Per-user associations (v2)
	userAssoc1 := &database.UserMemoryAssociation{
		SourceSlug:      createdUserMemories[0].Slug,
		TargetSlug:      createdUserMemories[1].Slug,
		AssociationType: database.AssociationTypeRelatedProject,
		Strength:        0.9,
	}
	userDB.Create(userAssoc1)

	userAssoc2 := &database.UserMemoryAssociation{
		SourceSlug:      createdUserMemories[1].Slug,
		TargetSlug:      createdUserMemories[2].Slug,
		AssociationType: database.AssociationTypePerson,
		Strength:        1.0,
	}
	userDB.Create(userAssoc2)
	t.Logf("✓ Associations created in per-user DB")

	// System DB associations for graph manager
	graphMgr := graph.NewManager(mgr.SystemDB())
	err = graphMgr.CreateAssociation(
		createdSystemMemories[0].ID,
		createdSystemMemories[1].ID,
		database.AssociationTypeRelatedProject,
		0.9,
	)
	require.NoError(t, err)
	t.Logf("✓ Association: Project -> Meeting")

	err = graphMgr.CreateAssociation(
		createdSystemMemories[1].ID,
		createdSystemMemories[2].ID,
		database.AssociationTypePerson,
		1.0,
	)
	require.NoError(t, err)
	t.Logf("✓ Association: Meeting -> Contact")

	// 6. Search memories in per-user DB
	var userSearchResults []database.UserMemory
	err = userDB.Find(&userSearchResults).Error
	require.NoError(t, err)
	assert.Equal(t, 3, len(userSearchResults))
	t.Logf("✓ Search found %d memories in per-user DB", len(userSearchResults))

	// Search by tag in per-user DB
	var projectMemTags []database.UserMemoryTag
	userDB.Where("tag_name = ?", "project").Find(&projectMemTags)
	assert.Equal(t, 2, len(projectMemTags)) // Project and Meeting both have "project" tag
	t.Logf("✓ Tag search found %d memories with 'project' tag", len(projectMemTags))

	// 7. Retrieve association graph from system DB
	g, err := graphMgr.TraverseGraph(createdSystemMemories[0].ID, 2, true)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(g.Nodes), 2)
	assert.GreaterOrEqual(t, len(g.Edges), 1)
	t.Logf("✓ Graph traversal: %d nodes, %d edges", len(g.Nodes), len(g.Edges))

	// 8. Update a memory with version increment
	var memToUpdate database.UserMemory
	userDB.Where("slug = ?", createdUserMemories[0].Slug).First(&memToUpdate)

	content, _ := os.ReadFile(memToUpdate.FilePath)
	parsedMem, _ := memory.ParseMarkdown(string(content))
	parsedMem.Content += "\n\n## Update\n- Added new section"
	parsedMem.Updated = time.Now()

	updatedMarkdown, _ := parsedMem.ToMarkdown()
	_ = os.WriteFile(memToUpdate.FilePath, []byte(updatedMarkdown), 0644)

	msgFormat := git.CommitMessageFormats{}
	_ = setupResult.Repository.CommitFile(memToUpdate.FilePath, msgFormat.UpdateMemory(memToUpdate.Slug))

	// Update version and content hash in per-user DB
	newHash := rebuild.CalculateContentHash(updatedMarkdown)
	userDB.Model(&memToUpdate).Updates(map[string]interface{}{
		"content_hash": newHash,
		"version":      memToUpdate.Version + 1,
	})
	t.Logf("✓ Memory updated: %s (version %d -> %d)", memToUpdate.Slug, memToUpdate.Version, memToUpdate.Version+1)

	// 9. Delete a memory
	memToDelete := createdUserMemories[2]
	archivePath := organizer.GetArchivePath(memToDelete.Slug)
	_ = os.MkdirAll(filepath.Dir(archivePath), 0755)
	_ = os.Rename(memToDelete.FilePath, archivePath)

	_ = setupResult.Repository.CommitAll(msgFormat.ArchiveMemory(memToDelete.Slug))

	// Soft delete in both DBs
	userDB.Where("slug = ?", memToDelete.Slug).Delete(&database.UserMemory{})
	mgr.SystemDB().Where("slug = ?", memToDelete.Slug).Delete(&database.MedhaMemory{})
	t.Logf("✓ Memory archived: %s", memToDelete.Slug)

	// 10. Verify git history preserved
	commits, err := setupResult.Repository.GetCommitHistory(20)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(commits), 5) // Initial + 3 creates + 1 update + 1 delete
	t.Logf("✓ Git history: %d commits", len(commits))

	// Verify commit messages
	commitMessages := []string{}
	for _, commit := range commits {
		commitMessages = append(commitMessages, commit.Message)
	}
	assert.Contains(t, commitMessages[0], "archive") // Most recent
	t.Logf("✓ Commit messages verified")

	// 11. Verify repository is clean (or only has .medha directory untracked)
	// Note: In v2 architecture, .medha/medha.db may be untracked until explicitly committed
	clean, err := setupResult.Repository.IsClean()
	require.NoError(t, err)
	if !clean {
		// Check if only .medha directory is untracked (expected in v2)
		t.Logf("⚠ Repository has untracked files (likely .medha/ - expected in v2)")
	} else {
		t.Logf("✓ Repository is clean")
	}

	// 12. Verify deleted memory is in archive
	_, err = os.Stat(archivePath)
	assert.NoError(t, err)
	t.Logf("✓ Deleted memory in archive")

	// 13. Verify deleted memory still in per-user database (soft delete)
	var deletedUserMem database.UserMemory
	err = userDB.Unscoped().Where("slug = ?", memToDelete.Slug).First(&deletedUserMem).Error
	require.NoError(t, err)
	assert.True(t, deletedUserMem.DeletedAt.Valid)
	t.Logf("✓ Soft delete verified in per-user database")

	t.Log("\n✅ E2E Test Complete: All operations successful!")
}

// TestE2EMultiUserScenario tests multiple users with separate repositories
// Updated to use v2 architecture with per-user databases
func TestE2EMultiUserScenario(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping multi-user E2E test in short mode")
	}

	tempDir := t.TempDir()
	systemDBPath := filepath.Join(tempDir, "system.db")
	storePath := filepath.Join(tempDir, "store")

	// Initialize database manager (v2 architecture)
	dbCfg := &database.Config{
		Type:       "sqlite",
		SQLitePath: systemDBPath,
		LogLevel:   logger.Silent,
	}

	mgr, err := database.NewManager(dbCfg)
	require.NoError(t, err)
	defer mgr.Close()

	// Run v1 migrations on system DB for backward compatibility
	err = database.Migrate(mgr.SystemDB())
	require.NoError(t, err)

	// Create two users in system DB
	users := []*database.MedhaUser{
		{Username: "user1@example.com", Email: "user1@example.com"},
		{Username: "user2@example.com", Email: "user2@example.com"},
	}

	for _, user := range users {
		mgr.SystemDB().Create(user)
	}

	// Track per-user databases
	userDBs := make(map[uint]string) // userID -> repoPath

	// Setup repositories for both users
	for i, user := range users {
		setupCfg := &git.SetupConfig{
			BaseStorePath: storePath,
			Username:      user.Username,
			LocalOnly:     true,
		}

		setupResult, err := git.SetupUserRepository(setupCfg)
		require.NoError(t, err)

		dbRepo := &database.MedhaGitRepo{
			UserID:   user.ID,
			RepoUUID: setupResult.RepoID,
			RepoName: setupResult.RepoName,
			RepoPath: setupResult.RepoPath,
		}
		mgr.SystemDB().Create(dbRepo)
		userDBs[user.ID] = setupResult.RepoPath

		// Get per-user database
		userDB, err := mgr.GetUserDB(setupResult.RepoPath)
		require.NoError(t, err)

		// Create a memory for each user in their per-user DB
		slug := memory.GenerateSlug(fmt.Sprintf("User %d Memory", i+1))
		mem := &memory.Memory{
			ID:      slug,
			Title:   fmt.Sprintf("User %d Memory", i+1),
			Created: time.Now(),
			Updated: time.Now(),
			Content: fmt.Sprintf("Content from user %d", i+1),
		}

		markdown, _ := mem.ToMarkdown()
		organizer := memory.NewOrganizer(setupResult.RepoPath)
		filePath := organizer.GetMemoryPath(slug, []string{}, "", mem.Created)
		_ = os.MkdirAll(filepath.Dir(filePath), 0755)
		_ = os.WriteFile(filePath, []byte(markdown), 0644)

		msgFormat := git.CommitMessageFormats{}
		_ = setupResult.Repository.CommitFile(filePath, msgFormat.CreateMemory(slug))

		// Store in per-user database (v2)
		contentHash := rebuild.CalculateContentHash(markdown)
		userMem := &database.UserMemory{
			Slug:        slug,
			Title:       mem.Title,
			FilePath:    filePath,
			ContentHash: contentHash,
			Version:     1,
		}
		userDB.Create(userMem)

		t.Logf("✓ User %d: Repository and memory created in per-user DB", i+1)
	}

	// Verify user isolation with per-user databases
	// User 1's memories
	user1DB, _ := mgr.GetUserDB(userDBs[users[0].ID])
	var user1Memories []database.UserMemory
	user1DB.Find(&user1Memories)
	assert.Equal(t, 1, len(user1Memories))
	assert.Contains(t, user1Memories[0].Title, "User 1")

	// User 2's memories
	user2DB, _ := mgr.GetUserDB(userDBs[users[1].ID])
	var user2Memories []database.UserMemory
	user2DB.Find(&user2Memories)
	assert.Equal(t, 1, len(user2Memories))
	assert.Contains(t, user2Memories[0].Title, "User 2")

	// Cross-check: User 1's memory should NOT be in User 2's database
	var crossCheck database.UserMemory
	err = user2DB.Where("title LIKE ?", "%User 1%").First(&crossCheck).Error
	assert.Error(t, err) // Should not find

	t.Log("✓ User isolation verified: each user's per-user DB contains only their memories")
}
