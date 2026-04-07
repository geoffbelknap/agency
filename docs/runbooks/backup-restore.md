# Backup & Restore

## Trigger

Scheduled backup, migration to new machine, disaster recovery, or state preservation before destructive operation.

## What to Back Up

| Path | Contents | Priority |
|------|----------|----------|
| `~/.agency/config.yaml` | Auth tokens, HMAC key, gateway config | Critical |
| `~/.agency/credentials/` | Encrypted credential store + key | Critical |
| `~/.agency/knowledge/` | Knowledge graph database + ontology | High |
| `~/.agency/agents/` | Agent configs, constraints, identity, workspace data | High |
| `~/.agency/audit/` | HMAC-signed audit logs | High |
| `~/.agency/notifications.yaml` | Notification destinations | Medium |
| `~/.agency/hub-cache/` | Hub registry cache | Low (re-downloadable) |
| `~/.agency/infrastructure/` | Routing config, service definitions | Low (regenerable) |

## Backup Procedure

### Full backup

```bash
BACKUP_DIR="/path/to/backups/agency-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$BACKUP_DIR"

# Stop agents to ensure consistent state
agency list | grep running | awk '{print $1}' | while read agent; do
    agency stop "$agent"
done

# Copy everything
cp -r ~/.agency/ "$BACKUP_DIR/"

# Remove runtime artifacts (not needed for restore)
rm -rf "$BACKUP_DIR/run/"
rm -rf "$BACKUP_DIR/gateway.pid"
rm -rf "$BACKUP_DIR/.cache/"

echo "Backup saved to $BACKUP_DIR"
ls -lh "$BACKUP_DIR"
```

### Minimal backup (credentials + config only)

```bash
BACKUP_DIR="/path/to/backups/agency-minimal-$(date +%Y%m%d)"
mkdir -p "$BACKUP_DIR"

cp ~/.agency/config.yaml "$BACKUP_DIR/"
cp -r ~/.agency/credentials/ "$BACKUP_DIR/"
cp ~/.agency/notifications.yaml "$BACKUP_DIR/" 2>/dev/null || true
```

### Knowledge graph backup

```bash
agency knowledge export > /path/to/backups/knowledge-$(date +%Y%m%d).json
```

## Restore Procedure

### Full restore

```bash
# Stop daemon
kill "$(cat ~/.agency/gateway.pid)" 2>/dev/null || true

# Restore from backup
cp -r /path/to/backups/agency-YYYYMMDD-HHMMSS/ ~/.agency/

# Remove stale runtime files
rm -f ~/.agency/gateway.pid
rm -rf ~/.agency/run/

# Restart
agency setup   # reinitializes daemon, checks infrastructure
agency infra status
agency admin doctor
```

### Restore to new machine

```bash
# Install Agency on new machine
brew install geoffbelknap/tap/agency
# or: download from GitHub releases

# Copy backup
scp -r user@old-machine:/path/to/backup/ ~/.agency/

# Remove machine-specific artifacts
rm -f ~/.agency/gateway.pid
rm -rf ~/.agency/run/

# Setup (uses existing config, doesn't overwrite tokens)
agency setup
agency infra up
agency admin doctor
```

### Restore knowledge graph only

```bash
agency knowledge restore < /path/to/backups/knowledge-YYYYMMDD.json
agency knowledge stats
```

### Restore credentials only

```bash
cp /path/to/backups/credentials/store.enc ~/.agency/credentials/
cp /path/to/backups/credentials/.key ~/.agency/credentials/

# Verify
agency creds list
agency creds test <credential-name>
```

## What admin destroy Preserves

`agency admin destroy` removes agents and infrastructure but preserves:
- Knowledge graph (`~/.agency/knowledge/`)
- Credential store (`~/.agency/credentials/`)
- Config (`~/.agency/config.yaml`)
- Audit logs (`~/.agency/audit/`)

This is intentional — organizational knowledge and credentials survive resets.

## Verification

- [ ] `agency infra status` shows all components healthy
- [ ] `agency admin doctor` passes
- [ ] `agency creds list` shows expected credentials
- [ ] `agency creds test <key>` passes for critical credentials
- [ ] `agency knowledge stats` shows expected node/edge counts
- [ ] Agents can be created and started
