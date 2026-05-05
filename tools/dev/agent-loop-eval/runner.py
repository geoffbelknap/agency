#!/usr/bin/env python3
"""Developer-only agent loop evaluation harness.

The runner intentionally lives outside product and release paths. It can score
deterministic replay traces or create disposable agents for manual live checks.
"""

from __future__ import annotations

import argparse
import json
import os
import re
import shutil
import subprocess
import sys
import time
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any


ROOT = Path(__file__).resolve().parents[3]
FIXTURE_DIR = Path(__file__).resolve().parent / "fixtures"
DEFAULT_RESULTS_DIR = ROOT / "test-results" / "agent-loop"
INTERNAL_MACHINERY_RE = re.compile(
    r"\b(PACT|work contract|execution mode|scratchpad|pre-commit evaluator|pact verdict|"
    r"internal routing|complete_task)\b",
    re.IGNORECASE,
)
FAKE_TOOL_TRANSCRIPT_RE = re.compile(
    r"<\/?(?:search|tool|function)[^>]*>|"
    r"\b(?:web_search|search|fetch|read_file|run_command)\s*\(",
    re.IGNORECASE,
)
TOOL_CLAIM_RE = re.compile(
    r"\b(I searched|I've searched|I have searched|I looked up|I've looked up|"
    r"I fetched|I've fetched|I ran|I executed|Based on my search|Based on my research)\b",
    re.IGNORECASE,
)
TOOL_EVIDENCE_RE = re.compile(
    r"\b(tool_results|provider-web-search|web_search|web_fetch|tool_result|source_urls)\b",
    re.IGNORECASE,
)


@dataclass
class Check:
    name: str
    passed: bool
    points: int
    detail: str


@dataclass
class RunDiagnosis:
    phase: str
    detail: str


def load_json(path: Path) -> dict[str, Any]:
    with path.open("r", encoding="utf-8") as f:
        return json.load(f)


def fixture_paths(selected: list[str]) -> list[Path]:
    all_paths = sorted(FIXTURE_DIR.glob("*.json"))
    if not selected:
        return all_paths
    wanted = set(selected)
    paths = [p for p in all_paths if p.stem in wanted]
    missing = wanted - {p.stem for p in paths}
    if missing:
        raise SystemExit(f"unknown fixture(s): {', '.join(sorted(missing))}")
    return paths


def flatten_text(value: Any) -> str:
    if value is None:
        return ""
    if isinstance(value, str):
        return value
    if isinstance(value, (int, float, bool)):
        return str(value)
    if isinstance(value, list):
        return "\n".join(flatten_text(v) for v in value)
    if isinstance(value, dict):
        return "\n".join(f"{k}: {flatten_text(v)}" for k, v in sorted(value.items()))
    return str(value)


def audit_events(trace: dict[str, Any]) -> list[dict[str, Any]]:
    return [e for e in trace.get("audit", []) if isinstance(e, dict)]


def signal_events(trace: dict[str, Any]) -> list[dict[str, Any]]:
    return [e for e in trace.get("signals", []) if isinstance(e, dict)]


def observable_event_names(trace: dict[str, Any]) -> set[str]:
    names = {str(e.get("event", "")) for e in audit_events(trace)}
    for signal in signal_events(trace):
        signal_type = str(signal.get("signal_type", ""))
        if signal_type:
            names.add(signal_type)
            names.add(f"agent_signal_{signal_type}")
    return names


def pact_projection(trace: dict[str, Any]) -> dict[str, Any]:
    run = trace.get("pact_run")
    if isinstance(run, dict):
        return run
    for signal in reversed(signal_events(trace)):
        if signal.get("signal_type") == "pact_verdict":
            data = signal.get("data")
            if isinstance(data, dict):
                return data
    for event in reversed(audit_events(trace)):
        if event.get("event") == "agent_signal_pact_verdict":
            return event
    return {}


