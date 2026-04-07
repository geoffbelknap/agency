# Graph ACL Model — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Activate classification-based access control on the knowledge graph — four tiers mapped to scope rules via operator-configurable YAML, auto-scope merge at ingestion time.

**Architecture:** Classification config at `~/.agency/knowledge/classification.yaml` loaded by knowledge service. `add_node()` merges tier scope into node scope when classification is set. Existing Phase 1b scope enforcement does the rest. CLI for config management, gateway proxy for endpoint.

**Tech Stack:** Python (knowledge service), Go (gateway, CLI), YAML

**Spec:** `docs/specs/graph-acl-model.md`

---

## Task 1: Classification Config Loader + Auto-Scope Merge

**Files:**
- Create: `images/knowledge/classification.py`
- Modify: `images/knowledge/store.py`
- Test: `images/tests/test_classification.py`

Implement classification config loader, auto-scope merge in add_node(), and tests covering all tiers including unrecognized classification defaulting to internal.

## Task 2: Default Config + Setup Integration

**Files:**
- Modify: `internal/orchestrate/infra.go`

Write default `classification.yaml` during `agency setup` alongside ontology. Register default classification roles (role:internal, role:restricted, role:confidential) in the principal registry.

## Task 3: Server Endpoint + CLI

**Files:**
- Modify: `images/knowledge/server.py`
- Modify: `internal/knowledge/proxy.go`
- Modify: `internal/api/routes.go`
- Modify: `internal/api/handlers_hub.go`
- Modify: `internal/cli/commands.go`

GET /classification endpoint, gateway proxy, `agency knowledge classification show/set/grant/revoke` CLI.

## Task 4: Full Validation

Run all tests, Go build, push.
