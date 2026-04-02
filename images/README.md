# Container Architecture

## Per-Agent Network Layout

```
                 ┌─────────────────────────────────────────────────────┐
                 │                    Internet                         │
                 └──────────────────────┬──────────────────────────────┘
                                        │
                 ┌──────────────────────┼──────── egress-net ──────────┐
                 │               ┌──────┴──────┐                       │
                 │               │   egress    │ mitmproxy             │
                 │               │   :3128     │ domain filtering,     │
                 │               └─────────────┘ credential swap       │
                 └──────────────────────┼──────────────────────────────┘
                                        │
                 ┌──────────────────────┼──────── mediation-net ───────┐
                 │               ┌──────┴──────┐                       │
                 │               │  enforcer   │ per-agent HTTP proxy  │
                 │               │   :8080     │ service credentials,  │
                 │               └──────┬──────┘ request logging       │
                 └──────────────────────┼──────────────────────────────┘
                                        │
                 ┌──────────────────────┼──────── agent-net ───────────┐
                 │               ┌──────┴──────┐                       │
                 │               │  workspace  │                       │
                 │               │  (agent)    │                       │
                 │               │  seccomp    │                       │
                 │               └─────────────┘                       │
                 └─────────────────────────────────────────────────────┘

                 ┌──────────────────────────────── mediation-net ──────┐
                 │  ┌──────────┐  ┌───────────┐                          │
                 │  │  comms   │  │ knowledge │                          │
                 │  │  :18091  │  │  :18092   │                          │
                 │  │          │  │ graph RAG │                          │
                 │  └──────────┘  └───────────┘                          │
                 └─────────────────────────────────────────────────────┘
```

## Container Inventory

| Image | Base | Scope | Purpose | Health Check |
|---|---|---|---|---|
| workspace | debian:bookworm-slim | per-agent | Agent execution environment | - |
| enforcer | Go (scratch) | per-agent | HTTP proxy, audit logging, domain allowlisting (32MB) | TCP :18080 |
| body | python:3.12-slim | per-agent | Autonomous agent loop, built-in tools | - |
| egress | python:3.12-slim (mitmproxy) | shared | Domain filtering, credential swap | TCP :3128 |
| comms | python:3.12-slim (aiohttp) | shared | Channel-based agent messaging | HTTP :18091/health |
| knowledge | python:3.12-slim (aiohttp) | shared | Knowledge graph, rule + LLM ingestion | HTTP :18092/health |

## Defense-in-Depth Layers

Each layer runs in its own isolation boundary (tenet 6):

1. **Network isolation** - Docker networks segment agent, mediation, and egress traffic
2. **Egress proxy** - Domain filtering, credential swap at the network edge
3. **Enforcer** - Per-agent HTTP mediation, XPIA scanning, budget tracking, audit logging, domain allowlisting
4. **Container hardening** - Read-only mounts, memory limits, dropped capabilities
5. **Custom seccomp profile** - ~100 allowed syscalls on workspace containers (vs Docker default ~300)

The agent workspace has no direct path to any external resource. All traffic flows through the enforcer on the agent network, which routes to shared infrastructure on the mediation network.
