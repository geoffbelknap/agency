import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "body"))

from images.body.work_contract import classify_work, contract_prompt, validate_completion


def test_classifies_latest_request_as_current_info():
    contract = classify_work("Find me MSFT's most recent SEC filing")

    assert contract.kind == "current_info"
    assert contract.requires_action is True
    assert contract.required_evidence == ["current_source_or_blocker"]
    assert "[WORK_CONTRACT]" in contract_prompt(contract)


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


def test_current_info_completion_accepts_tool_evidence():
    contract = classify_work("latest SEC filing").to_dict()

    verdict = validate_completion(contract, {"tool_results": [{"tool": "web_search", "ok": True}]}, "The source says X.")
    assert verdict["verdict"] == "completed"
