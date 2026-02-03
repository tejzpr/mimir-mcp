# Deployment Guide

## Prerequisites

- Go 1.23 or higher
- Git installed
- PostgreSQL (for production) or SQLite (development)
- SAML Identity Provider (DUO or Okta)
- GitHub account (for remote sync)

## Installation Steps

### 1. Clone Repository

```bash
git clone <repository-url>
cd mimir-mcp

# Initialize submodules
git submodule update --init --recursive
```

### 2. Install Dependencies

```bash
make deps
```

### 3. Run Setup Script

```bash
make setup
```

This creates:
- `~/.mimir/configs/` - Configuration directory
- `~/.mimir/db/` - Database directory
- `~/.mimir/store/` - Git repositories directory
- Sample `config.json`

### 4. Configure SAML

Edit `~/.mimir/configs/config.json`:

```json
{
  "saml": {
    "entity_id": "https://your-domain.com",
    "acs_url": "https://your-domain.com/saml/acs",
    "metadata_url": "https://your-domain.com/saml/metadata",
    "idp_metadata": "<IdP metadata XML or URL>",
    "certificate": "-----BEGIN CERTIFICATE-----\n...",
    "private_key": "-----BEGIN RSA PRIVATE KEY-----\n...",
    "provider": "okta"
  }
}
```

#### Generate SP Certificate

```bash
openssl req -x509 -newkey rsa:2048 \
  -keyout sp-key.pem \
  -out sp-cert.pem \
  -days 365 \
  -nodes \
  -subj "/CN=your-domain.com"
```

Copy contents of `sp-cert.pem` to `certificate` field and `sp-key.pem` to `private_key` field.

### 5. Configure Database

**For SQLite** (Development):
```json
{
  "database": {
    "type": "sqlite",
    "sqlite_path": "/Users/username/.mimir/db/mimir.db"
  }
}
```

**For PostgreSQL** (Production):

```bash
# Create database
createdb mimir_prod

# Create user
createuser -P mimir
```

```json
{
  "database": {
    "type": "postgres",
    "postgres_dsn": "postgresql://mimir:password@localhost:5432/mimir_prod"
  }
}
```

### 6. Build

```bash
make build
```

Binary will be created at `bin/mimir`.

### 7. Run

```bash
# Development
make run

# Production (with binary)
./bin/mimir
```

## Production Deployment

### Systemd Service

Create `/etc/systemd/system/mimir.service`:

```ini
[Unit]
Description=Mimir MCP Server
After=network.target postgresql.service

[Service]
Type=simple
User=mimir
Group=mimir
WorkingDirectory=/opt/mimir
ExecStart=/opt/mimir/bin/mimir
Restart=always
RestartSec=10

# Security
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/home/mimir/.mimir

[Install]
WantedBy=multi-user.target
```

Enable and start:

```bash
sudo systemctl enable mimir
sudo systemctl start mimir
sudo systemctl status mimir
```

### Nginx Reverse Proxy

Create `/etc/nginx/sites-available/mimir`:

```nginx
server {
    listen 80;
    server_name your-domain.com;
    
    # Redirect to HTTPS
    return 301 https://$server_name$request_uri;
}

server {
    listen 443 ssl http2;
    server_name your-domain.com;

    ssl_certificate /etc/ssl/certs/your-domain.com.pem;
    ssl_certificate_key /etc/ssl/private/your-domain.com.key;
    
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers HIGH:!aNULL:!MD5;

    location / {
        proxy_pass http://localhost:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

Enable:

```bash
sudo ln -s /etc/nginx/sites-available/mimir /etc/nginx/sites-enabled/
sudo nginx -t
sudo systemctl reload nginx
```

### Docker Deployment

Create `Dockerfile`:

```dockerfile
FROM golang:1.23-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o bin/mimir cmd/server/main.go

FROM alpine:latest

RUN apk --no-cache add ca-certificates git

WORKDIR /root/

COPY --from=builder /app/bin/mimir .
COPY --from=builder /app/web ./web

EXPOSE 8080

CMD ["./mimir"]
```

Create `docker-compose.yml`:

```yaml
version: '3.8'

services:
  postgres:
    image: postgres:15-alpine
    environment:
      POSTGRES_DB: mimir
      POSTGRES_USER: mimir
      POSTGRES_PASSWORD: ${DB_PASSWORD}
    volumes:
      - postgres_data:/var/lib/postgresql/data
    ports:
      - "5432:5432"

  mimir:
    build: .
    ports:
      - "8080:8080"
    volumes:
      - ~/.mimir:/root/.mimir
    depends_on:
      - postgres
    environment:
      - HOME=/root

volumes:
  postgres_data:
```

Run:

```bash
docker-compose up -d
```

## Monitoring

### Health Check Endpoint

Add to your server:

```bash
curl http://localhost:8080/health
# Response: {"status": "ok", "version": "1.0.0"}
```

### Logging

Logs are written to stdout. Capture with systemd or Docker:

```bash
# View systemd logs
sudo journalctl -u mimir -f

