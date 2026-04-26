## What This Document Covers

The specification for a general-purpose, ASK-compliant agent runtime. This is
the contract between Agency's enforcement infrastructure and the process that
runs inside the workspace container. Any conforming runtime — regardless of
implementation language — can be started by Agency and operate under its
security guarantees.

The runtime is to agents what a container runtime (runc, crun) is to containers: a minimal, correct execution engine that handles lifecycle, resource management, and interface contracts while staying out of the way of the workload.

> **Status:** Design specification. The body runtime (Python,
> `agency/images/body/body.py`) is the sole runtime implementation and has been
> aligned to this spec. Streaming, parallel tool execution, LLM-based context
> summarization, persistent memory, and crash recovery are all implemented.
>
> Current implementation note: the target is a world-class agent runtime in its
> own right. The body runtime is not there yet. One major reason is that
> governance-first PACT concerns have shaped too much of the implementation too
> early. The intended direction is to strengthen the runtime as an agent runtime
> independently, with PACT acting as an execution-governance protocol layered
> onto that capable runtime rather than compensating for weak core execution.

---

## Part 1: Design Principles

### The Runtime Is Infrastructure, Not Application

The runtime is the autonomic nervous system. It handles what every agent needs regardless of purpose: talking to the LLM, dispatching tools, managing context, emitting signals, staying alive. It does not contain business logic, personality, domain knowledge, or opinions about how to solve problems.

Everything that makes an agent distinct — identity, constraints, skills, tools, memory — arrives as mounted configuration. The runtime reads and executes; it never generates governance.

The runtime should be excellent even before governance-specific execution
protocols are layered in. Tool use, planning, recovery, context handling,
delegation, long-running execution, and artifact handling are runtime quality
concerns in their own right; they are not “solved” merely because a governance
protocol can block bad outcomes later.

### Language and Performance

The runtime is not restricted to Python. The reference implementation may be written in Go, Rust, or TypeScript — whichever best serves the requirements of:

- **Low startup latency.** An agent should be ready to accept a task within seconds of container start, not tens of seconds.
- **Low memory overhead.** The runtime itself should consume minimal resident memory. The workspace's memory budget belongs to the agent's tools and work products, not the runtime process.
- **Concurrent tool execution.** Multiple tool calls should execute in parallel when the LLM requests them. The runtime must not serialize independent operations.
- **Streaming.** LLM responses should stream to the operator and to tool dispatch as tokens arrive, not buffer until completion.
- **High connection density.** A single host running dozens of agents needs runtimes that don't bloat per-instance.

### ASK Compliance Is Structural

The runtime enforces the following properties by construction, not by policy:

1. It never generates or modifies governance files (constraints, policy, AGENTS.md, FRAMEWORK.md). It reads them.
2. It never writes to the audit log. Signals go to a defined file; the enforcement layer handles audit.
3. It never contacts external services directly. All LLM and service traffic routes through the enforcer proxy.
4. It never accesses credentials directly. API keys are held by the enforcer; the runtime authenticates to the enforcer, not to upstream providers.
5. It confines tool execution to the workspace boundary. Path traversal and symlink escapes are blocked.

These are not configurable. A runtime that violates any of them is non-conforming.

---

## Part 2: Runtime Interface Contract

The runtime is a process (PID 1 in the workspace container) that communicates with Agency's enforcement infrastructure through a defined set of interfaces. No other communication paths exist.

### 2.1 Filesystem Interface

The runtime discovers its configuration and communicates state through the filesystem. These paths are fixed by convention.

#### Read-Only Mounts (Superego Layer)

| Path | Content | Purpose |
|---|---|---|
| `/agency/identity.md` | Operator-authored identity seed | System prompt component |
| `/agency/FRAMEWORK.md` | ASK framework governance | System prompt component |
| `/agency/AGENTS.md` | Generated constraints summary | System prompt component |
| `/agency/constraints.yaml` | Raw constraint definitions | Reference (not parsed by runtime) |
| `/agency/skills-manifest.json` | Available skills catalog | Skill discovery |
| `/agency/mcp-servers.json` | MCP server configuration | Server startup |
| `/agency/services-manifest.json` | Granted service definitions | Service tool registration |
| `/agency/skills/<name>/` | Skill directories with SKILL.md | Skill content (read-only) |

