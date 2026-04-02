"""Tests for comms context injection and heartbeat unread checks."""

from unittest.mock import MagicMock, patch

import pytest

from images.body.comms_tools import build_comms_context, check_comms_unreads


class TestBuildCommsContext:
    @patch("images.body.comms_tools._http")
    def test_context_with_messages(self, mock_http):
        channels_resp = MagicMock()
        channels_resp.json.return_value = [
            {"name": "chefhub-beta", "topic": "Beta readiness",
             "members": ["scout", "pm"]},
        ]
        unreads_resp = MagicMock()
        unreads_resp.json.return_value = {
            "chefhub-beta": {"unread": 2, "mentions": 1},
        }
        messages_resp = MagicMock()
        messages_resp.json.return_value = [
            {"author": "pm", "content": "Launch API first",
             "timestamp": "2026-03-08T05:00:00Z",
             "flags": {"decision": True}},
            {"author": "operator", "content": "Sounds good, proceed",
             "timestamp": "2026-03-08T05:01:00Z",
             "flags": {}},
        ]
        mock_http.get.side_effect = [channels_resp, unreads_resp, messages_resp]

        context = build_comms_context("http://comms:18091", "scout")
        assert "chefhub-beta" in context
        assert "2 unread" in context
        assert "1 mention" in context
        assert "Launch API first" in context
        assert "[DECISION]" in context
        assert "Beta readiness" in context

    @patch("images.body.comms_tools._http")
    def test_context_empty_when_no_channels(self, mock_http):
        channels_resp = MagicMock()
        channels_resp.json.return_value = []
        mock_http.get.return_value = channels_resp

        context = build_comms_context("http://comms:18091", "scout")
        assert context == ""

    @patch("images.body.comms_tools._http")
    def test_context_empty_on_error(self, mock_http):
        mock_http.get.side_effect = Exception("connection refused")
        context = build_comms_context("http://comms:18091", "scout")
        assert context == ""

    @patch("images.body.comms_tools._http")
    def test_context_shows_blocker_flag(self, mock_http):
        channels_resp = MagicMock()
        channels_resp.json.return_value = [
            {"name": "dev", "members": ["scout"]},
        ]
        unreads_resp = MagicMock()
        unreads_resp.json.return_value = {"dev": {"unread": 0, "mentions": 0}}
        messages_resp = MagicMock()
        messages_resp.json.return_value = [
            {"author": "pm", "content": "Blocked on API keys",
             "timestamp": "2026-03-08T05:00:00Z",
             "flags": {"blocker": True}},
        ]
        mock_http.get.side_effect = [channels_resp, unreads_resp, messages_resp]

        context = build_comms_context("http://comms:18091", "scout")
        assert "[BLOCKER]" in context

    @patch("images.body.comms_tools._http")
    def test_comms_context_includes_norms(self, mock_http):
        """Comms context includes communication norms section."""
        channels_resp = MagicMock()
        channels_resp.json.return_value = [
            {"name": "dev", "type": "team", "topic": "Development"}
        ]
        unreads_resp = MagicMock()
        unreads_resp.json.return_value = {"dev": {"unread": 0, "mentions": 0}}
        messages_resp = MagicMock()
        messages_resp.json.return_value = []
        mock_http.get.side_effect = [channels_resp, unreads_resp, messages_resp]

        result = build_comms_context("http://comms:18091", "scout")
        assert "## Communication Norms" in result
        assert "team member" in result.lower()
        assert "need-to-know" in result.lower()
        assert "adversarial" in result.lower()

    @patch("images.body.comms_tools._http")
    def test_comms_norms_not_in_empty_context(self, mock_http):
        """No norms if agent has no channels."""
        channels_resp = MagicMock()
        channels_resp.json.return_value = []
        mock_http.get.return_value = channels_resp

        result = build_comms_context("http://comms:18091", "scout")
        assert "## Communication Norms" not in result

    @patch("images.body.comms_tools._http")
    def test_context_multiple_channels(self, mock_http):
        channels_resp = MagicMock()
        channels_resp.json.return_value = [
            {"name": "dev", "members": ["scout"]},
            {"name": "product", "members": ["scout"]},
        ]
        unreads_resp = MagicMock()
        unreads_resp.json.return_value = {
            "dev": {"unread": 1, "mentions": 0},
            "product": {"unread": 0, "mentions": 0},
        }
        dev_msgs = MagicMock()
        dev_msgs.json.return_value = [
            {"author": "pm", "content": "Schema ready",
             "timestamp": "2026-03-08T05:00:00Z", "flags": {}},
        ]
        product_msgs = MagicMock()
        product_msgs.json.return_value = []
        mock_http.get.side_effect = [
            channels_resp, unreads_resp, dev_msgs, product_msgs,
        ]

        context = build_comms_context("http://comms:18091", "scout")
        assert "#dev" in context
        assert "#product" in context
        assert "1 unread" in context


    @patch("images.body.comms_tools._http")
    def test_guidelines_include_actionable_rules(self, mock_http):
        """Effective channel use section has actionable rules."""
        channels_resp = MagicMock()
        channels_resp.json.return_value = [
            {"name": "dev", "type": "team", "topic": "Development"}
        ]
        unreads_resp = MagicMock()
        unreads_resp.json.return_value = {"dev": {"unread": 0, "mentions": 0}}
        messages_resp = MagicMock()
        messages_resp.json.return_value = []
        mock_http.get.side_effect = [channels_resp, unreads_resp, messages_resp]

        result = build_comms_context("http://comms:18091", "scout")
        assert "Read channels before" in result
        assert "substantive" in result
        assert "knowledge" in result.lower()

    @patch("images.body.comms_tools._http")
    def test_guidelines_warn_against_passive_waiting(self, mock_http):
        """Effective channel use warns against passive waiting."""
        channels_resp = MagicMock()
        channels_resp.json.return_value = [
            {"name": "dev", "type": "team", "topic": "Development"}
        ]
        unreads_resp = MagicMock()
        unreads_resp.json.return_value = {"dev": {"unread": 0, "mentions": 0}}
        messages_resp = MagicMock()
        messages_resp.json.return_value = []
        mock_http.get.side_effect = [channels_resp, unreads_resp, messages_resp]

        result = build_comms_context("http://comms:18091", "scout")
        assert "wait" in result.lower()


