from body import Body


class _InterruptionController:
    def __init__(self):
        self.started = []
        self.ended = 0

    def start_task(self, task_id):
        self.started.append(task_id)

    def end_task(self):
        self.ended += 1


def test_dm_mission_trigger_task_from_event_queue_is_accepted():
    body = Body.__new__(Body)
    body._interruption_controller = _InterruptionController()
    body._loop_events = []
    body._signals = []
    body._tasks = []
    body._interests_registered = []
    body._interests_cleared = 0

    body._emit_loop_event = lambda name, payload, **fields: body._loop_events.append((name, payload, fields))
    body._emit_signal = lambda name, payload: body._signals.append((name, payload))
    body._register_auto_interests = lambda task: body._interests_registered.append(task)
    body._conversation_loop = lambda task: body._tasks.append(task)

    def clear_interests():
        body._interests_cleared += 1

    body._clear_interests = clear_interests

    task = {
        "task_id": "dm-task-1",
        "source": "channel:dm-loop-eval-current-info",
        "task_content": "[Mission trigger: channel dm-loop-eval-current-info / message]\n\nFind the current stable Node.js release.",
    }

    body._handle_task_event(task, delivery="event_queue")

    assert body._tasks == [task]
    assert body._interruption_controller.started == ["dm-task-1"]
    assert body._interruption_controller.ended == 1
    assert body._signals == [("task_accepted", {"task_id": "dm-task-1"})]
    assert body._loop_events == [("agent_loop_task_seen", task, {"delivery": "event_queue"})]
    assert body._interests_registered == [task]
    assert body._interests_cleared == 1


def test_direct_dm_task_delivery_uses_idle_reply_path():
    body = Body.__new__(Body)
    body._loop_events = []
    body._idle_events = []
    body._tasks = []

    body._emit_loop_event = lambda name, payload, **fields: body._loop_events.append((name, payload, fields))
    body._handle_idle_mention = lambda event: body._idle_events.append(event)
    body._conversation_loop = lambda task: body._tasks.append(task)

    task = {
        "task_id": "task-1",
        "content": (
            "[Mission trigger: channel dm-fc-fresh / message]\n\n"
            "New event from dm-fc-fresh:\n"
            "  content: immediate message after readiness-gated start\n\n"
            "Process this according to your mission instructions."
        ),
        "work_item_id": "evt-msg-abc123",
        "metadata": {
            "author": "_operator",
            "channel": "dm-fc-fresh",
            "channel_type": "direct",
        },
    }

    body._handle_task_event(task, delivery="context_fallback")

    assert body._tasks == []
    assert body._idle_events == [{
        "v": 1,
        "type": "message",
        "channel": "dm-fc-fresh",
        "match": "direct",
        "matched_keywords": [],
        "summary": "immediate message after readiness-gated start",
        "message": {
            "id": "abc123",
            "channel": "dm-fc-fresh",
            "author": "_operator",
            "content": "immediate message after readiness-gated start",
        },
        "source": "task_delivery",
    }]
    assert body._loop_events == [
        ("agent_loop_task_seen", task, {"delivery": "context_fallback", "normalized_as": "direct_message"})
    ]
