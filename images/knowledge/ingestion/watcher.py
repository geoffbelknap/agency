"""Directory watcher for auto-ingesting files into the knowledge graph.

Uses watchdog for real-time filesystem events when available.
Falls back to manual polling via poll_once() when watchdog is not installed.
"""

from __future__ import annotations

import logging
import os
from typing import Callable

logger = logging.getLogger(__name__)

try:
    from watchdog.observers import Observer
    from watchdog.events import FileSystemEventHandler, FileCreatedEvent, FileModifiedEvent
    _WATCHDOG_AVAILABLE = True
except ImportError:
    Observer = None  # type: ignore[assignment,misc]
    FileSystemEventHandler = object  # type: ignore[assignment,misc]
    _WATCHDOG_AVAILABLE = False


class _IngestHandler(FileSystemEventHandler):  # type: ignore[misc]
    """Watchdog handler that filters by extension and ignores dotfiles."""

    def __init__(self, extensions: set[str], on_file: Callable[[str], None]) -> None:
        super().__init__()
        self._extensions = extensions
        self._on_file = on_file

    def _should_handle(self, path: str) -> bool:
        basename = os.path.basename(path)
        if basename.startswith("."):
            return False
        _, ext = os.path.splitext(basename)
        return ext.lower() in self._extensions

    def on_created(self, event) -> None:  # type: ignore[override]
        if not event.is_directory and self._should_handle(event.src_path):
            self._on_file(event.src_path)

    def on_modified(self, event) -> None:  # type: ignore[override]
        if not event.is_directory and self._should_handle(event.src_path):
            self._on_file(event.src_path)


class FileWatcher:
    """Watches a directory for new/modified files and invokes a callback.

    Uses watchdog for real-time events when available. Always supports
    manual polling via poll_once() regardless of watchdog availability.
    """

    INGEST_EXTENSIONS: set[str] = {
        ".md", ".txt", ".yaml", ".yml", ".json", ".toml",
        ".py", ".go", ".js", ".ts", ".html", ".pdf",
    }

    def __init__(self, watch_dir: str, on_file: Callable[[str], None]) -> None:
        self._watch_dir = watch_dir
        self._on_file = on_file
        self._observer = None  # type: Observer | None  # type: ignore[assignment]
        # Track seen files as {path: mtime} to detect new and modified files.
        self._seen: dict[str, float] = {}

    @staticmethod
    def available() -> bool:
        """Return True if watchdog is installed."""
        return _WATCHDOG_AVAILABLE

    def start(self) -> None:
        """Start watching the directory.

        Creates the directory if it does not exist.  Uses watchdog when
        available; otherwise logs a fallback message.
        """
        os.makedirs(self._watch_dir, exist_ok=True)

        if not _WATCHDOG_AVAILABLE:
            logger.info(
                "watchdog not installed — use poll_once() for manual directory scanning"
            )
            return

        handler = _IngestHandler(self.INGEST_EXTENSIONS, self._on_file)
        self._observer = Observer()
        self._observer.schedule(handler, self._watch_dir, recursive=True)
        self._observer.start()
        logger.info("FileWatcher started on %s (watchdog)", self._watch_dir)

    def stop(self) -> None:
        """Stop watching. Safe to call multiple times."""
        if self._observer is not None:
            self._observer.stop()
            self._observer.join()
            self._observer = None
            logger.info("FileWatcher stopped")

    def poll_once(self) -> list[str]:
        """Scan the directory for new or modified files.

        Returns a list of file paths that are new or have been modified
        since the last poll.  Tracks files by (path, mtime) so unchanged
        files are not reported again.

        Works without watchdog — this is the polling fallback.
        """
        os.makedirs(self._watch_dir, exist_ok=True)
        changed: list[str] = []

        for dirpath, dirnames, filenames in os.walk(self._watch_dir):
            # Skip hidden directories
            dirnames[:] = [d for d in dirnames if not d.startswith(".")]

            for fname in filenames:
                if fname.startswith("."):
                    continue
                _, ext = os.path.splitext(fname)
                if ext.lower() not in self.INGEST_EXTENSIONS:
                    continue

                full = os.path.join(dirpath, fname)
                try:
                    mtime = os.path.getmtime(full)
                except OSError:
                    continue

                prev = self._seen.get(full)
                if prev is None or mtime != prev:
                    self._seen[full] = mtime
                    changed.append(full)

        return changed