class TestGetUnreadMessages:
    """Tests for get_unread_messages helper."""

    def test_returns_messages_from_unread_channels(self):
        from images.body.comms_tools import get_unread_messages

        mock_unreads = {"general": {"unread": 2, "mentions": 0}}
        mock_messages = [
            {"content": "Review findings", "author": "analyst"},
            {"content": "Update posted", "author": "researcher"},
        ]

        with patch("images.body.comms_tools._http") as mock_http:
            unreads_resp = MagicMock()
            unreads_resp.json.return_value = mock_unreads
            msgs_resp = MagicMock()
            msgs_resp.status_code = 200
            msgs_resp.json.return_value = mock_messages
            mock_http.get.side_effect = [unreads_resp, msgs_resp]

            result = get_unread_messages("http://comms:18091", "test-agent")
            assert len(result) == 2
            assert result[0]["channel"] == "general"
            assert result[0]["content"] == "Review findings"
            assert result[0]["sender"] == "analyst"

    def test_returns_empty_on_error(self):
        from images.body.comms_tools import get_unread_messages

        with patch("images.body.comms_tools._http") as mock_http:
            mock_http.get.side_effect = Exception("connection refused")
            result = get_unread_messages("http://comms:18091", "test-agent")
            assert result == []


class TestCheckCommsUnreads:
    @patch("images.body.comms_tools._http")
    def test_unreads_summary(self, mock_http):
        unreads_resp = MagicMock()
        unreads_resp.json.return_value = {
            "chefhub-beta": {"unread": 3, "mentions": 1},
            "product-strategy": {"unread": 0, "mentions": 0},
        }
        mock_http.get.return_value = unreads_resp

        summary = check_comms_unreads("http://comms:18091", "scout")
        assert "3 unread" in summary
        assert "chefhub-beta" in summary
        assert "1 mention" in summary
        assert "product-strategy" not in summary

    @patch("images.body.comms_tools._http")
    def test_no_unreads(self, mock_http):
        unreads_resp = MagicMock()
        unreads_resp.json.return_value = {}
        mock_http.get.return_value = unreads_resp

        summary = check_comms_unreads("http://comms:18091", "scout")
        assert summary == ""

    @patch("images.body.comms_tools._http")
    def test_comms_unavailable(self, mock_http):
        mock_http.get.side_effect = Exception("connection refused")
        summary = check_comms_unreads("http://comms:18091", "scout")
        assert summary == ""

    @patch("images.body.comms_tools._http")
    def test_multiple_channels_with_unreads(self, mock_http):
        unreads_resp = MagicMock()
        unreads_resp.json.return_value = {
            "dev": {"unread": 2, "mentions": 0},
            "product": {"unread": 5, "mentions": 3},
        }
        mock_http.get.return_value = unreads_resp

        summary = check_comms_unreads("http://comms:18091", "scout")
        assert "#dev" in summary
        assert "#product" in summary
        assert "2 unread" in summary
        assert "5 unread" in summary
        assert "3 mentions" in summary