The runtime MUST NOT write to any path under `/agency/` except the writable mounts below.

#### Writable Mounts (Agent State)

| Path | Content | Purpose |
|---|---|---|
| `/agency/state/` | Runtime-managed state | Signals, heartbeat, conversation log |
| `/agency/state/agent-signals.jsonl` | Signal emission file | Append-only signal stream |
| `/agency/state/heartbeat.json` | Health status | Periodic liveness proof |
| `/agency/state/conversation.jsonl` | In-flight conversation | Crash recovery |
| `/agency/state/conversation-meta.json` | Conversation metadata | Task tracking |
| `/workspace/` | Agent workspace | Tool execution, work products |
| `/workspace/.memory/` | Persistent memory | Agent-managed, survives restarts |

#### Session Context (Writable by Host, Readable by Runtime)

| Path | Content | Purpose |
|---|---|---|
| `/agency/state/session-context.json` | Task delivery and mode | Host writes, runtime polls |

### 2.2 Network Interface

The runtime has exactly one network path: to the enforcer sidecar on the agent-internal network.

| Endpoint | Protocol | Purpose |
|---|---|---|
| `http://enforcer:18080/v1/chat/completions` | OpenAI-compatible | LLM inference |
| `http://enforcer:18080/v1/services/<name>` | HTTP proxy | Service API dispatch |
| `http://enforcer:8081/mediation/comms` | HTTP proxy | Comms service (mediated) |
| `http://enforcer:8081/mediation/knowledge` | HTTP proxy | Knowledge service (mediated) |

The comms and knowledge endpoints are on the unauthenticated constraint port (8081). The enforcer proxies these to the shared comms and knowledge containers on the mediation network. The workspace does not connect to comms or knowledge directly.

The runtime MUST NOT attempt to resolve or connect to any other host. DNS resolution is restricted to the agent-internal network by container configuration.

### 2.3 Signal Protocol

The runtime communicates state changes to the enforcement layer by appending JSON lines to `agent-signals.jsonl`. This is write-only from the runtime's perspective — the enforcement layer reads.

#### Required Signals

```jsonl
{"signal_type":"ready","timestamp":"...","data":{}}
{"signal_type":"heartbeat","timestamp":"...","data":{"status":"ok"}}
{"signal_type":"task_accepted","timestamp":"...","data":{"task_id":"..."}}
{"signal_type":"task_complete","timestamp":"...","data":{"task_id":"...","result":"...","turns":N}}
{"signal_type":"progress_update","timestamp":"...","data":{"content":"...","task_id":"..."}}
{"signal_type":"escalation","timestamp":"...","data":{"reason":"...","severity":"...","task_id":"..."}}
{"signal_type":"self_halt","timestamp":"...","data":{"reason":"...","tried":"...","needs":"..."}}
{"signal_type":"error","timestamp":"...","data":{"error":"...","fatal":bool}}
```

#### Signal Semantics

- `ready` — emitted once after initialization completes. The enforcement layer does not deliver tasks before this signal.
- `heartbeat` — emitted at a regular interval (default 30s). Absence triggers liveness probe failure.
- `escalation` — the runtime surfaces the agent's request to escalate to the operator. The enforcement layer decides routing.
- `self_halt` — the agent is requesting a halt. The runtime emits the signal and enters a quiescent state (stops accepting tasks, completes no further LLM calls). The enforcement layer handles the actual halt.

### 2.4 Process Lifecycle

```
Container starts
  → Runtime initializes (load config, assemble system prompt, start MCP servers)
  → Emit "ready" signal
  → Enter task loop:
      → Poll session-context.json for new tasks
      → On new task: emit "task_accepted", run conversation loop
      → On task complete: emit "task_complete"
      → On idle: heartbeat every 30s
  → On SIGTERM: graceful shutdown (complete current tool call, emit final state, exit)
  → On SIGKILL: immediate termination (no cleanup)
```

