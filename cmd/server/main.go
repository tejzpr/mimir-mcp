// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/tejzpr/medha-mcp/internal/auth"
	"github.com/tejzpr/medha-mcp/internal/config"
	"github.com/tejzpr/medha-mcp/internal/crypto"
	"github.com/tejzpr/medha-mcp/internal/database"
	"github.com/tejzpr/medha-mcp/internal/git"
	"github.com/tejzpr/medha-mcp/internal/rebuild"
	"github.com/tejzpr/medha-mcp/internal/server"
	"github.com/tejzpr/medha-mcp/pkg/scheduler"
	"gorm.io/gorm/logger"
)

// Version is set at build time via ldflags (e.g. goreleaser -X main.Version={{.Version}}).
var Version string

func main() {
	// CRITICAL: MCP servers must ONLY output JSON-RPC to stdout
	// Redirect all logging to stderr
	log.SetOutput(os.Stderr)
	// Define command-line flags
	httpMode := flag.Bool("http", false, "Run in HTTP server mode (default: stdio for MCP)")
	withAccessingUser := flag.Bool("with-accessinguser", false, "Use ACCESSING_USER env var for user identity (stdio mode only)")
	rebuildDB := flag.Bool("rebuilddb", false, "Rebuild system database index from git repository")
	rebuildUserDB := flag.String("rebuild-userdb", "", "Rebuild per-user database (requires 'all' or username/path)")
	forceRebuild := flag.Bool("force", false, "Force rebuild (requires --rebuilddb or --rebuild-userdb)")
	dbType := flag.String("db-type", "", "Database type (sqlite or postgres)")
	dbPath := flag.String("db-path", "", "Database path (for sqlite)")
	dbDSN := flag.String("db-dsn", "", "Database DSN (for postgres)")
	configPath := flag.String("config", "", "Path to config file")
	port := flag.Int("port", 0, "Server port (HTTP mode only)")
	
	// Embedding flags
	enableEmbeddings := flag.Bool("enable-embeddings", false, "Enable semantic search with embeddings")
	embeddingURL := flag.String("embedding-url", "", "Embedding API base URL")
	embeddingModel := flag.String("embedding-model", "", "Embedding model name")
	embeddingKey := flag.String("embedding-key", "", "Embedding API key (alternative to env var)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Medha MCP Server\n\n")
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Server Mode:\n")
		fmt.Fprintf(os.Stderr, "  %s                          Start MCP server (stdio) using system user (whoami)\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s --with-accessinguser     Start MCP server (stdio) using ACCESSING_USER env var\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s --http                   Start HTTP server with SAML authentication\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nDatabase Rebuild:\n")
		fmt.Fprintf(os.Stderr, "  %s --rebuilddb                          Rebuild system database index\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s --rebuilddb --force                  Rebuild and overwrite existing data\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s --rebuild-userdb all                 Rebuild all users' per-user databases\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s --rebuild-userdb <username>          Rebuild specific user's per-user database\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s --rebuild-userdb <path> --force      Rebuild per-user database at path (force)\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nEmbeddings:\n")
		fmt.Fprintf(os.Stderr, "  %s --enable-embeddings   Enable semantic search with embeddings\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nOptions:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nEnvironment Variables:\n")
		fmt.Fprintf(os.Stderr, "  DB_TYPE            Database type (sqlite or postgres)\n")
		fmt.Fprintf(os.Stderr, "  DB_PATH            SQLite database path\n")
		fmt.Fprintf(os.Stderr, "  DB_DSN             PostgreSQL connection string\n")
		fmt.Fprintf(os.Stderr, "  PORT               Server port (HTTP mode only)\n")
		fmt.Fprintf(os.Stderr, "  ENCRYPTION_KEY     Encryption key for PAT tokens\n")
		fmt.Fprintf(os.Stderr, "  ACCESSING_USER     Username (required with --with-accessinguser)\n")
		fmt.Fprintf(os.Stderr, "  OPENAI_API_KEY     OpenAI API key (required when embeddings enabled)\n")
	}

	flag.Parse()

	// Validate flag combinations
	if *forceRebuild && !*rebuildDB && *rebuildUserDB == "" {
		log.Fatal("ERROR: --force can only be used with --rebuilddb or --rebuild-userdb")
	}
	if *rebuildDB && *httpMode {
		log.Fatal("ERROR: --rebuilddb and --http cannot be used together")
	}
	if *rebuildUserDB != "" && *httpMode {
		log.Fatal("ERROR: --rebuild-userdb and --http cannot be used together")
	}
	if *rebuildDB && *rebuildUserDB != "" {
		log.Fatal("ERROR: --rebuilddb and --rebuild-userdb cannot be used together")
	}
	if *withAccessingUser && *httpMode {
		log.Fatal("ERROR: --with-accessinguser can only be used with stdio mode (not --http)")
	}

	if *rebuildDB {
		log.Println("Starting Medha system database rebuild...")
	} else if *rebuildUserDB != "" {
		log.Println("Starting Medha per-user database rebuild...")
	} else {
		log.Println("Starting Medha MCP Server...")
	}

	// Load configuration
	var cfg *config.Config
	var err error

	if *configPath != "" {
		cfg, err = config.LoadFromPath(*configPath)
		if err != nil {
			log.Printf("Warning: Failed to load config from %s: %v", *configPath, err)
			log.Println("Using defaults")
			cfg = config.DefaultConfig()
		} else {
			log.Printf("Loaded configuration from %s", *configPath)
		}
	} else {
		cfg, err = config.Load()
		if err != nil {
			log.Printf("Warning: Failed to load default config: %v", err)
			log.Println("Using built-in defaults")
			cfg = config.DefaultConfig()
		} else {
			log.Printf("Loaded configuration from ~/.medha/configs/config.json")
		}
	}

	// Apply environment variable overrides
	applyEnvOverrides(cfg)

	// Apply CLI flag overrides (highest priority)
	applyCLIOverrides(cfg, *dbType, *dbPath, *dbDSN, *port)

	// Apply embedding CLI overrides
	applyEmbeddingCLIOverrides(cfg, *enableEmbeddings, *embeddingURL, *embeddingModel, *embeddingKey)

	// Force local auth (always use local authentication)
	cfg.Auth.Type = "local"

	// Log final configuration
	log.Printf("Configuration: database=%s", cfg.Database.Type)

	// Create database manager (handles system DB connection and migrations)
	dbCfg := &database.Config{
		Type:        cfg.Database.Type,
		SQLitePath:  cfg.Database.SQLitePath,
		PostgresDSN: cfg.Database.PostgresDSN,
		LogLevel:    logger.Silent, // CRITICAL: Silence GORM stdout output for MCP
	}

	dbMgr, err := database.NewManager(dbCfg)
	if err != nil {
		log.Fatalf("Failed to create database manager: %v", err)
	}
	defer dbMgr.Close()

	log.Printf("Connected to database: %s", cfg.Database.Type)

	// Run additional system DB migrations (NewManager runs basic migrations)
	if err := database.Migrate(dbMgr.SystemDB()); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	log.Println("Database migrations completed")

	// Create indexes
	if err := database.CreateIndexes(dbMgr.SystemDB()); err != nil {
		log.Printf("Warning: Failed to create indexes: %v", err)
	}

	// Get encryption key from config/env, or generate if not provided
	encryptionKey := getOrGenerateEncryptionKey(cfg)

	// REBUILD MODE: Run rebuild and exit
	if *rebuildDB {
		runRebuildMode(cfg, dbMgr, *forceRebuild)
		return
	}

	// REBUILD USER DB MODE: Run per-user database rebuild and exit
	if *rebuildUserDB != "" {
		runRebuildUserDBMode(cfg, dbMgr, *rebuildUserDB, *forceRebuild)
		return
	}

	// SERVER MODE: Detect mode and run appropriately
	if *httpMode {
		log.Println("Running in HTTP server mode")
		runHTTPMode(cfg, dbMgr, encryptionKey)
	} else {
		if *withAccessingUser {
			log.Println("Running in stdio mode (MCP) with ACCESSING_USER authentication")
		} else {
			log.Println("Running in stdio mode (MCP)")
		}
		runStdioMode(cfg, dbMgr, encryptionKey, *withAccessingUser)
	}
}

