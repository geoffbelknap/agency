# Principal Management

## Trigger

Managing the principal registry — creating, updating, suspending, or revoking principals. Understanding the authority hierarchy and permission model.

## Concepts

- **Principal**: Any entity with a UUID identity — operators, agents, teams, roles, channels, services
- **Registry**: Gateway-side SQLite at `~/.agency/registry.db`. Source of truth for all principal identities.
- **Permission model**: Hierarchical ceiling — parent defines max permissions, children can only narrow
- **Coverage principal**: When a principal is suspended, authority transfers to its coverage principal

## Listing Principals

```bash
agency registry list
agency registry list --type agent
agency registry list --type operator
agency registry list --type team
```

Via API: `GET /api/v1/admin/registry/list`

## Viewing a Principal

```bash
agency registry show <name>
```

Shows: UUID, type, status, parent, permissions, creation time.

### Effective permissions

To see the resolved permission set (after hierarchy ceiling is applied):

Via API: `GET /api/v1/admin/registry/{uuid}/effective`

## Registering a Principal

Principals are typically created automatically (agents via `agency create`, operators via setup). Manual registration:

Via API: `POST /api/v1/admin/registry` with `{type, name}`

Graph principals proxy: `POST /api/v1/graph/principals` (registers in the knowledge service)

## Updating a Principal

```bash
agency registry update <name> --status active
agency registry update <name> --status suspended
```

Via API: `PUT /api/v1/admin/registry/{uuid}`

## Permission Model

### Hierarchy

```
Platform (root)
  └── Operator (*)
        └── Team
              └── Agent (knowledge.read, knowledge.write)
```

- **Operators** get `*` (all permissions) by default
- **Agents** get `knowledge.read` + `knowledge.write` by default
- Children can only narrow parent permissions, never widen (ceiling model)

### Wildcards

- `knowledge.*` — all knowledge permissions
- `*` — all permissions

### Route-level enforcement

Chi middleware maps routes to required permissions. Unmatched routes default to deny. Handler-level checks add resource-scoped validation (e.g., `canAccessAgent` checks if the principal can access a specific agent).

## Suspending a Principal

Suspension prevents the principal from authenticating. Use when:

- A human operator's access needs to be temporarily revoked
- An agent needs to be locked out without destroying its state
- Investigating suspicious activity before deciding on full revocation

```bash
agency registry update <name> --status suspended
```

### What happens on suspension

1. Principal cannot authenticate to the gateway
2. Authority transfers to the **coverage principal** (parent in hierarchy)
3. If no coverage principal exists: **fail-closed** — governed agents cannot act (ASK Tenet 16)
4. Running agents governed by this principal are affected immediately

### Verifying coverage

Before suspending, check that a coverage principal exists:

```bash
agency registry show <name>   # check parent field
agency registry show <parent> # verify parent is active
```

If the parent is also suspended, authority chains up. If no active ancestor exists, all governed entities fail-closed.

## Revoking a Principal

Revocation is stronger than suspension — it halts governed agents:

```bash
agency registry update <name> --status suspended
# Then for each governed agent:
agency halt <agent-name> --tier supervised --reason "principal revoked"
```

## Re-enabling a Principal

```bash
agency registry update <name> --status active
```

Governed agents do not auto-resume. Restart them explicitly:

```bash
agency start <agent-name>
```

## Deleting a Principal

```bash
agency registry delete <name>
```

Deletion is permanent. Ensure governed entities are handled first.

## Resolving a Principal

Look up a principal by name or UUID:

```bash
agency registry show <name-or-uuid>
```

Via API: `GET /api/v1/admin/registry/resolve?name=<name>` or `?uuid=<uuid>`

## Troubleshooting

### Agent can't authenticate after principal change

```bash
agency registry show <agent-name>
```

Check status. If suspended, re-enable:

```bash
agency registry update <agent-name> --status active
```

### Permission denied on an endpoint

```bash
# Check effective permissions
curl -sf -H "Authorization: Bearer $TOKEN" \
  http://localhost:8200/api/v1/admin/registry/<uuid>/effective
```

If the required permission is missing, it must be granted at a level at or above the principal in the hierarchy. Children cannot self-elevate (ASK Tenet 17).

### Orphaned authority

If a coverage principal was deleted or suspended without handling governed entities:

```bash
agency registry list --type agent
# For each agent with no active governance chain:
agency halt <agent-name> --tier supervised --reason "orphaned authority"
```

Then either re-enable the parent principal or reassign governance.

## Verification

- [ ] `agency registry list` shows all expected principals
- [ ] `agency registry show <name>` shows correct status and permissions
- [ ] Suspended principals cannot authenticate
- [ ] Coverage principals receive authority on suspension
- [ ] `agency admin doctor` passes after registry changes

## See Also

- [Security Incident Response](security-incident-response.md) — principal suspension during incidents
- [Agent Recovery](agent-recovery.md) — agents affected by principal changes
