"""Lightweight HTTP server for receiving constraint-change notifications from the enforcer."""

import json
import logging
import threading
from http.server import HTTPServer, BaseHTTPRequestHandler
from typing import Callable

log = logging.getLogger(__name__)

HOOK_PORT = 8090


class _HookHandler(BaseHTTPRequestHandler):
    def do_POST(self):
        if self.path == "/hooks/constraint-change":
            length = int(self.headers.get("Content-Length", 0))
            body = json.loads(self.rfile.read(length)) if length > 0 else {}
            version = body.get("version", 0)
            severity = body.get("severity", "LOW")
            log.info("Constraint change notification: version=%d severity=%s", version, severity)
            try:
                self.server.on_constraint_change(version, severity)
                self._respond(200, {"status": "ok"})
            except Exception as e:
                log.error("Constraint change handler failed: %s", e)
                self._respond(500, {"error": str(e)})
        elif self.path == "/hooks/config-change":
            log.info("Config change notification received")
            try:
                self.server.on_config_change()
                self._respond(200, {"status": "ok"})
            except Exception as e:
                log.error("Config change handler failed: %s", e)
                self._respond(500, {"error": str(e)})
        else:
            self._respond(404, {"error": "not found"})

    def do_GET(self):
        if self.path == "/health":
            self._respond(200, {"status": "ok"})
        elif self.path == "/hooks/constraint-change":
            self._respond(405, {"error": "method not allowed"})
        else:
            self._respond(404, {"error": "not found"})

    def _respond(self, code: int, body: dict):
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(json.dumps(body).encode())

    def log_message(self, format, *args):
        pass


class HookServer:
    def __init__(self, port: int = HOOK_PORT, on_constraint_change: Callable = None,
                 on_config_change: Callable = None):
        self.port = port
        self._on_constraint_change = on_constraint_change or (lambda v, s: None)
        self._on_config_change = on_config_change or (lambda: None)
        self._server = None
        self._thread = None

    def start(self):
        self._server = HTTPServer(("0.0.0.0", self.port), _HookHandler)
        self._server.on_constraint_change = self._on_constraint_change
        self._server.on_config_change = self._on_config_change
        self._thread = threading.Thread(target=self._server.serve_forever, daemon=True)
        self._thread.start()
        log.info("Hook server started on port %d", self.port)

    def stop(self):
        if self._server:
            self._server.shutdown()
            self._server = None
