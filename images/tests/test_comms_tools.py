"""Tests for comms tool definitions and HTTP handlers."""

import json
from unittest.mock import MagicMock, patch

import pytest

from images.body.comms_tools import register_comms_tools
from images.body.body import BuiltinToolRegistry


@pytest.fixture
def registry(tmp_path):
    reg = BuiltinToolRegistry(workspace_dir=tmp_path)
    register_comms_tools(reg, comms_url="http://comms:18091", agent_name="scout")
    return reg


class TestCommsToolRegistration:
    def test_comms_tools_registered(self, registry):
        assert registry.has_tool("send_message")
        assert registry.has_tool("read_messages")
        assert registry.has_tool("list_channels")
        assert registry.has_tool("get_unreads")
        assert registry.has_tool("search_messages")

    def test_send_message_definition(self, registry):
        defn = registry._tools["send_message"]["definition"]
        params = defn["function"]["parameters"]["properties"]
        assert "channel" in params
        assert "content" in params
        assert "channel" in defn["function"]["parameters"]["required"]
        assert "content" in defn["function"]["parameters"]["required"]

    def test_read_messages_definition(self, registry):
        defn = registry._tools["read_messages"]["definition"]
        params = defn["function"]["parameters"]["properties"]
        assert "channel" in params
        assert "limit" in params

    def test_search_messages_definition(self, registry):
        defn = registry._tools["search_messages"]["definition"]
        params = defn["function"]["parameters"]["properties"]
        assert "query" in params


class TestCommsToolHandlers:
    @pytest.fixture(autouse=True)
    def setup_registry(self, tmp_path):
        self.registry = BuiltinToolRegistry(workspace_dir=tmp_path)
        register_comms_tools(
            self.registry,
            comms_url="http://comms:18091",
            agent_name="scout",
        )

    @patch("images.body.comms_tools._http")
    def test_send_message_calls_api(self, mock_http):
        mock_resp = MagicMock()
        mock_resp.text = json.dumps({"id": "abc123", "content": "hello"})
        mock_resp.status_code = 201
        mock_http.post.return_value = mock_resp

        result = self.registry.call_tool("send_message", {
            "channel": "dev",
            "content": "hello",
        })
        mock_http.post.assert_called_once()
        call_args = mock_http.post.call_args
        assert "dev/messages" in call_args[0][0]
        data = json.loads(result)
        assert data["id"] == "abc123"

    @patch("images.body.comms_tools._http")
    def test_send_message_includes_author(self, mock_http):
        mock_resp = MagicMock()
        mock_resp.text = json.dumps({"id": "1"})
        mock_http.post.return_value = mock_resp

        self.registry.call_tool("send_message", {
            "channel": "dev", "content": "hi",
        })
        body = mock_http.post.call_args[1]["json"]
        assert body["author"] == "scout"

    @patch("images.body.comms_tools._http")
    def test_read_messages_passes_reader(self, mock_http):
        mock_resp = MagicMock()
        mock_resp.text = json.dumps([])
        mock_http.get.return_value = mock_resp

        self.registry.call_tool("read_messages", {"channel": "dev"})
        params = mock_http.get.call_args[1]["params"]
        assert params["reader"] == "scout"

    @patch("images.body.comms_tools._http")
    def test_list_channels_enriches_with_unreads(self, mock_http):
        channels_resp = MagicMock()
        channels_resp.json.return_value = [{"name": "dev", "type": "team"}]
        unreads_resp = MagicMock()
        unreads_resp.json.return_value = {"dev": {"unread": 2, "mentions": 1}}
        mock_http.get.side_effect = [channels_resp, unreads_resp]

        result = self.registry.call_tool("list_channels", {})
        data = json.loads(result)
        assert data[0]["unread"] == 2
        assert data[0]["mentions"] == 1

    @patch("images.body.comms_tools._http")
    def test_get_unreads_calls_api(self, mock_http):
        mock_resp = MagicMock()
        mock_resp.text = json.dumps({"dev": {"unread": 3, "mentions": 0}})
        mock_http.get.return_value = mock_resp

        result = self.registry.call_tool("get_unreads", {})
        data = json.loads(result)
        assert data["dev"]["unread"] == 3

    @patch("images.body.comms_tools._http")
    def test_search_passes_participant(self, mock_http):
        mock_resp = MagicMock()
        mock_resp.text = json.dumps([])
        mock_http.get.return_value = mock_resp

        self.registry.call_tool("search_messages", {"query": "recipe"})
        params = mock_http.get.call_args[1]["params"]
        assert params["q"] == "recipe"
        assert params["participant"] == "scout"

    @patch("images.body.comms_tools._http")
    def test_send_message_error_handling(self, mock_http):
        mock_http.post.side_effect = Exception("connection refused")

        result = self.registry.call_tool("send_message", {
            "channel": "nope", "content": "hello",
        })
        data = json.loads(result)
        assert "error" in data
