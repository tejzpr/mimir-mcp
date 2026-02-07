// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tools

import (
	"github.com/tejzpr/medha-mcp/internal/database"
	"github.com/tejzpr/medha-mcp/internal/embeddings"
	"github.com/tejzpr/medha-mcp/internal/git"
	"github.com/tejzpr/medha-mcp/internal/memory"
	"gorm.io/gorm"
)

// Query constants
const (
	querySlugEquals = "slug = ?"
)

// ToolContext holds shared dependencies for all tools
// In v2 architecture:
// - SystemDB: Global database for users, auth, repos (stays at ~/.medha/db/)
// - UserDB: Per-user database in .medha/medha.db inside git repo
// - DB: Kept for backward compatibility, points to SystemDB
// - EmbeddingService: Optional embedding service for semantic search
type ToolContext struct {
	DB               *gorm.DB            // Backward compatibility - points to SystemDB
	SystemDB         *gorm.DB            // Global database for users, auth, repos
	UserDB           *gorm.DB            // Per-user database in .medha/medha.db
	RepoPath         string
	DBMgr            *database.Manager   // Database manager for handling connections
	EmbeddingService *embeddings.Service // Optional embedding service for semantic search
}

// NewToolContext creates a new tool context (v1 style - for testing without UserDB)
func NewToolContext(db *gorm.DB, repoPath string) *ToolContext {
	return &ToolContext{
		DB:       db,
		SystemDB: db,
		RepoPath: repoPath,
	}
}

// NewToolContextWithManager creates a new tool context using the database manager
func NewToolContextWithManager(mgr *database.Manager, repoPath string) (*ToolContext, error) {
	userDB, err := mgr.GetUserDB(repoPath)
	if err != nil {
		return nil, err
	}

	return &ToolContext{
		DB:       mgr.SystemDB(), // Backward compatibility
		SystemDB: mgr.SystemDB(),
		UserDB:   userDB,
		RepoPath: repoPath,
		DBMgr:    mgr,
	}, nil
}

// NewToolContextWithEmbeddings creates a tool context with embedding service enabled
func NewToolContextWithEmbeddings(mgr *database.Manager, repoPath string, embeddingSvc *embeddings.Service) (*ToolContext, error) {
	ctx, err := NewToolContextWithManager(mgr, repoPath)
	if err != nil {
		return nil, err
	}
	ctx.EmbeddingService = embeddingSvc
	return ctx, nil
}

// SetEmbeddingService sets the embedding service for the tool context
func (tc *ToolContext) SetEmbeddingService(svc *embeddings.Service) {
	tc.EmbeddingService = svc
}

// HasEmbeddings returns true if embedding service is available and enabled
func (tc *ToolContext) HasEmbeddings() bool {
	return tc.EmbeddingService != nil && tc.EmbeddingService.IsEnabled()
}

// GetRepository opens the git repository for operations
func (tc *ToolContext) GetRepository() (*git.Repository, error) {
	return git.OpenRepository(tc.RepoPath)
}

// GetUserMemoryBySlug retrieves a UserMemory from the per-user database by slug
func (tc *ToolContext) GetUserMemoryBySlug(slug string) (*database.UserMemory, error) {
	if tc.UserDB == nil {
		return nil, gorm.ErrRecordNotFound
	}
	var mem database.UserMemory
	err := tc.UserDB.Where(querySlugEquals, slug).First(&mem).Error
	return &mem, err
}

// GetOrganizer returns a memory organizer for the repository
func (tc *ToolContext) GetOrganizer() *memory.Organizer {
	return memory.NewOrganizer(tc.RepoPath)
}

// HasUserDB returns true if the tool context has a per-user database
func (tc *ToolContext) HasUserDB() bool {
	return tc.UserDB != nil
}

// CloseUserDB closes the per-user database connection
// This should be called before git sync operations
func (tc *ToolContext) CloseUserDB() error {
	if tc.DBMgr != nil {
		return tc.DBMgr.CloseUserDB(tc.RepoPath)
	}
	if tc.UserDB != nil {
		sqlDB, err := tc.UserDB.DB()
		if err != nil {
			return err
		}
		return sqlDB.Close()
	}
	return nil
}

// ReopenUserDB reopens the per-user database connection
// This should be called after git sync operations
func (tc *ToolContext) ReopenUserDB() error {
	if tc.DBMgr != nil {
		userDB, err := tc.DBMgr.ReopenUserDB(tc.RepoPath)
		if err != nil {
			return err
		}
		tc.UserDB = userDB
	}
	return nil
}
