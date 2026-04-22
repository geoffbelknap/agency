import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "body"))

from images.body.body import (
    Body,
    _provider_tool_definitions,
    _provider_tool_prompt_section,
    _sanitize_outbound_content,
)


class _EmptyBuiltins:
    def get_tool_definitions(self):
        return []


def test_provider_web_search_declared_from_constraints(tmp_path):
    (tmp_path / "constraints.yaml").write_text(
        "agent: scout\n"
        "granted_capabilities:\n"
        "  - provider-web-search\n"
    )

    assert _provider_tool_definitions(tmp_path) == [{"type": "web_search"}]


def test_provider_web_search_declared_from_effective_provider_tools(tmp_path):
    (tmp_path / "constraints.yaml").write_text("agent: scout\ngranted_capabilities: []\n")
    (tmp_path / "provider-tools.yaml").write_text(
        "agent: scout\n"
        "grants:\n"
        "  - capability: provider-web-search\n"
        "    source: capabilities.yaml\n"
        "    granted_by: operator\n"
    )

    assert _provider_tool_definitions(tmp_path) == [{"type": "web_search"}]


def test_provider_tools_omitted_without_grant(tmp_path):
    (tmp_path / "constraints.yaml").write_text("agent: scout\ngranted_capabilities: []\n")

    assert _provider_tool_definitions(tmp_path) == []
    assert _provider_tool_prompt_section(tmp_path) == ""


def test_provider_tool_prompt_section_lists_web_search(tmp_path):
    (tmp_path / "provider-tools.yaml").write_text(
        "agent: scout\n"
        "grants:\n"
        "  - capability: provider-web-search\n"
    )

    section = _provider_tool_prompt_section(tmp_path)
    assert "# Provider Tools" in section
    assert "web_search" in section
    assert "do not simulate" in section


def test_sanitize_outbound_content_replaces_simulated_tool_markup():
    content = "I'll check.\n\n<search>\nquery: MSFT latest filing\n</search>\n\nweb_search(query: \"MSFT\")\n\nIt is X."

    sanitized = _sanitize_outbound_content(content)

    assert "<search>" not in sanitized
    assert "without guessing" in sanitized


def test_body_tool_collection_includes_provider_web_search(tmp_path):
    (tmp_path / "constraints.yaml").write_text(
        "agent: scout\n"
        "granted_capabilities:\n"
        "  - provider-web-search\n"
    )
    body = Body.__new__(Body)
    body.config_dir = Path(tmp_path)
    body._builtin_tools = _EmptyBuiltins()
    body._service_dispatcher = None
    body._mcp_tools = {}

    assert {"type": "web_search"} in body._get_all_tool_definitions()
