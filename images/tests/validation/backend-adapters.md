# Backend Adapter Validation

Use this lane for Docker, Podman, containerd, or Apple Container host adapter
work. Backend checks validate adapter hygiene; they do not replace the
backend-neutral runtime contract.

## Required Common Checks

```bash
go test ./...
./scripts/readiness/runtime-contract-smoke.sh --agent <agent>
agency admin doctor
```

Expected:

- Runtime manifest/status/validate succeeds.
- Doctor reports generic runtime health correctly.
- Backend warnings are labeled as backend hygiene.

## Docker

```bash
./scripts/readiness/docker-readiness-check.sh
```

Use Docker-native inspection only inside this lane, for example socket,
network, capability, readonly filesystem, and cleanup checks.

## Podman

```bash
./scripts/readiness/podman-readiness-check.sh
./scripts/readiness/podman-readiness-check.sh --full
```

Expected:

- Runtime status reports `backend=podman`.
- Podman-specific socket and network behavior does not leak into generic
  runtime validation.

## Containerd

```bash
./scripts/readiness/containerd-rootless-readiness-check.sh
./scripts/readiness/containerd-rootful-readiness-check.sh
```

Expected:

- Runtime status reports `backend=containerd`.
- Backend mode and endpoint are projected clearly.
- Rootless and rootful assumptions stay explicit.

## Apple Container

Apple Container is experimental and opt-in.

```bash
./scripts/readiness/apple-container-smoke.sh
```

Do not make this a default backend, required CI lane, branch-protection check,
or release-blocking path until lifecycle, event, network, cleanup, and doctor
semantics are complete.
