from pathlib import Path
from unittest.mock import MagicMock

import body as body_module
from body import Body
from pact_engine import ExecutionState, WorkContract


HANK_REPLAY = "I want to see if you can help me out by investigating this github repository..."


def _contract(kind: str = "chat") -> WorkContract:
    return WorkContract(
        kind=kind,
        requires_action=True,
        required_evidence=[],
        answer_requirements=[],
        allowed_terminal_states=["completed", "blocked"],
        reason="test",
        summary="Test contract.",
    )


def _task(content: str) -> dict:
    return {
        "task_id": "idle-reply-hank3-123",
        "metadata": {
            "pact_activation": {
                "content": content,
                "match_type": "direct",
                "source": "idle_direct:dm-hank3:operator",
                "channel": "dm-hank3",
                "author": "operator",
                "mission_active": False,
            },
            "work_contract": _contract("chat").to_dict(),
        },
    }


def _body(tmp_path: Path, *, context_depth: str, mission: dict | None = None) -> Body:
    (tmp_path / "PLATFORM.md").write_text("PLATFORM STATIC")
    memory_dir = tmp_path / ".memory"
    memory_dir.mkdir()

    body = Body.__new__(Body)
    body.config_dir = tmp_path
    body.memory_dir = memory_dir
    body.is_meeseeks = False
    body.agent_name = "hank3"
    body._active_mission = mission
    body._knowledge_url = "http://knowledge.test"
    body._cost_defaults = {"procedural_memory": {"max_retrieved": 5}, "episodic_memory": {"max_retrieved": 5}}
    body._config_overrides = {
        "identity.md": "IDENTITY STATIC",
        "FRAMEWORK.md": "FRAMEWORK STATIC",
        "AGENTS.md": "AGENTS STATIC",
    }
    body._fetch_config = lambda filename: None
    body._skills_manager = MagicMock()
    body._skills_manager.get_system_prompt_section.return_value = "SKILLS STATIC"
    body._execution_state = ExecutionState(task_id="task-123", agent="hank3", context_depth=context_depth)
    return body


def _patch_static_comms(monkeypatch) -> None:
    monkeypatch.setattr(
        "comms_tools.build_comms_context",
        lambda comms_url, agent_name: "COMMS STATIC",
    )


def test_minimal_context_includes_static_baseline_sections(tmp_path, monkeypatch):
    _patch_static_comms(monkeypatch)
    body = _body(tmp_path, context_depth="minimal")

    prompt = body.assemble_system_prompt()

    assert "FRAMEWORK STATIC" in prompt
    assert "AGENTS STATIC" in prompt
    assert "SKILLS STATIC" in prompt
    assert "PLATFORM STATIC" in prompt
    assert "COMMS STATIC" in prompt


def test_minimal_context_omits_dynamic_sections(tmp_path, monkeypatch):
    _patch_static_comms(monkeypatch)
    monkeypatch.setattr(body_module, "fetch_procedural_memory", lambda *args, **kwargs: "PROC MEMORY")
    monkeypatch.setattr(body_module, "fetch_episodic_memory", lambda *args, **kwargs: "EPISODIC MEMORY")
    monkeypatch.setattr(Body, "_fetch_org_context", lambda self: "ORG CONTEXT")
    body = _body(
        tmp_path,
        context_depth="minimal",
        mission={"status": "active", "id": "mission-123", "name": "Mission", "instructions": "Do work."},
    )
    (body.memory_dir / "repo.md").write_text("memory index entry")

    prompt = body.assemble_system_prompt()

    assert "PROC MEMORY" not in prompt
    assert "EPISODIC MEMORY" not in prompt
    assert "ORG CONTEXT" not in prompt
    assert "Memory Index" not in prompt


def test_full_context_includes_dynamic_sections(tmp_path, monkeypatch):
    _patch_static_comms(monkeypatch)
    monkeypatch.setattr(body_module, "fetch_procedural_memory", lambda *args, **kwargs: "PROC MEMORY")
    monkeypatch.setattr(body_module, "fetch_episodic_memory", lambda *args, **kwargs: "EPISODIC MEMORY")
    monkeypatch.setattr(Body, "_fetch_org_context", lambda self: "ORG CONTEXT")
    body = _body(
        tmp_path,
        context_depth="full",
        mission={"status": "active", "id": "mission-123", "name": "Mission", "instructions": "Do work."},
    )
    (body.memory_dir / "repo.md").write_text("memory index entry")

    prompt = body.assemble_system_prompt()

    assert "PROC MEMORY" in prompt
    assert "EPISODIC MEMORY" in prompt
    assert "ORG CONTEXT" in prompt
    assert "Memory Index" in prompt


def test_hank_style_prompt_includes_static_sections(tmp_path, monkeypatch):
    _patch_static_comms(monkeypatch)
    task = _task(HANK_REPLAY)
    body = _body(tmp_path, context_depth="task-relevant")
    body._execution_state = ExecutionState.from_task(task, agent="hank3")

    prompt = body.assemble_system_prompt()

    assert body._execution_state.context_depth == "task-relevant"
    assert "FRAMEWORK STATIC" in prompt
    assert "AGENTS STATIC" in prompt
    assert "SKILLS STATIC" in prompt
    assert "PLATFORM STATIC" in prompt
    assert "COMMS STATIC" in prompt
