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
	"github.com/tejzpr/mimir-mcp/internal/auth"
	"github.com/tejzpr/mimir-mcp/internal/config"
	"github.com/tejzpr/mimir-mcp/internal/crypto"
	"github.com/tejzpr/mimir-mcp/internal/database"
	"github.com/tejzpr/mimir-mcp/internal/git"
	"github.com/tejzpr/mimir-mcp/internal/rebuild"
	"github.com/tejzpr/mimir-mcp/internal/server"
	"github.com/tejzpr/mimir-mcp/pkg/scheduler"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	// CRITICAL: MCP servers must ONLY output JSON-RPC to stdout
	// Redirect all logging to stderr
	log.SetOutput(os.Stderr)
	// Define command-line flags
	httpMode := flag.Bool("http", false, "Run in HTTP server mode (default: stdio for MCP)")
	withAccessingUser := flag.Bool("with-accessinguser", false, "Use ACCESSING_USER env var for user identity (stdio mode only)")
	rebuildDB := flag.Bool("rebuilddb", false, "Rebuild database index from git repository")
	forceRebuild := flag.Bool("force", false, "Force rebuild (requires --rebuilddb)")
	dbType := flag.String("db-type", "", "Database type (sqlite or postgres)")
	dbPath := flag.String("db-path", "", "Database path (for sqlite)")
	dbDSN := flag.String("db-dsn", "", "Database DSN (for postgres)")
	configPath := flag.String("config", "", "Path to config file")
	port := flag.Int("port", 0, "Server port (HTTP mode only)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Mimir MCP Server\n\n")
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Server Mode:\n")
		fmt.Fprintf(os.Stderr, "  %s                          Start MCP server (stdio) using system user (whoami)\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s --with-accessinguser     Start MCP server (stdio) using ACCESSING_USER env var\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s --http                   Start HTTP server with SAML authentication\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nDatabase Rebuild:\n")
		fmt.Fprintf(os.Stderr, "  %s --rebuilddb           Rebuild database index from git repository\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s --rebuilddb --force   Rebuild and overwrite existing data\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nOptions:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nEnvironment Variables:\n")
		fmt.Fprintf(os.Stderr, "  DB_TYPE            Database type (sqlite or postgres)\n")
		fmt.Fprintf(os.Stderr, "  DB_PATH            SQLite database path\n")
		fmt.Fprintf(os.Stderr, "  DB_DSN             PostgreSQL connection string\n")
		fmt.Fprintf(os.Stderr, "  PORT               Server port (HTTP mode only)\n")
		fmt.Fprintf(os.Stderr, "  ENCRYPTION_KEY     Encryption key for PAT tokens\n")
		fmt.Fprintf(os.Stderr, "  ACCESSING_USER     Username (required with --with-accessinguser)\n")
	}

	flag.Parse()

	// Validate flag combinations
	if *forceRebuild && !*rebuildDB {
		log.Fatal("ERROR: --force can only be used with --rebuilddb")
	}
	if *rebuildDB && *httpMode {
		log.Fatal("ERROR: --rebuilddb and --http cannot be used together")
	}
	if *withAccessingUser && *httpMode {
		log.Fatal("ERROR: --with-accessinguser can only be used with stdio mode (not --http)")
	}

	if *rebuildDB {
		log.Println("Starting Mimir database rebuild...")
	} else {
		log.Println("Starting Mimir MCP Server...")
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
			log.Printf("Loaded configuration from ~/.mimir/configs/config.json")
		}
	}

	// Apply environment variable overrides
	applyEnvOverrides(cfg)

	// Apply CLI flag overrides (highest priority)
	applyCLIOverrides(cfg, *dbType, *dbPath, *dbDSN, *port)

	// Force local auth (always use local authentication)
	cfg.Auth.Type = "local"

	// Log final configuration
	log.Printf("Configuration: database=%s", cfg.Database.Type)

	// Connect to database with stderr logging for GORM
	dbCfg := &database.Config{
		Type:        cfg.Database.Type,
		SQLitePath:  cfg.Database.SQLitePath,
		PostgresDSN: cfg.Database.PostgresDSN,
		LogLevel:    logger.Silent, // CRITICAL: Silence GORM stdout output for MCP
	}

	db, err := database.Connect(dbCfg)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer database.Close(db)

	log.Printf("Connected to database: %s", cfg.Database.Type)

	// Run migrations
	if err := database.Migrate(db); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	log.Println("Database migrations completed")

	// Create indexes
	if err := database.CreateIndexes(db); err != nil {
		log.Printf("Warning: Failed to create indexes: %v", err)
	}

	// Get encryption key from config/env, or generate if not provided
	encryptionKey := getOrGenerateEncryptionKey(cfg)

	// REBUILD MODE: Run rebuild and exit
	if *rebuildDB {
		runRebuildMode(cfg, db, *forceRebuild)
		return
	}

	// SERVER MODE: Detect mode and run appropriately
	if *httpMode {
		log.Println("Running in HTTP server mode")
		runHTTPMode(cfg, db, encryptionKey)
	} else {
		if *withAccessingUser {
			log.Println("Running in stdio mode (MCP) with ACCESSING_USER authentication")
		} else {
			log.Println("Running in stdio mode (MCP)")
		}
		runStdioMode(cfg, db, encryptionKey, *withAccessingUser)
	}
}

