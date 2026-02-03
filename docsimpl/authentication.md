# Authentication Guide

Mimir MCP supports two authentication modes: **Local** (development) and **SAML** (production).

## Authentication Modes

### Local Authentication

**Purpose**: Development and testing  
**How it works**: Uses system username from `whoami` command  
**Best for**: Local development, single-user setups

**Configuration**:
```json
{
  "auth": {
    "type": "local"
  }
}
```

**Usage**:
```bash
# Default mode - just run
./bin/mimir

# Or explicitly
./bin/mimir --auth-type=local

# Or via environment
AUTH_TYPE=local ./bin/mimir
```

**Flow**:
1. User accesses `/auth`
2. Clicks "Continue with Local Auth"
3. Server runs `whoami` to get username
4. User automatically logged in
5. Token generated and stored
6. Repository created for user

**Pros**:
- Zero configuration
- Instant setup
- No external dependencies
- Perfect for development

**Cons**:
- Single-user only (system username)
- No enterprise integration
- Not suitable for production

---

### SAML Authentication

**Purpose**: Production and enterprise deployments  
**How it works**: Integrates with corporate IdP (DUO or Okta)  
**Best for**: Multi-user production systems

**Configuration**:
```json
{
  "auth": {
    "type": "saml"
  },
  "saml": {
    "entity_id": "https://your-domain.com",
    "acs_url": "https://your-domain.com/saml/acs",
    "metadata_url": "https://your-domain.com/saml/metadata",
    "idp_metadata": "https://idp.example.com/metadata",
    "certificate": "-----BEGIN CERTIFICATE-----\n...",
    "private_key": "-----BEGIN RSA PRIVATE KEY-----\n...",
    "provider": "okta"
  }
}
```

**Usage**:
```bash
# With config file
./bin/mimir --auth-type=saml

# Or via environment
AUTH_TYPE=saml ./bin/mimir
```

**Flow**:
1. User accesses `/auth`
2. Clicks "Sign in with SAML"
3. Redirected to IdP (DUO/Okta)
4. User authenticates at IdP
5. IdP sends SAML assertion
6. Server extracts username and email
7. Token generated and stored
8. Repository created for user

**Pros**:
- Enterprise SSO integration
- Multi-user support
- Centralized authentication
- Audit trails via IdP

**Cons**:
- Requires IdP setup
- More complex configuration
- External dependency

---

## Configuration Precedence

Settings are applied in this order (highest priority first):

### 1. Command-Line Arguments (Highest)
```bash
./bin/mimir --auth-type=local --db-type=sqlite
```

### 2. Environment Variables
```bash
export AUTH_TYPE=local
export DB_TYPE=sqlite
./bin/mimir
```

### 3. Configuration File
```json
{
  "auth": {"type": "local"},
  "database": {"type": "sqlite"}
}
```

### 4. Built-in Defaults (Lowest)
- Auth: `local`
- Database: `sqlite`
- Port: `8080`

---

## Quick Start Examples

### Zero-Config Start (Local + SQLite)
```bash
# Just run - uses whoami and SQLite
./bin/mimir

# Access at http://localhost:8080/auth
```

### Development with Custom Database
```bash
./bin/mimir --db-path=/tmp/dev.db
```

### Development with Environment Variables
```bash
# Create .env file
cat > .env << EOF
AUTH_TYPE=local
DB_TYPE=sqlite
DB_PATH=~/.mimir/db/dev.db
PORT=9000
EOF

# Run with environment
export $(cat .env | xargs)
./bin/mimir
```

### Production with SAML
```bash
# Using config file
./bin/mimir --config=config.production.json

# Or override auth type
./bin/mimir --config=config.production.json --auth-type=saml
```

### Mixed Configuration
```bash
# Config file has SAML, but override for local testing
./bin/mimir --config=prod.json --auth-type=local
```

---

## Environment Variables Reference

| Variable | Description | Example |
|----------|-------------|---------|
| `AUTH_TYPE` | Authentication mode | `local` or `saml` |
| `DB_TYPE` | Database type | `sqlite` or `postgres` |
| `DB_PATH` | SQLite database path | `~/.mimir/db/dev.db` |
| `DB_DSN` | PostgreSQL connection | `postgresql://user:pass@host/db` |
| `PORT` | Server port | `8080` |
| `ENCRYPTION_KEY` | PAT encryption key | Base64-encoded 32-byte key |

**Prefixed variants** (optional):
- `MIMIR_AUTH_TYPE`
- `MIMIR_DB_TYPE`
- etc.

---

## CLI Flags Reference

```bash
./bin/mimir [options]

Options:
  --auth-type string    Authentication type (saml or local)
  --db-type string      Database type (sqlite or postgres)
  --db-path string      Database path (for sqlite)
  --db-dsn string       Database DSN (for postgres)
  --port int            Server port
  --config string       Path to config file

Examples:
  ./bin/mimir
  ./bin/mimir --auth-type=local
  ./bin/mimir --auth-type=saml --config=prod.json
  ./bin/mimir --db-type=sqlite --db-path=/tmp/test.db
```

---

## Switching Authentication Modes

### Development → Production

**Before** (Development):
```bash
# Local auth, SQLite
./bin/mimir
```

**After** (Production):
```json
{
  "auth": {"type": "saml"},
  "saml": { /* full SAML config */ }
}
```

```bash
./bin/mimir --config=prod.json
```

### Testing SAML in Development

```bash
# Override production config for local testing
./bin/mimir --config=prod.json --auth-type=local
```

---

## Docker Deployment

### Local Mode (Development)
```yaml
version: '3.8'
services:
  mimir:
    image: mimir:latest
    environment:
      - AUTH_TYPE=local
      - DB_TYPE=sqlite
    volumes:
      - ~/.mimir:/root/.mimir
    ports:
      - "8080:8080"
```

