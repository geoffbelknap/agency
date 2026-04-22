from datetime import datetime, timezone

import pytest

from pact_engine import (
    ActivationContext,
    EvidenceEntry,
    ExecutionState,
    ToolObservation,
    ToolProvenance,
    ToolStatus,
    WorkContract,
    evaluate_pre_commit,
)


NOW = datetime(2026, 4, 22, 12, 0, tzinfo=timezone.utc)


def _activation(content: str = "hello") -> ActivationContext:
    return ActivationContext.from_message(
        content,
        source="idle_direct:dm-scout:operator",
        channel="dm-scout",
        author="operator",
    )


def _chat_state() -> ExecutionState:
    return ExecutionState(
        task_id="task-123",
        agent="scout",
        activation=_activation(),
        contract=WorkContract(kind="chat", requires_action=False),
    )


def _current_info_state() -> ExecutionState:
    return ExecutionState(
        task_id="task-123",
        agent="scout",
        activation=_activation("Find the latest release."),
        contract=WorkContract(
            kind="current_info",
            requires_action=True,
            required_evidence=["current_source_or_blocker"],
            answer_requirements=[],
        ),
    )


def _verdict(content: str, state: ExecutionState | None = None):
    return evaluate_pre_commit(state or _chat_state(), content=content, now=NOW)


def test_chat_let_me_search_without_evidence_blocks():
    verdict = _verdict("Let me search for the latest release.")

    assert verdict.committable is False
    assert verdict.reasons == ("honesty:simulated_tool_use:Let me search",)
    assert verdict.missing == ("mediated_tool_result",)


def test_chat_i_searched_without_evidence_blocks():
    verdict = _verdict("I searched the web and found the answer.")

    assert verdict.committable is False
    assert verdict.reasons == ("honesty:simulated_tool_use:I searched",)


def test_chat_i_searched_with_legacy_tool_result_passes_honesty_layer():
    state = _chat_state()
    state.evidence.record_entry(EvidenceEntry(kind="tool_result", producer="provider-web-search"))

    verdict = _verdict("I searched the web and found the answer.", state)

    assert verdict.committable is True
    assert verdict.reasons == ("committable",)


@pytest.mark.parametrize(
    "content",
    [
        "You can search for X.",
        "Consider searching for X.",
        "It might be worth searching for X.",
        "A search would probably find X.",
        "A lookup would probably find X.",
    ],
)
def test_hypothetical_or_user_addressed_search_phrases_do_not_match(content):
    verdict = _verdict(content)

    assert verdict.committable is True
    assert verdict.reasons == ("committable",)


@pytest.mark.parametrize(
    "content",
    [
        "I'll search for that now.",
        "I will search for that now.",
        "I can search for that.",
    ],
)
def test_future_intent_or_capability_search_phrases_do_not_match(content):
    verdict = _verdict(content)

    assert verdict.committable is True
    assert verdict.reasons == ("committable",)


def test_current_info_based_on_my_research_without_tool_result_blocks():
    state = _current_info_state()

    verdict = _verdict("Based on my research, the latest release is 1.2.3.", state)

    assert verdict.committable is False
    assert verdict.reasons == ("honesty:simulated_tool_use:Based on my research",)


def test_current_info_based_on_my_research_with_tool_result_passes_honesty_layer():
    state = _current_info_state()
    state.evidence.record_tool_result("provider-web-search", True)

    verdict = _verdict("Based on my research, the latest release is 1.2.3.", state)

    assert verdict.committable is True
    assert verdict.reasons == ("committable",)


def test_case_insensitive_tool_announcement_matching():
    verdict = _verdict("BASED ON MY SEARCH, the answer is X.")

    assert verdict.committable is False
    assert verdict.reasons == ("honesty:simulated_tool_use:Based on my search",)


def test_i_ran_false_positive_guard_does_not_match_ordinary_phrase():
    verdict = _verdict("I ran into an issue with the wording.")

    assert verdict.committable is True
    assert verdict.reasons == ("committable",)


def test_multiple_patterns_use_first_match_only():
    verdict = _verdict("I searched the web. Based on my research, the answer is X.")

    assert verdict.committable is False
    assert verdict.reasons == ("honesty:simulated_tool_use:I searched",)


def test_typed_provider_tool_observation_counts_as_qualifying_tool_result():
    state = _chat_state()
    state.tool_observations.append(
        ToolObservation(
            tool="provider-web-search",
            status=ToolStatus.ok,
            provenance=ToolProvenance.provider,
        )
    )

    verdict = _verdict("I searched the web and found the answer.", state)

    assert verdict.committable is True
    assert verdict.reasons == ("committable",)


def test_typed_error_tool_observation_does_not_count_as_qualifying_tool_result():
    state = _chat_state()
    state.tool_observations.append(
        ToolObservation(
            tool="provider-web-search",
            status=ToolStatus.error,
            provenance=ToolProvenance.provider,
        )
    )

    verdict = _verdict("I searched the web and found the answer.", state)

    assert verdict.committable is False
    assert verdict.reasons == ("honesty:simulated_tool_use:I searched",)
    assert verdict.missing == ("mediated_tool_result",)


def test_hank_replay_empty_chat_evidence_blocks_simulated_tool_use():
    state = _chat_state()
    state.tool_observations.clear()
    content = (
        "Let me search for the current release notes and check the latest maintainer guidance. "
        "Based on my research, the project now recommends the updated migration path and "
        "there are no active blockers for adopting it."
    )

    verdict = _verdict(content, state)

    assert verdict.committable is False
    assert verdict.reasons[0].startswith("honesty:simulated_tool_use:")
    assert verdict.missing == ("mediated_tool_result",)

