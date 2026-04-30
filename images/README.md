# Images

Container build contexts for the Agency runtime services. The Go gateway
binary (`cmd/gateway/`) orchestrates these containers; the gateway itself
runs on the host, not in a container.

This directory holds two kinds of content:

- **Service build contexts** (`body/`, `enforcer/`, etc.) — one subdirectory
  per service image, each with its own `Dockerfile`.
- **Shared Python code** at the top level (`models/`, `exceptions.py`,
  `logging_config.py`, `_sitecustomize.py`, `tests/`) — imported by the
  Python services via the `python-base` builder pattern. Not its own
  service.

## Service Inventory

| Image | Tier | Scope | Language | Purpose | Health |
|---|---|---|---|---|---|
| [body](body/) | core | per-agent | Python | Autonomous agent loop, PACT engine, built-in tools, ws listener | — |
| [enforcer](enforcer/) | core | per-agent | Go | HTTP proxy, audit logging, capability/budget validation, consent token verification, domain allowlist | TCP `:18080` |
| [workspace](workspace/) | core | per-agent | Debian + seccomp | Agent execution sandbox; isolated tool execution surface | — |
| [comms](comms/) | core | shared | Python | Channel-based messaging, DM transport, websocket fan-out | HTTP `:18091/health` |
| [knowledge](knowledge/) | core | shared | Python | Knowledge graph, ingestion, retrieval, synthesizer | HTTP `:18092/health` |
| [egress](egress/) | core | shared | Python (mitmproxy) | Domain filtering, credential swap at the network edge | TCP `:3128` |
| [gateway-proxy](gateway-proxy/) | core | shared | Alpine + socat | Cross-backend bridge from the mediation network to the host gateway socket | — |
| [intake](intake/) | experimental | shared | Python | Connector ingestion, work-item scheduling | — |
| [web-fetch](web-fetch/) | experimental | shared | Go | Sandboxed HTTP fetch for agents | — |
| [embeddings](embeddings/) | internal | shared | Ollama (vendored) | Local vector embeddings; only started when `KNOWLEDGE_EMBED_PROVIDER=ollama` | — |

Tier matches `internal/features/registry.go`. Core is part of the default
0.2.x surface; experimental is opt-in; internal is implementation detail
(may be started conditionally).

## Build Foundations

| Path | Purpose |
|---|---|
| [python-base/](python-base/) | Stable shared base layer for legacy shared Python service builds. The microVM body runtime artifact is self-contained and does not depend on a published `latest` base image. |
| [workspace-base/](workspace-base/) | Stable shared base for the workspace image (system packages, seccomp profile). |

The `Makefile` build matrix:

```make
CORE_IMAGES         = body enforcer comms knowledge egress workspace gateway-proxy
EXPERIMENTAL_IMAGES = intake web-fetch
ALL_IMAGES          = $(CORE_IMAGES) $(EXPERIMENTAL_IMAGES)
```

Release publishing uses the repo-owned daemonless OCI publisher to produce only
the supported microVM runtime OCI filesystem artifacts:

- `ghcr.io/geoffbelknap/agency-runtime-body:vX.Y.Z`
- `ghcr.io/geoffbelknap/agency-runtime-enforcer:vX.Y.Z`

Mutable `latest` tags are intentionally not published or consumed by the
microVM runtime path.

`embeddings` is not in the build matrix — it pulls the upstream `ollama/ollama`
image at runtime via `imageops.ResolveUpstream`. `python-base` and
`workspace-base` are foundational layers built separately.

## Per-Agent Network Topology

Each running agent is wired into three Docker networks (canonical names —
do not rename without updating orchestration code):

```
                 ┌─────────────────────────────────────────────────────┐
                 │                    Internet                         │
                 └──────────────────────┬──────────────────────────────┘
                                        │
                 ┌──────────────────────┼──── agency-egress-ext ───────┐
                 │               ┌──────┴──────┐                       │
                 │               │   egress    │ mitmproxy             │
                 │               │   :3128     │ domain filtering,     │
                 │               └─────────────┘ credential swap       │
                 └──────────────────────┼──────────────────────────────┘
                                        │
                 ┌──────────────────────┼──── agency-egress-int ───────┐
                 │               ┌──────┴──────┐                       │
                 │               │  enforcer   │ per-agent HTTP proxy  │
                 │               │   :8080     │ service credentials,  │
                 │               └──────┬──────┘ request logging       │
                 └──────────────────────┼──────────────────────────────┘
                                        │
                 ┌──────────────────────┼──── agency-gateway ──────────┐
                 │  ┌──────────┐  ┌─────┴───────┐  ┌───────────┐       │
                 │  │ workspace│  │  body       │  │ comms     │       │
                 │  │ (agent)  │  │ (agent loop)│  │ knowledge │       │
                 │  │ seccomp  │  │             │  │ intake    │       │
                 │  └──────────┘  └─────────────┘  └───────────┘       │
                 │                                                       │
                 │   gateway-proxy bridges this network to the host      │
                 │   gateway daemon over the agency-gateway-proxy socket │
                 └─────────────────────────────────────────────────────┘
```

Network rules (from `CLAUDE.md`):

- enforcers stay on the internal mediation plane only
- enforcers must not attach to `agency-operator` or other external-facing networks
- external access stays mediated through the egress path

## Defense-in-Depth Layers

Each layer runs in its own isolation boundary (ASK Tenet 6):

1. **Network isolation** — Docker networks segment agent, mediation, and egress traffic.
2. **Egress proxy** — Domain filtering, credential swap at the network edge.
3. **Enforcer** — Per-agent HTTP mediation, capability/budget validation, audit logging, consent-token verification.
4. **Container hardening** — Read-only mounts, memory limits, dropped capabilities.
5. **Custom seccomp profile** — ~100 allowed syscalls on workspace containers (vs Docker default ~300).

The agent workspace has no direct path to any external resource. All traffic
flows through the enforcer on the agent network, which routes to shared
infrastructure on the mediation network and out through the egress proxy.

## Tests

Cross-image Python integration tests live in [`tests/`](tests/) at this
directory's top level. Per-service unit tests live next to the code in each
service's own subdirectory.
