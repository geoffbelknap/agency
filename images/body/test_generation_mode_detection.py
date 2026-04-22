from objective_builder import build_objective, detect_generation_mode
from pact_engine import ActivationContext, ExecutionState, Objective, WorkContract
from work_contract import detect_generation_mode as exported_detect_generation_mode


def _activation(content: str) -> ActivationContext:
    return ActivationContext.from_message(content, source="idle_direct:dm-scout:operator")


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
        "task_id": "task-123",
        "started_at": "2026-04-22T12:00:00Z",
        "metadata": {
            "pact_activation": {
                "content": content,
                "match_type": "direct",
                "source": "idle_direct:dm-scout:operator",
                "channel": "dm-scout",
                "author": "operator",
                "mission_active": False,
            },
        },
    }


def _task_with_contract(content: str, contract: WorkContract) -> dict:
    task = _task(content)
    task["metadata"]["work_contract"] = contract.to_dict()
    return task


def test_01_empty_content_defaults_to_grounded():
    assert detect_generation_mode("") == "grounded"
    assert detect_generation_mode("   \n\t  ") == "grounded"


def test_02_bare_greeting_is_social():
    assert detect_generation_mode("hi") == "social"


def test_03_bare_greeting_is_case_insensitive_and_punctuation_tolerant():
    assert detect_generation_mode("Hello!") == "social"


def test_04_how_are_you_is_social():
    assert detect_generation_mode("how are you") == "social"


def test_05_mixed_greeting_and_work_stays_grounded():
    assert detect_generation_mode("hi, investigate this repo") == "grounded"


def test_06_persona_pattern_can_match_after_greeting_prefix():
    assert detect_generation_mode("hello, what's your favorite color?") == "persona"


def test_07_joke_request_is_creative():
    assert detect_generation_mode("tell me a joke") == "creative"


def test_08_haiku_request_is_creative():
    assert detect_generation_mode("write me a haiku about PACT") == "creative"


def test_09_brainstorm_request_is_creative():
    assert detect_generation_mode("brainstorm ideas for the roadmap") == "creative"


def test_10_name_query_is_persona():
    assert detect_generation_mode("what's your name") == "persona"


def test_11_who_are_you_query_is_persona():
    assert detect_generation_mode("who are you") == "persona"


def test_12_pretend_request_is_creative():
    assert detect_generation_mode("pretend to be a pirate") == "creative"


def test_13_plain_investigation_defaults_to_grounded():
    assert detect_generation_mode("investigate the graphify repo") == "grounded"


def test_14_analytical_ask_defaults_to_grounded():
    assert detect_generation_mode("can you analyze the recent release patterns of bun.js") == "grounded"


def test_15_thanks_is_social():
    assert detect_generation_mode("thanks!") == "social"


def test_16_detection_is_case_insensitive():
    assert detect_generation_mode("TELL ME A JOKE") == "creative"


def test_17_creative_priority_wins_over_persona():
    assert detect_generation_mode("tell me a joke about what's your name") == "creative"


def test_18_hank_replay_activation_defaults_to_grounded():
    content = "I want to see if you can help me out by investigating this github repository..."

    assert detect_generation_mode(content) == "grounded"


def test_19_build_objective_populates_generation_mode_from_activation_content():
    objective = build_objective(
        _activation("write me a haiku about PACT"),
        _contract("chat"),
        _task("write me a haiku about PACT"),
    )

    assert objective.generation_mode == "creative"


def test_20_build_objective_leaves_generation_mode_grounded_without_pattern():
    objective = build_objective(
        _activation("investigate the graphify repo"),
        _contract("task"),
        _task("investigate the graphify repo"),
    )

    assert objective.generation_mode == "grounded"


def test_21_objective_to_dict_round_trips_generation_mode():
    objective = Objective(statement="tell me a joke", kind="chat", generation_mode="creative")

    assert objective.to_dict()["generation_mode"] == "creative"


def test_22_execution_state_from_task_populates_objective_generation_mode():
    contract = _contract("chat")
    state = ExecutionState.from_task(
        _task_with_contract("tell me a joke", contract),
        agent="scout",
    )

    assert state.objective is not None
    assert state.objective.generation_mode == "creative"
    assert state.to_dict()["objective"]["generation_mode"] == "creative"
    assert exported_detect_generation_mode("tell me a joke") == "creative"
