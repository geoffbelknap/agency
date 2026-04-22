import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "body"))

from images.body.work_contract import (
    ActivationContext,
    ContractDefinition,
    EvaluationResult,
    EvidenceView,
    PactEvaluator,
    WorkContract,
    classify_activation,
    build_contract,
    classify_work,
    contract_definition,
    contract_prompt,
    extract_urls,
    format_blocked_completion,
    list_contract_kinds,
    validate_completion,
)


def test_activation_context_from_message_normalizes_fields():
    activation = ActivationContext.from_message(
        None,
        match_type="",
        mission_active=1,
        source=7,
        channel="dm:test",
        author="operator",
    )

    assert activation.content == ""
    assert activation.match_type == "direct"
    assert activation.mission_active is True
    assert activation.source == "7"
    assert activation.channel == "dm:test"
    assert activation.author == "operator"
    assert activation.to_dict() == {
        "content": "",
        "match_type": "direct",
        "source": "7",
        "channel": "dm:test",
        "author": "operator",
        "mission_active": True,
    }


def test_evaluation_result_serializes_compatibly():
    assert EvaluationResult("completed").to_dict() == {"verdict": "completed"}
    assert EvaluationResult(
        "needs_action",
        missing_evidence=("source_url",),
        message="Missing source.",
    ).to_dict() == {
        "verdict": "needs_action",
        "missing_evidence": ["source_url"],
        "message": "Missing source.",
    }


def test_evidence_view_normalizes_runtime_evidence():
    evidence = EvidenceView.from_dict({
        "tool_results": [{"tool": "provider-web-search"}, "ignored"],
        "observed": ["current_source", 7],
        "source_urls": [
            "https://nodejs.org/en,https://github.com/nodejs/node/releases.",
            "https://nodejs.org/en",
        ],
    })

    assert evidence.has_tool_or_observation() is True
    assert evidence.tool_results == ({"tool": "provider-web-search"},)
    assert evidence.observed == frozenset({"current_source", "7"})
    assert evidence.source_urls == (
        "https://nodejs.org/en",
        "https://github.com/nodejs/node/releases",
    )


def test_pact_evaluator_uses_explicit_registry():
    evaluator = PactEvaluator({
        "custom": ContractDefinition(
            kind="custom",
            summary="Custom test contract.",
            required_evidence=("custom_evidence",),
            answer_requirements=("custom_answer",),
        )
    })

    contract = evaluator.build_contract("custom", requires_action=True, reason="test")

    assert evaluator.list_contract_kinds() == ["custom"]
    assert contract.kind == "custom"
    assert contract.required_evidence == ["custom_evidence"]
    assert contract.answer_requirements == ["custom_answer"]
    with pytest.raises(ValueError, match="unknown work contract kind"):
        evaluator.contract_definition("current_info")


def test_pact_evaluator_classifies_typed_activation_contexts():
    evaluator = PactEvaluator()

    current_info = evaluator.classify_activation(ActivationContext(
        content="Find me MSFT's most recent SEC filing",
        match_type="direct",
        source="dm",
        channel="dm:test",
        author="operator",
    ))
    mission_task = evaluator.classify_activation(ActivationContext(
        content="please review the mission status",
        match_type="direct",
        mission_active=True,
    ))
    coordination = evaluator.classify_activation(ActivationContext(
        content="interesting background discussion",
        match_type="interest",
    ))

    assert current_info.kind == "current_info"
    assert mission_task.kind == "mission_task"
    assert coordination.kind == "coordination"


def test_classify_activation_wrapper_uses_default_evaluator():
    contract = classify_activation(ActivationContext(
        content="Find the latest Node.js release",
        match_type="direct",
        source="dm",
    ))

    assert contract.kind == "current_info"
    assert contract.requires_action is True


def test_pact_evaluator_returns_typed_evaluation_result():
    evaluator = PactEvaluator()
    contract = evaluator.classify_work("latest SEC filing").to_dict()

    verdict = evaluator.evaluate_completion(
        contract,
        {"tool_results": [{"tool": "web_search", "ok": True}]},
        "The source says X.",
    )

    assert isinstance(verdict, EvaluationResult)
    assert verdict.verdict == "needs_action"
    assert verdict.missing_evidence == ("source_url", "checked_date")
    assert verdict.to_dict()["missing_evidence"] == ["source_url", "checked_date"]


def test_contract_registry_contains_foundational_contracts():
    assert {
        "current_info",
        "code_change",
        "file_artifact",
        "external_side_effect",
        "operator_blocked",
    }.issubset(set(list_contract_kinds()))