# View Docker logs
docker logs -f mimir
```

### Metrics

Recommended monitoring:

- Server uptime
- Request rate
- Error rate
- Database connections
- Disk usage (git repos + database)
- Memory usage

## Backup & Recovery

### Database Backup

**SQLite**:
```bash
# Backup
cp ~/.mimir/db/mimir.db ~/.mimir/db/backup-$(date +%Y%m%d).db

# Restore
cp ~/.mimir/db/backup-20240115.db ~/.mimir/db/mimir.db
```

**PostgreSQL**:
```bash
# Backup
pg_dump mimir_prod > backup-$(date +%Y%m%d).sql

# Restore
psql mimir_prod < backup-20240115.sql
```

### Git Repository Backup

Git repositories are automatically backed up to GitHub if PAT is configured. For additional safety:

```bash
# Backup all repos
tar -czf mimir-repos-$(date +%Y%m%d).tar.gz ~/.mimir/store/

# Restore
tar -xzf mimir-repos-20240115.tar.gz -C ~/
```

### Configuration Backup

```bash
cp ~/.mimir/configs/config.json ~/.mimir/configs/config.json.backup
```

## Upgrades

### Minor Version Upgrades

```bash
# Stop server
sudo systemctl stop mimir

# Backup
./scripts/backup.sh

# Pull latest code
git pull origin main

# Rebuild
make build

# Restart
sudo systemctl start mimir
```

### Major Version Upgrades

1. **Review changelog** for breaking changes
2. **Backup** database and repositories
3. **Run migration scripts** (if provided)
4. **Test** in staging environment first
5. **Deploy** to production

## Troubleshooting

### Server Won't Start

**Check logs**:
```bash
sudo journalctl -u mimir -n 50
```

**Common issues**:
- Port already in use: Change port in config
- Database connection failed: Check DSN
- Missing config: Run `make setup`
- Permission denied: Check file permissions

### SAML Authentication Fails

**Debug steps**:
1. Check IdP metadata is correct
2. Verify certificate and private key
3. Ensure ACS URL matches IdP configuration
4. Check IdP logs for error details
5. Test with SAMLtool.com

### Git Sync Fails

**Common issues**:
- Invalid PAT token: Regenerate on GitHub
- PAT expired: Update token in database
- No write access: Check repository permissions
- Merge conflicts: Use `force: true` or resolve manually

### Database Issues

**SQLite locked**:
```bash
# Check for other processes
lsof ~/.mimir/db/mimir.db

# Kill if necessary
```

**PostgreSQL connection pool exhausted**:
```sql
-- Check active connections
SELECT count(*) FROM pg_stat_activity;

-- Increase max_connections in postgresql.conf
```

## Security Hardening

### 1. Network

- Run behind HTTPS/TLS
- Use firewall to restrict access
- Consider VPN for admin access

### 2. Authentication

- Enforce strong SAML policies
- Rotate encryption keys annually
- Use short token TTLs (6-12 hours)
- Enable MFA at IdP level

### 3. File System

```bash
# Restrict permissions
chmod 700 ~/.mimir
chmod 600 ~/.mimir/configs/config.json
chmod 700 ~/.mimir/store/*
```

### 4. Database

- Use strong passwords
- Enable SSL for PostgreSQL
- Restrict network access
- Regular backups

### 5. Git

- Use PAT tokens (not passwords)
- Set PAT expiration
- Minimum required scopes (repo read/write)
- Rotate tokens periodically

## Performance Tuning

### Database Optimization

**PostgreSQL**:
```sql
-- Increase shared buffers
ALTER SYSTEM SET shared_buffers = '256MB';

-- Enable parallel queries
ALTER SYSTEM SET max_parallel_workers_per_gather = 2;

-- Reload
SELECT pg_reload_conf();
```

### Git Repository Optimization

```bash
# Garbage collection
cd ~/.mimir/store/mimir-{UUID}
git gc --aggressive

# Prune old refs
git remote prune origin
```

### Application Tuning

- Increase sync interval for less frequent syncs
- Implement caching layer (future enhancement)
- Use connection pooling for database
- Limit max memory file size

## Scaling

### Horizontal Scaling

For multiple servers:

1. **Shared PostgreSQL** database
2. **Shared file system** or per-user routing
3. **Load balancer** (HAProxy, nginx)
4. **Session affinity** for same-user routing

### Vertical Scaling

- Increase server resources (CPU, RAM)
- Optimize database (indexes, query plans)
- Use SSD for git repositories
- Consider read replicas for database

## Support

For issues:
- Check logs first
- Review this documentation
- Check GitHub issues
- Contact support team

---

**Important**: Always test deployments in a staging environment before production.
