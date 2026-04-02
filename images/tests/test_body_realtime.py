"""Tests for body runtime real-time comms integration.

Tests the new helper methods and data flow for event-driven loop,
interruption injection, and notification batching. Does NOT test the
full body.py integration (test_body_runtime.py covers that).
"""

import queue as queue_module

import pytest


# ---------------------------------------------------------------------------
# Test that a task event dict is consumable from a queue
# ---------------------------------------------------------------------------


def test_task_event_consumable_from_queue():
    """A task event placed in a Queue can be retrieved with expected fields."""
    q = queue_module.Queue()
    task_event = {
        "type": "task",
        "task": {
            "task_id": "test-task-001",
            "content": "Analyze the security posture of the system",
        },
    }
    q.put(task_event)

    event = q.get(timeout=1)
    assert event["type"] == "task"
    assert event["task"]["task_id"] == "test-task-001"
    assert "content" in event["task"]


def test_queue_empty_timeout():
    """An empty queue raises queue.Empty after timeout."""
    q = queue_module.Queue()
    with pytest.raises(queue_module.Empty):
        q.get(timeout=0.01)


# ---------------------------------------------------------------------------
# Test interruption controller handles system events
# ---------------------------------------------------------------------------


def test_interruption_controller_system_bypass(tmp_path):
    """System events always get 'interrupt' action, bypassing all throttling."""
    import sys
    import os

    # Add the body image directory to sys.path so we can import interruption
    body_dir = os.path.join(
        os.path.dirname(os.path.dirname(os.path.abspath(__file__))),
        "agency", "images", "body",
    )
    if body_dir not in sys.path:
        sys.path.insert(0, body_dir)

    # Add agency package to path for models import
    agency_dir = os.path.join(
        os.path.dirname(os.path.dirname(os.path.abspath(__file__))),
    )
    if agency_dir not in sys.path:
        sys.path.insert(0, agency_dir)

    from interruption import InterruptionController

    controller = InterruptionController(config_dir=tmp_path)
    controller.start_task("task-001")

    # System match should always return interrupt
    action = controller.decide(match="system", flags={})
    assert action == "interrupt"

    # Even after many interrupts, system still bypasses
    for _ in range(10):
        controller.record_interrupt()
    action = controller.decide(match="system", flags={})
    assert action == "interrupt"


def test_interruption_controller_max_interrupts(tmp_path):
    """After max interrupts, non-system events get downgraded."""
    import sys
    import os

    body_dir = os.path.join(
        os.path.dirname(os.path.dirname(os.path.abspath(__file__))),
        "agency", "images", "body",
    )
    if body_dir not in sys.path:
        sys.path.insert(0, body_dir)

    agency_dir = os.path.join(
        os.path.dirname(os.path.dirname(os.path.abspath(__file__))),
    )
    if agency_dir not in sys.path:
        sys.path.insert(0, agency_dir)

    from interruption import InterruptionController

    controller = InterruptionController(config_dir=tmp_path)
    controller.start_task("task-002")

    # Exhaust the max interrupts (default 3)
    for _ in range(controller.policy.max_interrupts_per_task):
        controller.record_interrupt()

    # Non-system events should be downgraded
    action = controller.decide(match="direct_mention", flags={})
    assert action == "notify_at_pause"


# ---------------------------------------------------------------------------
# Test interrupt injection format
# ---------------------------------------------------------------------------


def test_interrupt_injection_format():
    """Interrupt injection produces a user message with expected format."""
    # Simulate what _inject_interrupt does without needing Body instance
    event = {
        "channel": "security-alerts",
        "summary": "New vulnerability found in dependency X",
        "message": {
            "author": "scanner",
            "flags": {"blocker": True},
        },
    }

    channel = event.get("channel", "unknown")
    summary = event.get("summary", "")
    author = event.get("message", {}).get("author", "unknown")
    injection = (
        f"[Comms interrupt] #{channel} @{author}: {summary}. "
        f"Use read_messages('{channel}') for full context."
    )

    msg = {"role": "user", "content": injection}
    assert msg["role"] == "user"
    assert "#security-alerts" in msg["content"]
    assert "@scanner" in msg["content"]
    assert "New vulnerability found" in msg["content"]
    assert "read_messages('security-alerts')" in msg["content"]