def test_current_info_registry_entry_matches_classification_defaults():
    definition = contract_definition("current_info")
    contract = classify_work("Find me MSFT's most recent SEC filing")

    assert contract.required_evidence == list(definition.required_evidence)
    assert contract.answer_requirements == list(definition.answer_requirements)
    assert contract.summary == definition.summary


def test_unknown_contract_kind_fails_closed():
    with pytest.raises(ValueError, match="unknown work contract kind"):
        build_contract("not_registered", requires_action=True, reason="test")

    verdict = validate_completion(
        {"kind": "not_registered", "requires_action": True},
        {},
        "Done.",
    )
    assert verdict["verdict"] == "blocked"
    assert verdict["missing_evidence"] == ["known_contract_kind"]


def test_contract_prompt_fails_closed_for_unknown_action_contract():
    with pytest.raises(ValueError, match="unknown work contract kind"):
        contract_prompt(WorkContract(kind="not_registered", requires_action=True))


def test_classifies_latest_request_as_current_info():
    contract = classify_work("Find me MSFT's most recent SEC filing")

    assert contract.kind == "current_info"
    assert contract.requires_action is True
    assert contract.required_evidence == ["current_source_or_blocker"]
    assert contract.answer_requirements == [
        "direct_answer",
        "primary_or_official_source",
        "source_url",
        "checked_date",
        "ambiguous_category_clarified",
    ]
    prompt = contract_prompt(contract)
    assert "[WORK_CONTRACT]" in prompt
    assert "[ANSWER_CONTRACT]" in prompt
    assert "official or primary source URL" in prompt
    assert "checked/as-of date" in prompt


def test_classifies_greeting_as_chat():
    contract = classify_work("hi there")

    assert contract.kind == "chat"
    assert contract.requires_action is False
    assert contract_prompt(contract) == ""


def test_current_info_completion_requires_evidence_or_blocker():
    contract = classify_work("latest SEC filing").to_dict()

    verdict = validate_completion(contract, {"tool_results": [], "observed": []}, "The answer is 2024.")
    assert verdict["verdict"] == "needs_action"
    assert verdict["missing_evidence"] == ["current_source_or_blocker"]


def test_current_info_completion_accepts_blocker():
    contract = classify_work("latest SEC filing").to_dict()

    verdict = validate_completion(contract, {"tool_results": [], "observed": []}, "I cannot access a current source.")
    assert verdict["verdict"] == "blocked"
    assert "I cannot verify this from an official/current source without guessing." in verdict["message"]
    assert "Evidence checked: tools=none recorded" in verdict["message"]


def test_current_info_completion_requires_source_url_in_answer():
    contract = classify_work("latest SEC filing").to_dict()

    verdict = validate_completion(contract, {"tool_results": [{"tool": "web_search", "ok": True}]}, "The source says X.")
    assert verdict["verdict"] == "needs_action"
    assert verdict["missing_evidence"] == ["source_url", "checked_date"]


def test_current_info_completion_requires_checked_date_in_answer():
    contract = classify_work("latest SEC filing").to_dict()

    verdict = validate_completion(
        contract,
        {"tool_results": [{"tool": "web_search", "ok": True}]},
        "Microsoft filed an 8-K. Source: https://www.sec.gov/Archives/example",
    )
    assert verdict["verdict"] == "needs_action"
    assert verdict["missing_evidence"] == ["checked_date"]


def test_current_info_completion_rejects_vague_search_results_answer():
    contract = classify_work("latest SEC filing").to_dict()

    verdict = validate_completion(
        contract,
        {"tool_results": [{"tool": "web_search", "ok": True}]},
        "Based on the search results, Microsoft filed an 8-K. Source: https://www.sec.gov/Archives/example. Checked: April 22, 2026.",
    )
    assert verdict["verdict"] == "needs_action"
    assert verdict["missing_evidence"] == ["named_source"]


def test_current_info_completion_rejects_my_search_results_answer():
    contract = classify_work("latest SEC filing").to_dict()

    verdict = validate_completion(
        contract,
        {"tool_results": [{"tool": "web_search", "ok": True}]},
        "Based on my search results, Microsoft filed an 8-K. Source: https://www.sec.gov/Archives/example. Checked: April 22, 2026.",
    )
    assert verdict["verdict"] == "needs_action"
    assert verdict["missing_evidence"] == ["named_source"]


def test_current_info_completion_requires_absolute_date_when_requested():
    contract = classify_work("Find the latest stable Node.js release. Include the release date.").to_dict()

    assert "requested_absolute_date" in contract["answer_requirements"]
    verdict = validate_completion(
        contract,
        {"tool_results": [{"tool": "web_search", "ok": True}]},
        "Node.js 25.9.0 is the latest release. Source: https://nodejs.org/en/blog/release/v25.9.0. Checked: April 22, 2026.",
    )
    assert verdict["verdict"] == "needs_action"
    assert verdict["missing_evidence"] == ["requested_absolute_date"]


