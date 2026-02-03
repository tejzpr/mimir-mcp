#!/bin/bash
# Mimir MCP Server Wrapper with Environment Variables
# This ensures the database path is always correctly set

# Set database path explicitly using user's home directory
export DB_PATH="$HOME/.mimir/db/mimir.db"

# Optional: Set encryption key if needed
# export ENCRYPTION_KEY="your-key-here"

# Run mimir with the environment variables set
exec "$(dirname "$0")/bin/mimir" "$@"
