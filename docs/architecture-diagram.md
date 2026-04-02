# Agency Platform Architecture

## Network Topology

```
                              INTERNET
                                 |
            ┌────────────────────┼────────────────────┐
            |                    ^                    ^
            v                    |                    |
      +-----------+      +--------------+     +--------------+
      | Webhooks  |      | LLM Providers|     | External APIs|
      | (GitHub,  |      | (Anthropic,  |     | (Brave,Slack,|
      |  Slack)   |      |  OpenAI)     |     |  GitHub API) |
      +-----+-----+      +------+-------+     +------+-------+
            |                    |                    |
            |  INBOUND           |  OUTBOUND          |  OUTBOUND
            |                    |                    |
            |                    |                    |
+-----------|--------------------|--------------------|-----------+
|           |       GATEWAY (host process, :8200)     |           |
|           |                                         |           |
|           |   REST API    MCP Server    WS Hub      |           |
|           |   Docker orchestration                  |           |
+-----------|---------+-------------------------------|-----------+
            |         |                               |
            |         | WS relay + REST               |
            v         v                               |
+-----------|---------|-------------------------------|----------+
|           |         |    MEDIATION NETWORK          |          |
|           |         |                               |          |
|   +-------+--+   +--+--------+   +----------+   +---+------+   |
|   | INTAKE   |   | COMMS     |   |KNOWLEDGE |   | EGRESS   |   |
|   | :8205    |   | :8080     |   | :8080    |   | :3128    |   |
|   |          |   |           |   |          |   |          |   |
|   | webhook  |   | channels  |   | graph    |   | TLS      |   |
|   | receiver |   | messages  |   | store    |   | intercept|   |
|   | routing  |   | WS relay  |   | org      |   | cred     |   |
|   | polling  |   | tasks     |   | context  |   | swap     |   |
|   | schedule |   | subscript |   | curation |   | domain   |   |
|   +----+-----+   +-----+-----+   +----+-----+   | filter   |   |
|        |               |             |          +----+-----+   |
|        | delivers      |             |               ^         |
|        | tasks via     |             |               |         |
|        | comms         |             |               |         |
|        +-------->------+             |               |         |
|                        |             |               |         |
+------------------------|-------------|--------------|----------+
                         |             |              |
                         ^             ^              ^
                         | /mediation/ | /mediation/  | egress-net
                         | comms       | knowledge    |
                         |             |              |
+------------------------|-------------|--------------|----------+
|                        |             |              |          |
|        AGENT-INTERNAL NETWORK (per agent, isolated) |          |
|                        |             |              |          |
|   +--------------------+-------------+--------------+-----+    |
|   |                    ENFORCER                           |    |
|   |                    :3128 (auth)  :8081 (no-auth)      |    |
|   |                                                       |    |
|   |  LLM proxy         mediation proxy    rate limiter    |    |
|   |  XPIA scanner      budget tracker     audit logger    |    | 
|   +---------------------------+---------------------------+    |
|                               |                                |
|                    +----------+----------+                     |
|                    |                     |                     |
|               +----+-----+                                    |
|               | WORKSPACE|                                    |
|               | (body)   |                                    |
|               |          |                                    |
|               | LLM loop |                                    |
|               | tools    |                                    |
|               | memory   |                                    |
|               | signals  |                                    |
|               | seccomp  |                                    |
|               +----------+                                    |
|                                                                |
+----------------------------------------------------------------+
```

## Egress Flow (agent makes outbound request)

```
1. Agent (body) calls LLM or external API
   |
2. HTTP_PROXY routes request to enforcer :3128
   |
3. Enforcer checks:
   - Rate limit (in-process, per-provider sliding window)
   - Budget (per-task, daily, monthly)
   - Domain allowlist
   - Service credential lookup
   |
4. Enforcer forwards to egress :3128 (on egress-net)
   |
5. Egress proxy:
   - TLS interception (mitmproxy)
   - Credential swap (agency-scoped token -> real API key)
   - Domain filtering (blocklists)
   |
6. Egress sends to upstream provider (internet)
   |
7. Response returns through same path
   |
8. Enforcer:
   - Records usage to audit log
   - Updates rate limiter from response headers
   - Scans tool-role messages for XPIA (on next LLM request)
   - Records budget cost
   |
9. Response delivered to agent
```

## Ingress Flow (message arrives for agent)

```
Via Web UI / CLI:

 1. User sends message in web UI or CLI
    |
 2. Gateway REST API receives POST /channels/{name}/messages
    |
 3. Gateway forwards to comms :8080 (on mediation network)
    |
 4. Comms stores message, fans out via WebSocket
    |
 5. Gateway's comms relay receives WS event, broadcasts to web UI
    |
    +-- Web UI displays message instantly (WebSocket append)
    |
 6. Comms delivers to agent's body via WS
    (body connects through enforcer :8081/mediation/comms)
    |
 7. Body runtime picks up message, creates idle-reply task
    |
 8. Body sends LLM request through enforcer (egress flow above)
    |
 9. Agent response posted to comms via enforcer mediation proxy
    |
10. Comms fans out -> gateway relay -> web UI displays response


Via Webhook (GitHub, Slack, etc.):

 1. External service sends webhook POST to intake :8205
    |
 2. Intake verifies signature, evaluates routing rules
    |
 3. Intake delivers task to agent via comms
    |
 4. Comms delivers task to agent's body via WS
    |
 5. Agent processes task (egress flow for any API calls)
    |
 6. Agent posts result to channel via enforcer mediation proxy
```

## Key Isolation Boundaries

```
+-------------------------------------------------------------+
|  TRUST BOUNDARY: agent cannot reach anything except enforcer  |
|                                                               |
|  workspace --> enforcer --> mediation services                 |
|                         --> egress --> internet                |
|                                                               |
|  workspace CANNOT reach:                                      |
|    - mediation network directly                               |
|    - other agents' networks                                   |
|    - gateway                                                  |
|    - internet (no default route)                              |
+-------------------------------------------------------------+
```
