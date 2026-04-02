"""Tests for extended connector requirements model."""
import pytest
from images.models.connector import (
    ConnectorRequires, ConnectorCredential, ConnectorAuth,
    ConnectorConfig, ConnectorSource,
)


class TestConnectorCredential:
    def test_minimal(self):
        cred = ConnectorCredential(name="API_KEY")
        assert cred.name == "API_KEY"
        assert cred.type == "secret"
        assert cred.scope == "service-grant"

    def test_config_type(self):
        cred = ConnectorCredential(name="ORG_ID", type="config", scope="env-var")
        assert cred.type == "config"
        assert cred.scope == "env-var"

    def test_full_fields(self):
        cred = ConnectorCredential(
            name="LC_API_KEY",
            description="LimaCharlie API key",
            type="secret",
            scope="service-grant",
            grant_name="limacharlie-api",
            setup_url="https://app.limacharlie.io/api-keys",
            example=None,
        )
        assert cred.grant_name == "limacharlie-api"
        assert cred.setup_url is not None


class TestConnectorAuth:
    def test_default_none(self):
        auth = ConnectorAuth()
        assert auth.type == "none"

    def test_jwt_exchange(self):
        auth = ConnectorAuth(
            type="jwt-exchange",
            token_url="https://jwt.limacharlie.io",
            token_params={"oid": "${LC_ORG_ID}", "secret": "${credential}"},
            token_response_field="jwt",
            token_ttl_seconds=3000,
        )
        assert auth.type == "jwt-exchange"
        assert auth.token_url == "https://jwt.limacharlie.io"
        assert auth.token_response_field == "jwt"


class TestConnectorRequiresExtended:
    def test_backward_compat_services_only(self):
        req = ConnectorRequires(services=["slack"])
        assert req.services == ["slack"]
        assert req.credentials == []
        assert req.auth is None
        assert req.egress_domains == []

    def test_full_requires(self):
        req = ConnectorRequires(
            services=["limacharlie-api"],
            credentials=[
                ConnectorCredential(name="LC_API_KEY", type="secret", scope="service-grant", grant_name="limacharlie-api"),
                ConnectorCredential(name="LC_ORG_ID", type="config", scope="env-var"),
            ],
            auth=ConnectorAuth(type="jwt-exchange", token_url="https://jwt.limacharlie.io"),
            egress_domains=["api.limacharlie.io", "jwt.limacharlie.io"],
        )
        assert len(req.credentials) == 2
        assert req.auth.type == "jwt-exchange"
        assert len(req.egress_domains) == 2

    def test_connector_with_extended_requires(self):
        """Full ConnectorConfig with extended requires parses correctly."""
        from images.models.connector import ConnectorRoute, GraphIngestRule, GraphIngestNode
        config = ConnectorConfig(
            name="test-connector",
            source=ConnectorSource(type="webhook"),
            requires=ConnectorRequires(
                credentials=[ConnectorCredential(name="KEY")],
                egress_domains=["api.example.com"],
            ),
            graph_ingest=[GraphIngestRule(nodes=[GraphIngestNode(kind="event", label="test")])],
        )
        assert len(config.requires.credentials) == 1
        assert config.requires.egress_domains == ["api.example.com"]

    def test_empty_requires_still_works(self):
        from images.models.connector import GraphIngestRule, GraphIngestNode
        config = ConnectorConfig(
            name="test",
            source=ConnectorSource(type="webhook"),
            graph_ingest=[GraphIngestRule(nodes=[GraphIngestNode(kind="event", label="test")])],
        )
        assert config.requires is None  # not set
