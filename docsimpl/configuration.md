# Configuration Guide

## Configuration File Location

`~/.mimir/configs/config.json`

## Command-Line Flags

### Server Mode

| Flag | Description |
|------|-------------|
| (default) | Start MCP server in stdio mode for Cursor |
| `--http` | Start HTTP server for web interface |
| `--port=PORT` | Server port (HTTP mode only) |

### Database Rebuild

| Flag | Description |
|------|-------------|
| `--rebuilddb` | Rebuild database index from git repository |
| `--force` | Force rebuild (requires `--rebuilddb`) |

### Database Options

| Flag | Description |
|------|-------------|
| `--db-type=TYPE` | Database type (`sqlite` or `postgres`) |
| `--db-path=PATH` | SQLite database path |
| `--db-dsn=DSN` | PostgreSQL connection string |
| `--config=PATH` | Path to config file |

### Environment Variables

| Variable | Description |
|----------|-------------|
| `DB_TYPE` | Database type |
| `DB_PATH` | SQLite database path |
| `DB_DSN` | PostgreSQL connection string |
| `PORT` | Server port (HTTP mode) |
| `ENCRYPTION_KEY` | Encryption key for PAT tokens |

### Examples

```bash
# Start stdio server (for Cursor MCP)
mimir

# Start HTTP server
mimir --http

# HTTP server on custom port
mimir --http --port=9000

# Rebuild database index from git
mimir --rebuilddb

# Force rebuild (overwrite existing data)
mimir --rebuilddb --force

# Use specific database
mimir --db-path=/custom/path/mimir.db
```

## Complete Configuration Example

```json
{
  "server": {
    "host": "localhost",
    "port": 8080,
    "tls": {
      "enabled": false,
      "cert_file": "/path/to/cert.pem",
      "key_file": "/path/to/key.pem"
    }
  },
  "database": {
    "type": "sqlite",
    "sqlite_path": "/Users/username/.mimir/db/mimir.db",
    "postgres_dsn": ""
  },
  "saml": {
    "entity_id": "https://your-domain.com",
    "acs_url": "https://your-domain.com/saml/acs",
    "metadata_url": "https://your-domain.com/saml/metadata",
    "idp_metadata": "https://okta.example.com/app/metadata",
    "certificate": "-----BEGIN CERTIFICATE-----\n...\n-----END CERTIFICATE-----",
    "private_key": "-----BEGIN RSA PRIVATE KEY-----\n...\n-----END RSA PRIVATE KEY-----",
    "provider": "okta"
  },
  "git": {
    "default_branch": "main",
    "sync_interval_minutes": 60
  },
  "security": {
    "encryption_key": "base64-encoded-32-byte-key",
    "token_ttl_hours": 24
  }
}
```

## Server Configuration

### host
- **Type**: string
- **Default**: `"localhost"`
- **Description**: Host address to bind to
- **Examples**: `"localhost"`, `"0.0.0.0"`, `"127.0.0.1"`

### port
- **Type**: integer
- **Default**: `8080`
- **Range**: 1-65535
- **Description**: Port to listen on

### tls.enabled
- **Type**: boolean
- **Default**: `false`
- **Description**: Enable HTTPS/TLS
- **Note**: Required for production

### tls.cert_file
- **Type**: string
- **Required if**: TLS enabled
- **Description**: Path to TLS certificate

### tls.key_file
- **Type**: string
- **Required if**: TLS enabled
- **Description**: Path to TLS private key

## Database Configuration

### type
- **Type**: enum
- **Values**: `"sqlite"` or `"postgres"`
- **Default**: `"sqlite"`
- **Description**: Database type to use

### sqlite_path
- **Type**: string
- **Required if**: type is sqlite
- **Default**: `"~/.mimir/db/mimir.db"`
- **Description**: Path to SQLite database file

### postgres_dsn
- **Type**: string
- **Required if**: type is postgres
- **Format**: `"postgresql://user:password@host:port/database"`
- **Example**: `"postgresql://mimir:secret@localhost:5432/mimir_prod"`

## SAML Configuration

