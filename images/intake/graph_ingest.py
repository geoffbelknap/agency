"""Graph ingest evaluation — maps event payloads to knowledge graph upserts.

Uses sandboxed Jinja2 templates for field extraction. Writes to the
knowledge store HTTP API with source_type "rule" and provenance tracking.
"""
import logging
from types import SimpleNamespace

import httpx
from jinja2.sandbox import SandboxedEnvironment, Undefined

from typing import Optional
from .router import match_route
from images.models.connector import GraphIngestRule

logger = logging.getLogger(__name__)
class _SilentUndefined(Undefined):
    """Undefined that returns empty string instead of raising."""
    def __str__(self):
        return ""
    def __getattr__(self, name):
        return self
    def __iter__(self):
        return iter([])
    def __bool__(self):
        return False

_env = SandboxedEnvironment(undefined=_SilentUndefined)
_http = httpx.Client(timeout=10)


def render_sandboxed_template(template_str: str, context: dict) -> str:
    """Render a Jinja2 template in a sandboxed environment.
    Only dict access allowed — no dunder access, no function calls.
    """
    tmpl = _env.from_string(template_str)
    return tmpl.render(**context)


def _post_node(knowledge_url: str, node: dict) -> Optional[str]:
    try:
        resp = _http.post(f"{knowledge_url}/ingest/nodes", json={"nodes": [node]})
        if resp.status_code < 300:
            data = resp.json()
            # Return node ID if the API provides it; ingested count > 0 means success
            return data.get("id") or (node["label"] if data.get("ingested", 0) > 0 else None)
        logger.warning("graph_ingest node upsert failed: %d %s", resp.status_code, resp.text)
    except Exception as e:
        logger.warning("graph_ingest node upsert error: %s", e)
    return None


def _post_edge(knowledge_url: str, source_label: str, target_label: str, relation: str) -> bool:
    """Post an edge using label-based resolution (knowledge API resolves labels to IDs)."""
    try:
        edge = {"source_label": source_label, "target_label": target_label, "relation": relation}
        resp = _http.post(f"{knowledge_url}/ingest/edges", json={"edges": [edge]})
        if resp.status_code < 300:
            return True
        logger.warning("graph_ingest edge upsert failed: %d %s", resp.status_code, resp.text)
    except Exception as e:
        logger.warning("graph_ingest edge upsert error: %s", e)
    return False


def evaluate_graph_ingest(
    rules: list[GraphIngestRule],
    payload: dict,
    knowledge_url: str,
    connector_name: str,
    work_item_id: str,
    event_buffer=None,
) -> int:
    """Evaluate graph_ingest rules against a payload. Returns count of upserted nodes."""
    count = 0
    for rule in rules:
        # Check match filter
        if rule.match is not None:
            # match_route expects an object with a .match attribute
            route_like = SimpleNamespace(match=rule.match)
            if not match_route(route_like, payload):
                continue

        # Correlation lookup (Feature 3 will extend this)
        correlated = None
        if hasattr(rule, 'correlate') and rule.correlate and event_buffer:
            join_value = payload.get(rule.correlate.on)
            if join_value is not None:
                correlated = event_buffer.lookup(
                    rule.correlate.source, rule.correlate.on, join_value, rule.correlate.window_seconds
                )
            if correlated is None:
                continue  # Skip — no correlation match

        # Build template context
        ctx = {"payload": payload}
        if correlated:
            ctx["correlated"] = correlated

        # Render and upsert nodes
        node_ids = {}
        for node_def in rule.nodes:
            label = render_sandboxed_template(node_def.label, ctx) if "{{" in node_def.label else node_def.label
            props = {}
            for k, v in node_def.properties.items():
                props[k] = render_sandboxed_template(v, ctx) if "{{" in v else v
            props["_provenance_connector"] = connector_name
            props["_provenance_work_item"] = work_item_id
            node = {"label": label, "kind": node_def.kind, "summary": "", "source_type": "rule", "properties": props}
            node_id = _post_node(knowledge_url, node)
            if node_id:
                node_ids[label] = node_id
                count += 1

        # Render and upsert edges using label-based resolution
        for edge_def in rule.edges:
            from_label = render_sandboxed_template(edge_def.from_label, ctx) if "{{" in edge_def.from_label else edge_def.from_label
            to_label = render_sandboxed_template(edge_def.to_label, ctx) if "{{" in edge_def.to_label else edge_def.to_label
            if from_label and to_label:
                _post_edge(knowledge_url, from_label, to_label, edge_def.relation)

    return count
