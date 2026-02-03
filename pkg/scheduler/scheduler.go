// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package scheduler

import (
	"log"
	"time"

	"github.com/tejzpr/mimir-mcp/internal/crypto"
	"github.com/tejzpr/mimir-mcp/internal/database"
	"github.com/tejzpr/mimir-mcp/internal/git"
	"gorm.io/gorm"
)

// Scheduler handles periodic git sync operations
type Scheduler struct {
	db            *gorm.DB
	interval      time.Duration
	encryptionKey []byte
	stopChan      chan bool
}

// NewScheduler creates a new scheduler
func NewScheduler(db *gorm.DB, intervalMinutes int, encryptionKey []byte) *Scheduler {
	return &Scheduler{
		db:            db,
		interval:      time.Duration(intervalMinutes) * time.Minute,
		encryptionKey: encryptionKey,
		stopChan:      make(chan bool),
	}
}

// Start begins the scheduler
func (s *Scheduler) Start() {
	ticker := time.NewTicker(s.interval)
	go func() {
		for {
			select {
			case <-ticker.C:
				s.syncAllRepositories()
			case <-s.stopChan:
				ticker.Stop()
				return
			}
		}
	}()
}

// Stop stops the scheduler
func (s *Scheduler) Stop() {
	s.stopChan <- true
}

// syncAllRepositories syncs all user repositories
func (s *Scheduler) syncAllRepositories() {
	var repos []database.MimirGitRepo
	if err := s.db.Where("pat_token_encrypted != ''").Find(&repos).Error; err != nil {
		log.Printf("Failed to fetch repositories: %v", err)
		return
	}

	for _, repo := range repos {
		if err := s.syncRepository(&repo); err != nil {
			log.Printf("Failed to sync repo %s: %v", repo.RepoName, err)
		}
	}
}

// syncRepository syncs a single repository
func (s *Scheduler) syncRepository(repo *database.MimirGitRepo) error {
	// Decrypt PAT
	pat, err := crypto.DecryptPAT(repo.PATTokenEncrypted, s.encryptionKey)
	if err != nil {
		return err
	}

	// Open repository
	gitRepo, err := git.OpenRepository(repo.RepoPath)
	if err != nil {
		return err
	}

	// Sync with last-write-wins
	_, err = gitRepo.Sync(pat, true)
	return err
}