def test_current_info_completion_accepts_absolute_date_when_requested():
    contract = classify_work("Find the latest stable Node.js release. Include the release date.").to_dict()

    verdict = validate_completion(
        contract,
        {"tool_results": [{"tool": "web_search", "ok": True}]},
        "Node.js 25.9.0 was released on April 1, 2026. Source: Node.js https://nodejs.org/en/blog/release/v25.9.0. Checked: April 22, 2026.",
    )
    assert verdict["verdict"] == "completed"


def test_current_info_completion_accepts_tool_evidence_and_answer_contract():
    contract = classify_work("latest SEC filing").to_dict()

    verdict = validate_completion(
        contract,
        {"tool_results": [{"tool": "web_search", "ok": True}]},
        "Microsoft's latest SEC filing is an 8-K. Source: SEC EDGAR https://www.sec.gov/Archives/example. Checked: April 22, 2026.",
    )
    assert verdict["verdict"] == "completed"


def test_extract_urls_trims_trailing_sentence_punctuation():
    assert extract_urls("Source: https://nodejs.org/en/blog/release/v25.9.0.") == [
        "https://nodejs.org/en/blog/release/v25.9.0"
    ]


def test_extract_urls_splits_comma_separated_provider_metadata():
    urls = extract_urls(
        "https://nodejs.org/en,https://github.com/nodejs/node/releases, "
        "https://example.com/path."
    )

    assert urls == [
        "https://nodejs.org/en",
        "https://github.com/nodejs/node/releases",
        "https://example.com/path",
    ]


def test_current_info_completion_requires_answer_url_from_evidence_when_available():
    contract = classify_work("latest SEC filing").to_dict()

    verdict = validate_completion(
        contract,
        {
            "tool_results": [{"tool": "provider-web-search", "ok": True}],
            "source_urls": ["https://www.sec.gov/Archives/edgar/data/example"],
        },
        (
            "Microsoft's latest SEC filing is an 8-K. "
            "Source: https://example.com/secondary. Checked: April 22, 2026."
        ),
    )

    assert verdict["verdict"] == "needs_action"
    assert verdict["missing_evidence"] == ["source_url_from_evidence"]


def test_current_info_completion_accepts_answer_url_from_evidence():
    contract = classify_work("latest SEC filing").to_dict()

    verdict = validate_completion(
        contract,
        {
            "tool_results": [{"tool": "provider-web-search", "ok": True}],
            "source_urls": ["https://www.sec.gov/Archives/edgar/data/example"],
        },
        (
            "Microsoft's latest SEC filing is an 8-K. "
            "Source: SEC EDGAR https://www.sec.gov/Archives/edgar/data/example. "
            "Checked: April 22, 2026."
        ),
    )

    assert verdict["verdict"] == "completed"


def test_format_blocked_completion_summarizes_tool_evidence():
    contract = classify_work("Find the latest stable Node.js release").to_dict()

    response = format_blocked_completion(
        contract,
        {
            "tool_results": [{"tool": "provider-web-search", "ok": True}],
            "source_urls": ["https://nodejs.org/en/blog/release/v25.9.0"],
        },
        "I cannot verify this.",
        checked_at="April 22, 2026",
    )

    assert response == (
        "I cannot verify this from an official/current source without guessing.\n"
        "\n"
        "- Blocked: Available source URLs did not satisfy the official/current-source evidence contract.\n"
        "- Evidence checked: tools=provider-web-search\n"
        "- Source URLs observed:\n"
        "  - https://nodejs.org/en/blog/release/v25.9.0\n"
        "- What would unblock this: an official or primary source URL that directly supports the requested current fact.\n"
        "- Checked: April 22, 2026."
    )


def test_format_blocked_completion_caps_observed_urls():
    contract = classify_work("Find the latest stable Node.js release").to_dict()
    urls = [f"https://example.com/{idx}" for idx in range(7)]

    response = format_blocked_completion(
        contract,
        {
            "tool_results": [{"tool": "provider-web-search", "ok": True}],
            "source_urls": urls,
        },
        checked_at="April 22, 2026",
    )

    assert "https://example.com/0" in response
    assert "https://example.com/4" in response
    assert "https://example.com/5" not in response
    assert "...and 2 more observed URLs." in response


def test_format_blocked_completion_leaves_non_current_info_content_alone():
    contract = classify_work("debug the failing test").to_dict()

    assert format_blocked_completion(contract, {}, "I cannot run pytest.") == "I cannot run pytest."