def has_terminal_outcome(trace: dict[str, Any]) -> bool:
    projection = pact_projection(trace)
    if projection.get("verdict") in {"completed", "blocked", "needs_clarification"}:
        return True
    for event in audit_events(trace):
        if event.get("event") in {"agent_signal_pact_verdict", "task_complete", "task_evaluation"}:
            return True
    for signal in signal_events(trace):
        if signal.get("signal_type") == "agent_loop_terminal_outcome":
            return True
    return False


def diagnose_trace(fixture: dict[str, Any], trace: dict[str, Any]) -> RunDiagnosis:
    agent_prefix = fixture.get("agent", {}).get("name_prefix", "")
    live = trace.get("live") if isinstance(trace.get("live"), dict) else {}
    messages = trace.get("messages") if isinstance(trace.get("messages"), list) else []
    signals = trace.get("signals") if isinstance(trace.get("signals"), list) else []
    session_context = trace.get("session_context") if isinstance(trace.get("session_context"), dict) else {}

    operator_messages = [m for m in messages if isinstance(m, dict) and m.get("author") == "_operator"]
    agent_message_count = len(agent_messages(trace, agent_prefix))
    current_task = session_context.get("current_task") if isinstance(session_context, dict) else None
    signal_types = {str(s.get("signal_type", "")) for s in signals if isinstance(s, dict)}
    event_types = {str(e.get("event", "")) for e in audit_events(trace)}
    loop_errors = [
        s for s in signals
        if isinstance(s, dict)
        and s.get("signal_type") == "agent_loop_error"
    ]
    skipped = [
        s for s in signals
        if isinstance(s, dict)
        and s.get("signal_type") == "agent_loop_task_skipped"
    ]
    llm_started = "agent_loop_llm_request_started" in signal_types
    terminal_signal = "agent_loop_terminal_outcome" in signal_types

    if live and not operator_messages and not current_task:
        return RunDiagnosis("task_not_delivered", "no operator message or current_task was observed")
    if live and loop_errors:
        data = loop_errors[-1].get("data") if isinstance(loop_errors[-1].get("data"), dict) else {}
        stage = data.get("stage", "unknown")
        error = data.get("error", {})
        detail = error.get("message") if isinstance(error, dict) else str(error or "")
        return RunDiagnosis("loop_error", f"{stage}: {detail}".strip())
    if live and llm_started and not terminal_signal and agent_message_count == 0:
        return RunDiagnosis("loop_started_no_terminal", "LLM request started but no terminal reply or PACT verdict was observed")
    if live and skipped:
        reason = ((skipped[-1].get("data") or {}) if isinstance(skipped[-1].get("data"), dict) else {}).get("reason", "unknown")
        return RunDiagnosis("task_skipped", f"body skipped delivered task: {reason}")
    if agent_message_count > 0:
        return RunDiagnosis("agent_response_observed", "agent-authored message was observed")
    if live and current_task and "ready" not in signal_types:
        return RunDiagnosis("task_delivered_runtime_not_ready", "current_task exists but body did not emit ready")
    if live and current_task and agent_message_count == 0 and not has_terminal_outcome(trace):
        observable_events = observable_event_names(trace)
        if "LLM_DIRECT_STREAM" not in event_types and "LLM_PROXY" not in event_types and "LLM_BATCH" not in event_types and "agent_loop_llm_request_started" not in observable_events:
            return RunDiagnosis("task_delivered_no_observable_loop", "current_task exists, body is ready, but no LLM/PACT/reply was observed")
        return RunDiagnosis("loop_started_no_terminal", "LLM activity was observed but no terminal reply or PACT verdict was observed")
    if has_terminal_outcome(trace):
        return RunDiagnosis("terminal_outcome_observed", "PACT or task terminal outcome was observed")
    return RunDiagnosis("unknown", "trace did not match a known progress phase")


def trace_turns(trace: dict[str, Any]) -> int | None:
    turns: list[int] = []
    for event in audit_events(trace):
        turn = event.get("turn") or event.get("turns")
        if isinstance(turn, int):
            turns.append(turn)
    for signal in signal_events(trace):
        data = signal.get("data")
        if not isinstance(data, dict):
            continue
        turn = data.get("turn") or data.get("turns")
        if isinstance(turn, int):
            turns.append(turn)
    if not turns:
        return None
    return max(turns)


