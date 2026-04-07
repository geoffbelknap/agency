# Operator Runbooks

Procedural guides for operating the Agency platform. Each runbook follows a consistent format: trigger condition, steps, verification, rollback.

## Index

| Runbook | When to Use |
|---------|------------|
| [Initial Setup](initial-setup.md) | First-time deployment or fresh environment |
| [Upgrade](upgrade.md) | Upgrading Agency to a new version |
| [Infrastructure Recovery](infrastructure-recovery.md) | Infra containers down, network issues, Docker problems |
| [Agent Recovery](agent-recovery.md) | Agent crashed, stuck, unresponsive, or corrupted |
| [Credential Rotation](credential-rotation.md) | Scheduled rotation, compromised key, or expired credential |
| [Security Incident Response](security-incident-response.md) | Suspected agent compromise, XPIA detection, anomalous behavior |
| [Backup & Restore](backup-restore.md) | Scheduled backup, disaster recovery, state migration |
| [Validation Checklist](validation-checklist.md) | Post-deployment, post-upgrade, or periodic health verification |
