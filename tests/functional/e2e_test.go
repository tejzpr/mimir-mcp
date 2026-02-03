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
	"github.com/tejzpr/mimir-mcp/internal/database"
	"github.com/tejzpr/mimir-mcp/internal/git"
	"github.com/tejzpr/mimir-mcp/internal/graph"
	"github.com/tejzpr/mimir-mcp/internal/memory"
	"gorm.io/gorm/logger"
)

// TestE2ECompleteWorkflow tests a complete end-to-end user scenario
func TestE2ECompleteWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	// Setup
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	storePath := filepath.Join(tempDir, "store")

	// 1. Initialize database
	dbCfg := &database.Config{
		Type:       "sqlite",
		SQLitePath: dbPath,
		LogLevel:   logger.Silent,
	}

	db, err := database.Connect(dbCfg)
	require.NoError(t, err)
	defer database.Close(db)

	err = database.Migrate(db)
	require.NoError(t, err)

	// 2. Create user (simulating SAML authentication)
	user := &database.MimirUser{
		Username: "john.doe@example.com",
		Email:    "john.doe@example.com",
	}
	result := db.Create(user)
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

	// Store repo in database
	dbRepo := &database.MimirGitRepo{
		UserID:   user.ID,
		RepoUUID: setupResult.RepoID,
		RepoName: setupResult.RepoName,
		RepoPath: setupResult.RepoPath,
	}
	db.Create(dbRepo)

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

	var createdMemories []*database.MimirMemory
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

		// Store in database
		dbMem := &database.MimirMemory{
			UserID:   user.ID,
			RepoID:   dbRepo.ID,
			Slug:     slug,
			Title:    memData.title,
			FilePath: filePath,
		}
		db.Create(dbMem)
		createdMemories = append(createdMemories, dbMem)

		// Store tags
		for _, tagName := range memData.tags {
			var tag database.MimirTag
			db.Where("name = ?", tagName).FirstOrCreate(&tag, database.MimirTag{Name: tagName})

			memTag := &database.MimirMemoryTag{
				MemoryID: dbMem.ID,
				TagID:    tag.ID,
			}
			db.Create(memTag)
		}

		t.Logf("✓ Memory created: %s (%s)", memData.title, slug)
	}

	// 5. Create associations
	graphMgr := graph.NewManager(db)

	// Project -> Meeting
	err = graphMgr.CreateAssociation(
		createdMemories[0].ID,
		createdMemories[1].ID,
		database.AssociationTypeRelatedProject,
		0.9,
	)
	require.NoError(t, err)
	t.Logf("✓ Association: Project -> Meeting")

	// Meeting -> Contact (John Doe)
	err = graphMgr.CreateAssociation(
		createdMemories[1].ID,
		createdMemories[2].ID,
		database.AssociationTypePerson,
		1.0,
	)
	require.NoError(t, err)
	t.Logf("✓ Association: Meeting -> Contact")

	// 6. Search memories
	var searchResults []database.MimirMemory
	err = db.Where("user_id = ?", user.ID).Find(&searchResults).Error
	require.NoError(t, err)
	assert.Equal(t, 3, len(searchResults))
	t.Logf("✓ Search found %d memories", len(searchResults))

	// Search by tag
	var projectTag database.MimirTag
	db.Where("name = ?", "project").First(&projectTag)

	var projectMemTags []database.MimirMemoryTag
	db.Where("tag_id = ?", projectTag.ID).Find(&projectMemTags)
	assert.Equal(t, 2, len(projectMemTags)) // Project and Meeting both have "project" tag
	t.Logf("✓ Tag search found %d memories with 'project' tag", len(projectMemTags))

	// 7. Retrieve association graph
	g, err := graphMgr.TraverseGraph(createdMemories[0].ID, 2, true)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(g.Nodes), 2)
	assert.GreaterOrEqual(t, len(g.Edges), 1)
	t.Logf("✓ Graph traversal: %d nodes, %d edges", len(g.Nodes), len(g.Edges))

	// 8. Update a memory
	var memToUpdate database.MimirMemory
	db.First(&memToUpdate, createdMemories[0].ID)

	content, _ := os.ReadFile(memToUpdate.FilePath)
	parsedMem, _ := memory.ParseMarkdown(string(content))
	parsedMem.Content += "\n\n## Update\n- Added new section"
	parsedMem.Updated = time.Now()

	updatedMarkdown, _ := parsedMem.ToMarkdown()
	_ = os.WriteFile(memToUpdate.FilePath, []byte(updatedMarkdown), 0644)

	msgFormat := git.CommitMessageFormats{}
	_ = setupResult.Repository.CommitFile(memToUpdate.FilePath, msgFormat.UpdateMemory(memToUpdate.Slug))
	t.Logf("✓ Memory updated: %s", memToUpdate.Slug)

	// 9. Delete a memory
	memToDelete := createdMemories[2]
	archivePath := organizer.GetArchivePath(memToDelete.Slug)
	_ = os.MkdirAll(filepath.Dir(archivePath), 0755)
	_ = os.Rename(memToDelete.FilePath, archivePath)

	_ = setupResult.Repository.CommitAll(msgFormat.ArchiveMemory(memToDelete.Slug))
	db.Delete(memToDelete)
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

	// 11. Verify repository is clean
	clean, err := setupResult.Repository.IsClean()
	require.NoError(t, err)
	assert.True(t, clean)
	t.Logf("✓ Repository is clean")

	// 12. Verify deleted memory is in archive
	_, err = os.Stat(archivePath)
	assert.NoError(t, err)
	t.Logf("✓ Deleted memory in archive")

	// 13. Verify deleted memory still in database (soft delete)
	var deletedMem database.MimirMemory
	err = db.Unscoped().First(&deletedMem, memToDelete.ID).Error
	require.NoError(t, err)
	assert.True(t, deletedMem.DeletedAt.Valid)
	t.Logf("✓ Soft delete verified in database")

	t.Log("\n✅ E2E Test Complete: All operations successful!")
}