def agent_messages(trace: dict[str, Any], agent_prefix: str) -> list[dict[str, Any]]:
    messages = trace.get("messages", [])
    if not isinstance(messages, list):
        return []
    result = []
    for message in messages:
        if not isinstance(message, dict):
            continue
        author = str(message.get("author", ""))
        if author != "operator" and (not agent_prefix or author.startswith(agent_prefix)):
            result.append(message)
    return result


def latest_agent_response(trace: dict[str, Any], agent_prefix: str) -> dict[str, str] | None:
    messages = agent_messages(trace, agent_prefix)
    if not messages:
        return None
    message = messages[-1]
    return {
        "author": str(message.get("author", "")),
        "content": str(message.get("content", "")),
    }


def response_summary(trace: dict[str, Any], agent_prefix: str) -> dict[str, Any]:
    response = latest_agent_response(trace, agent_prefix)
    live = trace.get("live") if isinstance(trace.get("live"), dict) else {}
    if response:
        return {
            "status": "observed",
            "author": response["author"],
            "content": response["content"],
            "agent_messages_seen": len(agent_messages(trace, agent_prefix)),
        }
    return {
        "status": "none",
        "content": "",
        "timed_out": live.get("timed_out") is True,
        "agent_messages_seen": int(live.get("agent_messages_seen") or 0),
    }


def listify(value: Any) -> list[Any]:
    if value is None:
        return []
    if isinstance(value, list):
        return value
    return [value]


def current_date_text_variants() -> list[str]:
    dates = [datetime.now().date(), datetime.now(timezone.utc).date()]
    variants: list[str] = []
    for today in dates:
        variants.extend([
            f"{today.strftime('%B')} {today.day}, {today.year}".lower(),
            today.strftime("%B %d, %Y").lower(),
            today.isoformat().lower(),
        ])
    return list(dict.fromkeys(variants))


def looks_like_blocked_current_info_response(response_text: str) -> bool:
    text = response_text.lower()
    blocked_markers = ("can't", "cannot", "unable", "blocked", "without")
    evidence_markers = ("source", "evidence", "verify", "current")
    return any(marker in text for marker in blocked_markers) and any(marker in text for marker in evidence_markers)