### entity_id
- **Type**: string
- **Required**: Yes (for SAML)
- **Description**: SP entity identifier
- **Example**: `"https://your-domain.com"`

### acs_url
- **Type**: string
- **Required**: Yes (for SAML)
- **Description**: Assertion Consumer Service URL
- **Example**: `"https://your-domain.com/saml/acs"`

### metadata_url
- **Type**: string
- **Required**: Yes (for SAML)
- **Description**: SP metadata endpoint
- **Example**: `"https://your-domain.com/saml/metadata"`

### idp_metadata
- **Type**: string
- **Required**: Yes (for SAML)
- **Description**: IdP metadata XML or URL
- **Formats**:
  - URL: `"https://idp.example.com/metadata"`
  - XML: Full EntityDescriptor XML string

### certificate
- **Type**: string
- **Required**: Yes (for SAML)
- **Format**: PEM-encoded X.509 certificate
- **Note**: Newlines must be preserved (`\n`)

### private_key
- **Type**: string
- **Required**: Yes (for SAML)
- **Format**: PEM-encoded RSA private key
- **Note**: Keep secure, never commit to version control

### provider
- **Type**: enum
- **Values**: `"duo"` or `"okta"`
- **Default**: `"okta"`
- **Description**: SAML provider type

## Git Configuration

### default_branch
- **Type**: string
- **Default**: `"main"`
- **Description**: Default branch name for new repositories

### sync_interval_minutes
- **Type**: integer
- **Default**: `60`
- **Range**: >= 1
- **Description**: Minutes between automatic syncs

## Security Configuration

### encryption_key
- **Type**: string
- **Format**: Base64-encoded 32-byte key
- **Required**: Yes (generated automatically if missing)
- **Description**: Key for encrypting PAT tokens
- **Generation**:
  ```bash
  # Run server once, it will generate and print a key
  # Copy the key to your config.json
  ```

### token_ttl_hours
- **Type**: integer
- **Default**: `24`
- **Range**: >= 1
- **Description**: Hours until access token expires
- **Note**: Refresh token TTL is 2x this value

## SAML Provider Setup

### Okta Configuration

1. **Create SAML App** in Okta Admin Console
2. **Set SSO URL** to your ACS URL
3. **Set Audience URI** to your Entity ID
4. **Download** IdP metadata XML
5. **Generate** SP certificate and key:
   ```bash
   openssl req -x509 -newkey rsa:2048 -keyout key.pem -out cert.pem -days 365 -nodes
   ```

### DUO Configuration

1. **Create Application** in DUO Admin Panel
2. **Select** "SAML Service Provider"
3. **Configure** ACS URL and Entity ID
4. **Download** metadata
5. **Generate** certificate and key (same as Okta)

## Environment-Specific Configurations

### Development

```json
{
  "server": {
    "host": "localhost",
    "port": 8080,
    "tls": {"enabled": false}
  },
  "database": {
    "type": "sqlite",
    "sqlite_path": "~/.mimir/db/dev.db"
  },
  "git": {
    "sync_interval_minutes": 5
  }
}
```

### Production

```json
{
  "server": {
    "host": "0.0.0.0",
    "port": 443,
    "tls": {
      "enabled": true,
      "cert_file": "/etc/ssl/certs/mimir.pem",
      "key_file": "/etc/ssl/private/mimir.key"
    }
  },
  "database": {
    "type": "postgres",
    "postgres_dsn": "postgresql://user:pass@db.internal:5432/mimir"
  },
  "git": {
    "sync_interval_minutes": 60
  },
  "security": {
    "token_ttl_hours": 12
  }
}
```

## Validation

The server validates configuration on startup:

- Database type must be sqlite or postgres
- Port must be valid (1-65535)
- Sync interval must be >= 1
- Token TTL must be >= 1
- If SAML configured, all SAML fields required
- If TLS enabled, cert and key files must exist

Validation errors prevent server startup with clear error messages.

## Configuration Updates

To apply configuration changes:

1. Edit `~/.mimir/configs/config.json`
2. Restart the server
3. Changes take effect immediately

Note: Some changes may require database migration or reinitialization.
