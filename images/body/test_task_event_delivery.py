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