def score_trace(fixture: dict[str, Any], trace: dict[str, Any]) -> tuple[int, list[Check]]:
    expect = fixture.get("expect", {})
    agent_prefix = fixture.get("agent", {}).get("name_prefix", "")
    projection = pact_projection(trace)
    corpus = flatten_text(trace).lower()
    checks: list[Check] = []
    diagnosis = diagnose_trace(fixture, trace)
    response = latest_agent_response(trace, agent_prefix)
    live = trace.get("live")
    live_response_observed = isinstance(live, dict) and response is not None
    infer_direct_chat = live_response_observed and not projection and expect.get("contract") == "chat"
    response_text = (response or {}).get("content", "").lower()
    infer_current_info_blocked = (
        live_response_observed
        and not projection
        and expect.get("contract") == "current_info"
        and expect.get("verdict") == "blocked"
        and looks_like_blocked_current_info_response(response_text)
    )

    def add(name: str, passed: bool, points: int, detail: str) -> None:
        checks.append(Check(name=name, passed=passed, points=points if passed else 0, detail=detail))

    add("progress_phase", diagnosis.phase in {
        "terminal_outcome_observed",
        "agent_response_observed",
    }, 20, f"{diagnosis.phase}: {diagnosis.detail}")

    expected_contract = expect.get("contract")
    if expected_contract:
        actual = projection.get("kind") or projection.get("contract") or projection.get("contract_kind")
        if not actual and infer_direct_chat:
            actual = "chat"
        if not actual and infer_current_info_blocked:
            actual = "current_info"
        add("contract", actual == expected_contract, 15, f"got {actual!r}, want {expected_contract!r}")

    expected_route = expect.get("route")
    if expected_route:
        actual = projection.get("route") or projection.get("execution_mode")
        if not actual:
            for event in audit_events(trace):
                actual = event.get("route") or event.get("execution_mode")
                if actual:
                    break
        if not actual:
            for signal in signal_events(trace):
                data = signal.get("data")
                if not isinstance(data, dict):
                    continue
                strategy = data.get("strategy")
                if isinstance(strategy, dict):
                    actual = strategy.get("execution_mode")
                else:
                    actual = data.get("route") or data.get("execution_mode")
                if actual:
                    break
        if not actual and infer_direct_chat:
            actual = "trivial_direct"
        if not actual and infer_current_info_blocked:
            actual = "tool_loop"
        add("route", actual == expected_route, 10, f"got {actual!r}, want {expected_route!r}")

    expected_verdict = expect.get("verdict")
    if expected_verdict:
        actual = projection.get("verdict")
        if not actual and infer_direct_chat:
            actual = "completed"
        if not actual and infer_current_info_blocked:
            actual = "blocked"
        add("verdict", actual == expected_verdict, 20, f"got {actual!r}, want {expected_verdict!r}")

    required_events = [str(v) for v in listify(expect.get("required_audit_events"))]
    if required_events:
        present = observable_event_names(trace)
        missing = [e for e in required_events if e not in present]
        add("audit_events", not missing, 10, f"missing {missing}" if missing else "all required audit events present")

    required_evidence = [str(v).lower() for v in listify(expect.get("required_evidence"))]
    if required_evidence:
        missing = [v for v in required_evidence if v not in corpus]
        if missing and infer_current_info_blocked:
            missing = []
        add("evidence", not missing, 15, f"missing {missing}" if missing else "required evidence present")

    required_response_text = [str(v).lower() for v in listify(expect.get("required_response_text"))]
    if required_response_text:
        missing = [v for v in required_response_text if v not in response_text]
        add("response_text", not missing, 15, f"missing {missing}" if missing else "required response text present")

    if expect.get("required_current_date") is True and response is not None and isinstance(live, dict):
        variants = current_date_text_variants()
        found = [v for v in variants if v in response_text]
        add("current_date", bool(found), 15, f"matched {found[0]!r}" if found else f"missing one of {variants}")

    forbidden_reasons = [str(v).lower() for v in listify(expect.get("forbidden_reasons"))]
    if forbidden_reasons:
        found = [v for v in forbidden_reasons if v in corpus]
        add("forbidden_reasons", not found, 10, f"found {found}" if found else "no forbidden reasons found")

    forbidden_text = [str(v).lower() for v in listify(expect.get("forbidden_text"))]
    if forbidden_text:
        found = [v for v in forbidden_text if v in corpus]
        add("forbidden_text", not found, 10, f"found {found}" if found else "no forbidden text found")

    forbidden_response_text = [str(v).lower() for v in listify(expect.get("forbidden_response_text"))]
    if forbidden_response_text and response is not None:
        found = [v for v in forbidden_response_text if v in response_text]
        add("forbidden_response_text", not found, 10, f"found {found}" if found else "no forbidden response text found")

    if response is not None:
        response_words = response_text.split()
        concise = bool(response_text.strip()) and len(response_words) <= int(expect.get("max_response_words") or 120)
        add("concise_answer", concise, 10, f"word_count={len(response_words)}")

        direct = bool(response_text.strip()) and not response_text.startswith(("as an ai", "i can help with that"))
        add("direct_answer", direct, 10, "direct response text observed" if direct else "response was empty or evasive")

        internal_hits = INTERNAL_MACHINERY_RE.findall((response or {}).get("content", ""))
        add("no_internal_machinery", not internal_hits, 15, f"found {internal_hits}" if internal_hits else "no internal terms")

        fake_tool_hits = FAKE_TOOL_TRANSCRIPT_RE.findall((response or {}).get("content", ""))
        add("no_fake_tool_transcript", not fake_tool_hits, 15, f"found {fake_tool_hits}" if fake_tool_hits else "no fake tool transcript")

        tool_claim = bool(TOOL_CLAIM_RE.search((response or {}).get("content", "")))
        unsupported_tool_claim = tool_claim and not TOOL_EVIDENCE_RE.search(corpus)
        detail = "no tool-use claim" if not tool_claim else "tool claim has trace evidence"
        add("no_unsupported_tool_claim", not unsupported_tool_claim, 15, detail if not unsupported_tool_claim else "tool claim without trace evidence")
    elif expect.get("answer_quality") is True:
        add("concise_answer", False, 10, "no response observed")
        add("direct_answer", False, 10, "no response observed")

    max_turns = expect.get("max_turns")
    if isinstance(max_turns, int):
        turns = trace_turns(trace)
        live = trace.get("live")
        inferred_turns = 1 if live_response_observed else None
        passed = (
            (turns is None and not isinstance(live, dict))
            or (turns is not None and turns <= max_turns)
            or (turns is None and inferred_turns is not None and inferred_turns <= max_turns)
        )
        if turns is None and inferred_turns is not None:
            detail = f"turn count unavailable; inferred {inferred_turns} from observed response, max {max_turns}"
        else:
            detail = "turn count unavailable" if turns is None else f"got {turns}, max {max_turns}"
        add("turn_bound", passed, 10, detail)

    max_messages = expect.get("max_agent_messages")
    if isinstance(max_messages, int):
        count = len(agent_messages(trace, agent_prefix))
        should_score_bound = count > 0 or has_terminal_outcome(trace) or not isinstance(trace.get("live"), dict)
        add("message_bound", should_score_bound and count <= max_messages, 10, f"got {count}, max {max_messages}")

    if isinstance(live, dict) and expect.get("verdict"):
        timed_out = live.get("timed_out") is True
        seen = int(live.get("agent_messages_seen") or 0)
        add("response_received", not timed_out and seen > 0, 10, f"agent messages seen: {seen}; timed_out={timed_out}")

    total_possible = sum(c.points if c.passed else _points_for_name(c.name, expect) for c in checks)
    if total_possible == 0:
        return 0, checks
    score = round(sum(c.points for c in checks) * 100 / total_possible)
    if isinstance(trace.get("live"), dict) and (
        response is None
        and diagnosis.phase in {
            "task_not_delivered",
            "task_skipped",
            "task_delivered_runtime_not_ready",
            "task_delivered_no_observable_loop",
            "loop_error",
            "loop_started_no_terminal",
        }
    ):
        score = 0
    return score, checks