// runRebuildMode authenticates user, finds repo, and runs database rebuild
func runRebuildMode(cfg *config.Config, dbMgr *database.Manager, force bool) {
	db := dbMgr.SystemDB()

	// Initialize local auth
	tokenManager := auth.NewTokenManager(db, cfg.Security.TokenTTL)
	localAuth := auth.NewLocalAuthenticator(tokenManager)

	// Get or create system user
	user, _, err := localAuth.Authenticate(db)
	if err != nil {
		log.Fatalf("Failed to authenticate local user: %v", err)
	}

	log.Printf("User authenticated: %s (ID: %d)", user.Username, user.ID)

	// Find user's git repository
	var repo database.MedhaGitRepo
	err = db.Where("user_id = ?", user.ID).First(&repo).Error
	if err != nil {
		// Try to find repo on filesystem
		homeDir, _ := os.UserHomeDir()
		storePath := filepath.Join(homeDir, ".medha", "store")
		expectedRepoPath := git.GetUserRepositoryPath(storePath, user.Username)

		if _, statErr := os.Stat(expectedRepoPath); statErr == nil {
			// Folder exists but not in DB - create DB record
			log.Printf("Found repository folder: %s", expectedRepoPath)
			repo = database.MedhaGitRepo{
				UserID:   user.ID,
				RepoUUID: user.Username,
				RepoName: fmt.Sprintf("medha-%s", user.Username),
				RepoPath: expectedRepoPath,
			}
			if err := db.Create(&repo).Error; err != nil {
				log.Fatalf("Failed to create repository record: %v", err)
			}
		} else {
			log.Fatalf("No git repository found for user. Run medha server first to initialize the repository.")
		}
	}

	log.Printf("Using repository: %s", repo.RepoPath)

	// Verify repository exists on filesystem
	if _, err := os.Stat(repo.RepoPath); os.IsNotExist(err) {
		log.Fatalf("Repository path does not exist: %s", repo.RepoPath)
	}

	// Run rebuild
	opts := rebuild.Options{
		Force: force,
	}

	result, err := rebuild.RebuildIndex(db, user.ID, repo.ID, repo.RepoPath, opts)
	if err != nil {
		log.Fatalf("Rebuild failed: %v", err)
	}

	// Print results
	log.Println("Rebuild completed successfully")
	log.Printf("  Memories processed: %d", result.MemoriesProcessed)
	log.Printf("  Memories created:   %d", result.MemoriesCreated)
	log.Printf("  Memories skipped:   %d", result.MemoriesSkipped)
	log.Printf("  Associations:       %d", result.AssociationsCreated)

	if len(result.Errors) > 0 {
		log.Printf("  Warnings: %d", len(result.Errors))
		for _, e := range result.Errors {
			log.Printf("    - %s", e)
		}
	}
}

