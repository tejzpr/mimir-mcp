// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package graph

import (
	"fmt"

	"github.com/tejzpr/mimir-mcp/internal/database"
	"gorm.io/gorm"
)

// Manager handles graph operations
type Manager struct {
	db *gorm.DB
}

// NewManager creates a new graph manager
func NewManager(db *gorm.DB) *Manager {
	return &Manager{db: db}
}

// CreateAssociation creates a new association between memories
func (m *Manager) CreateAssociation(sourceID, targetID uint, associationType string, strength float64) error {
	assoc := &database.MimirMemoryAssociation{
		SourceMemoryID:  sourceID,
		TargetMemoryID:  targetID,
		AssociationType: associationType,
		Strength:        strength,
	}

	if err := m.db.Create(assoc).Error; err != nil {
		return fmt.Errorf("failed to create association: %w", err)
	}

	return nil
}

// GetAssociations retrieves all associations for a memory
func (m *Manager) GetAssociations(memoryID uint) ([]database.MimirMemoryAssociation, error) {
	var associations []database.MimirMemoryAssociation
	err := m.db.Where("source_memory_id = ? OR target_memory_id = ?", memoryID, memoryID).
		Find(&associations).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get associations: %w", err)
	}
	return associations, nil
}

// GetOutgoingAssociations retrieves associations where the memory is the source
func (m *Manager) GetOutgoingAssociations(memoryID uint) ([]database.MimirMemoryAssociation, error) {
	var associations []database.MimirMemoryAssociation
	err := m.db.Where("source_memory_id = ?", memoryID).Find(&associations).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get outgoing associations: %w", err)
	}
	return associations, nil
}

// GetIncomingAssociations retrieves associations where the memory is the target
func (m *Manager) GetIncomingAssociations(memoryID uint) ([]database.MimirMemoryAssociation, error) {
	var associations []database.MimirMemoryAssociation
	err := m.db.Where("target_memory_id = ?", memoryID).Find(&associations).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get incoming associations: %w", err)
	}
	return associations, nil
}

// DeleteAssociation deletes an association
func (m *Manager) DeleteAssociation(sourceID, targetID uint) error {
	result := m.db.Where("source_memory_id = ? AND target_memory_id = ?", sourceID, targetID).
		Delete(&database.MimirMemoryAssociation{})
	if result.Error != nil {
		return fmt.Errorf("failed to delete association: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("association not found")
	}
	return nil
}
