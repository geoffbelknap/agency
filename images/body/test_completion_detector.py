from completion_detector import detect_anthropic


def test_end_turn_single_text_is_terminal():
    outcome = detect_anthropic({"stop_reason": "end_turn", "content": [{"type": "text", "text": "Done."}]})

    assert outcome.is_terminal is True
    assert outcome.has_pending_tool_use is False
    assert outcome.final_text == "Done."
    assert outcome.stop_reason == "end_turn"


def test_tool_use_is_non_terminal_with_pending_tool():
    outcome = detect_anthropic({"stop_reason": "tool_use", "content": [{"type": "tool_use", "name": "send_message"}]})

    assert outcome.is_terminal is False
    assert outcome.has_pending_tool_use is True
    assert outcome.final_text == ""
    assert outcome.stop_reason == "tool_use"


def test_pause_turn_is_non_terminal_with_pending_tool():
    outcome = detect_anthropic({"stop_reason": "pause_turn", "content": []})

    assert outcome.is_terminal is False
    assert outcome.has_pending_tool_use is True
    assert outcome.stop_reason == "pause_turn"


def test_stop_sequence_is_terminal():
    outcome = detect_anthropic({"stop_reason": "stop_sequence", "content": [{"type": "text", "text": "Stopped."}]})

    assert outcome.is_terminal is True
    assert outcome.final_text == "Stopped."


def test_max_tokens_is_terminal():
    outcome = detect_anthropic({"stop_reason": "max_tokens", "content": [{"type": "text", "text": "Partial"}]})

    assert outcome.is_terminal is True
    assert outcome.final_text == "Partial"


def test_refusal_is_terminal_with_refusal_text():
    outcome = detect_anthropic({"stop_reason": "refusal", "content": [{"type": "text", "text": "I can't comply."}]})

    assert outcome.is_terminal is True
    assert outcome.final_text == "I can't comply."


def test_unknown_stop_reason_is_non_terminal():
    outcome = detect_anthropic({"stop_reason": "unexpected", "content": [{"type": "text", "text": "Maybe done."}]})

    assert outcome.is_terminal is False
    assert outcome.has_pending_tool_use is False
    assert outcome.final_text == "Maybe done."


def test_missing_stop_reason_is_non_terminal():
    outcome = detect_anthropic({"content": [{"type": "text", "text": "Maybe done."}]})

    assert outcome.is_terminal is False
    assert outcome.has_pending_tool_use is False
    assert outcome.stop_reason == ""


def test_empty_content_has_empty_final_text():
    outcome = detect_anthropic({"stop_reason": "end_turn", "content": []})

    assert outcome.is_terminal is True
    assert outcome.final_text == ""


def test_mixed_blocks_concatenate_text_and_tool_use_wins():
    outcome = detect_anthropic({
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