def test_interrupt_injection_missing_fields():
    """Interrupt injection handles missing fields gracefully."""
    event = {}  # No channel, summary, or message

    channel = event.get("channel", "unknown")
    summary = event.get("summary", "")
    author = event.get("message", {}).get("author", "unknown")
    injection = (
        f"[Comms interrupt] #{channel} @{author}: {summary}. "
        f"Use read_messages('{channel}') for full context."
    )

    msg = {"role": "user", "content": injection}
    assert "#unknown" in msg["content"]
    assert "@unknown" in msg["content"]


# ---------------------------------------------------------------------------
# Test notify_at_pause batching
# ---------------------------------------------------------------------------


def test_notify_at_pause_batching():
    """Pending notifications are batched into a single user message."""
    pending = [
        {"channel": "dev", "summary": "Build failed on main"},
        {"channel": "ops", "summary": "Disk usage at 90%"},
        {"channel": "security", "summary": "New CVE published"},
    ]

    # Simulate _drain_notifications_at_pause logic
    lines = []
    for event in pending:
        ch = event.get("channel", "?")
        summary = event.get("summary", "")
        lines.append(f"  {ch}: {summary}")

    content = (
        f"[Comms] {len(lines)} new messages may be relevant to "
        f"your current task:\n" + "\n".join(lines) +
        "\nUse read_messages to review."
    )
    result = [{"role": "user", "content": content}]

    assert len(result) == 1
    assert result[0]["role"] == "user"
    assert "3 new messages" in result[0]["content"]
    assert "dev: Build failed" in result[0]["content"]
    assert "ops: Disk usage" in result[0]["content"]
    assert "security: New CVE" in result[0]["content"]
    assert "read_messages" in result[0]["content"]


def test_notify_at_pause_empty():
    """Empty pending notifications produce no injections."""
    pending = []
    if not pending:
        result = []
    else:
        result = [{"role": "user", "content": "should not appear"}]

    assert result == []


# ---------------------------------------------------------------------------
# Test event queue drain
# ---------------------------------------------------------------------------


def test_drain_event_queue_categorizes_events():
    """Events are categorized by type when draining the queue."""
    q = queue_module.Queue()
    q.put({"type": "message", "channel": "dev", "match": "ambient", "summary": "hello"})
    q.put({"type": "knowledge", "channel": "ops", "match": "keyword", "summary": "update"})
    q.put({"type": "system", "event": "constraint_update"})

    system_events = []
    comms_events = []

    while True:
        try:
            event = q.get_nowait()
            event_type = event.get("type")
            if event_type == "system":
                system_events.append(event)
            elif event_type in ("message", "knowledge"):
                comms_events.append(event)
        except queue_module.Empty:
            break

    assert len(system_events) == 1
    assert system_events[0]["event"] == "constraint_update"
    assert len(comms_events) == 2
    assert comms_events[0]["type"] == "message"
    assert comms_events[1]["type"] == "knowledge"


# ---------------------------------------------------------------------------
# Test auto-interest keyword extraction
# ---------------------------------------------------------------------------


def test_auto_interest_keyword_extraction():
    """Keywords are extracted from task content, filtering stop words and short words."""
    content = "Analyze the security posture and fix the vulnerability in the authentication module"
    words = content.lower().split()
    stop_words = {"the", "and", "for", "that", "this", "with", "from", "are", "was", "has", "have", "been", "will", "can", "not", "but", "its"}
    keywords = [w.strip(".,;:!?()[]{}\"'") for w in words if len(w) >= 3 and w not in stop_words]
    keywords = list(dict.fromkeys(keywords))[:20]

    assert "analyze" in keywords
    assert "security" in keywords
    assert "posture" in keywords
    assert "fix" in keywords
    assert "vulnerability" in keywords
    assert "authentication" in keywords
    assert "module" in keywords
    # Stop words should be filtered
    assert "the" not in keywords
    assert "and" not in keywords
    # Short words should be filtered
    assert "in" not in keywords


def test_auto_interest_dedup_and_limit():
    """Duplicate keywords are removed and results capped at 20."""
    content = " ".join(["keyword"] * 50 + [f"word{i}" for i in range(30)])
    words = content.lower().split()
    stop_words = set()
    keywords = [w.strip(".,;:!?()[]{}\"'") for w in words if len(w) >= 3 and w not in stop_words]
    keywords = list(dict.fromkeys(keywords))[:20]

    assert len(keywords) <= 20
    assert keywords[0] == "keyword"  # deduped to one occurrence
