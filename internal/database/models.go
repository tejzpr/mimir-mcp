// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package database

import (
	"time"

	"gorm.io/gorm"
)

// MimirUser represents a user in the system
type MimirUser struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	Username  string         `gorm:"uniqueIndex;not null" json:"username"`
	Email     string         `json:"email"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

// TableName specifies the table name for MimirUser
func (MimirUser) TableName() string {
	return "mimir_users"
}

// MimirAuthToken represents authentication tokens for users
type MimirAuthToken struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	UserID       uint      `gorm:"index;not null" json:"user_id"`
	AccessToken  string    `gorm:"type:text;not null" json:"access_token"`
	RefreshToken string    `gorm:"type:text" json:"refresh_token"`
	ExpiresAt    time.Time `gorm:"not null" json:"expires_at"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`

	// Foreign key relationship
	User MimirUser `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE" json:"-"`
}

// TableName specifies the table name for MimirAuthToken
func (MimirAuthToken) TableName() string {
	return "mimir_auth_tokens"
}

// MimirGitRepo represents a git repository for a user
type MimirGitRepo struct {
	ID                uint      `gorm:"primaryKey" json:"id"`
	UserID            uint      `gorm:"index;not null" json:"user_id"`
	RepoUUID          string    `gorm:"uniqueIndex;not null" json:"repo_uuid"`
	RepoName          string    `gorm:"not null" json:"repo_name"`
	RepoURL           string    `json:"repo_url"`
	RepoPath          string    `gorm:"not null" json:"repo_path"`
	PATTokenEncrypted string    `gorm:"type:text" json:"-"` // Never expose in JSON
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`

	// Foreign key relationship
	User MimirUser `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE" json:"-"`
}

// TableName specifies the table name for MimirGitRepo
func (MimirGitRepo) TableName() string {
	return "mimir_git_repos"
}

// MimirMemory represents a memory stored in the git repository
type MimirMemory struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	UserID    uint           `gorm:"index;not null" json:"user_id"`
	RepoID    uint           `gorm:"index;not null" json:"repo_id"`
	Slug      string         `gorm:"uniqueIndex;not null" json:"slug"`
	Title     string         `gorm:"not null" json:"title"`
	FilePath  string         `gorm:"not null" json:"file_path"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`

	// Human-aligned memory features
	SupersededBy   *string   `gorm:"column:superseded_by;index" json:"superseded_by,omitempty"`   // Slug of memory that supersedes this one
	LastAccessedAt time.Time `gorm:"column:last_accessed_at" json:"last_accessed_at"`            // Last time this memory was retrieved
	AccessCount    int       `gorm:"column:access_count;default:0" json:"access_count"`          // Number of times this memory was accessed

	// Foreign key relationships
	User MimirUser    `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE" json:"-"`
	Repo MimirGitRepo `gorm:"foreignKey:RepoID;constraint:OnDelete:CASCADE" json:"-"`
}

// TableName specifies the table name for MimirMemory
func (MimirMemory) TableName() string {
	return "mimir_memories"
}

// MimirMemoryAssociation represents associations between memories
type MimirMemoryAssociation struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	SourceMemoryID  uint      `gorm:"index;not null" json:"source_memory_id"`
	TargetMemoryID  uint      `gorm:"index;not null" json:"target_memory_id"`
	AssociationType string    `gorm:"not null" json:"association_type"`
	Strength        float64   `gorm:"default:0.5" json:"strength"`
	Metadata        string    `gorm:"type:text" json:"metadata"` // JSON metadata
	CreatedAt       time.Time `json:"created_at"`

	// Foreign key relationships
	SourceMemory MimirMemory `gorm:"foreignKey:SourceMemoryID;constraint:OnDelete:CASCADE" json:"-"`
	TargetMemory MimirMemory `gorm:"foreignKey:TargetMemoryID;constraint:OnDelete:CASCADE" json:"-"`
}

// TableName specifies the table name for MimirMemoryAssociation
func (MimirMemoryAssociation) TableName() string {
	return "mimir_memory_associations"
}

// MimirTag represents a tag that can be applied to memories
type MimirTag struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"uniqueIndex;not null" json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// TableName specifies the table name for MimirTag
func (MimirTag) TableName() string {
	return "mimir_tags"
}

// MimirMemoryTag represents the many-to-many relationship between memories and tags
type MimirMemoryTag struct {
	MemoryID uint `gorm:"primaryKey" json:"memory_id"`
	TagID    uint `gorm:"primaryKey" json:"tag_id"`

	// Foreign key relationships
	Memory MimirMemory `gorm:"foreignKey:MemoryID;constraint:OnDelete:CASCADE" json:"-"`
	Tag    MimirTag    `gorm:"foreignKey:TagID;constraint:OnDelete:CASCADE" json:"-"`
}

// TableName specifies the table name for MimirMemoryTag
func (MimirMemoryTag) TableName() string {
	return "mimir_memory_tags"
}

// MimirAnnotation represents a note or correction on a memory without changing the original content
type MimirAnnotation struct {
	ID        uint        `gorm:"primaryKey" json:"id"`
	MemoryID  uint        `gorm:"index;not null" json:"memory_id"`
	Type      string      `gorm:"not null" json:"type"` // correction, clarification, context, deprecated
	Content   string      `gorm:"type:text;not null" json:"content"`
	CreatedAt time.Time   `json:"created_at"`
	Memory    MimirMemory `gorm:"foreignKey:MemoryID;constraint:OnDelete:CASCADE" json:"-"`
}

// TableName specifies the table name for MimirAnnotation
func (MimirAnnotation) TableName() string {
	return "mimir_annotations"
}

// AnnotationType constants for memory annotations
const (
	AnnotationTypeCorrection    = "correction"
	AnnotationTypeClarification = "clarification"
	AnnotationTypeContext       = "context"
	AnnotationTypeDeprecated    = "deprecated"
)

// ValidAnnotationTypes returns all valid annotation types
func ValidAnnotationTypes() []string {
	return []string{
		AnnotationTypeCorrection,
		AnnotationTypeClarification,
		AnnotationTypeContext,
		AnnotationTypeDeprecated,
	}
}

// IsValidAnnotationType checks if an annotation type is valid
func IsValidAnnotationType(aType string) bool {
	for _, valid := range ValidAnnotationTypes() {
		if aType == valid {
			return true
		}
	}
	return false
}

// AssociationType constants for memory associations
const (
	AssociationTypeRelatedProject = "related_project"
	AssociationTypePerson         = "person"
	AssociationTypeFollows        = "follows"
	AssociationTypePrecedes       = "precedes"
	AssociationTypeReferences     = "references"
	AssociationTypeRelatedTo      = "related_to"
	AssociationTypeSupersedes     = "supersedes" // New memory replaces old one
	AssociationTypePartOf         = "part_of"    // Memory is part of a larger concept
)

// ValidAssociationTypes returns all valid association types
func ValidAssociationTypes() []string {
	return []string{
		AssociationTypeRelatedProject,
		AssociationTypePerson,
		AssociationTypeFollows,
		AssociationTypePrecedes,
		AssociationTypeReferences,
		AssociationTypeRelatedTo,
		AssociationTypeSupersedes,
		AssociationTypePartOf,
	}
}

// IsValidAssociationType checks if an association type is valid
func IsValidAssociationType(aType string) bool {
	for _, valid := range ValidAssociationTypes() {
		if aType == valid {
			return true
		}
	}
	return false
}
