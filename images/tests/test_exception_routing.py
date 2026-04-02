"""Tests for exception routing to appropriate principals."""

import pytest
import yaml

from images.tests.support.policy.routing import ExceptionRequest, ExceptionRouter


@pytest.fixture
def routing_home(tmp_path):
    home = tmp_path / ".agency"
    home.mkdir()

    principals = {
        "version": "0.1",
        "humans": [
            {
                "id": "alice",
                "name": "Alice",
                "roles": ["operator", "security_lead"],
                "created": "2026-01-01",
                "exception_domains": ["security"],
            },
            {
                "id": "bob",
                "name": "Bob",
                "roles": ["privacy_officer"],
                "created": "2026-01-01",
                "exception_domains": ["privacy"],
            },
            {
                "id": "carol",
                "name": "Carol",
                "roles": ["legal"],
                "created": "2026-01-01",
                "exception_domains": ["legal", "compliance"],
            },
        ],
        "agents": [],
        "teams": [],
        "exception_routes": [
            {
                "domain": "security",
                "approvers": ["alice"],
            },
            {
                "domain": "privacy",
                "approvers": ["bob", "carol"],
                "requires_dual_approval": True,
            },
            {
                "domain": "compliance",
                "approvers": ["carol"],
            },
        ],
    }
    (home / "principals.yaml").write_text(yaml.dump(principals))
    return home


class TestExceptionRequest:
    def test_create_request(self):
        req = ExceptionRequest(
            request_id="exc-001",
            agent_name="dev-assistant",
            parameter="risk_tolerance",
            requested_value="high",
            reason="Need to run risky operations",
        )
        assert req.domain == "security"
        assert req.status == "pending"

    def test_domain_auto_detected(self):
        req = ExceptionRequest(
            request_id="exc-002",
            agent_name="agent",
            parameter="logging",
            requested_value="optional",
            reason="test",
        )
        assert req.domain == "compliance"

    def test_explicit_domain(self):
        req = ExceptionRequest(
            request_id="exc-003",
            agent_name="agent",
            parameter="custom_param",
            requested_value="val",
            reason="test",
            domain="privacy",
        )
        assert req.domain == "privacy"

    def test_unknown_param_domain(self):
        req = ExceptionRequest(
            request_id="exc-004",
            agent_name="agent",
            parameter="something_new",
            requested_value="val",
            reason="test",
        )
        assert req.domain == "general"

    def test_serialization(self):
        req = ExceptionRequest(
            request_id="exc-005",
            agent_name="agent",
            parameter="risk_tolerance",
            requested_value="high",
            reason="testing",
        )
        d = req.to_dict()
        req2 = ExceptionRequest.from_dict(d)
        assert req2.request_id == "exc-005"
        assert req2.parameter == "risk_tolerance"


