# Runtime Smoke

Use this runbook when validating the backend-neutral runtime contract path for
agent start, restart, status, and transport wiring.

## Preconditions

- Docker Desktop is running and `docker info` succeeds.
- The gateway token is configured in `~/.agency/config.yaml`.
- The gateway is reachable on `127.0.0.1:8200`.

If you are testing a patched local binary, stop the regular daemon first so the
smoke binary owns port `8200`.

## One-Command Smoke

Run the packaged smoke path:

```bash
bash ./scripts/runtime-contract-smoke.sh --agent <agent-name>
```

What it covers:

- `go test ./...`
- gateway binary build
- gateway health probe
- `GET /api/v1/agents/{name}/runtime/manifest`
- `GET /api/v1/agents/{name}/runtime/status`
- `POST /api/v1/agents/{name}/runtime/validate`
- `agency admin doctor`

If the agent has not been started through the runtime supervisor path yet, the
runtime endpoint checks are skipped until a manifest exists.

## Disposable Agent Flow

Use this when you want a clean live repro for start or restart behavior.

```bash
curl -fsS \
  -H "Authorization: Bearer $(awk '/^token:[[:space:]]*/ {print $2; exit}' ~/.agency/config.yaml)" \
  -H 'Content-Type: application/json' \
  -d '{"name":"runtime-smoke","preset":"generalist"}' \
  http://127.0.0.1:8200/api/v1/agents

curl -sS \
  -H "Authorization: Bearer $(awk '/^token:[[:space:]]*/ {print $2; exit}' ~/.agency/config.yaml)" \
  -H 'Accept: application/x-ndjson' \
  -X POST \
  http://127.0.0.1:8200/api/v1/agents/runtime-smoke/start
```

Expected outcome:

- startup reaches phase 7 and emits a `complete` event
- runtime manifest persists under `~/.agency/agents/runtime-smoke/runtime/manifest.yaml`
- runtime status reports `phase=running` and `healthy=true`

Inspect the runtime surfaces directly:

```bash
curl -fsS \
  -H "Authorization: Bearer $(awk '/^token:[[:space:]]*/ {print $2; exit}' ~/.agency/config.yaml)" \
  http://127.0.0.1:8200/api/v1/agents/runtime-smoke/runtime/manifest

curl -fsS \
  -H "Authorization: Bearer $(awk '/^token:[[:space:]]*/ {print $2; exit}' ~/.agency/config.yaml)" \
  http://127.0.0.1:8200/api/v1/agents/runtime-smoke/runtime/status

curl -fsS \
  -H "Authorization: Bearer $(awk '/^token:[[:space:]]*/ {print $2; exit}' ~/.agency/config.yaml)" \
  -X POST \
  http://127.0.0.1:8200/api/v1/agents/runtime-smoke/runtime/validate
```

Restart coverage:

```bash
curl -fsS \
  -H "Authorization: Bearer $(awk '/^token:[[:space:]]*/ {print $2; exit}' ~/.agency/config.yaml)" \
  -X POST \
  http://127.0.0.1:8200/api/v1/agents/runtime-smoke/restart
```

Restart should re-enter the canonical seven-phase flow and rotate the scoped
enforcer credential instead of reusing the previous token.

## Troubleshooting

- If phase 2 fails, inspect the enforcer container health and logs first.
- If runtime status is available but backend inspection is not, the persisted
  runtime manifest remains the source of truth until backend connectivity
  returns.
- `agency admin doctor` environment findings such as dangling images or Docker
  address-pool drift are deployment hygiene issues, not runtime-contract
  correctness failures.