The runtime MUST handle SIGTERM gracefully. It MUST NOT trap or ignore SIGKILL.

---

## Part 3: Core Subsystems

### 3.1 LLM Client

The LLM client manages the conversation with the language model through the enforcer proxy.

#### Requirements

- **Protocol:** OpenAI chat completions API (messages + tools format). The enforcer translates to the actual provider.
- **Streaming:** The client MUST support streaming responses (`stream: true`). Non-streaming is acceptable as a fallback but streaming is the primary mode.
- **Retry with backoff:** Transient errors (429, 502, 503, timeout) retry with exponential backoff. Non-transient errors (400, 401, 403) fail immediately.
- **Request cancellation:** On SIGTERM, in-flight LLM requests are cancelled (not waited on).

#### Model Configuration

The runtime does not select or configure models. It reads the model identifier from the environment (`AGENCY_MODEL`) and passes it through to the enforcer. Model routing, failover, and provider selection are enforcer concerns.

### 3.2 Tool System

Tools are the runtime's extension mechanism. Every capability the agent has beyond text generation is a tool. The tool system is a registry that unifies multiple tool sources under one dispatch interface.

#### Tool Sources (Priority Order)

1. **MCP servers** — external processes communicating via MCP stdio protocol. Configured in `mcp-servers.json`. Highest priority: if an MCP tool name conflicts with other sources, MCP wins.
2. **Service tools** — HTTP APIs dispatched through the enforcer proxy. Configured in `services-manifest.json`. Hot-reloaded when the manifest changes (operator runs `agency grant`/`agency revoke`).
3. **Built-in tools** — filesystem and execution tools provided by the runtime itself.
4. **Skill tools** — skill activation and invocation from the skills catalog.

#### Built-In Tools

The runtime provides a minimal set of workspace tools. These are the only tools the runtime itself implements.

| Tool | Purpose | Boundary Enforced |
|---|---|---|
| `read_file` | Read file content with optional offset/limit | Workspace path only |
| `write_file` | Write content to a file | Workspace path only |
| `list_directory` | List directory contents | Workspace path only |
| `execute_command` | Run a shell command | Workspace cwd, safe env |
| `search_files` | Grep for patterns in files | Workspace path only |
| `save_memory` | Write to persistent memory | Memory path only |

All file-based tools enforce workspace boundary confinement:
- Paths are resolved against `/workspace/` and validated to remain within it after symlink resolution.
- Absolute paths outside `/workspace/` are rejected.
- `..` traversal that escapes the workspace is rejected.
- The `execute_command` tool strips sensitive environment variables (API keys, tokens, secrets) before spawning the subprocess.

#### Tool Call Dispatch

```
LLM returns tool_calls[]
  → For each tool_call:
      → Check MCP tools → if match, dispatch via MCP client
      → Check service tools → if match, dispatch via enforcer HTTP proxy
      → Check built-in tools → if match, dispatch via local handler
      → Check skill tools → if match, dispatch via skill runner
      → No match → return error to LLM
  → Collect all results
  → Append tool results to conversation
  → Continue LLM loop
```

#### Parallel Tool Execution

When the LLM returns multiple tool calls in a single response, the runtime SHOULD execute independent calls concurrently. Tool calls are independent when they have no data dependency on each other (which is always the case for tool calls in a single LLM response — the LLM cannot express ordering within a response).

### 3.3 Context Management

The context window is a finite resource. The runtime manages it to prevent truncation failures and maintain conversation coherence.

#### Strategy

1. **Estimate token usage** after each LLM response. Use character-based estimation (configurable ratio, default 4 chars/token).
2. **When usage exceeds threshold** (default 70% of context window):
   - Preserve the system prompt (message index 0).
   - Preserve the N most recent messages (default 10).
   - Summarize older messages into a condensed form.
3. **Summarization** uses the LLM itself. The runtime sends a summarization request to the enforcer with the messages to be condensed, receives a summary, and replaces the old messages with a single summary message.
4. **Never drop the system prompt.** The system prompt contains identity, constraints, and framework governance. Losing it is a security event.

#### Smart Summarization

