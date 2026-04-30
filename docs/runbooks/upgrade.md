# Upgrade

## Trigger

New Agency version available. Applies to one-shot, Homebrew, and source installs.

## Prerequisites

- Current installation working (`agency admin doctor` passes)
- No agents actively processing critical tasks (check `agency list`)

## Steps

### 1. Stop all agents

```bash
agency list --format text
# For each running agent:
agency stop <agent-name>
```

### 2. Record current state

```bash
agency --version > /tmp/agency-pre-upgrade.txt
agency infra status >> /tmp/agency-pre-upgrade.txt
agency status >> /tmp/agency-pre-upgrade.txt
```

### 3. Upgrade the binary

**Homebrew:**
```bash
brew tap geoffbelknap/tap
brew upgrade agency
```

**One-shot installer:**
```bash
curl -fsSL https://geoffbelknap.github.io/agency/install.sh | bash
```

This upgrades the `agency` binary and installed runtime assets directly. It
does not install Agency through Homebrew.

**Source build fallback:**
```bash
cd /path/to/agency
git pull
make install
```

### 4. Verify new version

```bash
agency --version
```

### 5. Rebuild OCI image artifacts

```bash
make images    # source build
# or
agency infra rebuild   # triggers image refresh
```

### 6. Restart infrastructure

```bash
agency infra down
agency infra up
agency infra status
```

Wait for all components to show healthy.

### 7. Run doctor

```bash
agency admin doctor
```

### 8. Check version mismatches and capacity

```bash
agency status
agency infra capacity
```

The binary version and OCI artifact build IDs should match. Stale artifacts auto-rebuild on next `agency start`, but `make images` or `agency infra rebuild` handles it proactively. Capacity config (`~/.agency/capacity.yaml`) survives upgrades — re-run `agency setup` only if host resources changed.

### 9. Restart agents

```bash
# For each agent that was running:
agency start <agent-name>
agency show <agent-name>
```

### 10. Validate

```bash
agency send <agent-name> "Confirm you're working after the upgrade."
```

## Verification

- [ ] `agency --version` shows new version
- [ ] `agency status` shows no version mismatches
- [ ] `agency infra status` shows all components healthy
- [ ] `agency admin doctor` passes
- [ ] At least one agent starts and responds

## Rollback

**Homebrew:**
```bash
brew reinstall agency@<previous-version>
```

**Source build:**
```bash
git checkout <previous-tag>
make install
agency infra down
agency infra up
```

Then restart agents.
