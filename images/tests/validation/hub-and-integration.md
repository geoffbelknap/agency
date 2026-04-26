# Hub And Integration Validation

Use this lane for hub catalog, OCI sync, connectors, packs, intake, provider
tools, and integration setup flows.

## Automated Lane

```bash
go test ./internal/hub ./internal/api ./internal/models
./scripts/test-live-hub-operator-oci.sh
```

For broader hub scenarios:

```bash
./scripts/test-live-hub-oci.sh
./scripts/test-live-hub-pack-operator-oci.sh
./scripts/test-live-hub-upgrade-oci.sh
```

## Hub Expectations

- Hub update/search/list/info are available through gateway surfaces.
- Hub-managed files are not edited directly when the customization point is
  elsewhere.
- Install/remove/upgrade operations are audited.
- Signature verification is exercised when `cosign` is installed.

## Connectors And Intake

Required observations:

- Connector schemas validate strictly.
- Connector activation is explicit and reversible.
- Inbound events preserve provenance and principal context.
- Route delivery is mediated through gateway/event surfaces.
- Intake items are visible and reviewable.

## Provider Tools

Required observations:

- Provider tools are declared from constraints/effective grants.
- Tool use remains externally mediated and auditable.
- Simulated tool claims without evidence are blocked by the body/honesty path.