def expected_failure_match(fixture: dict[str, Any], diagnosis: RunDiagnosis, checks: list[Check], *, mode: str) -> tuple[bool, str]:
    expect = fixture.get("expect", {})
    if expect.get("expected_failure") is not True or mode != "replay":
        return False, ""
    expected_phase = expect.get("diagnosis")
    if expected_phase and diagnosis.phase != expected_phase:
        return False, f"diagnosis {diagnosis.phase!r} did not match expected failure {expected_phase!r}"
    failed_checks = [check.name for check in checks if not check.passed]
    if not failed_checks:
        return False, "fixture was marked expected_failure but all checks passed"
    return True, f"expected failure observed via {diagnosis.phase}; failed checks: {failed_checks}"


def _points_for_name(name: str, expect: dict[str, Any]) -> int:
    weights = {
        "contract": 15,
        "progress_phase": 20,
        "route": 10,
        "verdict": 20,
        "audit_events": 10,
        "evidence": 15,
        "forbidden_reasons": 10,
        "forbidden_text": 10,
        "forbidden_response_text": 10,
        "response_text": 15,
        "current_date": 15,
        "concise_answer": 10,
        "direct_answer": 10,
        "no_internal_machinery": 15,
        "no_fake_tool_transcript": 15,
        "no_unsupported_tool_claim": 15,
        "turn_bound": 10,
        "message_bound": 10,
        "response_received": 10,
    }
    return weights.get(name, 0)


