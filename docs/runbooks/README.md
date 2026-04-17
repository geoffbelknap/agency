# Operator Runbooks

Procedural guides for operating the Agency platform. Each runbook follows a consistent format: trigger condition, steps, verification, rollback.

Status note:
- runbooks without a qualifier apply to the supported core `0.2.x` path
- some runbooks cover experimental or internal operator workflows and are marked explicitly on the page

## Recommended Validation Paths

Use these as the default validation sequence after a change:

- New runtime or host adapter:
  [Backend Adapter Release Checklist](backend-adapter-release-checklist.md)
  -> [Runtime Smoke](runtime-smoke.md)
  -> [Validation Checklist](validation-checklist.md)
  Podman shortcut: `make podman-readiness` for CI smoke, `make podman-readiness-full` for release validation, or manual GitHub dispatch of `Podman Readiness` with `full_e2e=true`
- Runtime, lifecycle, transport, or manifest changes:
  [Runtime Smoke](runtime-smoke.md) -> [Validation Checklist](validation-checklist.md) -> [Agent Recovery](agent-recovery.md)
- Web, operator, DM, or comms changes:
  [Validation Checklist](validation-checklist.md) with the disposable live web E2E section, then [Monitoring & Observability](monitoring-and-observability.md)
- Infrastructure or Docker hygiene changes:
  [Initial Setup](initial-setup.md) or [Upgrade](upgrade.md), then [Validation Checklist](validation-checklist.md), then [Infrastructure Recovery](infrastructure-recovery.md) if anything degrades

## Index

### Setup & Maintenance

| Runbook | When to Use |
|---------|------------|
| [Initial Setup](initial-setup.md) | First-time deployment or fresh environment |
| [Upgrade](upgrade.md) | Upgrading Agency to a new version |
| [Backup & Restore](backup-restore.md) | Scheduled backup, disaster recovery, state migration |
| [Backend Adapter Release Checklist](backend-adapter-release-checklist.md) | Release-style validation gate for Docker, Podman, and future adapters |
| [Runtime Smoke](runtime-smoke.md) | Runtime-contract validation for start, restart, status, transport, and manifest persistence |
| [Validation Checklist](validation-checklist.md) | Post-deployment, post-upgrade, or periodic health verification |

### Operations

| Runbook | When to Use |
|---------|------------|
| [Mission Management](mission-management.md) | Creating, configuring, assigning, or troubleshooting missions |
| [Knowledge Management](knowledge-management.md) | Graph ingestion, classification, ontology, communities, quarantine |
| [Hub & Capabilities](hub-and-capabilities.md) | Installing components, managing capabilities, presets, web-fetch |
| [Routing & Providers](routing-and-providers.md) | Adding providers, configuring tiers, routing optimizer |
| [Budget & Cost](budget-and-cost.md) | Budget configuration, cost attribution, economics reporting |
| [Notifications & Webhooks](notifications-and-webhooks.md) | Alerting destinations, inbound webhooks, event subscriptions |
| [Monitoring & Observability](monitoring-and-observability.md) | Trajectory monitoring, signals, meeseeks, semantic cache, audit |
| [Principal Management](principal-management.md) | Registry CRUD, suspension/revocation, authority hierarchy |

### Incident Response & Recovery

| Runbook | When to Use |
|---------|------------|
| [Infrastructure Recovery](infrastructure-recovery.md) | Infra containers down, network issues, Docker problems |
| [Agent Recovery](agent-recovery.md) | Agent crashed, stuck, unresponsive, or corrupted |
| [Credential Rotation](credential-rotation.md) | Scheduled rotation, compromised key, or expired credential |
| [Security Incident Response](security-incident-response.md) | Suspected agent compromise, XPIA detection, anomalous behavior |
