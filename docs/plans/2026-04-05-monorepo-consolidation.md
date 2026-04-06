# Monorepo Consolidation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move agency-web into `agency/web/` via git subtree merge, enabling atomic API+UI pull requests.

**Architecture:** `git subtree add` brings agency-web's full tree and history into the agency repo at `web/`. Makefile, CI, release workflow, and docs are updated to reference the new location. The standalone agency-web repo is deleted.

**Tech Stack:** Git subtree, Make, GitHub Actions, Node 22, Vite 8

**Spec:** `docs/specs/monorepo-consolidation.md`

---

### Task 1: Subtree merge

**Files:**
- Modify: `web/` (new directory, created by subtree add)

- [ ] **Step 1: Add agency-web as a remote**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency
git remote add agency-web https://github.com/geoffbelknap/agency-web.git
git fetch agency-web
```

- [ ] **Step 2: Subtree add**

```bash
git subtree add --prefix=web agency-web main --squash
```

The `--squash` flag collapses agency-web history into a single merge commit. This keeps the agency log clean while preserving full blame via the squashed commit.

Expected: A merge commit creating `web/` with all agency-web files.

- [ ] **Step 3: Verify the subtree**

```bash
ls web/src/app/lib/api.ts   # should exist
ls web/package.json          # should exist
ls web/Dockerfile            # should exist
ls web/CLAUDE.md             # should exist
```

- [ ] **Step 4: Remove the remote**

```bash
git remote remove agency-web
```

- [ ] **Step 5: Commit**

The subtree add already created a merge commit. No additional commit needed. Verify:

```bash
git log --oneline -3
```

Expected: A merge commit like "Squashed 'web/' content from commit XXXXXXX".

---

### Task 2: Update Makefile

**Files:**
- Modify: `Makefile:88-97`

- [ ] **Step 1: Update AGENCY_WEB_DIR and comment**

Change lines 88-89 from:
```makefile
# agency-web lives in the workspace (../agency-web)
AGENCY_WEB_DIR ?= $(shell cd .. && pwd)/agency-web
```

To:
```makefile
# agency-web source (monorepo)
AGENCY_WEB_DIR ?= $(SOURCE_DIR)/web
```

- [ ] **Step 2: Verify the web target builds**

```bash
make web 2>&1 | head -5
```

Expected: "Building agency-web..." followed by Docker build output (or an error about Docker not running, which is fine — the path resolution is what we're testing).

- [ ] **Step 3: Commit**

```bash
git add Makefile
git commit -m "build: point AGENCY_WEB_DIR to monorepo web/"
```

---

### Task 3: Update CI workflow

**Files:**
- Modify: `.github/workflows/ci.yml`

- [ ] **Step 1: Add web-test job**

Add this job after the `python-test` job in `.github/workflows/ci.yml`:

```yaml

  web-test:
    runs-on: ubuntu-latest
    permissions:
      contents: read
    steps:
      - uses: actions/checkout@v6

      - uses: actions/setup-node@v4
        with:
          node-version: '22'

      - name: Install dependencies
        working-directory: web
        run: npm ci

      - name: Build
        working-directory: web
        run: npm run build

      - name: Test
        working-directory: web
        run: npm test
```

- [ ] **Step 2: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: add web-test job for monorepo web/"
```

---

### Task 4: Update release-images workflow

**Files:**
- Modify: `.github/workflows/release-images.yml:88-128`

- [ ] **Step 1: Replace the build-and-push-web job**

The current `build-and-push-web` job checks out the separate `agency-web` repo. Replace lines 88-128 with:

```yaml
  build-and-push-web:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@v6

      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@v4

      - name: Set up QEMU
        uses: docker/setup-qemu-action@v4

      - name: Log in to GHCR
        uses: docker/login-action@v4
        with:
          registry: ${{ env.REGISTRY }}
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Extract version
        id: version
        env:
          REF_NAME: ${{ github.ref_name }}
        run: echo "version=${REF_NAME#v}" >> "$GITHUB_OUTPUT"

      - name: Build and push
        uses: docker/build-push-action@v7
        with:
          context: web
          file: web/Dockerfile
          platforms: linux/amd64,linux/arm64
          push: true
          build-args: |
            BUILD_ID=${{ github.sha }}
          tags: |
            ${{ env.IMAGE_PREFIX }}/agency-web:v${{ steps.version.outputs.version }}
            ${{ env.IMAGE_PREFIX }}/agency-web:latest
          cache-from: type=gha,scope=web
          cache-to: type=gha,scope=web,mode=max
```

Key change: `uses: actions/checkout@v6` now checks out the agency repo (default), and the build context points to `web/` instead of `.`.

- [ ] **Step 2: Commit**