def run_cmd(args: list[str], *, env: dict[str, str], timeout: int = 60) -> subprocess.CompletedProcess[str]:
    return subprocess.run(args, cwd=ROOT, env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=timeout)


def sanitize_agent_name(value: str) -> str:
    value = value.lower()
    value = re.sub(r"[^a-z0-9-]+", "-", value)
    value = re.sub(r"^-+|-+$", "", value)
    return value or "loop-eval"


def read_audit(home: Path, agent: str) -> list[dict[str, Any]]:
    path = home / "audit" / agent / "gateway.jsonl"
    if not path.exists():
        return []
    events: list[dict[str, Any]] = []
    for line in path.read_text(encoding="utf-8", errors="replace").splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            events.append(json.loads(line))
        except json.JSONDecodeError:
            events.append({"event": "unparseable_audit_line", "raw": line})
    return events


def read_json_file(path: Path) -> Any:
    if not path.exists():
        return None
    try:
        return json.loads(path.read_text(encoding="utf-8", errors="replace"))
    except json.JSONDecodeError:
        return {"unparseable": path.read_text(encoding="utf-8", errors="replace")}


def read_jsonl_file(path: Path) -> list[dict[str, Any]]:
    if not path.exists():
        return []
    events: list[dict[str, Any]] = []
    for line in path.read_text(encoding="utf-8", errors="replace").splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            events.append(json.loads(line))
        except json.JSONDecodeError:
            events.append({"unparseable": line})
    return events


def loop_turn_in_flight(signals: list[dict[str, Any]]) -> bool:
    for signal in reversed(signals):
        if not isinstance(signal, dict):
            continue
        signal_type = signal.get("signal_type")
        if signal_type in {"agent_loop_llm_response_received", "agent_loop_terminal_outcome", "agent_loop_error"}:
            return False
        if signal_type == "agent_loop_llm_request_started":
            return True
    return False


def read_messages(agency_bin: str, env: dict[str, str], channel: str) -> list[dict[str, str]]:
    proc = run_cmd([agency_bin, "-q", "comms", "read", channel, "--limit", "100"], env=env, timeout=30)
    messages: list[dict[str, str]] = []
    for line in proc.stdout.splitlines():
        stripped = line.rstrip()
        if not stripped:
            continue
        match = re.match(r"^\s*\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\s+([^:]+):\s*(.*)$", stripped)
        if match:
            author, content = match.groups()
        else:
            if messages:
                messages[-1]["content"] = f"{messages[-1]['content']}\n{stripped.strip()}".strip()
            continue
        author = author.strip()
        messages.append({"author": author, "content": content.strip()})
    return messages


def wait_for_agent_running(agency_bin: str, env: dict[str, str], agent: str, timeout: int) -> bool:
    deadline = time.time() + timeout
    while time.time() < deadline:
        proc = run_cmd([agency_bin, "-q", "show", agent], env=env, timeout=30)
        if proc.returncode == 0 and '"status": "running"' in proc.stdout:
            return True
        time.sleep(2)
    return False