// runRebuildUserDBMode rebuilds the per-user database for specified target
// target can be: "all" (all users), username, or filesystem path
func runRebuildUserDBMode(cfg *config.Config, dbMgr *database.Manager, target string, force bool) {
	db := dbMgr.SystemDB()
	opts := rebuild.Options{Force: force}

	if target == "all" {
		// Rebuild all users' per-user databases
		var repos []database.MedhaGitRepo
		if err := db.Find(&repos).Error; err != nil {
			log.Fatalf("Failed to query repositories: %v", err)
		}

		if len(repos) == 0 {
			log.Fatal("No repositories found in system database")
		}

		log.Printf("Found %d repositories to rebuild", len(repos))

		var successCount, failCount int
		for _, repo := range repos {
			log.Printf("Rebuilding per-user database for: %s", repo.RepoPath)

			if _, err := os.Stat(repo.RepoPath); os.IsNotExist(err) {
				log.Printf("  WARNING: Repository path does not exist, skipping: %s", repo.RepoPath)
				failCount++
				continue
			}

			userDB, err := database.OpenUserDB(repo.RepoPath)
			if err != nil {
				log.Printf("  ERROR: Failed to open per-user database: %v", err)
				failCount++
				continue
			}

			result, err := rebuild.RebuildUserIndex(userDB, repo.RepoPath, opts)

			// Close the database
			sqlDB, _ := userDB.DB()
			sqlDB.Close()

			if err != nil {
				log.Printf("  ERROR: Rebuild failed: %v", err)
				failCount++
				continue
			}

			log.Printf("  ✓ Processed: %d, Created: %d, Skipped: %d, Associations: %d",
				result.MemoriesProcessed, result.MemoriesCreated,
				result.MemoriesSkipped, result.AssociationsCreated)
			successCount++
		}

		log.Printf("\nRebuild completed: %d succeeded, %d failed", successCount, failCount)
		return
	}

	// Single target: could be username or path
	var repoPath string

	// Check if target is a path (absolute or relative)
	if filepath.IsAbs(target) || target == "." || target == ".." || 
		(len(target) > 0 && (target[0] == '.' || target[0] == '/')) {
		// Treat as path
		absPath, err := filepath.Abs(target)
		if err != nil {
			log.Fatalf("Invalid path: %v", err)
		}
		repoPath = absPath
	} else {
		// Treat as username - lookup in database
		var repo database.MedhaGitRepo
		err := db.Joins("JOIN medha_users ON medha_users.id = medha_git_repos.user_id").
			Where("medha_users.username = ?", target).
			First(&repo).Error

		if err != nil {
			// Also try partial match on repo path
			err = db.Where("repo_path LIKE ?", "%"+target+"%").First(&repo).Error
			if err != nil {
				log.Fatalf("No repository found for user or path: %s", target)
			}
		}
		repoPath = repo.RepoPath
	}

	// Verify path exists
	if _, err := os.Stat(repoPath); os.IsNotExist(err) {
		log.Fatalf("Repository path does not exist: %s", repoPath)
	}

	log.Printf("Rebuilding per-user database at: %s", repoPath)

	// Open per-user database
	userDB, err := database.OpenUserDB(repoPath)
	if err != nil {
		log.Fatalf("Failed to open per-user database: %v", err)
	}
	defer func() {
		sqlDB, _ := userDB.DB()
		sqlDB.Close()
	}()

	// Run rebuild
	result, err := rebuild.RebuildUserIndex(userDB, repoPath, opts)
	if err != nil {
		log.Fatalf("Rebuild failed: %v", err)
	}

	// Print results
	log.Println("Per-user database rebuild completed successfully")
	log.Printf("  Memories processed: %d", result.MemoriesProcessed)
	log.Printf("  Memories created:   %d", result.MemoriesCreated)
	log.Printf("  Memories skipped:   %d", result.MemoriesSkipped)
	log.Printf("  Associations:       %d", result.AssociationsCreated)

	if len(result.Errors) > 0 {
		log.Printf("  Warnings: %d", len(result.Errors))
		for _, e := range result.Errors {
			log.Printf("    - %s", e)
		}
	}
}

