import json
import hashlib
import threading
from http.server import HTTPServer, BaseHTTPRequestHandler
from unittest.mock import MagicMock


class MockEnforcerHandler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == "/constraints":
            constraints = {"budget": {"max_daily_usd": 5.0}}
            # Canonical JSON: sorted keys, compact separators (matches Go json.Marshal)
            data = json.dumps(constraints, sort_keys=True, separators=(",", ":")).encode()
            h = hashlib.sha256(data).hexdigest()
            self._respond(200, {
                "version": 2,
                "hash": h,
                "severity": "MEDIUM",
                "constraints": constraints,
                "sealed_at": "2026-03-21T14:30:00Z",
            })

    def do_POST(self):
        if self.path == "/constraints/ack":
            length = int(self.headers.get("Content-Length", 0))
            body = json.loads(self.rfile.read(length))
            self.server.last_ack = body
            self._respond(200, {"status": "ok"})

    def _respond(self, code, body):
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(json.dumps(body).encode())

    def log_message(self, *args):
        pass


def test_reload_constraints():
    # Start mock enforcer on dynamic port
    server = HTTPServer(("127.0.0.1", 0), MockEnforcerHandler)
    port = server.server_address[1]
    server.last_ack = None
    t = threading.Thread(target=server.serve_forever, daemon=True)
    t.start()

    try:
        from body import Body
        body = Body.__new__(Body)
        body.enforcer_url = f"http://127.0.0.1:{port}"
        # Body uses _mcp_policy as the internal attribute name
        body._mcp_policy = None
        body._log = MagicMock()

        body.reload_constraints(version=2, severity="MEDIUM")

        assert server.last_ack is not None
        assert server.last_ack["version"] == 2
        assert len(server.last_ack["hash"]) == 64  # SHA-256 hex
    finally:
        server.shutdown()
