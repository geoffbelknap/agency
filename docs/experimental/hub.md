---
title: "Hub"
description: "Experimental package and component registry for sharing packs, presets, and connectors."
---


The Hub is a git-backed component registry for sharing and installing packs, presets, and connectors. Think of it as a package manager for Agency components.

> Experimental surface: the Hub remains in active development, but it is not
> part of the default `0.2.x` core Agency contract. Core Agency should be
> understandable and usable without depending on Hub lifecycle flows.

## Searching

Find available components:

```bash
agency hub search security              # Search by keyword
agency hub search connector             # Find connectors
agency hub search pack                   # Find packs
```

Results show component name, type, version, and description.

## Installing

```bash
agency hub install red-team
```

This downloads the component to `~/.agency/hub-cache/` and resolves any transitive dependencies. If a pack requires a connector, both are installed.

### What Gets Installed

| Component Type | Installed To | What Happens |
|---------------|-------------|-------------|
| **Pack** | `~/.agency/hub-cache/<name>/` | Ready to deploy with `agency deploy` |
| **Preset** | `~/.agency/presets/<name>.yaml` | Available as `--preset <name>` |
| **Connector** | `~/.agency/hub-cache/<name>/` | Ready to activate |

## Listing Installed Components

```bash
agency hub list
```

Shows all installed components, their type, version, and install date.

## Component Details

```bash
agency hub info red-team
```

Shows full details: description, version, dependencies, files, and usage instructions.

## Updating

```bash
agency hub update
```

Refreshes the hub index from configured sources. This syncs the git repository to get the latest available components.

## Removing

```bash
agency hub remove red-team
```

Removes the component from the local cache. Does not affect currently deployed agents — you need to teardown first.

## Using Installed Components

### Deploy a Pack

```bash
agency deploy ~/.agency/hub-cache/red-team/pack.yaml
```

### Use an Installed Preset

```bash
agency create my-agent --preset custom-preset-from-hub
```

### Activate an Installed Connector

```bash
agency connector activate slack-events
```

## Hub Sources

The hub pulls from git repositories configured in `~/.agency/config.yaml`:

```yaml
hub:
  sources:
    - name: default
      url: https://github.com/your-org/agency-hub.git
      branch: main
    - name: internal
      url: git@github.com:your-company/agency-components.git
      branch: main
```

You can configure multiple sources — the hub searches across all of them.

## Component Structure

### Pack

```
my-pack/
├── pack.yaml           # Pack definition
└── README.md           # Usage instructions (optional)
```

### Preset

```
my-preset/
└── preset.yaml         # Preset definition
```

### Connector

```
my-connector/
├── connector.yaml      # Connector definition
└── README.md           # Setup instructions (optional)
```

## Dependency Resolution

The hub resolves transitive dependencies automatically. If pack A requires connector B, installing pack A also installs connector B.

Dependencies are declared in the pack's `requires` field:

```yaml
requires:
  - slack-events          # Connector dependency
  - brave-search          # Service dependency
```

The hub checks that all dependencies are satisfiable before installing.