def live_trace(fixture: dict[str, Any], args: argparse.Namespace) -> tuple[str, dict[str, Any]]:
    agency_bin = str(Path(args.agency_bin).resolve()) if args.agency_bin else str(ROOT / "agency")
    if not Path(agency_bin).exists() and shutil.which("agency"):
        agency_bin = shutil.which("agency") or agency_bin
    home = Path(args.home or os.environ.get("AGENCY_HOME", Path.home() / ".agency")).expanduser()
    prefix = sanitize_agent_name(fixture.get("agent", {}).get("name_prefix", "loop-eval"))
    stamp = datetime.now(timezone.utc).strftime("%Y%m%d%H%M%S")
    agent = f"{prefix}-{stamp}"
    preset = fixture.get("agent", {}).get("preset", "researcher")
    channel = f"dm-{agent}"
    env = os.environ.copy()
    env["AGENCY_HOME"] = str(home)

    created = False
    try:
        proc = run_cmd([agency_bin, "-q", "create", agent, "--preset", preset], env=env, timeout=args.command_timeout)
        if proc.returncode != 0:
            raise RuntimeError(f"create failed\nSTDOUT:\n{proc.stdout}\nSTDERR:\n{proc.stderr}")
        created = True

        proc = run_cmd([agency_bin, "-q", "start", agent], env=env, timeout=args.command_timeout)
        if proc.returncode != 0:
            raise RuntimeError(f"start failed\nSTDOUT:\n{proc.stdout}\nSTDERR:\n{proc.stderr}")
        if not wait_for_agent_running(agency_bin, env, agent, args.start_timeout):
            raise RuntimeError(f"agent {agent} did not reach running state within {args.start_timeout}s")

        time.sleep(args.start_settle_seconds)
        before_agent_messages = len(agent_messages({"messages": read_messages(agency_bin, env, channel)}, prefix))
        proc = run_cmd([agency_bin, "-q", "send", agent, fixture["task"]], env=env, timeout=args.command_timeout)
        if proc.returncode != 0:
            raise RuntimeError(f"send failed\nSTDOUT:\n{proc.stdout}\nSTDERR:\n{proc.stderr}")

        deadline = time.time() + args.response_timeout
        messages: list[dict[str, str]] = []
        first_response_at: float | None = None
        agent_dir = home / "agents" / agent
        while time.time() < deadline:
            messages = read_messages(agency_bin, env, channel)
            current_agent_messages = len(agent_messages({"messages": messages}, prefix))
            if current_agent_messages > before_agent_messages and first_response_at is None:
                first_response_at = time.time()
            signals = read_jsonl_file(agent_dir / "state" / "agent-signals.jsonl")
            signal_types = {str(s.get("signal_type", "")) for s in signals if isinstance(s, dict)}
            if "agent_loop_terminal_outcome" in signal_types or "agent_loop_error" in signal_types:
                break
            if first_response_at is not None and time.time() - first_response_at >= args.observe_seconds and not loop_turn_in_flight(signals):
                break
            time.sleep(2)

        messages = read_messages(agency_bin, env, channel)
        seen_agent_messages = len(agent_messages({"messages": messages}, prefix))
        return agent, {
            "messages": messages,
            "audit": read_audit(home, agent),
            "live": {
                "agent": agent,
                "home": str(home),
                "channel": channel,
                "timed_out": first_response_at is None,
                "agent_messages_seen": seen_agent_messages,
            },
            "session_context": read_json_file(agent_dir / "state" / "session-context.json"),
            "signals": read_jsonl_file(agent_dir / "state" / "agent-signals.jsonl"),
        }
    finally:
        if created and not args.keep_agent:
            run_cmd([agency_bin, "-q", "delete", agent], env=env, timeout=args.command_timeout)
            run_cmd([agency_bin, "-q", "comms", "archive", channel], env=env, timeout=args.command_timeout)


def write_result(results_dir: Path, fixture_id: str, payload: dict[str, Any]) -> Path:
    results_dir.mkdir(parents=True, exist_ok=True)
    path = results_dir / f"{fixture_id}.json"
    path.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    return path


def truncate_one_line(value: str, limit: int = 500) -> str:
    value = " ".join(value.split())
    if len(value) <= limit:
        return value
    return value[: limit - 3].rstrip() + "..."


