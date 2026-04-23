"""Provider-native task termination detection."""

from __future__ import annotations

from dataclasses import dataclass


@dataclass(frozen=True)
class TurnOutcome:
    """Runtime's interpretation of a provider response."""

    is_terminal: bool
    has_pending_tool_use: bool
    final_text: str
    stop_reason: str


def _content_blocks(response: dict) -> list[dict]:
    content = response.get("content")
    if isinstance(content, list):
        return [block for block in content if isinstance(block, dict)]

    choices = response.get("choices")
    if isinstance(choices, list) and choices:
        choice = choices[0] if isinstance(choices[0], dict) else {}
        message = choice.get("message") if isinstance(choice.get("message"), dict) else {}
        blocks: list[dict] = []
        message_content = message.get("content")
        if isinstance(message_content, list):
            blocks.extend(block for block in message_content if isinstance(block, dict))
        elif isinstance(message_content, str) and message_content:
            blocks.append({"type": "text", "text": message_content})
        for tool_call in message.get("tool_calls") or []:
            if isinstance(tool_call, dict):
                function = tool_call.get("function") if isinstance(tool_call.get("function"), dict) else {}
                blocks.append({
                    "type": "tool_use",
                    "id": tool_call.get("id", ""),
                    "name": function.get("name", ""),
                    "input": function.get("arguments", ""),
                })
        return blocks

    return []


def _stop_reason(response: dict) -> str:
    value = response.get("stop_reason")
    if isinstance(value, str):
        return value

    choices = response.get("choices")
    if isinstance(choices, list) and choices:
        choice = choices[0] if isinstance(choices[0], dict) else {}
        value = choice.get("stop_reason")
        if isinstance(value, str):
            return value
        message = choice.get("message") if isinstance(choice.get("message"), dict) else {}
        value = message.get("stop_reason")
        if isinstance(value, str):
            return value

    return ""


def _final_text(blocks: list[dict]) -> str:
    text_parts: list[str] = []
    for block in blocks:
        if block.get("type") != "text":
            continue
        text = block.get("text")
        if isinstance(text, str):
            text_parts.append(text)
    return "\n".join(text_parts)


def detect_anthropic(response: dict) -> TurnOutcome:
    """Parse an Anthropic API response into a TurnOutcome."""

    response = response if isinstance(response, dict) else {}
    blocks = _content_blocks(response)
    stop_reason = _stop_reason(response)
    has_tool_use_block = any(block.get("type") == "tool_use" for block in blocks)
    final_text = _final_text(blocks)

    if stop_reason == "tool_use":
        return TurnOutcome(False, True, final_text, stop_reason)
    if stop_reason == "pause_turn":
        return TurnOutcome(False, True, final_text, stop_reason)
    if has_tool_use_block:
        return TurnOutcome(False, True, final_text, stop_reason)
    if stop_reason in {"end_turn", "stop_sequence", "max_tokens", "refusal"}:
        return TurnOutcome(True, False, final_text, stop_reason)
    return TurnOutcome(False, False, final_text, stop_reason)
