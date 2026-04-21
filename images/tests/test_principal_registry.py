"""Tests for the PrincipalRegistry snapshot-based module."""

import uuid

import pytest

import sys
import os
sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "knowledge"))
from principal_registry import PrincipalRegistry


SAMPLE_SNAPSHOT = {
    "principals": [
        {
            "uuid": "aaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
            "type": "operator",
            "name": "alice",
            "created_at": "2026-01-01T00:00:00Z",
            "metadata": {"email": "alice@example.com"},
        },
        {
            "uuid": "1111-2222-3333-4444-555555555555",
            "type": "agent",
            "name": "scout",
            "created_at": "2026-01-02T00:00:00Z",
            "metadata": {},
        },
        {
            "uuid": "6666-7777-8888-9999-aaaaaaaaaaaa",
            "type": "agent",
            "name": "analyst",
            "created_at": "2026-01-03T00:00:00Z",
            "metadata": {},
        },
        {
            "uuid": "bbbb-cccc-dddd-eeee-ffffffffffff",
            "type": "channel",
            "name": "general",
            "created_at": "2026-01-04T00:00:00Z",
            "metadata": {},
        },
    ]
}


@pytest.fixture
def registry():
    """Create a PrincipalRegistry from snapshot data."""
    return PrincipalRegistry(snapshot_data=SAMPLE_SNAPSHOT)


@pytest.fixture
def empty_registry():
    """Create an empty PrincipalRegistry."""
    return PrincipalRegistry()


class TestLoadData:
    def test_load_from_snapshot_data(self, registry):
        assert len(registry.list_all()) == 4

    def test_empty_registry(self, empty_registry):
        assert empty_registry.list_all() == []

    def test_load_empty_principals(self):
        reg = PrincipalRegistry(snapshot_data={"principals": []})
        assert reg.list_all() == []

    def test_load_null_principals(self):
        reg = PrincipalRegistry(snapshot_data={"principals": None})
        assert reg.list_all() == []

    def test_load_missing_principals_key(self):
        reg = PrincipalRegistry(snapshot_data={})
        assert reg.list_all() == []


class TestResolve:
    def test_resolve_existing(self, registry):
        result = registry.resolve("1111-2222-3333-4444-555555555555")
        assert result is not None
        assert result["uuid"] == "1111-2222-3333-4444-555555555555"
        assert result["type"] == "agent"
        assert result["name"] == "scout"

    def test_resolve_unknown_returns_none(self, registry):
        assert registry.resolve("nonexistent-uuid") is None

    def test_resolve_with_metadata(self, registry):
        result = registry.resolve("aaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
        assert result["metadata"] == {"email": "alice@example.com"}


class TestResolveName:
    def test_resolve_name_existing(self, registry):
        result = registry.resolve_name("agent", "scout")
        assert result == "1111-2222-3333-4444-555555555555"

    def test_resolve_name_unknown_returns_none(self, registry):
        assert registry.resolve_name("team", "nonexistent") is None

    def test_resolve_name_wrong_type(self, registry):
        # "alice" is an operator, not an agent
        assert registry.resolve_name("agent", "alice") is None


class TestListByType:
    def test_list_agents(self, registry):
        agents = registry.list_by_type("agent")
        assert len(agents) == 2
        names = {a["name"] for a in agents}
        assert names == {"scout", "analyst"}

    def test_list_operators(self, registry):
        ops = registry.list_by_type("operator")
        assert len(ops) == 1
        assert ops[0]["name"] == "alice"

    def test_list_empty_type(self, registry):
        assert registry.list_by_type("role") == []


class TestListAll:
    def test_list_all(self, registry):
        all_principals = registry.list_all()
        assert len(all_principals) == 4

    def test_list_all_empty(self, empty_registry):
        assert empty_registry.list_all() == []


class TestFormatId:
    def test_format_id(self):
        uid = str(uuid.uuid4())
        result = PrincipalRegistry.format_id("agent", uid)
        assert result == f"agent:{uid}"


class TestParseId:
    def test_parse_uuid_format(self, registry):
        uid = "aaaa-bbbb-cccc-dddd-eeee-ffffffffffff"
        # 36 chars with 4 dashes = UUID-like
        principal_id = f"operator:{uid}"
        ptype, pval = registry.parse_id(principal_id)
        assert ptype == "operator"
        assert pval == uid

    def test_parse_name_resolvable(self, registry):
        ptype, pval = registry.parse_id("agent:scout")
        assert ptype == "agent"
        assert pval == "1111-2222-3333-4444-555555555555"

    def test_parse_name_unresolvable(self, registry):
        ptype, pval = registry.parse_id("agent:unknown-agent")
        assert ptype == "agent"
        assert pval == "unknown-agent"

    def test_parse_invalid_format(self, registry):
        with pytest.raises(ValueError, match="Invalid principal ID"):
            registry.parse_id("no-colon-here")


class TestLoadFile:
    def test_load_nonexistent_file(self, tmp_path):
        reg = PrincipalRegistry(snapshot_path=str(tmp_path / "missing.json"))
        assert reg.list_all() == []

    def test_load_file(self, tmp_path):
        import json
        path = tmp_path / "registry.json"
        path.write_text(json.dumps(SAMPLE_SNAPSHOT))
        reg = PrincipalRegistry(snapshot_path=str(path))
        assert len(reg.list_all()) == 4
        assert reg.resolve_name("operator", "alice") == "aaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
