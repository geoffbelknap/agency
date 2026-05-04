from session_scratchpad import build_session_scratchpad, format_recent_transcript


def test_session_scratchpad_resolves_follow_up_against_recent_request():
    scratchpad = build_session_scratchpad(
        channel="dm-jarvis",
        participant="_operator",
        latest_message="Whatever one is most recent",
        recent_messages=[
            {"author": "_operator", "content": "PLTR's more recent SEC filing"},
            {"author": "jarvis", "content": "Could you clarify the filing type?"},
            {"author": "_operator", "content": "Whatever one is most recent"},
        ],
    )

    section = scratchpad.to_prompt_section()

    assert scratchpad.follow_up is True
    assert scratchpad.previous_user_request == "PLTR's more recent SEC filing"
    assert "[SESSION_CONTEXT]" in section
    assert "active_entities: PLTR, SEC" in section
    assert "most_recent_user_request: PLTR's more recent SEC filing" in section
    assert "temporary session state" in section


def test_format_recent_transcript_bounds_and_skips_empty_messages():
    transcript = format_recent_transcript(
        [
            {"author": "_operator", "content": ""},
            {"author": "_operator", "content": "A" * 600},
        ]
    )

    assert "Recent conversation in this channel:" in transcript
    assert "_operator: " + ("A" * 500) + "..." in transcript
