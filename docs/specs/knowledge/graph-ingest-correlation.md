---
description: "Graph_ingest can only create nodes and edges from data within a single payload. When multiple connectors describe fac..."
status: "Implemented"
---

# Graph Ingest Cross-Source Correlation

**Date:** 2026-03-29
**Status:** Implemented

## Problem

Graph_ingest can only create nodes and edges from data within a single payload. When multiple connectors describe facets of the same infrastructure (e.g., UniFi devices, hosts, and sites APIs), there's no way to create edges between nodes from different connectors because the payloads use different identifiers for the same entity.

Example: `unifi-sites` has `hostId` but not `hostName`. The `unifi` connector creates Device nodes labeled by `hostName`. An edge from the site's network_segment to the device can't resolve because the labels don't match.

## Existing Infrastructure

The `EventBuffer` (`images/intake/correlation.py`) already exists for cross-source correlation. It:
- Records payloads per connector with timestamps
- Supports `lookup(connector_name, field, value, window_seconds)` to find matching payloads from other connectors
- Has lazy eviction of expired entries
- Is already wired into `_route_and_deliver()` — every payload is recorded before routing

The `CorrelateConfig` model exists in `agency_core/models/connector.py`:
```python
class CorrelateConfig(BaseModel):
    source: str           # connector name to look up
    on: str               # dot-path field to match
    window_seconds: int   # time window for correlation
```

And `graph_ingest.py` already checks `rule.correlate` — but currently only uses it for match gating (skip rule if no correlation match), not for template variable injection.

## Design

Extend graph_ingest correlation to inject matched payload fields into the Jinja2 template context, enabling cross-connector label resolution.

### Connector YAML

Add a `correlate` block to graph_ingest rules. When present, the matched payload is available as `{{correlated.*}}` in templates alongside `{{payload.*}}`.

```yaml
# unifi-sites connector
graph_ingest:
  - correlate:
      source: unifi           # look up recent payloads from the unifi connector
      on: hostId              # match this field in both payloads
      window_seconds: 3600    # within the last hour
    nodes:
      - kind: network_segment
        label: "{{payload.meta.desc or payload.meta.name}}"
        properties:
          site_id: "{{payload.siteId}}"
          host_id: "{{payload.hostId}}"
          source: "unifi"
    edges:
      - relation: ON_SEGMENT
        from_kind: Device
        # Use the correlated payload's hostName (from unifi connector)
        # Falls back to hostId if no correlation match
        from_label: "{{correlated.hostName or payload.hostId}}"
        to_kind: network_segment
        to_label: "{{payload.meta.desc or payload.meta.name}}"
```

### How It Works

1. Graph_ingest evaluates a rule with a `correlate` block
2. Calls `event_buffer.lookup(correlate.source, correlate.on, payload[correlate.on], correlate.window_seconds)`
3. If a match is found, the matched payload is available as `correlated` in the Jinja2 template context
4. If no match is found, `correlated` is an empty dict — `{{correlated.hostName}}` renders as empty string, and the `or` fallback activates

### Implementation

**File:** `images/intake/graph_ingest.py`

In `evaluate_graph_ingest()`, the correlation lookup already happens (for match gating). Extend it to pass the correlated payload into the Jinja2 render context:

```python
# Current: correlation only gates the match
if rule.correlate:
    match = buffer.lookup(rule.correlate.source, rule.correlate.on,
                          _resolve_dot_path(payload, rule.correlate.on),
                          rule.correlate.window_seconds)
    if match is None:
        continue  # skip rule

# New: pass correlated payload into template context
correlated = {}
if rule.correlate and event_buffer:
    match_val = _resolve_dot_path(payload, rule.correlate.on)
    if match_val is not None:
        match = event_buffer.lookup(rule.correlate.source, rule.correlate.on,
                                     match_val, rule.correlate.window_seconds)
        if match:
            correlated = match  # the full matched payload

# Pass to Jinja2 templates
context = {"payload": payload, "correlated": correlated}
```

Then update `_render_template()` to accept the full context dict instead of just `payload`:

```python
# Current
label = _render_template(node_def["label"], payload)

# New
label = _render_template(node_def["label"], context)
```

And the Jinja2 render uses `**context` instead of `payload=payload`.

### CorrelateConfig Model

Already exists in `agency_core/models/connector.py`. No changes needed:

```python
class CorrelateConfig(BaseModel):
    source: str
    on: str
    window_seconds: int = 300
```

### Scope

**In scope:**
- Inject correlated payload into Jinja2 template context as `{{correlated.*}}`
- Fallback to empty dict when no correlation match
- Works with existing `EventBuffer.lookup()`

**Not in scope:**
- Multi-source correlation (correlating across 3+ connectors)
- Correlation on computed/derived fields
- Persistent correlation store (EventBuffer is in-memory, time-windowed)
- Changes to the EventBuffer itself

### Test Plan

1. Unit test: graph_ingest with correlate block, correlated payload injected into templates
2. Unit test: no correlation match → `{{correlated.field}}` renders empty, `or` fallback works
3. Integration test: two connectors polling → second connector's graph_ingest uses first connector's data via correlation

### Affected Files

- Modify: `images/intake/graph_ingest.py` — inject correlated context
- Modify: `agency_core/models/connector.py` — no changes needed (model already exists)
- Test: `tests/test_graph_ingest.py` — add correlation template tests
