"""Tests for in-memory cross-source correlation."""
import time
import threading
from images.intake.correlation import EventBuffer


class TestEventBuffer:
    def test_record_and_lookup(self):
        buf = EventBuffer()
        buf.record("conn-a", {"ip": "1.2.3.4", "name": "alpha"})
        result = buf.lookup("conn-a", "ip", "1.2.3.4", window_seconds=60)
        assert result is not None
        assert result["name"] == "alpha"

    def test_lookup_no_match(self):
        buf = EventBuffer()
        buf.record("conn-a", {"ip": "1.2.3.4"})
        result = buf.lookup("conn-a", "ip", "9.9.9.9", window_seconds=60)
        assert result is None

    def test_lookup_wrong_connector(self):
        buf = EventBuffer()
        buf.record("conn-a", {"ip": "1.2.3.4"})
        result = buf.lookup("conn-b", "ip", "1.2.3.4", window_seconds=60)
        assert result is None

    def test_expired_entries_not_returned(self):
        buf = EventBuffer()
        buf.record("conn-a", {"ip": "1.2.3.4"})
        buf._buffers["conn-a"][0] = (time.time() - 120, {"ip": "1.2.3.4"})
        result = buf.lookup("conn-a", "ip", "1.2.3.4", window_seconds=60)
        assert result is None

    def test_returns_most_recent_match(self):
        buf = EventBuffer()
        buf.record("conn-a", {"ip": "1.2.3.4", "seq": 1})
        buf.record("conn-a", {"ip": "1.2.3.4", "seq": 2})
        result = buf.lookup("conn-a", "ip", "1.2.3.4", window_seconds=60)
        assert result["seq"] == 2

    def test_drop_clears_connector(self):
        buf = EventBuffer()
        buf.record("conn-a", {"ip": "1.2.3.4"})
        buf.drop("conn-a")
        result = buf.lookup("conn-a", "ip", "1.2.3.4", window_seconds=60)
        assert result is None

    def test_thread_safety(self):
        buf = EventBuffer()
        errors = []
        def writer():
            for i in range(100):
                try:
                    buf.record("conn-a", {"ip": f"1.2.3.{i % 256}"})
                except Exception as e:
                    errors.append(e)
        def reader():
            for _ in range(100):
                try:
                    buf.lookup("conn-a", "ip", "1.2.3.1", window_seconds=60)
                except Exception as e:
                    errors.append(e)
        threads = [threading.Thread(target=writer), threading.Thread(target=reader)]
        for t in threads:
            t.start()
        for t in threads:
            t.join()
        assert len(errors) == 0
