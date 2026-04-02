"""Tests for function agent exception recommendations."""

import pytest
import yaml
from unittest.mock import patch

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
                "roles": ["operator"],
                "created": "2026-01-01",
                "exception_domains": ["security"],
            },
        ],
        "agents": [],
        "teams": [],
        "exception_routes": [
            {"domain": "security", "approvers": ["alice"]},
        ],
    }
    (home / "principals.yaml").write_text(yaml.dump(principals))
    return home


def _create_routed_request(router, request_id="exc-rec-001"):
    req = ExceptionRequest(
        request_id=request_id,
        agent_name="dev-agent",
        parameter="risk_tolerance",
        requested_value="high",
        reason="Need elevated risk",
    )
    return router.route(req)


class TestRecommend:
    def test_valid_recommendation(self, routing_home):
        router = ExceptionRouter(agency_home=routing_home)
        _create_routed_request(router)

        with patch.object(router, "_validate_recommender", return_value=True):
            result = router.recommend(
                "exc-rec-001", "security-monitor", "approve", "Risk is acceptable"
            )

        assert result is not None
        assert len(result.recommendations) == 1
        assert result.recommendations[0]["agent"] == "security-monitor"
        assert result.recommendations[0]["action"] == "approve"
        assert result.recommendations[0]["reasoning"] == "Risk is acceptable"

    def test_deny_recommendation(self, routing_home):
        router = ExceptionRouter(agency_home=routing_home)
        _create_routed_request(router)

        with patch.object(router, "_validate_recommender", return_value=True):
            result = router.recommend(
                "exc-rec-001", "compliance-agent", "deny", "Policy violation risk"
            )

        assert result.recommendations[0]["action"] == "deny"

    def test_invalid_action_rejected(self, routing_home):
        router = ExceptionRouter(agency_home=routing_home)
        _create_routed_request(router)

        result = router.recommend(
            "exc-rec-001", "agent", "maybe", "Not sure"
        )
        assert result is None

    def test_nonexistent_request(self, routing_home):
        router = ExceptionRouter(agency_home=routing_home)
        with patch.object(router, "_validate_recommender", return_value=True):
            result = router.recommend(
                "nonexistent", "agent", "approve", "reason"
            )
        assert result is None

    def test_invalid_recommender_rejected(self, routing_home):
        router = ExceptionRouter(agency_home=routing_home)
        _create_routed_request(router)

        with patch.object(router, "_validate_recommender", return_value=False):
            result = router.recommend(
                "exc-rec-001", "random-agent", "approve", "reason"
            )
        assert result is None

    def test_multiple_recommendations(self, routing_home):
        router = ExceptionRouter(agency_home=routing_home)
        _create_routed_request(router)

        with patch.object(router, "_validate_recommender", return_value=True):
            router.recommend("exc-rec-001", "agent-a", "approve", "Looks safe")
            result = router.recommend("exc-rec-001", "agent-b", "deny", "Too risky")

        assert len(result.recommendations) == 2

    def test_recommendation_does_not_change_status(self, routing_home):
        router = ExceptionRouter(agency_home=routing_home)
        _create_routed_request(router)

        with patch.object(router, "_validate_recommender", return_value=True):
            result = router.recommend(
                "exc-rec-001", "agent", "approve", "reason"
            )

        assert result.status == "routed"  # Not approved

    def test_recommendation_persists(self, routing_home):
        router = ExceptionRouter(agency_home=routing_home)
        _create_routed_request(router)

        with patch.object(router, "_validate_recommender", return_value=True):
            router.recommend("exc-rec-001", "agent", "approve", "reason")

        router2 = ExceptionRouter(agency_home=routing_home)
        loaded = router2.get("exc-rec-001")
        assert len(loaded.recommendations) == 1

    def test_recommendation_serialization(self):
        req = ExceptionRequest(
            request_id="exc-ser",
            agent_name="agent",
            parameter="risk_tolerance",
            requested_value="high",
            reason="test",
        )
        req.recommendations = [
            {
                "agent": "monitor",
                "action": "approve",
                "reasoning": "Safe",
                "timestamp": "2026-03-07T12:00:00Z",
            }
        ]
        d = req.to_dict()
        req2 = ExceptionRequest.from_dict(d)
        assert len(req2.recommendations) == 1
        assert req2.recommendations[0]["agent"] == "monitor"
