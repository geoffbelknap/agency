import sys
from dataclasses import dataclass, field
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "body"))

from images.body.work_contract import EvidenceLedger, classify_work, validate_completion


@dataclass(frozen=True)
class PactTrajectoryFixture:
    name: str
    activation: str
    expected_contract_kind: str
    evidence: dict
    output: str
    expected_terminal_outcome: str
    required_evidence: tuple[str, ...] = ()
    output_assertions: tuple[str, ...] = ()
    forbidden_actions: tuple[str, ...] = ()
    expected_tool_classes: tuple[str, ...] = ()
    expected_route: str = "body-runtime"


@dataclass(frozen=True)
class PactTrajectoryResult:
    contract_kind: str
    terminal_outcome: str
    missing_evidence: tuple[str, ...] = ()
    output: str = ""


def _run_fixture(fixture: PactTrajectoryFixture) -> PactTrajectoryResult:
    contract = classify_work(fixture.activation).to_dict()
    verdict = validate_completion(contract, fixture.evidence, fixture.output)
    return PactTrajectoryResult(
        contract_kind=contract["kind"],
        terminal_outcome=str(verdict["verdict"]),
        missing_evidence=tuple(verdict.get("missing_evidence") or ()),
        output=fixture.output,
    )


def _ledger_with_current_source(url: str) -> dict:
    ledger = EvidenceLedger()
    ledger.record_tool_result("provider-web-search", True)
    ledger.observe("current_source", producer="provider-web-search")
    ledger.record_source_url(url, producer="provider-web-search")
    return ledger.to_dict()


def _ledger_with_artifact(path: str) -> dict:
    ledger = EvidenceLedger()
    ledger.record_artifact_path(path, metadata={"artifact_id": "task-123"})
    return ledger.to_dict()


def _ledger_with_code_change(path: str, command: str) -> dict:
    ledger = EvidenceLedger()
    ledger.record_tool_result("write_file", True)
    ledger.record_changed_file(path, producer="write_file")
    ledger.record_tool_result("execute_command", True)
    ledger.record_validation_result(command, True, producer="execute_command", metadata={"exit_code": 0})
    return ledger.to_dict()


TRAJECTORY_FIXTURES = (
    PactTrajectoryFixture(
        name="current-info-with-source",
        activation="Find the latest stable Node.js release",
        expected_contract_kind="current_info",
        expected_tool_classes=("current-info",),
        required_evidence=("current_source_or_blocker",),
        evidence=_ledger_with_current_source("https://nodejs.org/en/blog/release/v24.15.0"),
        output=(
            "Node.js 24.15.0 LTS is the latest stable release. "
            "Source: Node.js https://nodejs.org/en/blog/release/v24.15.0. "
            "Checked: April 22, 2026."
        ),
        expected_terminal_outcome="completed",
        output_assertions=("https://nodejs.org/en/blog/release/v24.15.0", "Checked: April 22, 2026"),
    ),
    PactTrajectoryFixture(
        name="current-info-without-evidence",
        activation="Find the latest stable Node.js release",
        expected_contract_kind="current_info",
        expected_tool_classes=("current-info",),
        required_evidence=("current_source_or_blocker",),
        evidence={},
        output="Node.js 24.15.0 LTS is the latest stable release.",
        expected_terminal_outcome="needs_action",
    ),
    PactTrajectoryFixture(
        name="file-artifact-with-runtime-path",
        activation="Create a markdown report summarizing the release notes",
        expected_contract_kind="file_artifact",
        required_evidence=("artifact_path_or_blocker",),
        evidence=_ledger_with_artifact(".results/task-123.md"),
        output="Created the markdown report: .results/task-123.md",
        expected_terminal_outcome="completed",
        output_assertions=(".results/task-123.md",),
    ),
    PactTrajectoryFixture(
        name="code-change-with-validation",
        activation="Fix the failing pytest test in the parser module",
        expected_contract_kind="code_change",
        expected_tool_classes=("file-write", "validation"),
        required_evidence=("code_change_result_or_blocker", "tests_or_blocker"),
        evidence=_ledger_with_code_change("parser.py", "pytest tests/test_parser.py"),
        output="Changed parser.py. Validation: pytest tests/test_parser.py",
        expected_terminal_outcome="completed",
        output_assertions=("parser.py", "pytest tests/test_parser.py"),
    ),
    PactTrajectoryFixture(
        name="operator-blocked-with-unblocker",
        activation="I am blocked waiting for operator approval to continue",
        expected_contract_kind="operator_blocked",
        evidence={},
        output="Blocked: approval is missing. What would unblock this: operator approval to continue.",
        expected_terminal_outcome="blocked",
        output_assertions=("Blocked:", "operator approval"),
        forbidden_actions=("external-side-effect",),
    ),
)


def test_pact_trajectory_fixtures_cover_foundational_contracts():
    assert {fixture.expected_contract_kind for fixture in TRAJECTORY_FIXTURES} == {
        "current_info",
        "file_artifact",
        "code_change",
        "operator_blocked",
    }


def test_pact_trajectory_fixtures_have_contract_metadata():
    for fixture in TRAJECTORY_FIXTURES:
        assert fixture.name
        assert fixture.activation
        assert fixture.expected_route == "body-runtime"
        assert fixture.expected_terminal_outcome in {"completed", "blocked", "needs_action"}


def test_pact_trajectory_fixtures_evaluate_as_expected():
    for fixture in TRAJECTORY_FIXTURES:
        result = _run_fixture(fixture)

        assert result.contract_kind == fixture.expected_contract_kind, fixture.name
        assert result.terminal_outcome == fixture.expected_terminal_outcome, fixture.name
        for needle in fixture.output_assertions:
            assert needle in result.output, fixture.name


def test_pact_trajectory_missing_evidence_is_explicit():
    fixture = next(item for item in TRAJECTORY_FIXTURES if item.name == "current-info-without-evidence")
    result = _run_fixture(fixture)

    assert result.terminal_outcome == "needs_action"
    assert result.missing_evidence == ("current_source_or_blocker",)
