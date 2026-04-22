import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "body"))

from images.body.work_contract import (
    classify_work,
    contract_prompt,
    extract_urls,
    format_blocked_completion,
    validate_completion,
)


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
        "Blocked: Available source URLs did not satisfy the official/current-source evidence contract.\n"
        "Evidence checked: tools=provider-web-search\n"
        "Source URLs observed: https://nodejs.org/en/blog/release/v25.9.0\n"
        "What would unblock this: an official or primary source URL that directly supports the requested current fact.\n"
        "Checked: April 22, 2026."
    )


def test_format_blocked_completion_leaves_non_current_info_content_alone():
    contract = classify_work("debug the failing test").to_dict()

    assert format_blocked_completion(contract, {}, "I cannot run pytest.") == "I cannot run pytest."
