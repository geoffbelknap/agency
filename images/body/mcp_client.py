"""MCP client — stdio JSON-RPC 2.0 transport for MCP servers.

Spawns MCP server processes and communicates via stdin/stdout.
Used by the body runtime to dispatch tool calls to MCP servers.
"""

import json
import os
import select
import subprocess
import time
from typing import Optional


class MCPClient:
    """Minimal MCP client over stdio transport.

    Spawns an MCP server as a child process and communicates via
    JSON-RPC 2.0 over stdin/stdout.
    """

    def __init__(self, command: str, args: Optional[list[str]] = None, env: Optional[dict] = None):
        self.command = command
        self.args = args or []
        self.env = env
        self._proc: Optional[subprocess.Popen] = None
        self._request_id = 0
        self._tools: Optional[list[dict]] = None

    def start(self) -> None:
        """Spawn the MCP server subprocess."""
        merged_env = {**os.environ, **(self.env or {})}
        self._proc = subprocess.Popen(
            [self.command, *self.args],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            env=merged_env,
        )
        # Brief check for immediate crash
        time.sleep(0.1)
        if self._proc.poll() is not None:
            stderr = self._proc.stderr.read().decode(errors="replace").strip()
            raise RuntimeError(
                f"MCP server exited immediately (code {self._proc.returncode})"
                + (f": {stderr}" if stderr else "")
            )

    def initialize(self) -> dict:
        """Send MCP initialize handshake."""
        try:
            resp = self._send_request("initialize", {
                "protocolVersion": "2024-11-05",
                "capabilities": {},
                "clientInfo": {"name": "agency-body", "version": "0.1"},
            }, timeout=10)
        except RuntimeError as e:
            stderr = self._get_stderr()
            detail = f": {stderr}" if stderr else ""
            raise RuntimeError(f"MCP initialize failed: {e}{detail}") from e
        # Send initialized notification
        self._send_notification("notifications/initialized", {})
        return resp

    def list_tools(self) -> list[dict]:
        """Get available tools from the server."""
        resp = self._send_request("tools/list", {})
        self._tools = resp.get("tools", [])
        return self._tools

    def call_tool(self, name: str, arguments: Optional[dict] = None) -> dict:
        """Execute a tool call."""
        return self._send_request("tools/call", {
            "name": name,
            "arguments": arguments or {},
        }, timeout=30)

    def shutdown(self) -> None:
        """Cleanly shut down the MCP server."""
        if self._proc and self._proc.poll() is None:
            try:
                self._send_notification("notifications/cancelled", {})
            except Exception:
                pass
            self._proc.terminate()
            try:
                self._proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                self._proc.kill()
            self._proc = None

    def _next_id(self) -> int:
        self._request_id += 1
        return self._request_id

    def _send_request(self, method: str, params: dict, timeout: Optional[float] = None) -> dict:
        """Send a JSON-RPC request and wait for the response."""
        if not self._proc or self._proc.poll() is not None:
            stderr = self._get_stderr()
            detail = f": {stderr}" if stderr else ""
            raise RuntimeError(f"MCP server process is not running{detail}")

        msg = {
            "jsonrpc": "2.0",
            "id": self._next_id(),
            "method": method,
            "params": params,
        }
        self._write_message(msg)
        return self._read_response(msg["id"], timeout=timeout)

    def _send_notification(self, method: str, params: dict) -> None:
        """Send a JSON-RPC notification (no response expected)."""
        if not self._proc or self._proc.poll() is not None:
            return
        msg = {
            "jsonrpc": "2.0",
            "method": method,
            "params": params,
        }
        self._write_message(msg)

    def _write_message(self, msg: dict) -> None:
        """Write a JSON-RPC message to the server's stdin."""
        body = json.dumps(msg).encode()
        header = f"Content-Length: {len(body)}\r\n\r\n".encode()
        self._proc.stdin.write(header + body)
        self._proc.stdin.flush()

    def _get_stderr(self) -> str:
        """Read available stderr from the MCP server process."""
        if not self._proc or not self._proc.stderr:
            return ""
        try:
            # Non-blocking read of whatever stderr is available
            import fcntl
            fd = self._proc.stderr.fileno()
            flags = fcntl.fcntl(fd, fcntl.F_GETFL)
            fcntl.fcntl(fd, fcntl.F_SETFL, flags | os.O_NONBLOCK)
            try:
                data = self._proc.stderr.read()
                return data.decode(errors="replace").strip() if data else ""
            except (BlockingIOError, OSError):
                return ""
            finally:
                fcntl.fcntl(fd, fcntl.F_SETFL, flags)
        except Exception:
            return ""

    def _read_response(self, expected_id: int, timeout: Optional[float] = None) -> dict:
        """Read a JSON-RPC response from the server's stdout."""
        deadline = time.monotonic() + timeout if timeout else None

        # Read headers
        content_length = None
        while True:
            if deadline and time.monotonic() > deadline:
                raise RuntimeError("MCP server response timed out")
            if deadline:
                remaining = deadline - time.monotonic()
                ready, _, _ = select.select([self._proc.stdout], [], [], max(0, remaining))
                if not ready:
                    raise RuntimeError("MCP server response timed out")

            line = self._proc.stdout.readline()
            if not line:
                stderr = self._get_stderr()
                detail = f": {stderr}" if stderr else ""
                raise RuntimeError(f"MCP server closed connection{detail}")
            line = line.decode().strip()
            if line == "":
                break
            if line.lower().startswith("content-length:"):
                content_length = int(line.split(":", 1)[1].strip())

        if content_length is None:
            raise RuntimeError("MCP response missing Content-Length header")

        body = self._proc.stdout.read(content_length)
        resp = json.loads(body)

        # Skip notifications, wait for our response
        if "id" not in resp:
            return self._read_response(expected_id, timeout=timeout)

        if resp.get("id") != expected_id:
            return self._read_response(expected_id, timeout=timeout)

        if "error" in resp:
            err = resp["error"]
            raise RuntimeError(f"MCP error {err.get('code')}: {err.get('message')}")

        return resp.get("result", {})
