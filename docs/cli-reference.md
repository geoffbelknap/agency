---
title: "CLI Reference"
description: "Complete command reference for the agency CLI, with agent operations top-level and related operations grouped into subcommands."
---


Complete command reference for the `agency` CLI. All commands follow a Docker-style layout — agent operations are top-level, related operations are grouped into subcommands.

> Status: Mixed reference. The supported `0.2.x` core CLI path centers on
> `quickstart`, agent lifecycle, DM workflow, comms, credentials, policy,
> infrastructure, and core admin commands. Commands for teams, missions, hub,
> intake, notifications, and similar broader surfaces are experimental unless
> explicitly enabled.

## Setup

```bash
agency init                              # Initialize ~/.agency/
agency init --operator <name>            # Named operator identity
agency quickstart                        # Guided first-run core setup
agency quickstart --no-docker-start      # Skip Docker auto-start attempt
```

## Agent Lifecycle

```bash
agency create <name>                     # Create agent (default preset: generalist)
agency create <name> --preset <preset>   # Create with specific preset
agency create <name> --type <type>       # Create with specific type (standard|coordinator|function)
agency list                              # List all agents
agency list --active                     # List running agents only
agency show <name>                       # Agent details and status
agency start <name>                      # Start (seven-phase sequence)
agency stop <name>                       # Supervised halt (graceful)
agency stop <name> --immediate           # Immediate halt (SIGTERM)
agency stop <name> --emergency           # Emergency halt (SIGKILL)
agency resume <name>                     # Resume stopped agent
agency restart <name>                    # Full teardown + restart
agency delete <name>                     # Delete agent (must be stopped)
```

`coordinator` and `function` agent types remain experimental relative to the
default single-agent `0.2.x` path.

## Tasks and Observation

```bash
agency send <agent> "<message>"          # Send DM to agent
agency send <agent> --report "<message>" # Send with report request
agency send <channel> "<message>"        # Send to channel
agency log <name>                        # Current session audit log
agency log <name> --all                  # All sessions
agency log <name> --filter <category>    # Filter by category
agency status                            # Full system status
```

## Credentials

```bash
agency creds set --name KEY --value VAL --kind provider --scope platform --protocol api-key
agency creds list                        # List credentials (values redacted)
agency creds list --kind service         # Filter by kind
agency creds show <name>                 # Show credential details
agency creds show <name> --show-value    # Reveal actual secret (logged)
agency creds delete <name>               # Delete a credential
agency creds rotate <name> --value <new> # Rotate credential value
agency creds test <name>                 # Test credential connectivity
agency creds group create <name> --protocol jwt-exchange  # Create credential group
```

## Grants

```bash
agency grant <name> <service> --key-env <VAR>    # Grant service (key from env var)
agency grant <name> <service> --key-file <path>   # Grant service (key from file)
agency grant <name> <service> --key-stdin          # Grant service (key from stdin)
agency revoke <name> <service>                     # Revoke service access
```

## Channels

```bash
agency comms create <name>             # Create a channel
agency comms list                      # List all channels
agency comms send <channel> "<msg>"    # Send message to channel
agency comms read <channel>            # Read channel messages
agency comms read <channel> --limit N  # Read last N messages
agency comms search "<query>"          # Search across all channels
agency comms search "<query>" --channel <name>  # Search specific channel
```

## Teams

Experimental.

```bash
agency team create <name>                # Create a team
agency team list                         # List all teams
agency team show <name>                  # Team details and members
agency team activity <name>              # Team activity log
```

## Capabilities

```bash
agency cap list                          # List all registered capabilities
agency cap show <name>                   # Show agent's accessible capabilities
agency cap enable <service> --key $KEY   # Enable a service with API key
agency cap disable <service>             # Disable a service
agency cap add mcp <name> --command <cmd>    # Register MCP server
agency cap add api <name> --url <url>        # Register API service
agency cap delete <name>                     # Remove a capability
```

## Policy

```bash
agency policy check <name>               # Validate policy chain for agent
agency policy show <name>                # Show effective policy
agency policy template list              # List reusable policy templates
agency policy validate <file>            # Validate a policy file
agency policy exception grant <name> ... # Grant exception delegation
```

## Deploy and Teardown

Experimental pack/deployment workflow.

```bash
agency deploy <pack.yaml>               # Deploy a pack
agency teardown <pack-name>             # Tear down a deployed pack
```

## Intake

Experimental.

```bash
agency intake items                      # List work items
agency intake stats                      # Intake statistics
```

## Hub

Experimental broader distribution and registry surface. Core provider setup uses
the built-in provider catalog and `agency quickstart`.

```bash
agency hub search <query>                # Search available components
agency hub install <component>           # Install and activate from hub
agency hub list                          # List installed components
agency hub update                        # Refresh hub index
agency hub info <component>              # Component details
agency hub remove <component>            # Remove installed component
agency hub instances                     # List all active instances
agency hub <nameOrID> activate           # Activate an instance
agency hub <nameOrID> deactivate         # Deactivate an instance
agency hub <nameOrID> config             # Show/set instance config
```

## Infrastructure

```bash
agency infra up                          # Build + start shared infrastructure
agency infra down                        # Stop shared infrastructure
agency infra rebuild                     # Rebuild infrastructure images
agency infra status                      # Container and image status
agency infra reload                      # Hot-reload configurations
```

## Runtime

```bash
agency runtime manifest <agent>          # Persisted agent runtime contract
agency runtime status <agent>            # Projected agent runtime status
agency runtime validate <agent>          # Fail-closed runtime validation
```

## Notifications

Experimental.

```bash
agency notify list                       # List notification destinations
agency notify add <name> --url <url>     # Add notification destination
agency notify add <name> --url <url> --type ntfy --events operator_alert,error
agency notify remove <name>              # Remove notification destination
agency notify test <name>                # Send test notification
```

## Admin

```bash
agency admin doctor                      # Verify security guarantees
agency admin rebuild <agent>             # Regenerate all derived config files
agency admin trust show <name>           # Trust level for agent
agency admin trust list                  # All agents' trust levels
agency admin audit <name>                # Audit log queries
agency admin knowledge stats             # Knowledge graph statistics
agency admin department list             # List departments
agency admin department show <name>      # Department details
agency admin egress show <name>          # Egress rules for agent
agency admin destroy --yes               # Remove everything (preserves knowledge)
```

`trust` and `department` admin commands are experimental governance surfaces.

## Evaluation

```bash
agency eval list-tasks                   # List available eval tasks
agency eval run                          # Run full evaluation
agency eval run --tier <tier>            # Evaluate specific tier
agency eval report                       # View evaluation results
```

## Global Options

Most commands support these options:

| Option | Description |
|--------|------------|
| `--help` | Show help for any command |
| `--verbose` / `-v` | Verbose output |

## MCP Server

Agency also exposes all operations as MCP tools (85 tools) for AI assistants:

```bash
agency mcp-server                        # Start MCP server (stdio, Go native)
```

Once installed, an AI assistant can operate Agency through natural conversation using the same operations listed above.
