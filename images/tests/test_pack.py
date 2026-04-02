"""Tests for pack schema validation."""

import pytest
import yaml

from images.models.pack import (
    PackAgent,
    PackChannel,
    PackConfig,
    PackCredential,
    PackRequires,
    PackTeam,
)


class TestPackAgent:
    def test_minimal(self):
        agent = PackAgent(name="dev", preset="generalist")
        assert agent.name == "dev"
        assert agent.preset == "generalist"
        assert agent.role == "standard"
        assert agent.skills == []
        assert agent.connectors == []
        assert agent.workspace is None
        assert agent.agent_type is None

    def test_full(self):
        agent = PackAgent(
            name="lead",
            preset="soc-lead",
            workspace="soc-ws",
            role="coordinator",
            agent_type="security-reviewer",
            skills=["threat-analysis"],
            connectors=["splunk-soc"],
        )
        assert agent.role == "coordinator"
        assert agent.skills == ["threat-analysis"]

    def test_invalid_role(self):
        with pytest.raises(Exception):
            PackAgent(name="x", preset="y", role="invalid")

    def test_agent_def_accepts_host(self):
        agent = PackAgent(name="researcher", preset="researcher", host="worker-1")
        assert agent.host == "worker-1"

    def test_agent_def_host_optional(self):
        agent = PackAgent(name="researcher", preset="researcher")
        assert agent.host is None


class TestPackChannel:
    def test_defaults(self):
        ch = PackChannel(name="general")
        assert ch.topic == ""
        assert ch.private is False

    def test_private(self):
        ch = PackChannel(name="secret", topic="classified", private=True)
        assert ch.private is True


class TestPackCredential:
    def test_defaults(self):
        cred = PackCredential(name="anthropic")
        assert cred.required is True
        assert cred.description == ""

    def test_optional(self):
        cred = PackCredential(name="slack", required=False, description="Slack token")
        assert cred.required is False


class TestPackRequires:
    def test_empty(self):
        req = PackRequires()
        assert req.presets == []
        assert req.connectors == []
        assert req.skills == []
        assert req.workspaces == []
        assert req.policies == []

    def test_populated(self):
        req = PackRequires(presets=["a", "b"], skills=["c"])
        assert req.presets == ["a", "b"]


class TestPackTeam:
    def test_minimal(self):
        team = PackTeam(
            name="test-team",
            agents=[PackAgent(name="dev", preset="generalist")],
        )
        assert team.name == "test-team"
        assert len(team.agents) == 1
        assert team.channels == []

    def test_with_channels(self):
        team = PackTeam(
            name="test-team",
            agents=[PackAgent(name="dev", preset="generalist")],
            channels=[PackChannel(name="general")],
        )
        assert len(team.channels) == 1


class TestPackConfig:
    def test_minimal(self):
        config = PackConfig(
            name="test-pack",
            team=PackTeam(
                name="test-team",
                agents=[PackAgent(name="dev", preset="generalist")],
            ),
        )
        assert config.kind == "pack"
        assert config.name == "test-pack"
        assert config.version == "1.0.0"
        assert config.credentials == []
        assert config.policy is None

    def test_full_yaml_roundtrip(self):
        raw = {
            "kind": "pack",
            "name": "soc-ops",
            "version": "2.0.0",
            "description": "SOC operations",
            "author": "acme",
            "requires": {
                "presets": ["soc-lead", "soc-analyst"],
                "skills": ["threat-analysis"],
            },
            "team": {
                "name": "soc-team",
                "agents": [
                    {
                        "name": "lead",
                        "preset": "soc-lead",
                        "role": "coordinator",
                        "skills": ["threat-analysis"],
                    },
                    {
                        "name": "analyst-1",
                        "preset": "soc-analyst",
                        "role": "standard",
                    },
                ],
                "channels": [
                    {"name": "soc-alerts", "topic": "Alert triage"},
                ],
            },
            "credentials": [
                {"name": "anthropic", "required": True},
            ],
            "policy": {"parameters": {"max_turns": 50}},
        }
        config = PackConfig.model_validate(raw)
        assert config.name == "soc-ops"
        assert len(config.team.agents) == 2
        assert config.team.agents[0].role == "coordinator"
        assert config.requires.presets == ["soc-lead", "soc-analyst"]
        assert config.credentials[0].name == "anthropic"

    def test_wrong_kind_rejected(self):
        with pytest.raises(Exception):
            PackConfig(
                kind="connector",
                name="test",
                team=PackTeam(
                    name="t",
                    agents=[PackAgent(name="d", preset="g")],
                ),
            )

    def test_extra_fields_rejected(self):
        with pytest.raises(Exception):
            PackConfig(
                name="test",
                team=PackTeam(
                    name="t",
                    agents=[PackAgent(name="d", preset="g")],
                ),
                unknown_field="bad",
            )

    def test_empty_agents_rejected(self):
        with pytest.raises(Exception):
            PackConfig(
                name="test",
                team=PackTeam(name="t", agents=[]),
            )

    def test_duplicate_agent_names_rejected(self):
        with pytest.raises(Exception):
            PackConfig(
                name="test",
                team=PackTeam(
                    name="t",
                    agents=[
                        PackAgent(name="dev", preset="generalist"),
                        PackAgent(name="dev", preset="generalist"),
                    ],
                ),
            )

    def test_duplicate_channel_names_rejected(self):
        with pytest.raises(Exception):
            PackConfig(
                name="test",
                team=PackTeam(
                    name="t",
                    agents=[PackAgent(name="dev", preset="generalist")],
                    channels=[
                        PackChannel(name="ch"),
                        PackChannel(name="ch"),
                    ],
                ),
            )

    def test_load_from_yaml_file(self, tmp_path):
        pack_yaml = tmp_path / "pack.yaml"
        pack_yaml.write_text(yaml.dump({
            "kind": "pack",
            "name": "file-test",
            "team": {
                "name": "ft",
                "agents": [{"name": "dev", "preset": "generalist"}],
            },
        }))
        data = yaml.safe_load(pack_yaml.read_text())
        config = PackConfig.model_validate(data)
        assert config.name == "file-test"
