// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package functional

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
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

func TestLoadConcurrentWrites(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping load test in short mode")
	}

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	repoPath := filepath.Join(tempDir, "repo")

	// Setup
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

	// Concurrent write test
	concurrency := 10
	memoriesPerRoutine := 5
	totalMemories := concurrency * memoriesPerRoutine

	var wg sync.WaitGroup
	errors := make(chan error, totalMemories)
	successCount := 0
	var mu sync.Mutex

	startTime := time.Now()

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(routineID int) {
			defer wg.Done()

			for j := 0; j < memoriesPerRoutine; j++ {
				title := fmt.Sprintf("Memory %d-%d", routineID, j)
				slug := memory.GenerateSlugWithDate(title, time.Now())

				mem := &memory.Memory{
					ID:      slug,
					Title:   title,
					Created: time.Now(),
					Updated: time.Now(),
					Content: fmt.Sprintf("Content for %s", title),
				}

				markdown, err := mem.ToMarkdown()
				if err != nil {
					errors <- err
					continue
				}

				organizer := memory.NewOrganizer(repoPath)
				filePath := organizer.GetMemoryPath(slug, []string{}, "", mem.Created)

				// Thread-safe directory creation
				mu.Lock()
				_ = os.MkdirAll(filepath.Dir(filePath), 0755)
				err = os.WriteFile(filePath, []byte(markdown), 0644)
				if err == nil {
					msgFormat := git.CommitMessageFormats{}
					err = repo.CommitFile(filePath, msgFormat.CreateMemory(slug))
				}
				mu.Unlock()

				if err != nil {
					errors <- err
					continue
				}

				// Store in database (GORM handles concurrency)
				dbMem := &database.MimirMemory{
					UserID:   user.ID,
					RepoID:   dbRepo.ID,
					Slug:     slug,
					Title:    title,
					FilePath: filePath,
				}
				if err := db.Create(dbMem).Error; err != nil {
					errors <- err
					continue
				}

				mu.Lock()
				successCount++
				mu.Unlock()
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	duration := time.Since(startTime)

	// Check for errors
	errorCount := 0
	for err := range errors {
		t.Logf("Error: %v", err)
		errorCount++
	}

	t.Logf("\n=== Load Test Results ===")
	t.Logf("Concurrent routines: %d", concurrency)
	t.Logf("Memories per routine: %d", memoriesPerRoutine)
	t.Logf("Total memories: %d", totalMemories)
	t.Logf("Successful: %d", successCount)
	t.Logf("Errors: %d", errorCount)
	t.Logf("Duration: %v", duration)
	t.Logf("Throughput: %.2f memories/sec", float64(successCount)/duration.Seconds())

	// Verify at least 80% success rate
	successRate := float64(successCount) / float64(totalMemories)
	assert.GreaterOrEqual(t, successRate, 0.8, "Success rate should be at least 80%%")
}

func TestLoadLargeMemoryFile(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping large file test in short mode")
	}

	tempDir := t.TempDir()
	repoPath := filepath.Join(tempDir, "repo")

	repo, err := git.InitRepository(repoPath)
	require.NoError(t, err)

	// Create a large memory file (1MB of content)
	largeContent := ""
	for i := 0; i < 10000; i++ {
		largeContent += fmt.Sprintf("## Section %d\n\nLorem ipsum dolor sit amet, consectetur adipiscing elit. ", i)
	}

	mem := &memory.Memory{
		ID:      "large-memory-test",
		Title:   "Large Memory Test",
		Created: time.Now(),
		Updated: time.Now(),
		Content: largeContent,
	}

	markdown, err := mem.ToMarkdown()
	require.NoError(t, err)

	filePath := filepath.Join(repoPath, "large-memory.md")
	err = os.WriteFile(filePath, []byte(markdown), 0644)
	require.NoError(t, err)

	// Commit
	startTime := time.Now()
	msgFormat := git.CommitMessageFormats{}
	err = repo.CommitFile(filePath, msgFormat.CreateMemory("large-memory-test"))
	duration := time.Since(startTime)

	require.NoError(t, err)
	t.Logf("✓ Large file committed in %v (size: %d bytes)", duration, len(markdown))

	// Verify file can be read back
	content, err := os.ReadFile(filePath)
	require.NoError(t, err)
	assert.Greater(t, len(content), 500000) // At least 500KB
}

func TestLoadDeepGraphTraversal(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping deep graph test in short mode")
	}

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

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

	user := &database.MimirUser{Username: "testuser"}
	db.Create(user)

	repo := &database.MimirGitRepo{
		UserID:   user.ID,
		RepoUUID: "test-uuid",
		RepoName: "test-repo",
		RepoPath: "/tmp/test",
	}
	db.Create(repo)

	// Create a chain of 10 memories
	var memories []*database.MimirMemory
	for i := 0; i < 10; i++ {
		dbMem := &database.MimirMemory{
			UserID:   user.ID,
			RepoID:   repo.ID,
			Slug:     fmt.Sprintf("memory-%d", i),
			Title:    fmt.Sprintf("Memory %d", i),
			FilePath: fmt.Sprintf("/tmp/memory-%d.md", i),
		}
		db.Create(dbMem)
		memories = append(memories, dbMem)
	}

	// Create sequential associations
	graphMgr := graph.NewManager(db)
	for i := 0; i < 9; i++ {
		err := graphMgr.CreateAssociation(
			memories[i].ID,
			memories[i+1].ID,
			database.AssociationTypeFollows,
			0.8,
		)
		require.NoError(t, err)
	}

	// Traverse 5 hops
	startTime := time.Now()
	g, err := graphMgr.TraverseGraph(memories[0].ID, 5, true)
	duration := time.Since(startTime)

	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(g.Nodes), 6) // 0 through 5
	assert.GreaterOrEqual(t, len(g.Edges), 5)

	t.Logf("✓ Deep graph traversal (5 hops) completed in %v", duration)
	t.Logf("  Nodes: %d, Edges: %d", len(g.Nodes), len(g.Edges))
}