class TestExceptionRouter:
    def test_route_security_exception(self, routing_home):
        router = ExceptionRouter(agency_home=routing_home)
        req = ExceptionRequest(
            request_id="exc-sec-001",
            agent_name="dev-assistant",
            parameter="risk_tolerance",
            requested_value="high",
            reason="Risky task",
        )
        routed = router.route(req)
        assert routed.status == "routed"
        assert "alice" in routed.routed_to
        assert not routed.requires_dual_approval

    def test_route_privacy_requires_dual(self, routing_home):
        router = ExceptionRouter(agency_home=routing_home)
        req = ExceptionRequest(
            request_id="exc-prv-001",
            agent_name="agent",
            parameter="data_access",
            requested_value="pii",
            reason="Need PII access",
            domain="privacy",
        )
        routed = router.route(req)
        assert routed.status == "routed"
        assert "bob" in routed.routed_to
        assert "carol" in routed.routed_to
        assert routed.requires_dual_approval

    def test_route_compliance(self, routing_home):
        router = ExceptionRouter(agency_home=routing_home)
        req = ExceptionRequest(
            request_id="exc-comp-001",
            agent_name="agent",
            parameter="logging",
            requested_value="optional",
            reason="Performance",
            domain="compliance",
        )
        routed = router.route(req)
        assert "carol" in routed.routed_to

    def test_route_unknown_domain_falls_back(self, routing_home):
        router = ExceptionRouter(agency_home=routing_home)
        req = ExceptionRequest(
            request_id="exc-unk-001",
            agent_name="agent",
            parameter="custom",
            requested_value="val",
            reason="test",
            domain="marketing",
        )
        routed = router.route(req)
        assert routed.status == "routed"
        assert "operator" in routed.routed_to

    def test_route_domain_from_principal_exception_domains(self, routing_home):
        """Falls back to exception_domains on principals when no route defined."""
        # Remove the explicit route for security
        principals = yaml.safe_load(
            (routing_home / "principals.yaml").read_text()
        )
        principals["exception_routes"] = [
            r for r in principals["exception_routes"]
            if r["domain"] != "security"
        ]
        (routing_home / "principals.yaml").write_text(yaml.dump(principals))

        router = ExceptionRouter(agency_home=routing_home)
        req = ExceptionRequest(
            request_id="exc-fb-001",
            agent_name="agent",
            parameter="risk_tolerance",
            requested_value="high",
            reason="test",
        )
        routed = router.route(req)
        # alice has exception_domains: ["security"]
        assert "alice" in routed.routed_to

    def test_approve_single(self, routing_home):
        router = ExceptionRouter(agency_home=routing_home)
        req = ExceptionRequest(
            request_id="exc-appr-001",
            agent_name="agent",
            parameter="risk_tolerance",
            requested_value="high",
            reason="test",
        )
        router.route(req)

        approved = router.approve("exc-appr-001", "alice")
        assert approved is not None
        assert approved.status == "approved"
        assert len(approved.approvals) == 1
        assert approved.resolved_at is not None

    def test_dual_approval_needs_two(self, routing_home):
        router = ExceptionRouter(agency_home=routing_home)
        req = ExceptionRequest(
            request_id="exc-dual-001",
            agent_name="agent",
            parameter="data_access",
            requested_value="pii",
            reason="test",
            domain="privacy",
        )
        router.route(req)

        # First approval — still routed
        result1 = router.approve("exc-dual-001", "bob")
        assert result1.status == "routed"
        assert len(result1.approvals) == 1

        # Second approval — now approved
        result2 = router.approve("exc-dual-001", "carol")
        assert result2.status == "approved"
        assert len(result2.approvals) == 2

    def test_deny(self, routing_home):
        router = ExceptionRouter(agency_home=routing_home)
        req = ExceptionRequest(
            request_id="exc-deny-001",
            agent_name="agent",
            parameter="risk_tolerance",
            requested_value="high",
            reason="test",
        )
        router.route(req)

        denied = router.deny("exc-deny-001", "alice", "Too risky")
        assert denied.status == "denied"
        assert len(denied.denials) == 1
        assert denied.denials[0]["reason"] == "Too risky"

    def test_approve_wrong_principal(self, routing_home):
        router = ExceptionRouter(agency_home=routing_home)
        req = ExceptionRequest(
            request_id="exc-wrong-001",
            agent_name="agent",
            parameter="risk_tolerance",
            requested_value="high",
            reason="test",
        )
        router.route(req)

        result = router.approve("exc-wrong-001", "bob")
        assert result is None  # bob not in routed_to for security

    def test_list_pending(self, routing_home):
        router = ExceptionRouter(agency_home=routing_home)
        # Create and route two requests
        for i in range(2):
            req = ExceptionRequest(
                request_id=f"exc-list-{i}",
                agent_name="agent",
                parameter="risk_tolerance",
                requested_value="high",
                reason="test",
            )
            router.route(req)

        # Approve one
        router.approve("exc-list-0", "alice")

        pending = router.list_pending()
        assert len(pending) == 1
        assert pending[0].request_id == "exc-list-1"

    def test_list_for_principal(self, routing_home):
        router = ExceptionRouter(agency_home=routing_home)

        # Security request (alice)
        req1 = ExceptionRequest(
            request_id="exc-plist-1",
            agent_name="agent",
            parameter="risk_tolerance",
            requested_value="high",
            reason="test",
        )
        router.route(req1)

        # Privacy request (bob, carol)
        req2 = ExceptionRequest(
            request_id="exc-plist-2",
            agent_name="agent",
            parameter="data",
            requested_value="pii",
            reason="test",
            domain="privacy",
        )
        router.route(req2)

        alice_requests = router.list_for_principal("alice")
        assert len(alice_requests) == 1
        assert alice_requests[0].request_id == "exc-plist-1"

        bob_requests = router.list_for_principal("bob")
        assert len(bob_requests) == 1
        assert bob_requests[0].request_id == "exc-plist-2"

    def test_persistence(self, routing_home):
        router = ExceptionRouter(agency_home=routing_home)
        req = ExceptionRequest(
            request_id="exc-persist",
            agent_name="agent",
            parameter="risk_tolerance",
            requested_value="high",
            reason="test",
        )
        router.route(req)

        # Load from a fresh router instance
        router2 = ExceptionRouter(agency_home=routing_home)
        loaded = router2.get("exc-persist")
        assert loaded is not None
        assert loaded.status == "routed"
        assert loaded.routed_to == ["alice"]

    def test_no_principals_file(self, tmp_path):
        home = tmp_path / ".agency-empty"
        home.mkdir()
        router = ExceptionRouter(agency_home=home)
        req = ExceptionRequest(
            request_id="exc-noprinc",
            agent_name="agent",
            parameter="risk_tolerance",
            requested_value="high",
            reason="test",
        )
        routed = router.route(req)
        assert routed.routed_to == ["operator"]

    def test_get_nonexistent(self, routing_home):
        router = ExceptionRouter(agency_home=routing_home)
        assert router.get("nonexistent") is None
