// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package memory

import "time"

// Memory represents a memory with metadata and content
type Memory struct {
	ID           string        `yaml:"id" json:"id"`
	Title        string        `yaml:"title" json:"title"`
	Tags         []string      `yaml:"tags" json:"tags"`
	Created      time.Time     `yaml:"created" json:"created"`
	Updated      time.Time     `yaml:"updated" json:"updated"`
	SupersededBy string        `yaml:"superseded_by,omitempty" json:"superseded_by,omitempty"`
	Associations []Association `yaml:"associations,omitempty" json:"associations,omitempty"`
	Annotations  []Annotation  `yaml:"annotations,omitempty" json:"annotations,omitempty"`
	Content      string        `yaml:"-" json:"content"`
}

// Association represents a link between memories
type Association struct {
	Target   string  `yaml:"target" json:"target"`
	Type     string  `yaml:"type" json:"type"`
	Strength float64 `yaml:"strength" json:"strength"`
}

// Annotation represents a note or correction on a memory
type Annotation struct {
	Type      string    `yaml:"type" json:"type"`
	Content   string    `yaml:"content" json:"content"`
	CreatedAt time.Time `yaml:"created_at" json:"created_at"`
}

// AssociationType constants
const (
	AssociationTypeRelatedProject = "related_project"
	AssociationTypePerson         = "person"
	AssociationTypeFollows        = "follows"
	AssociationTypePrecedes       = "precedes"
	AssociationTypeReferences     = "references"
	AssociationTypeRelatedTo      = "related_to"
)
