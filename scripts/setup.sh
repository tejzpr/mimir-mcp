#!/bin/bash

set -e

echo "🧠 Mimir MCP - Setup Script"
echo "===================================="
echo ""

# Create directory structure
echo "📁 Creating directory structure..."
mkdir -p ~/.mimir/{configs,db,store}
echo "✓ Directories created at ~/.mimir/"

# Generate sample config if it doesn't exist
CONFIG_FILE="$HOME/.mimir/configs/config.json"
if [ ! -f "$CONFIG_FILE" ]; then
    echo "📝 Generating sample configuration..."
    cat > "$CONFIG_FILE" <<EOF
{
  "server": {
    "host": "localhost",
    "port": 8080,
    "tls": {
      "enabled": false,
      "cert_file": "",
      "key_file": ""
    }
  },
  "database": {
    "type": "sqlite",
    "sqlite_path": "$HOME/.mimir/db/mimir.db",
    "postgres_dsn": ""
  },
  "saml": {
    "entity_id": "",
    "acs_url": "",
    "metadata_url": "",
    "idp_metadata": "",
    "certificate": "",
    "private_key": "",
    "provider": "okta"
  },
  "git": {
    "default_branch": "main",
    "sync_interval_minutes": 60
  },
  "security": {
    "encryption_key": "",
    "token_ttl_hours": 24
  }
}
EOF
    echo "✓ Sample config created at $CONFIG_FILE"
    echo ""
    echo "⚠️  IMPORTANT: Edit $CONFIG_FILE and configure:"
    echo "   - SAML settings (entity_id, acs_url, idp_metadata, certificate, private_key)"
    echo "   - Database settings if using Postgres"
    echo "   - Generate an encryption key by running the server once (it will generate one for you)"
else
    echo "✓ Configuration already exists at $CONFIG_FILE"
fi

echo ""
echo "✅ Setup complete!"
echo ""
echo "Next steps:"
echo "1. Edit $CONFIG_FILE with your SAML configuration"
echo "2. Run 'make build' to build the server"
echo "3. Run 'make run' or './bin/mimir' to start the server"
echo "4. Access http://localhost:8080/auth to authenticate"
echo ""
