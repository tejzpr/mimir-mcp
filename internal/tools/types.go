// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tools

import (
	"github.com/tejzpr/mimir-mcp/internal/database"
	"github.com/tejzpr/mimir-mcp/internal/git"
	"github.com/tejzpr/mimir-mcp/internal/memory"
	"gorm.io/gorm"
)

// ToolContext holds shared dependencies for all tools
type ToolContext struct {
	DB       *gorm.DB
	RepoPath string
}

// NewToolContext creates a new tool context
func NewToolContext(db *gorm.DB, repoPath string) *ToolContext {
	return &ToolContext{
		DB:       db,
		RepoPath: repoPath,
	}
}

// GetRepository opens the git repository for operations
func (tc *ToolContext) GetRepository() (*git.Repository, error) {
	return git.OpenRepository(tc.RepoPath)
}

// GetMemoryBySlug retrieves a memory from the database by slug
func (tc *ToolContext) GetMemoryBySlug(slug string) (*database.MimirMemory, error) {
	var mem database.MimirMemory
	err := tc.DB.Where("slug = ?", slug).First(&mem).Error
	return &mem, err
}

// GetOrganizer returns a memory organizer for the repository
func (tc *ToolContext) GetOrganizer() *memory.Organizer {
	return memory.NewOrganizer(tc.RepoPath)
}
