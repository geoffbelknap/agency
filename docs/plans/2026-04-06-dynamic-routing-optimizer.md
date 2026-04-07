# Dynamic Routing Optimizer — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a background optimizer that tracks per-model success rates per task type, computes costs, and suggests cheaper routing when quality thresholds are met.

**Architecture:** New `internal/routing/optimizer.go` with background goroutine. Reads enforcer audit data, aggregates stats, generates suggestions. Approval writes to `routing.local.yaml`. API endpoints + CLI for suggestions/stats/approve/reject.

**Tech Stack:** Go (gateway)

**Spec:** `docs/specs/dynamic-routing-optimizer.md`

---

## Task 1: RoutingOptimizer — Stats Aggregation + Suggestion Logic

Create the core optimizer with stats tracking and suggestion generation. This is the largest task — the core logic.

**Files:**
- Create: `internal/routing/optimizer.go`
- Create: `internal/routing/optimizer_test.go`

Tests: stats computation from mock call data, success rate calculation, cost calculation via existing CalculateCost(), suggestion generation with threshold checks, deduplication, insufficient data handling.

Implementation: ModelTaskStats struct, RoutingSuggestion struct, RoutingOptimizer with RecordCall(), runCycle(), Stats(), Suggestions() methods. Persist stats to JSON file.

## Task 2: API Endpoints

**Files:**
- Create: `internal/api/handlers_routing_optimizer.go`
- Modify: `internal/api/routes.go`

Endpoints: GET /routing/suggestions, POST /routing/suggestions/{id}/approve, POST /routing/suggestions/{id}/reject, GET /routing/stats.

## Task 3: Approval Flow — routing.local.yaml

**Files:**
- Modify: `internal/routing/optimizer.go`

Approve() reads/creates routing.local.yaml, writes the override, signals SIGHUP to enforcers.

## Task 4: CLI + Gateway Wiring

**Files:**
- Modify: `internal/cli/commands.go`
- Modify: `internal/orchestrate/infra.go` or `cmd/gateway/main.go`

CLI: `agency routing suggestions/approve/reject/stats`. Start optimizer goroutine on gateway startup.

## Task 5: Full Validation + Push
