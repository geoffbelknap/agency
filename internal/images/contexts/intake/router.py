"""Connector routing engine — match rules and render templates."""

import re
from datetime import timedelta

_RE_PREFIX = "re:"

from jinja2 import BaseLoader, Environment, TemplateSyntaxError, Undefined

from agency_core.models.connector import ConnectorRoute

_jinja_env = Environment(loader=BaseLoader(), undefined=Undefined)


def match_route(route: ConnectorRoute, payload: dict) -> bool:
    """Check if a payload matches a route's match rules. All fields must match (AND).

    Pattern semantics:
      None        → field must be absent or null (no real value)
      "*"         → field must be present with a non-null value
      "re:<pat>"  → field value must match regex <pat> (search, not full match)
      list        → field value must be in the list
      str         → field value must equal the string
    """
    for field, pattern in route.match.items():
        value = payload.get(field)
        if pattern is None:
            # Require field to be absent or null
            if value is not None:
                return False
            continue
        if value is None:
            return False
        if pattern == "*":
            continue
        if isinstance(pattern, str) and pattern.startswith(_RE_PREFIX):
            regex = pattern[len(_RE_PREFIX):]
            if not re.search(regex, str(value)):
                return False
        elif isinstance(pattern, list):
            if value not in pattern:
                return False
        else:
            if value != pattern:
                return False
    return True


def evaluate_routes(
    routes: list[ConnectorRoute], payload: dict
) -> tuple[int, ConnectorRoute] | None:
    """Evaluate routes in order. Returns (index, route) for first match, or None."""
    for i, route in enumerate(routes):
        if match_route(route, payload):
            return (i, route)
    return None


def render_template(template_str: str, payload: dict) -> str:
    """Render a Jinja2 template with payload fields.

    Missing fields render as empty string. Invalid templates return raw text.
    """
    try:
        template = _jinja_env.from_string(template_str)
        return template.render(**payload)
    except (TemplateSyntaxError, Exception):
        return template_str


_SLA_PATTERN = re.compile(r"^(\d+)([mhd])$")


def parse_sla_duration(sla: str | None) -> timedelta | None:
    """Parse SLA duration string (e.g. '15m', '2h', '1d') into timedelta."""
    if sla is None:
        return None
    m = _SLA_PATTERN.match(sla)
    if not m:
        return None
    value, unit = int(m.group(1)), m.group(2)
    if unit == "m":
        return timedelta(minutes=value)
    elif unit == "h":
        return timedelta(hours=value)
    elif unit == "d":
        return timedelta(days=value)
    return None
