# Validation Plan — Post-2026-04-05 Session

**Context:** Massive feature session shipped 18+ items. Integration testing on macOS revealed cross-platform Docker Desktop issues. This plan covers what needs to be validated before anything else is built.

## Blocker: Egress CA Cert (must fix first)

mitmproxy generates its CA cert on first run at `/app/certs/`. The enforcer needs this cert to trust the egress proxy for HTTPS interception (Anthropic API calls). On a clean install, the cert doesn't exist when the enforcer first tries to call the LLM → TLS handshake fails → 502.

**Fix:** Pre-generate the mitmproxy CA cert during `agency setup` / `agency quickstart`, before any containers start. Write it to `~/.agency/infrastructure/egress/certs/`. The enforcer start sequence already mounts this path if the cert exists (`enforcer.go` line 130-156).

**Test:** `rm -rf ~/.agency && agency quickstart` → henry responds to first message.

## Priority 1: Clean Install Flow (macOS + Linux)

### Test A: Full quickstart from scratch (macOS)

```bash
agency serve stop
docker stop $(docker ps -q --filter "label=agency.managed")
docker rm $(docker ps -aq --filter "label=agency.managed")
rm -rf ~/.agency
make all
agency quickstart
```

**Expected:**
- [ ] All 12 infra services start (including gateway-proxy)
- [ ] Hub update succeeds (git source)
- [ ] Provider installed (anthropic)
- [ ] Agent created and started (7 phases)
- [ ] `agency send henry "hello"` → henry responds
- [ ] Demo task streams response in terminal
- [ ] Web UI loads at localhost:8280

### Test B: Full quickstart from scratch (Linux dev machine)

Same as Test A on the Linux dev environment.

**Expected:** Same results. Linux should not have the Docker Desktop socket issues.

### Test C: Quickstart idempotency

```bash
agency quickstart
# (should skip everything, show checkmarks)
agency quickstart
```

**Expected:**
- [ ] Phases 1-3 skip with "already configured/running"
- [ ] Phase 4 shows "henry already running"
- [ ] No errors, no restarts, no data loss

### Test D: Quickstart after upgrade

```bash
# Simulate an upgrade: change buildID
git commit --allow-empty -m "test"
make install
agency quickstart
```

**Expected:**
- [ ] Detects stale build on henry
- [ ] Restarts henry (not delete/recreate)
- [ ] Henry's identity and memory preserved
- [ ] henry responds to messages

## Priority 2: Core Agent Functionality

### Test E: Agent communication

```bash
agency send henry "What tools do you have available?"
```

**Expected:**
- [ ] Message delivered via DM channel
- [ ] henry calls LLM (no 401, no 502)
- [ ] henry responds with tool list
- [ ] Response visible in web UI

### Test F: Tool execution

```bash
agency send henry "What time is it?"
```

**Expected:**
- [ ] henry uses a tool to answer
- [ ] Tool result appears in enforcer audit log
- [ ] No LLM_CAPABILITY_MISMATCH errors

### Test G: Economics observability

```bash
curl http://localhost:8200/api/v1/agents/henry/economics -H "X-Agency-Token: $TOKEN"
```

**Expected:**
- [ ] Returns JSON with requests, tokens, cost
- [ ] TTFT/TPOT fields present (may be 0 if no streaming calls yet)
- [ ] No errors

## Priority 3: New Features (spot checks)

### Test H: Hub provider install

```bash
agency hub search gemini
agency hub install gemini
```

**Expected:**
- [ ] Gemini provider found in search
- [ ] Install succeeds
- [ ] routing.yaml updated with gemini models and capabilities

### Test I: Provider add (discovery)

```bash
agency hub provider add test-ollama http://localhost:11434/v1 --no-probe
```

**Expected:**
- [ ] Skeleton written to routing.local.yaml
- [ ] No crash

### Test J: Cache clear

```bash
agency cache clear --agent henry
```

**Expected:**
- [ ] Returns success
- [ ] No crash

### Test K: Quickstart with Google provider

```bash
rm -rf ~/.agency
agency quickstart --provider google --key <gemini-key> --no-demo
```

**Expected:**
- [ ] Google provider validated
- [ ] Gemini installed from hub
- [ ] Agent starts with Gemini routing

## Priority 4: Relay

### Test L: Relay on tinyfleck.io

Visit `https://app.tinyfleck.io` in incognito.

**Expected:**
- [ ] Login page shows (not the app)
- [ ] GitHub sign-in works
- [ ] Google sign-in works (after REL-8 fix verified)
- [ ] Waitlist link visible
- [ ] After auth: app loads (if tunnel connected)

### Test M: Relay container

```bash
agency status
```

**Expected:**
- [ ] Relay container listed (may show "not configured" if no relay.yaml)
- [ ] No crash from relay container

## Priority 5: Web UI

### Test N: Web UI basics

Open `http://localhost:8280`

**Expected:**
- [ ] App loads
- [ ] Channels sidebar shows
- [ ] Agent status visible
- [ ] Can send message to henry via UI
- [ ] henry's response appears in chat

## Issues Found During 2026-04-05 Validation

| Issue | Status | PR |
|---|---|---|
| Gateway-proxy Unix socket fails on macOS | **Fixed** — TCP via host-gateway | #32 |
| Egress credential resolver Unix socket fails | **Fixed** — TCP fallback | #32 |
| Infrastructure dirs not created before containers | **Fixed** — RunInit creates them | #32 |
| Hub default source OCI but registry empty | **Fixed** — reverted to git | #31 |
| Quickstart didn't install hub provider | **Fixed** | #28 |
| Quickstart stale daemon token mismatch | **Fixed** | #30, #32 |
| Enforcer rejects all calls when capabilities empty | **Fixed** — backward compat | #27 |
| Quickstart demo WebSocket panic | **Fixed** — recover() | #25, #26 |
| Tests open browser (example.com) | **Fixed** — stub openBrowser | agency-relay |
| Egress CA cert not generated on clean install | **OPEN** — next session | — |
| DM channel not auto-created on fresh agent | **Needs investigation** | — |
