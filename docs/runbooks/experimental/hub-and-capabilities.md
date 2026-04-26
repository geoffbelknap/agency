# Hub & Capabilities

> Status: Experimental operator runbook. Hub lifecycle and broader capability
> distribution remain outside the default supported `0.2.x` first-user path.
> Core provider setup uses the built-in provider catalog and `agency quickstart`.

## Trigger

Installing hub components, managing capabilities, configuring web-fetch, working with presets, or troubleshooting hub operations.

## Hub Overview

The hub is a component registry for packs, connectors, presets, and skills. The official hub publishes signed OCI artifacts to `ghcr.io/geoffbelknap/agency-hub`.

## Hub Sources

### List sources

```bash
agency hub list-sources
```

### Add a third-party source

```bash
# OCI registry
agency hub add-source my-corp ghcr.io/my-corp/hub

# Git URL
agency hub add-source my-team https://github.com/my-team/agency-hub.git
```

### Remove a source

```bash
agency hub remove-source my-corp
```

Signature verification is mandatory — unsigned artifacts are rejected (ASK Tenet 23).

OCI component installs require `cosign` on the operator machine because install verifies the artifact signature before activation. `agency hub update` and catalog browsing can run without `cosign`, but `agency hub install ...` for OCI-sourced components will fail if signature verification cannot run.

## Installing Components

### Search

```bash
agency hub search security
agency hub search --kind connector
agency hub search --kind pack
agency hub search --kind preset
```

### Install

```bash
agency hub install limacharlie --kind connector
```

Pulls from OCI, verifies cosign signature, extracts to hub cache.

### Activate

```bash
agency hub limacharlie activate
```

Activation provisions egress domains from the connector's `requires.egress_domains` and creates the hub instance.

### Configure

```bash
agency hub limacharlie config
```

Component configs use `${...}` placeholders for secrets and `config:` declarations for non-secret settings. Secrets are stored via `agency creds set`.

### Check health

```bash
agency hub limacharlie check
agency hub doctor   # all components
```

### Deactivate

```bash
agency hub limacharlie deactivate
```

### Remove

```bash
agency hub remove limacharlie
```

## Hub Instances

Components are activated as named instances, not anonymous installs:

```bash
agency hub instances   # list all active instances
agency hub show <name-or-id>   # instance detail
```

Via API:
- `GET /api/v1/hub/instances`
- `GET /api/v1/hub/{nameOrID}`

## Hub Updates and Upgrades

### Check for updates

```bash
agency hub update      # refresh sources (does not upgrade)
agency hub outdated    # show available upgrades
```

### Apply upgrades

```bash
agency hub upgrade                 # upgrade all
agency hub upgrade limacharlie     # upgrade specific component
```

Hub-managed files (`routing.yaml`, service definitions, ontology) are overwritten by `agency hub update`. Operator customizations go in `routing.local.yaml`, new service files, or `ontology.d/`.

## Presets

Presets define agent configurations (tier, capabilities, identity, hard limits):

```bash
agency hub presets list
agency hub presets show <name>
```

Create an agent from a preset:

```bash
agency create my-agent --preset security-analyst
```

### Preset CRUD via API

- `GET /api/v1/hub/presets`
- `POST /api/v1/hub/presets`
- `GET /api/v1/hub/presets/{name}`
- `PUT /api/v1/hub/presets/{name}`
- `DELETE /api/v1/hub/presets/{name}`

## Pack Deployment

Packs define a full team (multiple agents, presets, connectors):

```bash
agency hub deploy /path/to/pack.yaml
```

Teardown reverses a deployment:

```bash
agency hub teardown <pack-name>
```

Pack schema does not support missions. Missions are created and assigned separately via `agency mission create` / `agency mission assign`.

## Capabilities

Capabilities are features that can be enabled/disabled per agent.

### List capabilities

```bash
agency cap list
```

### Show capability details

```bash
agency cap show web-fetch
```

### Enable/disable

```bash
agency cap enable web-fetch
agency cap disable web-fetch
```

Capability hot-reload: `cap enable/disable` regenerates service manifests, writes grants, copies service definitions, and SIGHUPs enforcers — no agent restart needed.

