from completion_detector import detect_turn_outcome


def test_end_turn_single_text_is_terminal():
    outcome = detect_turn_outcome({"stop_reason": "end_turn", "content": [{"type": "text", "text": "Done."}]})

    assert outcome.is_terminal is True
    assert outcome.has_pending_tool_use is False
    assert outcome.final_text == "Done."
    assert outcome.stop_reason == "end_turn"


def test_tool_use_is_non_terminal_with_pending_tool():
    outcome = detect_turn_outcome({"stop_reason": "tool_use", "content": [{"type": "tool_use", "name": "send_message"}]})

    assert outcome.is_terminal is False
    assert outcome.has_pending_tool_use is True
    assert outcome.final_text == ""
    assert outcome.stop_reason == "tool_use"


def test_pause_turn_is_non_terminal_with_pending_tool():
    outcome = detect_turn_outcome({"stop_reason": "pause_turn", "content": []})

    assert outcome.is_terminal is False
    assert outcome.has_pending_tool_use is True
    assert outcome.stop_reason == "pause_turn"


def test_stop_sequence_is_terminal():
    outcome = detect_turn_outcome({"stop_reason": "stop_sequence", "content": [{"type": "text", "text": "Stopped."}]})

    assert outcome.is_terminal is True
    assert outcome.final_text == "Stopped."


def test_max_tokens_is_terminal():
    outcome = detect_turn_outcome({"stop_reason": "max_tokens", "content": [{"type": "text", "text": "Partial"}]})

    assert outcome.is_terminal is True
    assert outcome.final_text == "Partial"


def test_refusal_is_terminal_with_refusal_text():
    outcome = detect_turn_outcome({"stop_reason": "refusal", "content": [{"type": "text", "text": "I can't comply."}]})

    assert outcome.is_terminal is True
    assert outcome.final_text == "I can't comply."


def test_unknown_stop_reason_is_non_terminal():
    outcome = detect_turn_outcome({"stop_reason": "unexpected", "content": [{"type": "text", "text": "Maybe done."}]})

    assert outcome.is_terminal is False
    assert outcome.has_pending_tool_use is False
    assert outcome.final_text == "Maybe done."


def test_missing_stop_reason_is_non_terminal():
    outcome = detect_turn_outcome({"content": [{"type": "text", "text": "Maybe done."}]})

    assert outcome.is_terminal is False
    assert outcome.has_pending_tool_use is False
    assert outcome.stop_reason == ""


def test_empty_content_has_empty_final_text():
    outcome = detect_turn_outcome({"stop_reason": "end_turn", "content": []})

    assert outcome.is_terminal is True
    assert outcome.final_text == ""


def test_mixed_blocks_concatenate_text_and_tool_use_wins():
    outcome = detect_turn_outcome({
        "stop_reason": "end_turn",
        "content": [
            {"type": "text", "text": "One"},
            {"type": "tool_use", "name": "send_message"},
            {"type": "tool_result", "content": "ignored"},
            {"type": "text", "text": "Two"},
        ],
    })

    assert outcome.is_terminal is False
    assert outcome.has_pending_tool_use is True
    assert outcome.final_text == "One\nTwo"


def test_finish_reason_stop_is_terminal_without_stop_reason():
    outcome = detect_turn_outcome({
        "choices": [{
            "message": {"role": "assistant", "content": "Done."},
            "finish_reason": "stop",
        }],
    })

    assert outcome.is_terminal is True
    assert outcome.has_pending_tool_use is False
    assert outcome.final_text == "Done."
    assert outcome.stop_reason == ""


def test_finish_reason_tool_calls_is_non_terminal():
    outcome = detect_turn_outcome({
        "choices": [{
            "message": {
                "role": "assistant",
                "content": "Calling tool",
                "tool_calls": [{
                    "id": "call-1",
                    "type": "function",
                    "function": {"name": "send_message", "arguments": "{}"},
                }],
            },
            "finish_reason": "tool_calls",
        }],
    })

    assert outcome.is_terminal is False
    assert outcome.has_pending_tool_use is True
    assert outcome.final_text == "Calling tool"