```bash
git add .github/workflows/release-images.yml
git commit -m "ci: build web image from monorepo web/ directory"
```

---

### Task 5: Update documentation

**Files:**
- Modify: `CLAUDE.md`
- Modify: `README.md`
- Modify: `web/CLAUDE.md` (verify, likely no changes needed)

- [ ] **Step 1: Update CLAUDE.md**

Find and replace these references in `CLAUDE.md`:

1. Change:
```
Source lives in the sibling `agency-web` repo. `make web` builds the image (expects `../agency-web` to exist).
```
To:
```
Source lives in `web/` (monorepo). `make web` builds the image.
```

2. Change:
```
- **agency-web is containerized**: Runs as an infra container (`agency-web:latest`) on port 8280, started automatically by `agency setup` / `agency infra up`. Source lives in the sibling `agency-web` repo. `make web` builds the image (expects `../agency-web` to exist).
```
To:
```
- **agency-web is containerized**: Runs as an infra container (`agency-web:latest`) on port 8280, started automatically by `agency setup` / `agency infra up`. Source lives in `web/`. `make web` builds the image.
```

- [ ] **Step 2: Update README.md**

Find the related projects table and change:
```
| [agency-web](https://github.com/geoffbelknap/agency-web) | Web UI. Vite/React. Connects to the gateway REST API. |
```
To:
```
| [web/](web/) | Web UI. Vite/React. Connects to the gateway REST API. |
```

- [ ] **Step 3: Verify web/CLAUDE.md needs no changes**

```bash
grep -c '\.\.\/' web/CLAUDE.md
```

Expected: 0 (no parent directory references). If any exist, update them.

- [ ] **Step 4: Commit**

```bash
git add CLAUDE.md README.md
git commit -m "docs: update references from agency-web repo to web/"
```

---

### Task 6: Update workspace CLAUDE.md

**Files:**
- Modify: `/Users/geoffbelknap/Documents/GitHub/agency-workspace/CLAUDE.md`

- [ ] **Step 1: Update the layout table**

In the workspace root `CLAUDE.md`, change the layout section. Remove `agency-web/` as a standalone entry and update the description:

Change:
```
├── agency/              # Platform core — CLI, runtime, orchestration, MCP server
├── agency-web/          # Web UI — Vite/React, connects to gateway REST API
```
To:
```
├── agency/              # Platform core — CLI, runtime, orchestration, MCP server (includes web/ UI)
```

- [ ] **Step 2: Update the cross-repo relationships section**

Change the agency-web bullet:
```
- **Agency Web** is the web UI. It's a pure REST client — no shared code with Agency. It talks to `localhost:8200`. The canonical OpenAPI spec is at `agency/internal/api/openapi.yaml` — agency-web reads this for API types and endpoint reference.
```
To:
```
- **Agency Web** (`agency/web/`) is the web UI. It's a pure REST client — no shared code with the Go side. It talks to `localhost:8200`. The canonical OpenAPI spec is at `agency/internal/api/openapi.yaml` — the web UI reads this for API types and endpoint reference.
```

- [ ] **Step 3: Commit**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace
git add CLAUDE.md
git commit -m "docs: update workspace CLAUDE.md for agency-web monorepo move"
```

---

### Task 7: Delete the agency-web GitHub repo

- [ ] **Step 1: Verify everything is merged**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency
ls web/src/app/lib/api.ts && echo "Subtree OK"
grep 'SOURCE_DIR.*web' Makefile && echo "Makefile OK"
grep 'web-test' .github/workflows/ci.yml && echo "CI OK"
grep 'context: web' .github/workflows/release-images.yml && echo "Release OK"
```

All four should print OK.

- [ ] **Step 2: Delete the GitHub repo**

```bash
gh repo delete geoffbelknap/agency-web --yes
```

- [ ] **Step 3: Clean up workspace directory**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace
rm -rf agency-web
```

- [ ] **Step 4: Remove submodule entry if present**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace
git submodule deinit agency-web 2>/dev/null || true
git rm agency-web 2>/dev/null || true
# If .gitmodules was modified:
git diff --name-only
```

If changes were made, commit:
```bash
git add -A
git commit -m "chore: remove agency-web submodule from workspace"
```

---

### Task 8: Push and verify

- [ ] **Step 1: Push agency repo changes**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace/agency
git push origin main
```

- [ ] **Step 2: Verify CI passes**

```bash
gh run list --limit 1 --json status,conclusion,name
```

Expected: CI run triggered with go-test, python-test, and web-test jobs.

- [ ] **Step 3: Push workspace changes**

```bash
cd /Users/geoffbelknap/Documents/GitHub/agency-workspace
git push origin main
```