Summarization is an LLM call, not string truncation. The runtime asks the LLM to summarize the conversation so far, preserving key decisions, tool results, and task state. Tool call results are summarized to their essential output. The summary preserves enough context for the agent to continue the task without re-reading files or re-running commands.

### 3.4 MCP Client

The runtime hosts MCP (Model Context Protocol) servers as child processes and communicates via JSON-RPC 2.0 over stdio.

#### Lifecycle

1. On startup, read `mcp-servers.json`. For each server entry:
   - Spawn the process with configured command, args, and environment.
   - Send `initialize` handshake.
   - Send `initialized` notification.
   - Call `tools/list` to discover available tools.
   - Register discovered tools in the tool registry.
2. On tool call, dispatch to the appropriate MCP client.
3. On shutdown, send `notifications/cancelled` and terminate each server process (SIGTERM, then SIGKILL after 5s).

#### Error Handling

- If a server fails to start, log a warning and continue. Other servers and built-in tools remain available.
- If all servers fail, log an error but do not abort. The agent operates with reduced capability.
- If a server crashes mid-session, the runtime MAY attempt one restart. If the restart fails, the server's tools are removed from the registry and the LLM is informed via a system message.

### 3.5 Crash Recovery

The runtime persists conversation state to survive container restarts (OOM kills, node failures, runtime updates).

#### Mechanism

1. After each LLM call, persist the full message list to `conversation.jsonl` and metadata to `conversation-meta.json`.
2. On startup, check for existing conversation state. If found and the task ID matches the current session context, restore the conversation and continue.
3. On task completion, clear the conversation log.

#### Guarantees

- Recovery replays the conversation from the last persisted state. It does not re-execute tool calls.
- The system prompt is re-assembled from current mounted files on recovery (constraints may have been updated during the restart).
- If the conversation state is corrupt or unreadable, the runtime starts fresh and logs a warning.

### 3.6 Skills System

Skills are operator-provided capability packages following the agentskills.io standard. Each skill is a directory containing a `SKILL.md` file with frontmatter metadata and markdown instructions.

#### Discovery

The runtime reads `skills-manifest.json` at startup. This manifest is generated by the start sequence (Phase 3) from the `skills_dirs` declared in `agent.yaml`.

#### Activation

Skills are not loaded into the system prompt by default (to conserve context window). The runtime provides an `activate_skill` tool that loads a skill's content into the active context when the agent determines it needs that capability.

#### Skill Content

Skills are read-only. The runtime reads SKILL.md content and adds it to the conversation as a system message. The agent cannot modify skills.

---

## Part 4: Conversation Loop

The conversation loop is the core execution cycle. It is intentionally simple.

```
function conversation_loop(task):
    messages = [system_prompt, user_message(task)]
    tools = collect_all_tools()

    for turn in 1..MAX_TURNS:
        messages = manage_context(messages)
        persist_conversation(messages, task.id)

        response = call_llm(messages, tools, stream=true)
        messages.append(response.message)

        if response.has_tool_calls:
            results = execute_tools(response.tool_calls)  # parallel where possible
            messages.extend(tool_result_messages(results))
            continue

        if response.finish_reason == "stop":
            emit_signal("task_complete", task.id, response.content)
            auto_summarize_to_memory(task)
            clear_conversation_log()
            return

    emit_signal("task_complete", task.id, "max turns reached")
```

### Turn Limits

The default maximum is 50 turns per task. This is a safety limit, not a capability limit. It prevents runaway loops. The limit is configurable via environment variable (`AGENCY_MAX_TURNS`).

### Error Recovery Within the Loop

- LLM call failure: retry with backoff (3 attempts). If all fail, emit error signal and break.
- Tool call failure: return error to LLM as tool result. The LLM decides how to proceed.
- Context overflow: summarize and continue. If summarization itself fails, emit error and break.

---

## Part 5: Configuration

All runtime configuration arrives via environment variables and mounted files. The runtime has no config file of its own — it is configured by the enforcement layer.

### Environment Variables

