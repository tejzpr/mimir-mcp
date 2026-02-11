// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package database

import (
	"time"

	"gorm.io/gorm"
)

// MedhaUser represents a user in the system
type MedhaUser struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	Username  string         `gorm:"uniqueIndex;not null" json:"username"`
	Email     string         `json:"email"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

// TableName specifies the table name for MedhaUser
func (MedhaUser) TableName() string {
	return "medha_users"
}

// MedhaAuthToken represents authentication tokens for users
type MedhaAuthToken struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	UserID       uint      `gorm:"index;not null" json:"user_id"`
	AccessToken  string    `gorm:"type:text;not null" json:"access_token"`
	RefreshToken string    `gorm:"type:text" json:"refresh_token"`
	ExpiresAt    time.Time `gorm:"not null" json:"expires_at"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`

	// Foreign key relationship
	User MedhaUser `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE" json:"-"`
}

// TableName specifies the table name for MedhaAuthToken
func (MedhaAuthToken) TableName() string {
	return "medha_auth_tokens"
}

// MedhaGitRepo represents a git repository for a user
type MedhaGitRepo struct {
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
	User MedhaUser `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE" json:"-"`
}

// TableName specifies the table name for MedhaGitRepo
func (MedhaGitRepo) TableName() string {
	return "medha_git_repos"
}

// MedhaMemory represents a memory stored in the git repository
type MedhaMemory struct {
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
	User MedhaUser    `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE" json:"-"`
	Repo MedhaGitRepo `gorm:"foreignKey:RepoID;constraint:OnDelete:CASCADE" json:"-"`
}

// TableName specifies the table name for MedhaMemory
func (MedhaMemory) TableName() string {
	return "medha_memories"
}

// MedhaMemoryAssociation represents associations between memories
type MedhaMemoryAssociation struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	SourceMemoryID  uint      `gorm:"index;not null" json:"source_memory_id"`
	TargetMemoryID  uint      `gorm:"index;not null" json:"target_memory_id"`
	AssociationType string    `gorm:"not null" json:"association_type"`
	Strength        float64   `gorm:"default:0.5" json:"strength"`
	Metadata        string    `gorm:"type:text" json:"metadata"` // JSON metadata
	CreatedAt       time.Time `json:"created_at"`

	// Foreign key relationships
	SourceMemory MedhaMemory `gorm:"foreignKey:SourceMemoryID;constraint:OnDelete:CASCADE" json:"-"`
	TargetMemory MedhaMemory `gorm:"foreignKey:TargetMemoryID;constraint:OnDelete:CASCADE" json:"-"`
}

// TableName specifies the table name for MedhaMemoryAssociation
func (MedhaMemoryAssociation) TableName() string {
	return "medha_memory_associations"
}

// MedhaTag represents a tag that can be applied to memories
type MedhaTag struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"uniqueIndex;not null" json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// TableName specifies the table name for MedhaTag
func (MedhaTag) TableName() string {
	return "medha_tags"
}

// MedhaMemoryTag represents the many-to-many relationship between memories and tags
type MedhaMemoryTag struct {
	MemoryID uint `gorm:"primaryKey" json:"memory_id"`
	TagID    uint `gorm:"primaryKey" json:"tag_id"`

	// Foreign key relationships
	Memory MedhaMemory `gorm:"foreignKey:MemoryID;constraint:OnDelete:CASCADE" json:"-"`
	Tag    MedhaTag    `gorm:"foreignKey:TagID;constraint:OnDelete:CASCADE" json:"-"`
}

// TableName specifies the table name for MedhaMemoryTag
func (MedhaMemoryTag) TableName() string {
	return "medha_memory_tags"
}

// MedhaAnnotation represents a note or correction on a memory without changing the original content
type MedhaAnnotation struct {
	ID        uint        `gorm:"primaryKey" json:"id"`
	MemoryID  uint        `gorm:"index;not null" json:"memory_id"`
	Type      string      `gorm:"not null" json:"type"` // correction, clarification, context, deprecated
	Content   string      `gorm:"type:text;not null" json:"content"`
	CreatedAt time.Time   `json:"created_at"`
	Memory    MedhaMemory `gorm:"foreignKey:MemoryID;constraint:OnDelete:CASCADE" json:"-"`
}

// TableName specifies the table name for MedhaAnnotation
func (MedhaAnnotation) TableName() string {
	return "medha_annotations"
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

// isValidType is a generic helper to check if a type is in a list of valid types
func isValidType(aType string, validTypes []string) bool {
	for _, valid := range validTypes {
		if aType == valid {
			return true
		}
	}
	return false
}

// IsValidAnnotationType checks if an annotation type is valid
func IsValidAnnotationType(aType string) bool {
	return isValidType(aType, ValidAnnotationTypes())
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
	return isValidType(aType, ValidAssociationTypes())
}

// IsDirectionalType returns true for relationship types that should not be bidirectional
func IsDirectionalType(assocType string) bool {
	switch assocType {
	case AssociationTypeFollows,
		AssociationTypePrecedes,
		AssociationTypeSupersedes,
		AssociationTypePartOf:
		return true
	default:
		return false
	}
}
