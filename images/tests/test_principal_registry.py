"""Tests for the PrincipalRegistry module."""

import json
import sqlite3
import uuid

import pytest

from knowledge.principal_registry import PrincipalRegistry


@pytest.fixture
def registry():
    """Create a PrincipalRegistry with an in-memory database."""
    db = sqlite3.connect(":memory:")
    return PrincipalRegistry(db)


class TestRegister:
    def test_returns_uuid(self, registry):
        result = registry.register("operator", "alice")
        # Should be a valid UUID4
        parsed = uuid.UUID(result, version=4)
        assert str(parsed) == result

    def test_idempotent(self, registry):
        first = registry.register("operator", "alice")
        second = registry.register("operator", "alice")
        assert first == second

    def test_different_types_different_uuids(self, registry):
        op_uuid = registry.register("operator", "alice")
        agent_uuid = registry.register("agent", "alice")
        assert op_uuid != agent_uuid

    def test_different_names_different_uuids(self, registry):
        a = registry.register("operator", "alice")
        b = registry.register("operator", "bob")
        assert a != b

    def test_invalid_type_raises(self, registry):
        with pytest.raises(ValueError, match="Invalid principal type"):
            registry.register("invalid_type", "alice")

    def test_all_valid_types(self, registry):
        for t in PrincipalRegistry.VALID_TYPES:
            result = registry.register(t, f"test-{t}")
            assert result is not None

    def test_with_metadata(self, registry):
        meta = {"email": "alice@example.com", "department": "eng"}
        uid = registry.register("operator", "alice", metadata=meta)
        resolved = registry.resolve(uid)
        assert resolved["metadata"] == meta


class TestResolve:
    def test_resolve_existing(self, registry):
        uid = registry.register("agent", "scout")
        result = registry.resolve(uid)
        assert result["uuid"] == uid
        assert result["type"] == "agent"
        assert result["name"] == "scout"
        assert "created_at" in result
        assert result["metadata"] == {}

    def test_resolve_unknown_returns_none(self, registry):
        fake_uuid = str(uuid.uuid4())
        assert registry.resolve(fake_uuid) is None


class TestResolveName:
    def test_resolve_name_existing(self, registry):
        uid = registry.register("team", "security")
        result = registry.resolve_name("team", "security")
        assert result == uid

    def test_resolve_name_unknown_returns_none(self, registry):
        assert registry.resolve_name("team", "nonexistent") is None


class TestListByType:
    def test_list_by_type(self, registry):
        registry.register("agent", "scout")
        registry.register("agent", "analyst")
        registry.register("operator", "alice")

        agents = registry.list_by_type("agent")
        assert len(agents) == 2
        names = {a["name"] for a in agents}
        assert names == {"scout", "analyst"}

    def test_list_by_type_empty(self, registry):
        assert registry.list_by_type("role") == []


class TestListAll:
    def test_list_all(self, registry):
        registry.register("operator", "alice")
        registry.register("agent", "scout")
        registry.register("channel", "general")

        all_principals = registry.list_all()
        assert len(all_principals) == 3

    def test_list_all_empty(self, registry):
        assert registry.list_all() == []


class TestFormatId:
    def test_format_id(self, registry):
        uid = str(uuid.uuid4())
        result = registry.format_id("agent", uid)
        assert result == f"agent:{uid}"


class TestParseId:
    def test_parse_uuid_format(self, registry):
        uid = str(uuid.uuid4())
        principal_id = f"operator:{uid}"
        ptype, pval = registry.parse_id(principal_id)
        assert ptype == "operator"
        assert pval == uid

    def test_parse_legacy_name_resolvable(self, registry):
        uid = registry.register("agent", "scout")
        ptype, pval = registry.parse_id("agent:scout")
        assert ptype == "agent"
        assert pval == uid

    def test_parse_legacy_name_unresolvable(self, registry):
        ptype, pval = registry.parse_id("agent:unknown-agent")
        assert ptype == "agent"
        assert pval == "unknown-agent"

    def test_parse_invalid_format(self, registry):
        with pytest.raises(ValueError, match="Invalid principal_id format"):
            registry.parse_id("no-colon-here")