// getOrGenerateEncryptionKey gets encryption key from config or generates a new one
func getOrGenerateEncryptionKey(cfg *config.Config) []byte {
	if cfg.Security.EncryptionKey != "" {
		key, err := crypto.StringToKey(cfg.Security.EncryptionKey)
		if err != nil {
			log.Fatalf("Invalid encryption key in configuration: %v", err)
		}
		log.Println("Using encryption key from configuration")
		return key
	}

	log.Println("No encryption key provided, generating new one...")
	key, err := crypto.GenerateKey()
	if err != nil {
		log.Fatalf("Failed to generate encryption key: %v", err)
	}
	keyStr := crypto.KeyToString(key)
	log.Printf("Generated new encryption key: %s", keyStr)
	log.Println("IMPORTANT: Save this key to persist it across restarts:")
	log.Printf("  Config file:  Add 'encryption_key: \"%s\"' to security section", keyStr)
	log.Printf("  Environment:  export ENCRYPTION_KEY=\"%s\"", keyStr)
	return key
}

// applyEnvOverrides applies environment variable overrides to configuration
func applyEnvOverrides(cfg *config.Config) {
	// Database type
	if dbType := getEnv("DB_TYPE", "MEDHA_DB_TYPE"); dbType != "" {
		cfg.Database.Type = dbType
		log.Printf("Database type from ENV: %s", dbType)
	}

	// Database path (SQLite)
	if dbPath := getEnv("DB_PATH", "MEDHA_DB_PATH"); dbPath != "" {
		cfg.Database.SQLitePath = dbPath
		log.Printf("Database path from ENV")
	}

	// Database DSN (Postgres)
	if dbDSN := getEnv("DB_DSN", "MEDHA_DB_DSN"); dbDSN != "" {
		cfg.Database.PostgresDSN = dbDSN
		log.Printf("Database DSN from ENV (hidden)")
	}

	// Server port
	if portStr := getEnv("PORT", "MEDHA_PORT"); portStr != "" {
		if port, err := strconv.Atoi(portStr); err == nil {
			cfg.Server.Port = port
			log.Printf("Port from ENV: %d", port)
		}
	}

	// Encryption key
	if key := getEnv("ENCRYPTION_KEY", "MEDHA_ENCRYPTION_KEY"); key != "" {
		cfg.Security.EncryptionKey = key
		log.Printf("Encryption key from ENV (hidden)")
	}
}

