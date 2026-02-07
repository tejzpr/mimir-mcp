// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package integration

import (
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
	"gorm.io/gorm/logger"
)

// TestAssociationLifecycle tests association creation and graph traversal
// Updated to use v2 architecture with per-user database
func TestAssociationLifecycle(t *testing.T) {
	// Setup
	tempDir := t.TempDir()
	systemDBPath := filepath.Join(tempDir, "system.db")
	repoPath := filepath.Join(tempDir, "repo")
	require.NoError(t, os.MkdirAll(repoPath, 0755))

	// Create database manager (v2 architecture)
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

	// Create user and repo in system DB
	user := &database.MedhaUser{Username: "testuser"}
	mgr.SystemDB().Create(user)

	// Initialize git repository (skip commit operations if sandboxed)
	repo, gitErr := git.InitRepository(repoPath)
	gitAvailable := gitErr == nil

	dbRepo := &database.MedhaGitRepo{
		UserID:   user.ID,
		RepoUUID: "test-uuid",
		RepoName: "test-repo",
		RepoPath: repoPath,
	}
	mgr.SystemDB().Create(dbRepo)

	// Get per-user database
	userDB, err := mgr.GetUserDB(repoPath)
	require.NoError(t, err)

	// Create two memories using UserMemory (v2 model)
	createMemory := func(slug, title string) *database.UserMemory {
		mem := &memory.Memory{
			ID:      slug,
			Title:   title,
			Created: time.Now(),
			Updated: time.Now(),
			Content: "Test content",
		}

		markdown, _ := mem.ToMarkdown()
		organizer := memory.NewOrganizer(repoPath)
		filePath := organizer.GetMemoryPath(slug, []string{}, "", mem.Created)
		_ = os.MkdirAll(filepath.Dir(filePath), 0755)
		_ = os.WriteFile(filePath, []byte(markdown), 0644)

		// Only commit if git is available
		if gitAvailable && repo != nil {
			msgFormat := git.CommitMessageFormats{}
			_ = repo.CommitFile(filePath, msgFormat.CreateMemory(slug))
		}

		// Store in per-user database (v2)
		dbMem := &database.UserMemory{
			Slug:     slug,
			Title:    title,
			FilePath: filePath,
			Version:  1,
		}
		userDB.Create(dbMem)
		return dbMem
	}

	mem1 := createMemory("memory-1", "Memory 1")
	mem2 := createMemory("memory-2", "Memory 2")

	// Create association using UserMemoryAssociation (v2 model)
	assoc := &database.UserMemoryAssociation{
		SourceSlug:      mem1.Slug,
		TargetSlug:      mem2.Slug,
		AssociationType: database.AssociationTypeRelatedTo,
		Strength:        0.8,
	}
	err = userDB.Create(assoc).Error
	require.NoError(t, err)

	// Verify association in per-user database
	var associations []database.UserMemoryAssociation
	err = userDB.Where("source_slug = ?", mem1.Slug).Find(&associations).Error
	require.NoError(t, err)
	assert.Len(t, associations, 1)
	assert.Equal(t, mem2.Slug, associations[0].TargetSlug)
	assert.Equal(t, database.AssociationTypeRelatedTo, associations[0].AssociationType)

	// Test graph traversal using system DB (for backward compatibility)
	// Also create entries in system DB for graph manager
	systemMem1 := &database.MedhaMemory{
		UserID:   user.ID,
		RepoID:   dbRepo.ID,
		Slug:     mem1.Slug,
		Title:    mem1.Title,
		FilePath: mem1.FilePath,
	}
	systemMem2 := &database.MedhaMemory{
		UserID:   user.ID,
		RepoID:   dbRepo.ID,
		Slug:     mem2.Slug,
		Title:    mem2.Title,
		FilePath: mem2.FilePath,
	}
	mgr.SystemDB().Create(systemMem1)
	mgr.SystemDB().Create(systemMem2)

	graphMgr := graph.NewManager(mgr.SystemDB())
	err = graphMgr.CreateAssociation(systemMem1.ID, systemMem2.ID, database.AssociationTypeRelatedTo, 0.8)
	require.NoError(t, err)

	// Verify graph traversal
	g, err := graphMgr.TraverseGraph(systemMem1.ID, 1, true)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(g.Nodes), 2)
	assert.GreaterOrEqual(t, len(g.Edges), 1)
}

// TestAssociationLifecycle_UserDB tests associations using only the per-user database
func TestAssociationLifecycle_UserDB(t *testing.T) {
	tempDir := t.TempDir()
	repoPath := filepath.Join(tempDir, "repo")
	require.NoError(t, os.MkdirAll(repoPath, 0755))

	// Open per-user database directly
	userDB, err := database.OpenUserDB(repoPath)
	require.NoError(t, err)
	defer func() {
		sqlDB, _ := userDB.DB()
		sqlDB.Close()
	}()

	// Create memories
	mem1 := &database.UserMemory{Slug: "assoc-mem-1", Title: "Memory 1", FilePath: "1.md", Version: 1}
	mem2 := &database.UserMemory{Slug: "assoc-mem-2", Title: "Memory 2", FilePath: "2.md", Version: 1}
	mem3 := &database.UserMemory{Slug: "assoc-mem-3", Title: "Memory 3", FilePath: "3.md", Version: 1}
	require.NoError(t, userDB.Create(mem1).Error)
	require.NoError(t, userDB.Create(mem2).Error)
	require.NoError(t, userDB.Create(mem3).Error)

	// Create multiple associations
	associations := []*database.UserMemoryAssociation{
		{SourceSlug: "assoc-mem-1", TargetSlug: "assoc-mem-2", AssociationType: "related_to", Strength: 0.8},
		{SourceSlug: "assoc-mem-1", TargetSlug: "assoc-mem-3", AssociationType: "references", Strength: 0.5},
		{SourceSlug: "assoc-mem-2", TargetSlug: "assoc-mem-3", AssociationType: "part_of", Strength: 0.9},
	}
	for _, assoc := range associations {
		require.NoError(t, userDB.Create(assoc).Error)
	}

	// Query outgoing associations from mem1
	var outgoing []database.UserMemoryAssociation
	err = userDB.Where("source_slug = ?", "assoc-mem-1").Find(&outgoing).Error
	require.NoError(t, err)
	assert.Len(t, outgoing, 2)

	// Query incoming associations to mem3
	var incoming []database.UserMemoryAssociation
	err = userDB.Where("target_slug = ?", "assoc-mem-3").Find(&incoming).Error
	require.NoError(t, err)
	assert.Len(t, incoming, 2)

	// Test bidirectional query
	var bidirectional []database.UserMemoryAssociation
	err = userDB.Where("source_slug = ? OR target_slug = ?", "assoc-mem-2", "assoc-mem-2").Find(&bidirectional).Error
	require.NoError(t, err)
	assert.Len(t, bidirectional, 2) // One outgoing, one incoming
}
