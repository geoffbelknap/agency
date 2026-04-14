# OpenAPI Tiering Plan

Status: draft  
Last updated: 2026-04-13

Related:

- [2026-04-13-core-feature-tiering.md](/Users/geoffbelknap/Documents/GitHub/agency-workspace/agency/docs/plans/2026-04-13-core-feature-tiering.md)
- [2026-04-13-core-pruning-plan.md](/Users/geoffbelknap/Documents/GitHub/agency-workspace/agency/docs/plans/2026-04-13-core-pruning-plan.md)
- [internal/api/openapi.yaml](/Users/geoffbelknap/Documents/GitHub/agency-workspace/agency/internal/api/openapi.yaml)

## Goal

Make the OpenAPI contract tell the truth about Agency's product tiers.

Today the spec is treated as canonical, but it still presents the full platform
surface as if it were one equally supported API. That no longer matches the
product direction.

## Problem

The same spec currently serves all of these roles:

- source of truth for the web UI and clients
- builder/integrator contract
- MCP/tooling substrate
- route registration verification target

That is good.

What is not good is that the spec currently implies:

- missions are as supported as agent lifecycle
- hub and package surfaces are as durable as DM/comms
- graph governance is as supported as graph retrieval
- advanced admin and event surfaces are peer contract areas rather than mostly
  experimental or internal

If OpenAPI is canonical, it must reflect product tiering.

## Decision

Keep one canonical source spec for now.

Do not split the spec into multiple authored files yet.

Instead, add vendor extensions that classify tags and operations:

- `x-agency-tier: core | experimental | internal`
- `x-agency-stability: supported | experimental | internal`

Optional later additions:

- `x-agency-release-gate: blocking | soft | none`
- `x-agency-doc-default: true | false`

## Supported Contract Shape

The supported early-product subset should map to the `Core` tier:

- platform health and initialization
- agent lifecycle
- comms / DM core
- provider setup and basic routing
- budget / usage / audit core
- graph retrieval / query / stats
- infra basics
- core admin endpoints
- MCP-backed operator flows built on those same surfaces

The following should be marked `Experimental` unless promoted later:

- missions
- teams
- hub
- packages
- instances
- authz
- broad events / notifications / webhook management
- graph governance / ontology / quarantine / classification
- advanced admin governance surfaces

`Internal` should be reserved for surfaces that are not a supported builder
contract even if they remain in the gateway.

## Rollout

### Phase 1

Add tier metadata at the tag layer in
[internal/api/openapi.yaml](/Users/geoffbelknap/Documents/GitHub/agency-workspace/agency/internal/api/openapi.yaml).

This is low-risk and immediately useful for:

- generated docs
- SDK generation decisions
- MCP exposure defaults

### Phase 2

Add tier metadata at the operation layer, especially for mixed tags like:

- `Admin`
- `Infra`
- `Events`
- `Graph`
- `Platform`

This is where the truth gets sharper, because not every operation within those
tags belongs to the same tier.

### Phase 3

Generate filtered views from the canonical spec:

- `openapi-core.yaml`
- `openapi-full.yaml`

These should be derived artifacts, not separate hand-maintained sources.

## Near-Term Rules

Until operation-level annotations exist, consumers should treat:

- tag-level `x-agency-tier=core` as supported by default
- tag-level `x-agency-tier=experimental` as opt-in
- tag-level `x-agency-tier=internal` as non-public

Mixed tags should be called out as temporary exceptions and annotated further in
Phase 2.

## Strong Opinion

OpenAPI is now part of the product.

That means the spec should stop behaving like an inventory of everything the
gateway can do and start behaving like a truthful contract about what Agency is
prepared to support.