// applyCLIOverrides applies command-line flag overrides to configuration
func applyCLIOverrides(cfg *config.Config, dbType, dbPath, dbDSN string, port int) {
	if dbType != "" {
		cfg.Database.Type = dbType
		log.Printf("Database type from CLI: %s", dbType)
	}

	if dbPath != "" {
		cfg.Database.SQLitePath = dbPath
		log.Printf("Database path from CLI")
	}

	if dbDSN != "" {
		cfg.Database.PostgresDSN = dbDSN
		log.Printf("Database DSN from CLI (hidden)")
	}

	if port > 0 {
		cfg.Server.Port = port
		log.Printf("Port from CLI: %d", port)
	}
}

// getEnv tries multiple environment variable names and returns the first non-empty value
func getEnv(names ...string) string {
	for _, name := range names {
		if val := os.Getenv(name); val != "" {
			return val
		}
	}
	return ""
}

// applyEmbeddingCLIOverrides applies embedding-related CLI flag overrides
func applyEmbeddingCLIOverrides(cfg *config.Config, enableEmbeddings bool, embeddingURL, embeddingModel, embeddingKey string) {
	if enableEmbeddings {
		cfg.Embeddings.Enabled = true
		log.Printf("Embeddings enabled from CLI")
	}

	if embeddingURL != "" {
		cfg.Embeddings.BaseURL = embeddingURL
		log.Printf("Embedding URL from CLI")
	}

	if embeddingModel != "" {
		cfg.Embeddings.Model = embeddingModel
		log.Printf("Embedding model from CLI: %s", embeddingModel)
	}

	if embeddingKey != "" {
		// Set the API key directly in the environment for validation
		os.Setenv(cfg.Embeddings.APIKeyEnv, embeddingKey)
		log.Printf("Embedding API key from CLI (hidden)")
	}
}

