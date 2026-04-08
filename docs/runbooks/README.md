# Operator Runbooks

Procedural guides for operating the Agency platform. Each runbook follows a consistent format: trigger condition, steps, verification, rollback.

## Index

### Setup & Maintenance

| Runbook | When to Use |
|---------|------------|
| [Initial Setup](initial-setup.md) | First-time deployment or fresh environment |
| [Upgrade](upgrade.md) | Upgrading Agency to a new version |
| [Backup & Restore](backup-restore.md) | Scheduled backup, disaster recovery, state migration |
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
