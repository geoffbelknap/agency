# Core Feature Maturity Matrix

Status: draft  
Last updated: 2026-04-13

## Purpose

This document is the working inventory of Agency's core features and the
supporting elements required to consider them genuinely release-ready.

The goal is to stop evaluating features as isolated demos ("the core thing
works") and instead evaluate them as complete product surfaces:

- the core behavior itself
- the operator workflows around it
- observability and auditability
- UI / CLI affordances
- release/install implications

## Maturity Scale

| Level | Meaning |
|-------|---------|
| `Mature` | Core behavior works reliably; support surfaces are mostly in place; suitable as a foundation for `0.1.x`. |
| `Alpha-ready` | Works well enough for friendly testers, but still has known rough edges or missing support surfaces. |
| `Partial` | Important parts exist, but the feature is incomplete, inconsistent, or missing key support elements. |
| `Experimental` | Technically present, but not reliable enough to present as a supported product capability. |
| `Deferred` | Designed or scoped, but intentionally not part of the current release target. |

## Inventory

| Feature Area | Current Maturity | Core Capability | Supporting Elements Required | Current Gaps / Notes |
|--------------|------------------|-----------------|------------------------------|----------------------|
| Agent runtime core (`identity` / `constraints` / workspace / enforcer / body) | `Mature` | Create, start, run, stop agents with isolated workspace and mediated execution. | Presets, lifecycle CLI, status visibility, restart/recovery, audit logs. | This is one of the strongest parts of the system. |
| Agent lifecycle management | `Alpha-ready` | Create/start/stop/restart/delete agents; health and runtime state. | Web flows, CLI flows, recovery guidance, stale build detection, cleanup behavior. | Live flows work; some startup fragility still appears intermittently in certain agents. |
| Dynamic agent reconfiguration (prompt / identity / constraints changes on the fly) | `Alpha-ready` | Operators can update agent config and have live direct-message behavior reflect the new identity without a full recreate. | Safe live-apply semantics, body-side config refresh, clear UX for “what changed”, web/CLI affordances. | Live config updates now propagate into the direct DM path and are covered by the fundamentals gate. Remaining work is operator polish and richer UX, not basic correctness. |
| Comms / channels / DM workflow | `Mature` | Agents and operators communicate through channels and DMs. | Unreads, search, routing into agent prompts, web UI, realtime delivery. | Strong coverage and live validation. |
| Event bus / routing / subscriptions | `Alpha-ready` | Internal events, connector events, and notifications route through one bus. | Visibility into event delivery, operator debugging, mission health integration, notification wiring. | Core routing works, but operator-facing event inspection is still less polished than the execution path itself. |
| Model routing / provider abstraction | `Alpha-ready` | Route LLM traffic through configured providers and tiers. | Provider setup UX, routing config visibility, usage summaries, fallback clarity. | Multi-provider works; “why this model/provider was chosen” still needs better operator UX. |
| Budget enforcement | `Alpha-ready` | Enforcer tracks cost and enforces daily/monthly/task budgets. | Clear operator surfacing, warnings/exhaustion events, mission integration, usage reconciliation. | Core enforcement exists; release gate should explicitly verify live usage -> budget -> audit linkage. |
| Usage tracking / economics | `Alpha-ready` | Aggregate LLM usage, tokens, latency, estimated cost. | Reliable non-zero token/cost attribution, by-agent/by-model reporting, release smoke coverage. | Live provider-backed streaming calls now record tokens and estimated cost correctly. Remaining work is better reconciliation and operator polish, not basic correctness. |
| Audit logging | `Alpha-ready` | Infrastructure-written logs and HMAC-signed audit trail. | Easy inspection, useful categories, release gating, signal correlation, retention. | Strong underlying model; still need a better release gate tying task execution to auditable evidence. |
| Trajectory monitoring | `Alpha-ready` | Enforcer detects repetition / cycle / error-cascade anomalies. | Stable trajectory endpoint, `agency show` surfacing, notifications, critical-path tests, docs alignment. | Live trajectory endpoint is now working in the fundamentals gate. Still needs better operator surfacing and alerting before calling it mature. |
| Knowledge graph query / stats | `Alpha-ready` | Query graph, inspect stats, retrieve cached results and relationship data. | Reliable live updates, ingestion, ontology management, curation visibility. | Query/stats work. Registration appears eventually consistent enough to need retries in release tests. |
| Knowledge graph ingestion | `Alpha-ready` | Ingest documents/URLs into graph. | Working pipeline in local stack, extraction coverage, synthesis gating, review tooling. | Live universal ingest now works in the local stack and ingested content is queryable by extracted nodes. Remaining work is richer content indexing and better synthesis ergonomics. |
| Knowledge graph governance (classification / review / quarantine / ontology candidates) | `Alpha-ready` | Governance and structural controls around graph content. | Review workflows, candidate management, curation logs, operator education. | A lot is implemented, but this surface still needs more end-to-end operator validation. |
| Missions | `Alpha-ready` | Create, assign, pause/resume, complete, delete missions. | Wizard/UI, mission health, evaluations, economics, team integration. | Non-destructive live UI flows passed; still needs stronger “real operator workflow” coverage. |
| Team mission routing / failover / deconfliction | `Alpha-ready` | Missions can be assigned to teams, coordinators receive exclusive mission copies, no-coordinator teams can claim events, and coverage failover protects active missions. | Better operator UX, more human-readable observability, stronger CLI surfaces. | The dedicated live gate now passes end to end. Remaining work is support-surface polish, not baseline correctness. |
| Advanced coordinator delegation / multi-agent coordination UX | `Partial` | Coordinator semantics exist in docs and mission framing. | Real delegation tools, task graph/status UX, review flows, work splitting visibility, richer lifecycle semantics. | The broad coordinator vision is still ahead of the product: most dedicated coordinator tools in the spec are not implemented yet. |
| Packs / declarative deployment | `Alpha-ready` | Define teams/connectors/channels declaratively and deploy them. | Lifecycle UX, versioning discipline, hub alignment, deployment observability. | Core concept is strong; install/publish path needs more release polish. |
| Hub package management | `Alpha-ready` | Install presets, connectors, providers, services from hub. | Assurance/trust metadata, OCI publishing, upgrade path, support for unchanged metadata refresh. | Substantial progress, but assurance/source plumbing has only recently stabilized. |
| Connectors / intake | `Alpha-ready` | Poll/webhook/schedule/channel-watch intake into agents/runtime. | Route rendering, operator troubleshooting, auth/secret resolution, ingress maturity. | Core intake works; some connector families are much more mature than others. |
| Slack connector flow | `Alpha-ready` | Shortcut -> modal -> submit -> task routing via relay/public ingress. | Better modal UX, submission polish, consent/approval path polish, relay observability. | Technically working; still too rough to treat as fully mature. |
| Drive authority runtime (read + consent-gated mutation) | `Alpha-ready` | Drive read actions and consent-gated mutations work through the authority runtime. | Dedicated live gate, clearer operator docs, richer consent UX. | The live gate now passes for read, deny-without-consent, and allow-with-consent. Remaining work is mostly product polish around the connector/package surface. |
| Drive bounded-resource policy + richer operator UX | `Partial` | Bounded Drive administration with clear whitelist semantics and operator-friendly setup/use flows. | Real package-level whitelist metadata, better instance scaffolding, web/e2e coverage, stronger consent UX. | The authority subset works, but the public connector/package surface still needs more polish to make bounded Drive admin feel complete. |
| Web UI core shell / admin surfaces | `Alpha-ready` | Overview, Setup, Agents, Channels, Admin, Missions, Knowledge, Profiles. | Live non-destructive coverage, polished first-run path, good empty states, coherent copy. | Large improvement landed; still benefits from more human polish passes. |
| Web UI mutable operator flows | `Alpha-ready` | Create/edit non-destructive flows across core surfaces. | More real-world workflow passes, destructive-path confidence, clearer operator guidance. | Live mutable non-destructive suite is green. |
| Relay-hosted web access | `Alpha-ready` | Remote access to Agency UI via `agency-relay`. | Asset publishing automation, performance work, operator clarity, session stability. | Path works; supporting automation and polish are still catching up. |
| Security / mediation / credential isolation | `Mature` | ASK-aligned mediation, enforcer boundary, egress credential swap, auditability. | Doctor/reporting, docs, container/network hygiene, release-image discipline. | This is foundationally strong and should remain a core selling point. |
| Release / installability (CLI, Homebrew, GHCR) | `Partial` | Release workflows exist, Homebrew tap exists, GHCR image pipeline exists. | Reliable public package visibility, release smoke checks, clean-machine install test. | Current blocker: GHCR visibility drift on some images; `#188` fixes future publishes, not old tags. |

## Supporting Elements By Feature Type

Every feature should be evaluated across these dimensions before being called
`Mature`:

1. **Core behavior**
   Does the feature itself work correctly?

2. **Operator control**
   Can an operator configure it, inspect it, and recover from failure?

3. **Audit / observability**
   Does the platform record enough information to explain what happened?

4. **Web + CLI support**
   Is the capability coherent in both product surfaces, or intentionally scoped
   to one?

5. **Lifecycle behavior**
   Does it behave correctly on create/start/stop/restart/update/cleanup?

6. **Release/install behavior**
   Does it still work in the form testers will actually install?

## Immediate Release-Gate Focus

Before `0.1.x`, the features that deserve explicit release gating are:

- agent runtime core
- comms / DM flow
- routing / provider execution
- budget + usage + audit linkage
- trajectory endpoint / monitoring visibility
- graph query/stats and graph ingestion
- web core non-destructive operator flows
- release/install path (Homebrew + GHCR)

## Current Blockers Worth Treating As First-Class

These are not abstract maturity concerns — they are currently observable gaps:

- **GHCR release image visibility**
  - Some images were not anonymously pullable from GHCR.
  - `#188` addresses future publishes, but the release gate still has to prove the next tag fixes it.

## Recommended Next Artifact

This matrix should feed a second document:

- `0.1.x release gates by feature`

That follow-up should map each release-critical feature to:

- the exact automated checks
- the exact live smoke checks
- whether failure is blocking or non-blocking
