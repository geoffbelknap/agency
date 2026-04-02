import json
import threading
import time
import urllib.request

from hook_server import HookServer


def test_hook_server_starts_and_responds():
    events = []
    def on_constraint_change(version, severity):
        events.append({"version": version, "severity": severity})

    server = HookServer(port=9999, on_constraint_change=on_constraint_change)
    server.start()
    time.sleep(0.2)

    try:
        data = json.dumps({"version": 3, "severity": "MEDIUM"}).encode()
        req = urllib.request.Request(
            "http://127.0.0.1:9999/hooks/constraint-change",
            data=data,
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        resp = urllib.request.urlopen(req)
        assert resp.status == 200

        time.sleep(0.1)
        assert len(events) == 1
        assert events[0]["version"] == 3
        assert events[0]["severity"] == "MEDIUM"
    finally:
        server.stop()


def test_hook_server_rejects_get():
    server = HookServer(port=9998, on_constraint_change=lambda v, s: None)
    server.start()
    time.sleep(0.2)

    try:
        req = urllib.request.Request("http://127.0.0.1:9998/hooks/constraint-change")
        try:
            urllib.request.urlopen(req)
            assert False, "should have raised"
        except urllib.error.HTTPError as e:
            assert e.code == 405
    finally:
        server.stop()


def test_hook_server_health():
    server = HookServer(port=9997, on_constraint_change=lambda v, s: None)
    server.start()
    time.sleep(0.2)

    try:
        resp = urllib.request.urlopen("http://127.0.0.1:9997/health")
        assert resp.status == 200
    finally:
        server.stop()