| Variable | Default | Purpose |
|---|---|---|
| `AGENCY_CONFIG_DIR` | `/agency` | Root for mounted config files |
| `AGENCY_WORKSPACE` | `/workspace` | Workspace root path |
| `AGENCY_MODEL` | `standard` | Model alias or identifier for LLM calls |
| `AGENCY_ENFORCER_URL` | `http://enforcer:18080/v1` | Enforcer proxy base URL |
| `AGENCY_AGENT_NAME` | (required) | Agent name for signals and logging |
| `AGENCY_MAX_TURNS` | `50` | Maximum turns per task |
| `AGENCY_CONTEXT_WINDOW` | `200000` | Context window size in tokens |
| `AGENCY_CONTEXT_THRESHOLD` | `0.7` | Fraction of window that triggers summarization |
| `AGENCY_HEARTBEAT_INTERVAL` | `30` | Seconds between heartbeats |
| `AGENCY_LLM_TIMEOUT` | `120` | Seconds before LLM request times out |
| `AGENCY_LLM_API_KEY` | (required) | Scoped key for enforcer-mediated LLM authentication |

### Mounted File Discovery

On startup, the runtime probes for the existence of each mounted file and logs what it found:

```
[runtime] Config: identity.md=yes framework.md=yes agents.md=yes
[runtime] Config: skills-manifest.json=no (no skills)
[runtime] Config: mcp-servers.json=yes (3 servers)
[runtime] Config: services-manifest.json=yes (2 services)
```

Missing optional files are not errors. Missing required files (identity.md) are warnings — the runtime operates but with a degraded system prompt.

---

## Part 6: Extensibility

### Capability Registry

Capabilities (MCP servers, skills, services, tool packs, plugins) are managed centrally in the **Capability Registry** and distributed to agents through policy. The runtime does not know where capabilities came from — it consumes the same config files (`mcp-servers.json`, `services-manifest.json`, `skills-manifest.json`) regardless of whether they were manually configured or resolved from the registry via policy.

See **Capability-Registry.md** for the full design: registry structure, policy-controlled assignment, marketplace integration, credential policy, and migration path.

The runtime's extension points remain the same three mechanisms:

1. **MCP servers** — the primary extension point. Any capability can be packaged as an MCP server. The registry manages discovery and installation; policy controls assignment; the start sequence generates `mcp-servers.json`.

2. **Service grants** — external API access. The registry holds service definitions; policy controls which agents get which services with what credential scope; the enforcer handles credential lifecycle.

3. **Skills** — instruction packages. The registry holds skill content; policy controls assignment; skills are mounted read-only into the workspace.

### Adding New Built-In Tools

Built-in tools should be added sparingly. A tool belongs in the runtime only if:

- It requires workspace-level access that MCP servers cannot provide (filesystem, process execution).
- It is universally needed across all agent types.
- It has security implications that require tight integration with the runtime's boundary enforcement.

All other capabilities should be MCP servers, skills, or service definitions in the registry.

### Custom Runtimes

Organizations can build custom runtimes that conform to this specification. A conforming runtime must:

1. Read configuration from the defined filesystem paths.
2. Communicate with the enforcer via the defined network endpoints.
3. Emit signals in the defined format.
4. Handle SIGTERM gracefully.
5. Enforce workspace boundary confinement for all file operations.
6. Never write to read-only mounts.
7. Never attempt direct external network access.

Agency validates conformance through `agency doctor`, which inspects the running container for these properties.

---

## Part 7: Performance Targets

These are targets for the runtime implementation, not guarantees about LLM latency (which is provider-dependent).

| Metric | Target | Notes |
|---|---|---|
| Cold start to "ready" signal | < 3 seconds | Excluding MCP server startup |
| Memory footprint (runtime only) | < 50 MB RSS | Excluding tools and workspace |
| Heartbeat jitter | < 1 second | Stable interval for liveness detection |
| Tool dispatch overhead | < 10 ms | Time between LLM response and tool execution start |
| Signal write latency | < 1 ms | Append to JSONL file |
| Concurrent tool calls | >= 8 | Parallel execution capacity |
| Context summarization | < 5 seconds | LLM call for smart summarization |
| Graceful shutdown | < 10 seconds | From SIGTERM to exit |