// TestE2EMultiUserScenario tests multiple users with separate repositories
func TestE2EMultiUserScenario(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping multi-user E2E test in short mode")
	}

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	storePath := filepath.Join(tempDir, "store")

	// Initialize database
	dbCfg := &database.Config{
		Type:       "sqlite",
		SQLitePath: dbPath,
		LogLevel:   logger.Silent,
	}

	db, err := database.Connect(dbCfg)
	require.NoError(t, err)
	defer database.Close(db)

	err = database.Migrate(db)
	require.NoError(t, err)

	// Create two users
	users := []*database.MimirUser{
		{Username: "user1@example.com", Email: "user1@example.com"},
		{Username: "user2@example.com", Email: "user2@example.com"},
	}

	for _, user := range users {
		db.Create(user)
	}

	// Setup repositories for both users
	for i, user := range users {
		setupCfg := &git.SetupConfig{
			BaseStorePath: storePath,
			Username:      user.Username,
			LocalOnly:     true,
		}

		setupResult, err := git.SetupUserRepository(setupCfg)
		require.NoError(t, err)

		dbRepo := &database.MimirGitRepo{
			UserID:   user.ID,
			RepoUUID: setupResult.RepoID,
			RepoName: setupResult.RepoName,
			RepoPath: setupResult.RepoPath,
		}
		db.Create(dbRepo)

		// Create a memory for each user
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

		dbMem := &database.MimirMemory{
			UserID:   user.ID,
			RepoID:   dbRepo.ID,
			Slug:     slug,
			Title:    mem.Title,
			FilePath: filePath,
		}
		db.Create(dbMem)

		t.Logf("✓ User %d: Repository and memory created", i+1)
	}

	// Verify user isolation
	var user1Memories []database.MimirMemory
	db.Where("user_id = ?", users[0].ID).Find(&user1Memories)
	assert.Equal(t, 1, len(user1Memories))

	var user2Memories []database.MimirMemory
	db.Where("user_id = ?", users[1].ID).Find(&user2Memories)
	assert.Equal(t, 1, len(user2Memories))

	t.Log("✓ User isolation verified: each user sees only their memories")
}
