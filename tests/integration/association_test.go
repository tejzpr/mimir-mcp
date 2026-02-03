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
	"github.com/tejzpr/mimir-mcp/internal/database"
	"github.com/tejzpr/mimir-mcp/internal/git"
	"github.com/tejzpr/mimir-mcp/internal/graph"
	"github.com/tejzpr/mimir-mcp/internal/memory"
	"gorm.io/gorm/logger"
)

func TestAssociationLifecycle(t *testing.T) {
	// Setup
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	repoPath := filepath.Join(tempDir, "repo")

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

	// Create user and repo
	user := &database.MimirUser{Username: "testuser"}
	db.Create(user)

	repo, err := git.InitRepository(repoPath)
	require.NoError(t, err)

	dbRepo := &database.MimirGitRepo{
		UserID:   user.ID,
		RepoUUID: "test-uuid",
		RepoName: "test-repo",
		RepoPath: repoPath,
	}
	db.Create(dbRepo)

	// Create two memories
	createMemory := func(slug, title string) *database.MimirMemory {
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

		msgFormat := git.CommitMessageFormats{}
		_ = repo.CommitFile(filePath, msgFormat.CreateMemory(slug))

		dbMem := &database.MimirMemory{
			UserID:   user.ID,
			RepoID:   dbRepo.ID,
			Slug:     slug,
			Title:    title,
			FilePath: filePath,
		}
		db.Create(dbMem)
		return dbMem
	}

	mem1 := createMemory("memory-1", "Memory 1")
	mem2 := createMemory("memory-2", "Memory 2")

	// Create association
	graphMgr := graph.NewManager(db)
	err = graphMgr.CreateAssociation(mem1.ID, mem2.ID, database.AssociationTypeRelatedTo, 0.8)
	require.NoError(t, err)

	// Verify association in database
	associations, err := graphMgr.GetOutgoingAssociations(mem1.ID)
	require.NoError(t, err)
	assert.Len(t, associations, 1)
	assert.Equal(t, mem2.ID, associations[0].TargetMemoryID)
	assert.Equal(t, database.AssociationTypeRelatedTo, associations[0].AssociationType)

	// Test graph traversal
	g, err := graphMgr.TraverseGraph(mem1.ID, 1, true)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(g.Nodes), 2)
	assert.GreaterOrEqual(t, len(g.Edges), 1)
}