### Add a custom capability

```bash
agency cap add my-capability
```

### Delete a capability

```bash
agency cap delete my-capability
```

Via API:
- `GET /api/v1/admin/capabilities`
- `GET /api/v1/admin/capabilities/{name}`
- `POST /api/v1/admin/capabilities/{name}/enable`
- `POST /api/v1/admin/capabilities/{name}/disable`
- `POST /api/v1/admin/capabilities` (add)
- `DELETE /api/v1/admin/capabilities/{name}` (delete)

## Web-Fetch Service

Shared infra container for agents to fetch and read web pages.

### Enable

```bash
agency cap add web-fetch
agency cap enable web-fetch
```

Agents reach it via enforcer mediation (`/mediation/web-fetch`). External requests route through the egress proxy.

### Configuration

Config at `~/.agency/web-fetch/config.yaml`:

```yaml
# DNS blocklists (platform hard floor + operator additions)
dns_blocklist:
  - "*.internal"
  - "*.local"

# Content-type allowlist
allowed_content_types:
  - "text/html"
  - "application/json"

# Per-domain rate limiting
rate_limits:
  default: 10/min
```

### Security layers

1. DNS blocklists (platform hard floor + operator configurable)
2. Content-type allowlist
3. XPIA scanning on fetched content
4. Per-domain rate limiting
5. All requests through egress proxy

### Audit

Audit log at `~/.agency/audit/web-fetch/`.

## Connector Operations

### Requirements check

```bash
# Via API: GET /api/v1/hub/connectors/{name}/requirements
```

Shows what the connector needs (egress domains, credentials, capabilities).

### Configure a connector

```bash
# Via API: POST /api/v1/hub/connectors/{name}/configure
```

### Egress domains

Connectors auto-provision egress domains on activate:

```bash
agency admin egress domains      # list all egress domains with provenance
agency admin egress why <domain> # show why a domain is allowed
```

### Intake polling

```bash
agency intake stats                  # intake statistics
agency intake items                  # pending work items
agency intake poll <connector>       # trigger immediate poll
```

Via API: `GET /api/v1/hub/intake/poll-health`

## Hub Scaffolding and Publishing

### Scaffold a new component

```bash
agency hub create connector my-connector
agency hub create pack my-pack
```

### Validate before publishing

```bash
agency hub audit /path/to/component
```

### Publish

```bash
agency hub publish /path/to/component
```

## Agent Rebuild

After changing capabilities, presets, or hub configurations:

```bash
agency admin rebuild <agent-name>
```

Regenerates all derived files (manifest, services.yaml, PLATFORM.md, FRAMEWORK.md, AGENTS.md) in one step.

## Troubleshooting

### Hub install fails with signature error

Unsigned artifacts are rejected (ASK Tenet 23). Verify the source publishes signed OCI artifacts with cosign.

### Connector not receiving events

```bash
agency intake stats
agency intake poll <connector>
docker logs agency-infra-intake 2>&1 | tail -20
```

Check that the connector is activated and the intake container is healthy.

### Capability enable doesn't take effect

```bash
agency cap list   # verify it's enabled
agency show <agent-name>   # check agent grants
```

If the agent doesn't have the capability, the hot-reload may have failed. Restart the agent:

```bash
agency stop <agent-name>
agency start <agent-name>
```

### Hub update overwrites customizations

Hub-managed files are overwritten by design. Keep customizations in:
- `routing.local.yaml` (routing overrides)
- `~/.agency/knowledge/ontology.d/` (ontology extensions)
- New service files (not editing hub-managed ones)

## Verification

- [ ] `agency hub instances` shows all active instances
- [ ] `agency hub doctor` passes
- [ ] `agency cap list` shows expected capabilities
- [ ] Connectors receive and process events
- [ ] `agency admin egress domains` shows expected domains

## See Also

- [Routing & Providers](../routing-and-providers.md) — provider setup via hub
- [Credential Rotation](../credential-rotation.md) — connector credential management
- [Infrastructure Recovery](../infrastructure-recovery.md) — intake and web-fetch recovery