### SAML Mode (Production)
```yaml
version: '3.8'
services:
  mimir:
    image: mimir:latest
    environment:
      - AUTH_TYPE=saml
      - DB_TYPE=postgres
      - DB_DSN=postgresql://user:${DB_PASSWORD}@postgres:5432/mimir
    volumes:
      - ./config.json:/root/.mimir/configs/config.json:ro
      - mimir_store:/root/.mimir/store
    ports:
      - "443:8080"
```

---

## Kubernetes Deployment

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: mimir-config
data:
  AUTH_TYPE: "saml"
  DB_TYPE: "postgres"

---
apiVersion: v1
kind: Secret
metadata:
  name: mimir-secrets
type: Opaque
stringData:
  DB_DSN: "postgresql://user:password@postgres:5432/mimir"
  ENCRYPTION_KEY: "base64-encoded-key"

---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: mimir
spec:
  replicas: 2
  template:
    spec:
      containers:
      - name: mimir
        image: mimir:latest
        envFrom:
        - configMapRef:
            name: mimir-config
        - secretRef:
            name: mimir-secrets
        ports:
        - containerPort: 8080
```

---

## Security Considerations

### Local Mode
- **Security Level**: LOW - only for development
- **Use case**: Single developer, local machine
- **Network**: Bind to localhost only
- **Data**: Use non-sensitive test data

⚠️ **Never use local mode in production or on networked servers**

### SAML Mode
- **Security Level**: HIGH - production-ready
- **Use case**: Multi-user, enterprise deployments
- **Network**: Can bind to public interfaces with TLS
- **Data**: Production data

✅ **Always use SAML for production deployments**

---

## Troubleshooting

### Local Auth Issues

**Problem**: `whoami` command not found
```
Solution: Ensure you're on Unix-like system (Linux, macOS)
Windows users should use WSL or SAML mode
```

**Problem**: Username contains special characters
```
Solution: System automatically sanitizes username
Or use SAML with proper user accounts
```

### SAML Auth Issues

**Problem**: "SAML configuration required"
```
Solution: Ensure auth.type=saml and all SAML fields configured
Check: entity_id, acs_url, idp_metadata, certificate, private_key
```

**Problem**: "Failed to parse SAML response"
```
Solution: Verify IdP metadata is correct
Test SAML response at SAMLtool.com
Check certificate matches private key
```

### Configuration Override Issues

**Problem**: Environment variable not working
```
Solution: Check variable name (AUTH_TYPE not auth_type)
Try prefixed version: MIMIR_AUTH_TYPE
Verify: echo $AUTH_TYPE
```

**Problem**: CLI flag not working
```
Solution: Ensure flag format: --auth-type=local (not --auth-type local)
Check: ./bin/mimir --help
```

---

## Migration Guide

### From Local to SAML

1. **Export existing data** (if any):
   ```bash
   cp -r ~/.mimir/store ~/.mimir/store.backup
   cp ~/.mimir/db/mimir.db ~/.mimir/db/backup.db
   ```

2. **Update configuration**:
   ```json
   {
     "auth": {"type": "saml"},
     "saml": { /* configure SAML */ }
   }
   ```

3. **Test SAML authentication** first

4. **Map local users to SAML users** (manual process):
   - Export user data from database
   - Re-authenticate with SAML
   - Associate repositories manually if needed

### From SAML to Local (Testing)

```bash
# Temporarily override for testing
./bin/mimir --config=prod.json --auth-type=local

# Or permanently in config
{
  "auth": {"type": "local"}
}
```

---

## Best Practices

### Development
- Use `auth.type=local` for speed
- Keep config.json in version control (without secrets)
- Override via CLI/ENV for flexibility
- Use SQLite for simplicity

### Staging
- Test with SAML before production
- Use same auth mode as production
- PostgreSQL database
- TLS enabled

### Production
- Always use `auth.type=saml`
- PostgreSQL database
- TLS required
- Secrets in environment variables
- Strong token TTL policies

---

## Advanced Configuration

### Multi-Environment Setup

```bash
# Directory structure
configs/
├── base.json           # Common settings
├── development.json    # Dev overrides
├── staging.json        # Staging overrides
└── production.json     # Prod settings

# Run with specific environment
./bin/mimir --config=configs/development.json
```

### Secret Management

```bash
# Use external secret manager
export ENCRYPTION_KEY=$(vault kv get -field=key secret/mimir)
export DB_DSN=$(vault kv get -field=dsn secret/mimir)
./bin/mimir
```

### Dynamic Configuration

```bash
# Build configuration at runtime
cat > /tmp/runtime-config.json << EOF
{
  "auth": {"type": "${AUTH_MODE}"},
  "database": {"type": "${DB_TYPE}"}
}
EOF

./bin/mimir --config=/tmp/runtime-config.json
```

---

## FAQ

**Q: Can I use both auth modes simultaneously?**  
A: No, only one auth mode is active at a time. Switch via configuration.

**Q: Does local mode support multiple users?**  
A: No, local mode is single-user (system username). Use SAML for multi-user.

**Q: Can I switch auth modes without losing data?**  
A: Yes, but user accounts may need to be recreated. Repository data is preserved.

**Q: What happens if I don't specify auth type?**  
A: Defaults to `local` for ease of first-time setup.

**Q: Can I use environment variables with config file?**  
A: Yes, ENV vars override config file values.

**Q: How do I disable authentication entirely?**  
A: Not supported - all access requires authentication for security.

---

For more details, see:
- [Configuration Guide](configuration.md)
- [Deployment Guide](deployment.md)
- [API Reference](api-reference.md)