### Scaling Considerations

A single host running N agents runs N runtime processes (one per workspace container). The runtime should be designed so that:

- **Per-instance overhead is minimal.** The runtime should not maintain large in-memory caches, connection pools, or background threads beyond what's needed for the current task.
- **Idle agents are cheap.** An agent with no active task should consume near-zero CPU and minimal memory beyond its base footprint.
- **Startup is parallelizable.** Agency starts multiple agents concurrently. Runtimes must not depend on global locks or shared state.

---

## Part 8: Observability

### Structured Logging

The runtime logs to stdout in a structured format:

```
[runtime] 2026-03-07T14:30:00Z | event=startup | agent=dev-assistant
[runtime] 2026-03-07T14:30:01Z | event=config_loaded | identity=yes | skills=3 | mcp=2
[runtime] 2026-03-07T14:30:02Z | event=ready
[runtime] 2026-03-07T14:30:15Z | event=task_accepted | task_id=task-001
[runtime] 2026-03-07T14:30:16Z | event=llm_call | tokens_est=1200 | stream=true
[runtime] 2026-03-07T14:30:18Z | event=tool_call | tool=execute_command | duration_ms=340
[runtime] 2026-03-07T14:31:00Z | event=task_complete | task_id=task-001 | turns=4
```

Logs are written to stdout. The container runtime captures them. The runtime never writes to the audit log directory — that is the enforcement layer's responsibility.

### Metrics

The runtime exposes metrics via the heartbeat signal:

```json
{
  "signal_type": "heartbeat",
  "timestamp": "2026-03-07T14:31:30Z",
  "data": {
    "status": "ok",
    "active_task": "task-001",
    "turns_this_task": 4,
    "total_tasks": 12,
    "total_turns": 47,
    "mcp_servers_healthy": 2,
    "mcp_servers_failed": 0,
    "context_usage_pct": 0.35,
    "uptime_seconds": 3600
  }
}
```

---

## Part 9: Migration Path

### Current Status

The body runtime (`agency/images/body/body.py`) is the sole runtime and has been aligned to this spec. Completed migration steps:

1. **Standardized interfaces.** Filesystem paths, signal format, and environment variables aligned with this spec.
2. **Streaming.** SSE token-by-token streaming to stderr.
3. **Parallel tool execution.** ThreadPoolExecutor with max 4 workers.
4. **Smart summarization.** LLM-based context summarization replaces naive truncation.
5. **Persistent memory.** Topic-based agent memory in `/workspace/.memory/`.
6. **Crash recovery.** Conversation state persisted to JSONL, restored on restart.
7. **Communication tools.** Channel-based messaging (send_message, read_channel, etc.).
8. **Knowledge tools.** Organizational knowledge graph queries (query_knowledge, who_knows_about, etc.).
9. **Notification system.** Heartbeat-driven Haiku triage for actionable messages.

### Future

- **Performance baseline.** Measure cold start, memory, and dispatch overhead against the targets in Part 7.
- **Language decision.** Based on the performance baseline, decide whether Python meets the targets or whether a rewrite in Go/Rust/TypeScript is warranted.

---

## Part 10: Non-Goals

The runtime specification explicitly excludes:

- **Model selection and routing.** This is the enforcer's job.
- **Credential management.** This is the enforcer's job.
- **Network policy.** This is container and network configuration.
- **Audit logging.** This is the enforcement layer's job. The runtime emits signals; the enforcement layer logs.
- **Multi-agent communication.** Agents do not communicate directly. Coordination happens through the operator or coordinator agents via the enforcement layer.
- **UI/UX.** The runtime has no user interface. Operator interaction happens through Agency CLI commands that read signals and write session context.
- **Image building.** The runtime runs inside a pre-built container. It does not build images or manage Docker.

---

*See also: Capability-Registry.md for centralized capability management and policy-controlled assignment. Agent-Lifecycle.md for the start sequence that launches the runtime. Agency-Platform-Specification.md for the full platform context. Implementation-Roadmap.md for build priorities.*
