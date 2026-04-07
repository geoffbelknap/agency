"""Tests for the FileWatcher directory watch mode."""

from __future__ import annotations

import os
import sys
import time
from unittest.mock import MagicMock

import pytest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "knowledge"))
from ingestion.watcher import FileWatcher, _WATCHDOG_AVAILABLE


# ---------------------------------------------------------------------------
# Fixtures
# ---------------------------------------------------------------------------


@pytest.fixture()
def callback():
    return MagicMock()


@pytest.fixture()
def watcher(tmp_path, callback):
    return FileWatcher(str(tmp_path), callback)


# ---------------------------------------------------------------------------
# Basic interface
# ---------------------------------------------------------------------------


class TestAvailable:
    def test_returns_bool(self):
        assert isinstance(FileWatcher.available(), bool)

    def test_matches_module_flag(self):
        assert FileWatcher.available() == _WATCHDOG_AVAILABLE


# ---------------------------------------------------------------------------
# poll_once — works without watchdog
# ---------------------------------------------------------------------------


class TestPollOnce:
    def test_detects_new_file(self, tmp_path, watcher):
        (tmp_path / "readme.md").write_text("hello")
        found = watcher.poll_once()
        assert len(found) == 1
        assert found[0] == str(tmp_path / "readme.md")

    def test_detects_multiple_files(self, tmp_path, watcher):
        (tmp_path / "a.txt").write_text("a")
        (tmp_path / "b.yaml").write_text("b")
        found = watcher.poll_once()
        assert len(found) == 2

    def test_ignores_dotfiles(self, tmp_path, watcher):
        (tmp_path / ".hidden.md").write_text("secret")
        (tmp_path / "visible.md").write_text("public")
        found = watcher.poll_once()
        assert len(found) == 1
        assert "visible.md" in found[0]

    def test_ignores_unknown_extensions(self, tmp_path, watcher):
        (tmp_path / "data.csv").write_text("a,b,c")
        (tmp_path / "image.png").write_bytes(b"\x89PNG")
        (tmp_path / "notes.md").write_text("notes")
        found = watcher.poll_once()
        assert len(found) == 1
        assert "notes.md" in found[0]

    def test_idempotent_same_file_not_reported_twice(self, tmp_path, watcher):
        (tmp_path / "doc.md").write_text("v1")
        first = watcher.poll_once()
        assert len(first) == 1
        second = watcher.poll_once()
        assert len(second) == 0

    def test_reports_modified_file(self, tmp_path, watcher):
        p = tmp_path / "doc.md"
        p.write_text("v1")
        watcher.poll_once()
        # Ensure mtime changes (some filesystems have 1s resolution)
        time.sleep(0.05)
        os.utime(str(p), (time.time() + 1, time.time() + 1))
        found = watcher.poll_once()
        assert len(found) == 1

    def test_creates_directory_if_missing(self, tmp_path, callback):
        watch_dir = str(tmp_path / "subdir" / "deep")
        w = FileWatcher(watch_dir, callback)
        w.poll_once()
        assert os.path.isdir(watch_dir)

    def test_scans_subdirectories(self, tmp_path, watcher):
        sub = tmp_path / "nested"
        sub.mkdir()
        (sub / "deep.json").write_text("{}")
        found = watcher.poll_once()
        assert len(found) == 1
        assert "deep.json" in found[0]

    def test_all_supported_extensions(self, tmp_path, watcher):
        for ext in FileWatcher.INGEST_EXTENSIONS:
            (tmp_path / f"file{ext}").write_text("x")
        found = watcher.poll_once()
        assert len(found) == len(FileWatcher.INGEST_EXTENSIONS)


# ---------------------------------------------------------------------------
# start / stop
# ---------------------------------------------------------------------------


class TestStartStop:
    def test_start_stop_without_watchdog(self, tmp_path, callback, monkeypatch):
        """start() should not raise even without watchdog."""
        import ingestion.watcher as wmod

        monkeypatch.setattr(wmod, "_WATCHDOG_AVAILABLE", False)
        w = FileWatcher(str(tmp_path), callback)
        w.start()
        w.stop()

    @pytest.mark.skipif(not _WATCHDOG_AVAILABLE, reason="watchdog not installed")
    def test_start_stop_with_watchdog(self, tmp_path, callback):
        w = FileWatcher(str(tmp_path), callback)
        w.start()
        # Create a file and give watchdog time to pick it up
        (tmp_path / "live.md").write_text("hello")
        time.sleep(0.5)
        w.stop()
        callback.assert_called()
        args = callback.call_args_list
        assert any("live.md" in str(a) for a in args)

    def test_stop_idempotent(self, watcher):
        """Calling stop() without start() should not raise."""
        watcher.stop()
        watcher.stop()