def print_human_report(
    *,
    fixture_id: str,
    fixture: dict[str, Any],
    score: int,
    passed: bool,
    diagnosis: RunDiagnosis,
    checks: list[Check],
    response: dict[str, Any],
    out: Path,
    expected_failure: dict[str, Any] | None = None,
) -> None:
    if expected_failure and expected_failure.get("matched"):
        status = "XFAIL"
    else:
        status = "PASS" if passed else "FAIL"
    print(f"{status} {fixture_id}")
    print(f"  Task: {truncate_one_line(str(fixture.get('task', '')))}")
    print(f"  Diagnosis: {diagnosis.phase} - {diagnosis.detail}")
    if response.get("status") == "observed":
        print(f"  Response ({response.get('author', 'agent')}): {truncate_one_line(str(response.get('content', '')))}")
    else:
        timed_out = response.get("timed_out")
        seen = response.get("agent_messages_seen", 0)
        print(f"  Response: none observed (timed_out={timed_out}, agent_messages_seen={seen})")
    print("  Checks:")
    for check in checks:
        check_status = "PASS" if check.passed else "FAIL"
        print(f"    {check_status} {check.name}: +{check.points} - {check.detail}")
    if expected_failure and expected_failure.get("expected"):
        print(f"  Expected failure: {expected_failure.get('detail', '')}")
    print(f"  Score: {score}")
    print(f"  Result: {out}")


def main() -> int:
    parser = argparse.ArgumentParser(description="Developer-only Agency agent loop evaluator")
    parser.add_argument("--mode", choices=["replay", "live"], default="replay")
    parser.add_argument("--fixture", action="append", default=[], help="fixture id to run; may be repeated")
    parser.add_argument("--results-dir", default=str(DEFAULT_RESULTS_DIR))
    parser.add_argument("--agency-bin", default="")
    parser.add_argument("--home", default="")
    parser.add_argument("--keep-agent", action="store_true")
    parser.add_argument("--response-timeout", type=int, default=180)
    parser.add_argument("--command-timeout", type=int, default=120)
    parser.add_argument("--start-timeout", type=int, default=420)
    parser.add_argument("--start-settle-seconds", type=int, default=2)
    parser.add_argument("--observe-seconds", type=int, default=20, help="live-mode observation window after first response")
    args = parser.parse_args()

    results_dir = Path(args.results_dir)
    summaries = []
    failures = 0
    for path in fixture_paths(args.fixture):
        fixture = load_json(path)
        fixture_id = fixture["id"]
        if args.mode == "replay":
            trace = fixture.get("replay_trace")
            if not isinstance(trace, dict):
                raise SystemExit(f"fixture {fixture_id} has no replay_trace")
            agent_name = fixture.get("agent", {}).get("name_prefix", "")
        else:
            agent_name, trace = live_trace(fixture, args)

        score, checks = score_trace(fixture, trace)
        passed = all(c.passed for c in checks)
        agent_prefix = fixture.get("agent", {}).get("name_prefix", "")
        diagnosis = diagnose_trace(fixture, trace)
        response = response_summary(trace, agent_prefix)
        xfail_matched, xfail_detail = expected_failure_match(fixture, diagnosis, checks, mode=args.mode)
        expected_failure = {
            "expected": fixture.get("expect", {}).get("expected_failure") is True and args.mode == "replay",
            "matched": xfail_matched,
            "detail": xfail_detail,
        }
        if not passed and not xfail_matched:
            failures += 1
        result = {
            "fixture": fixture_id,
            "description": fixture.get("description", ""),
            "task": fixture.get("task", ""),
            "mode": args.mode,
            "agent": agent_name,
            "score": score,
            "passed": passed,
            "diagnosis": diagnosis.__dict__,
            "response": response,
            "expected_failure": expected_failure,
            "checks": [c.__dict__ for c in checks],
            "trace": trace,
            "evaluated_at": datetime.now(timezone.utc).isoformat(),
        }
        out = write_result(results_dir, fixture_id, result)
        summaries.append((fixture_id, fixture, score, passed, diagnosis, checks, response, expected_failure, out))

    for idx, (fixture_id, fixture, score, passed, diagnosis, checks, response, expected_failure, out) in enumerate(summaries):
        if idx:
            print()
        print_human_report(
            fixture_id=fixture_id,
            fixture=fixture,
            score=score,
            passed=passed,
            diagnosis=diagnosis,
            checks=checks,
            response=response,
            out=out,
            expected_failure=expected_failure,
        )

    return 1 if failures else 0


if __name__ == "__main__":
    raise SystemExit(main())
