# Monorepo Consolidation: agency-web into agency

## Summary

Move agency-web into `agency/web/` via git subtree merge, enabling atomic API+UI pull requests. Delete the standalone agency-web GitHub repo after migration.

## Motivation

agency-web is a pure REST client for the agency gateway. Every API change that affects the UI requires coordinating two repos, two PRs, and two review cycles. Moving the web source into the agency monorepo makes API+UI changes atomic — one branch, one PR, one merge.

## Approach

### Repo Surgery

Use `git subtree add --prefix=web <agency-web-remote> main` to bring the full agency-web tree and history into `agency/web/`. This preserves blame, log, and authorship for all existing web code.

After the subtree merge is on main:
1. Delete the `agency-web` GitHub repository
2. Remove the `agency-web` submodule entry from the workspace (if present in `.gitmodules`)
3. Clean up the workspace-level `agency-web/` directory pointer

### Directory Layout (post-merge)

```
agency/
├── cmd/gateway/        # Go CLI + daemon
├── internal/           # Go packages
├── images/             # Container image sources
├── web/                # ← agency-web source (moved here)
│   ├── src/
│   │   ├── app/
│   │   │   ├── components/
│   │   │   ├── screens/
│   │   │   ├── hooks/
│   │   │   ├── lib/
│   │   │   └── ...
│   │   ├── styles/
│   │   └── main.tsx
│   ├── bin/
│   ├── package.json
│   ├── vite.config.ts
│   ├── tsconfig.json
│   ├── Dockerfile
│   ├── nginx.conf
│   ├── CLAUDE.md
│   └── ...
├── docs/
├── presets/
├── Makefile
└── ...
```

### Build System Changes

**Makefile:**
- Change `AGENCY_WEB_DIR` from `$(SOURCE_DIR)/../agency-web` to `$(SOURCE_DIR)/web`
- `make web` and `make images-all` work unchanged — they already reference `$(AGENCY_WEB_DIR)/Dockerfile`
- No new targets needed

**Web build is self-contained:**
- `web/package.json`, `web/vite.config.ts`, `web/Dockerfile` — unchanged
- `npm install && npm run build` runs inside `web/`
- Vite dev server, TLS cert generation, proxy config — unchanged

### CI Changes

Current `ci.yml` runs Go tests and Python tests. After merge, add a **web** job:

```yaml
web:
  runs-on: ubuntu-latest
  if: contains(github.event.pull_request.changed_files, 'web/')  # path filter
  steps:
    - uses: actions/checkout@v4
    - uses: actions/setup-node@v4
      with:
        node-version: '22'
        cache: 'npm'
        cache-dependency-path: 'web/package-lock.json'
    - run: cd web && npm ci && npm run build && npm test
```

Path-filtered: only runs when `web/**` files change. Existing Go and Python jobs unchanged.

`release.yaml` (GoReleaser) is unaffected — it only builds the Go binary. `release-images.yml` should be extended to include the web image if it isn't already.

### Dev Workflow (post-merge)

```bash
git clone agency              # one clone, everything included
make all                      # Go binary + all images including web
make web                      # rebuild just the web image
cd web && npm run dev          # frontend dev with HMR
```

API+UI changes: one branch, one PR, one review.

### Documentation Updates

| File | Change |
|---|---|
| `agency/CLAUDE.md` | Update web references from `../agency-web` to `./web` |
| `agency/web/CLAUDE.md` | Keep as-is (paths are relative to web/) |
| Workspace `CLAUDE.md` | Remove agency-web from layout table, note it lives in `agency/web/` |
| `agency/README.md` | Update agency-web link to point at `web/` directory |

### What Doesn't Change

- The web app's runtime behavior (API endpoints, WebSocket proxy, auth)
- The web Docker image (same Dockerfile, same nginx config)
- The OpenAPI spec location (`internal/api/openapi.yaml`)
- How `agency setup` / `agency infra up` starts the web container
- The web app's CLAUDE.md and its conventions

## Sequencing

This is a prerequisite for R2 (OCI hub distribution) — not because R2 depends on it technically, but because consolidating first means a unified CI pipeline before adding OCI publishing complexity.