// runStdioMode runs the server in stdio mode for Cursor MCP
// If useAccessingUser is true, uses ACCESSING_USER env var for identity instead of whoami
func runStdioMode(cfg *config.Config, dbMgr *database.Manager, encryptionKey []byte, useAccessingUser bool) {
	db := dbMgr.SystemDB()

	// Initialize local auth
	tokenManager := auth.NewTokenManager(db, cfg.Security.TokenTTL)
	var localAuth *auth.LocalAuthenticator
	if useAccessingUser {
		localAuth = auth.NewLocalAuthenticatorWithAccessingUser(tokenManager)
	} else {
		localAuth = auth.NewLocalAuthenticator(tokenManager)
	}

	// Get or create user
	user, _, err := localAuth.Authenticate(db)
	if err != nil {
		log.Fatalf("Failed to authenticate user: %v", err)
	}

	if useAccessingUser {
		log.Printf("User authenticated via ACCESSING_USER: %s (ID: %d)", user.Username, user.ID)
	} else {
		log.Printf("Local user authenticated: %s (ID: %d)", user.Username, user.ID)
	}

	// Setup git repository for user
	homeDir, _ := os.UserHomeDir()
	storePath := filepath.Join(homeDir, ".medha", "store")

	setupCfg := &git.SetupConfig{
		BaseStorePath: storePath,
		Username:      user.Username, // Use username for deterministic folder naming
		RepoURL:       "",
		PAT:           "",
		LocalOnly:     true,
	}

	// Check if repo already exists (check both database and filesystem)
	var existingRepo database.MedhaGitRepo
	expectedRepoPath := git.GetUserRepositoryPath(storePath, user.Username)

	err = db.Where("user_id = ?", user.ID).First(&existingRepo).Error
	if err != nil {
		// No repo in database, check if folder exists on disk (recovery scenario)
		if _, statErr := os.Stat(expectedRepoPath); statErr == nil {
			// Folder exists but not in DB - recover by adding to DB
			log.Printf("Found existing repository folder, recovering: %s", expectedRepoPath)
			repo := &database.MedhaGitRepo{
				UserID:   user.ID,
				RepoUUID: user.Username, // Use username as the identifier
				RepoName: fmt.Sprintf("medha-%s", user.Username),
				RepoPath: expectedRepoPath,
			}
			db.Create(repo)
			log.Printf("Recovered git repository at: %s", expectedRepoPath)
		} else {
			// Create new repository
			result, err := git.SetupUserRepository(setupCfg)
			if err != nil {
				log.Fatalf("Failed to setup repository: %v", err)
			}

			// Store repo in database
			repo := &database.MedhaGitRepo{
				UserID:   user.ID,
				RepoUUID: result.RepoID,
				RepoName: result.RepoName,
				RepoPath: result.RepoPath,
			}
			db.Create(repo)
			log.Printf("Created git repository at: %s", result.RepoPath)
		}
	} else {
		log.Printf("Using existing git repository at: %s", existingRepo.RepoPath)
	}

	// Get repo path
	var repo database.MedhaGitRepo
	err = db.Where("user_id = ?", user.ID).First(&repo).Error
	if err != nil {
		log.Fatalf("Failed to get repository: %v", err)
	}

	// Create MCP server with database manager
	mcpServer, err := server.NewMCPServer(cfg, dbMgr, encryptionKey)
	if err != nil {
		log.Fatalf("Failed to create MCP server: %v", err)
	}

	// Register all tools for this user
	err = mcpServer.RegisterToolsForUser(user.ID, repo.RepoPath)
	if err != nil {
		log.Fatalf("Failed to register tools: %v", err)
	}

	log.Println("MCP server ready (stdio mode) - 7 tools registered")
	if mcpServer.HasEmbeddings() {
		log.Println("Semantic search enabled")
	}

	// Serve via stdio
	mcpGoServer := mcpServer.GetMCPServer()
	if err := mcpserver.ServeStdio(mcpGoServer); err != nil {
		log.Fatalf("MCP server error: %v", err)
	}
}

// runHTTPMode runs the server in HTTP mode for web interface
func runHTTPMode(cfg *config.Config, dbMgr *database.Manager, encryptionKey []byte) {
	db := dbMgr.SystemDB()

	// Initialize local auth
	tokenManager := auth.NewTokenManager(db, cfg.Security.TokenTTL)
	localAuth := auth.NewLocalAuthenticator(tokenManager)
	username, _ := localAuth.GetLocalUsername()
	log.Printf("Local authentication initialized (system user: %s)", username)

	// Create MCP server with database manager
	mcpServer, err := server.NewMCPServer(cfg, dbMgr, encryptionKey)
	if err != nil {
		log.Fatalf("Failed to create MCP server: %v", err)
	}

	log.Println("MCP server initialized")
	if mcpServer.HasEmbeddings() {
		log.Println("Semantic search enabled")
	}

	// Create HTTP server (simplified, local-only)
	httpServer := server.NewHTTPServer(mcpServer, nil, localAuth, "local", encryptionKey)

	// Register routes
	mux := http.NewServeMux()
	httpServer.RegisterRoutes(mux)

	// Start background scheduler
	sched := scheduler.NewScheduler(db, cfg.Git.SyncInterval, encryptionKey)
	sched.Start()
	defer sched.Stop()

	log.Printf("Background sync scheduler started (interval: %d minutes)", cfg.Git.SyncInterval)

	// Start HTTP server
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	log.Printf("HTTP server starting on %s", addr)

	if cfg.Server.TLS.Enabled {
		log.Println("TLS enabled")
		if err := http.ListenAndServeTLS(addr, cfg.Server.TLS.CertFile, cfg.Server.TLS.KeyFile, mux); err != nil {
			log.Fatalf("Server failed: %v", err)
		}
	} else {
		if err := http.ListenAndServe(addr, mux); err != nil {
			log.Fatalf("Server failed: %v", err)
		}
	}
}