// runRebuildMode authenticates user, finds repo, and runs database rebuild
func runRebuildMode(cfg *config.Config, db *gorm.DB, force bool) {
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
	var repo database.MimirGitRepo
	err = db.Where("user_id = ?", user.ID).First(&repo).Error
	if err != nil {
		// Try to find repo on filesystem
		homeDir, _ := os.UserHomeDir()
		storePath := filepath.Join(homeDir, ".mimir", "store")
		expectedRepoPath := git.GetUserRepositoryPath(storePath, user.Username)

		if _, statErr := os.Stat(expectedRepoPath); statErr == nil {
			// Folder exists but not in DB - create DB record
			log.Printf("Found repository folder: %s", expectedRepoPath)
			repo = database.MimirGitRepo{
				UserID:   user.ID,
				RepoUUID: user.Username,
				RepoName: fmt.Sprintf("mimir-%s", user.Username),
				RepoPath: expectedRepoPath,
			}
			if err := db.Create(&repo).Error; err != nil {
				log.Fatalf("Failed to create repository record: %v", err)
			}
		} else {
			log.Fatalf("No git repository found for user. Run mimir server first to initialize the repository.")
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
	if dbType := getEnv("DB_TYPE", "MIMIR_DB_TYPE"); dbType != "" {
		cfg.Database.Type = dbType
		log.Printf("Database type from ENV: %s", dbType)
	}

	// Database path (SQLite)
	if dbPath := getEnv("DB_PATH", "MIMIR_DB_PATH"); dbPath != "" {
		cfg.Database.SQLitePath = dbPath
		log.Printf("Database path from ENV")
	}

	// Database DSN (Postgres)
	if dbDSN := getEnv("DB_DSN", "MIMIR_DB_DSN"); dbDSN != "" {
		cfg.Database.PostgresDSN = dbDSN
		log.Printf("Database DSN from ENV (hidden)")
	}

	// Server port
	if portStr := getEnv("PORT", "MIMIR_PORT"); portStr != "" {
		if port, err := strconv.Atoi(portStr); err == nil {
			cfg.Server.Port = port
			log.Printf("Port from ENV: %d", port)
		}
	}

	// Encryption key
	if key := getEnv("ENCRYPTION_KEY", "MIMIR_ENCRYPTION_KEY"); key != "" {
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

// runStdioMode runs the server in stdio mode for Cursor MCP
// If useAccessingUser is true, uses ACCESSING_USER env var for identity instead of whoami
func runStdioMode(cfg *config.Config, db *gorm.DB, encryptionKey []byte, useAccessingUser bool) {
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
	storePath := filepath.Join(homeDir, ".mimir", "store")

	setupCfg := &git.SetupConfig{
		BaseStorePath: storePath,
		Username:      user.Username, // Use username for deterministic folder naming
		RepoURL:       "",
		PAT:           "",
		LocalOnly:     true,
	}

	// Check if repo already exists (check both database and filesystem)
	var existingRepo database.MimirGitRepo
	expectedRepoPath := git.GetUserRepositoryPath(storePath, user.Username)
	
	err = db.Where("user_id = ?", user.ID).First(&existingRepo).Error
	if err != nil {
		// No repo in database, check if folder exists on disk (recovery scenario)
		if _, statErr := os.Stat(expectedRepoPath); statErr == nil {
			// Folder exists but not in DB - recover by adding to DB
			log.Printf("Found existing repository folder, recovering: %s", expectedRepoPath)
			repo := &database.MimirGitRepo{
				UserID:   user.ID,
				RepoUUID: user.Username, // Use username as the identifier
				RepoName: fmt.Sprintf("mimir-%s", user.Username),
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
			repo := &database.MimirGitRepo{
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
	var repo database.MimirGitRepo
	err = db.Where("user_id = ?", user.ID).First(&repo).Error
	if err != nil {
		log.Fatalf("Failed to get repository: %v", err)
	}

	// Create MCP server
	mcpServer, err := server.NewMCPServer(cfg, db, encryptionKey)
	if err != nil {
		log.Fatalf("Failed to create MCP server: %v", err)
	}

	// Register all tools for this user
	err = mcpServer.RegisterToolsForUser(user.ID, repo.RepoPath)
	if err != nil {
		log.Fatalf("Failed to register tools: %v", err)
	}

	log.Println("MCP server ready (stdio mode) - 9 tools registered")

	// Serve via stdio
	mcpGoServer := mcpServer.GetMCPServer()
	if err := mcpserver.ServeStdio(mcpGoServer); err != nil {
		log.Fatalf("MCP server error: %v", err)
	}
}

// runHTTPMode runs the server in HTTP mode for web interface
func runHTTPMode(cfg *config.Config, db *gorm.DB, encryptionKey []byte) {
	// Initialize local auth
	tokenManager := auth.NewTokenManager(db, cfg.Security.TokenTTL)
	localAuth := auth.NewLocalAuthenticator(tokenManager)
	username, _ := localAuth.GetLocalUsername()
	log.Printf("Local authentication initialized (system user: %s)", username)

	// Create MCP server
	mcpServer, err := server.NewMCPServer(cfg, db, encryptionKey)
	if err != nil {
		log.Fatalf("Failed to create MCP server: %v", err)
	}

	log.Println("MCP server initialized")

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
