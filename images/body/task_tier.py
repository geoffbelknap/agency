"""Task routing axes and cost_mode expansion for the body runtime.

Classifies incoming tasks across reasoning depth, context depth, and model
capability based on typed runtime signals. The legacy task tier compatibility
shim remains for callers that still expect minimal/standard/full.
"""

from typing import Optional

TIER_ORDER = {"minimal": 0, "standard": 1, "full": 2}

COST_MODE_DEFAULTS = {
    "frugal": {
        "reflection": {"enabled": False},
        "success_criteria_eval_mode": "checklist_only",
        "procedural_memory": {"capture": False, "retrieve": False},
        "episodic_memory": {"capture": False, "retrieve": False, "tool_enabled": False},
        "cache": {"enabled": True, "ttl_hours": 24, "confidence_threshold": 0.92, "assist_threshold": 0.80},
    },
    "balanced": {
        "reflection": {"enabled": False},
        "success_criteria_eval_mode": "checklist_only",
        "procedural_memory": {"capture": True, "retrieve": True, "max_retrieved": 3, "include_failures": False},
        "episodic_memory": {"capture": True, "retrieve": True, "max_retrieved": 3, "tool_enabled": True},
        "cache": {"enabled": True, "ttl_hours": 24, "confidence_threshold": 0.92, "assist_threshold": 0.80},
    },
    "thorough": {
        "reflection": {"enabled": True, "max_rounds": 2},
        "success_criteria_eval_mode": "llm",
        "procedural_memory": {"capture": True, "retrieve": True, "max_retrieved": 5, "include_failures": True},
        "episodic_memory": {"capture": True, "retrieve": True, "max_retrieved": 5, "tool_enabled": True},
        "cache": {"enabled": True, "ttl_hours": 48, "confidence_threshold": 0.95, "assist_threshold": 0.85},
    },
}

TIER_FEATURES = {
    "minimal": {
        "trajectory": True,
        "fallback": False,
        "reflection": False,
        "evaluation": False,
        "procedural_inject": False,
        "procedural_capture": False,
        "episodic_inject": False,
        "episodic_capture": False,
        "recall_tool": False,
        "prompt_tier": "minimal",
    },
    "standard": {
        "trajectory": True,
        "fallback": True,
        "reflection": False,
        "evaluation": True,
        "procedural_inject": False,
        "procedural_capture": True,
        "episodic_inject": False,
        "episodic_capture": True,
        "recall_tool": True,
        "prompt_tier": "standard",
    },
    "full": {
        "trajectory": True,
        "fallback": True,
        "reflection": True,
        "evaluation": True,
        "procedural_inject": True,
        "procedural_capture": True,
        "episodic_inject": True,
        "episodic_capture": True,
        "recall_tool": True,
        "prompt_tier": "full",
    },
}

def _value(value) -> str:
    raw = getattr(value, "value", value)
    return str(raw or "").strip()


def _objective_attr(objective, attr: str) -> str:
    return _value(getattr(objective, attr, ""))


def _strategy_mode(strategy) -> str:
    return _value(getattr(strategy, "execution_mode", ""))


def _strategy_bool(strategy, attr: str) -> bool:
    return bool(getattr(strategy, attr, False))


def _contract_kind(task: dict, objective=None) -> str:
    kind = _objective_attr(objective, "kind")
    if kind:
        return kind
    if not isinstance(task, dict):
        return ""
    metadata = task.get("metadata") if isinstance(task.get("metadata"), dict) else {}
    contract = metadata.get("work_contract") if isinstance(metadata, dict) else None
    if isinstance(contract, dict):
        return str(contract.get("kind") or "").strip()
    return str(task.get("contract_kind") or task.get("kind") or "").strip()


def _cost_mode(mission: Optional[dict]) -> str:
    if not isinstance(mission, dict):
        return "balanced"
    return str(mission.get("cost_mode") or "balanced").strip().lower()


def _apply_reasoning_cost_mode(
    depth: str,
    *,
    mission: Optional[dict],
    generation_mode: str,
    risk_level: str,
    contract_kind: str,
) -> str:
    cost_mode = _cost_mode(mission)
    if cost_mode == "frugal":
        if risk_level == "escalated" or contract_kind == "external_side_effect":
            return "reflective"
        if depth == "deliberative":
            return "reflective"
        return depth
    if cost_mode == "thorough" and depth == "direct" and generation_mode == "grounded":
        return "reflective"
    return depth


def _apply_context_cost_mode(depth: str, *, mission: Optional[dict], generation_mode: str) -> str:
    cost_mode = _cost_mode(mission)
    if cost_mode == "frugal" and depth == "full":
        return "task-relevant"
    if cost_mode == "thorough" and depth == "minimal" and generation_mode == "grounded":
        return "task-relevant"
    return depth


def _apply_model_cost_mode(
    model: str,
    *,
    mission: Optional[dict],
    default_standard: str,
    default_large: str,
) -> str:
    if _cost_mode(mission) != "frugal":
        return model
    if model == default_large:
        return default_standard
    return model


def classify_reasoning_depth(
    task: dict,
    mission: Optional[dict],
    *,
    objective=None,
    strategy=None,
) -> str:
    """Return 'direct', 'reflective', or 'deliberative'."""
    task = task if isinstance(task, dict) else {}
    generation_mode = _objective_attr(objective, "generation_mode")
    risk_level = _objective_attr(objective, "risk_level")
    execution_mode = _strategy_mode(strategy)
    contract_kind = _contract_kind(task, objective)

    if execution_mode in {"clarify", "escalate"}:
        depth = "direct"
    elif risk_level == "escalated":
        depth = "deliberative"
    elif _strategy_bool(strategy, "needs_approval") or contract_kind == "external_side_effect":
        depth = "deliberative"
    elif risk_level == "high":
        depth = "reflective"
    elif _strategy_bool(strategy, "needs_planner") or contract_kind in {"code_change", "file_artifact"}:
        depth = "reflective"
    elif generation_mode in {"social", "creative", "persona"}:
        depth = "direct"
    else:
        depth = "reflective"

    return _apply_reasoning_cost_mode(
        depth,
        mission=mission,
        generation_mode=generation_mode,
        risk_level=risk_level,
        contract_kind=contract_kind,
    )


def classify_context_depth(
    task: dict,
    mission: Optional[dict],
    *,
    objective=None,
    strategy=None,
) -> str:
    """Return 'minimal', 'task-relevant', or 'full'."""
    task = task if isinstance(task, dict) else {}
    generation_mode = _objective_attr(objective, "generation_mode")
    execution_mode = _strategy_mode(strategy)
    contract_kind = _contract_kind(task, objective)

    if generation_mode in {"social", "creative", "persona"}:
        depth = "minimal"
    elif execution_mode in {"clarify", "escalate"}:
        depth = "minimal"
    elif isinstance(mission, dict) and mission.get("status") == "active":
        depth = "task-relevant"
    elif contract_kind in {"code_change", "file_artifact", "external_side_effect"}:
        depth = "task-relevant"
    elif generation_mode == "grounded":
        depth = "task-relevant"
    else:
        depth = "task-relevant"

    return _apply_context_cost_mode(depth, mission=mission, generation_mode=generation_mode)


def select_model(
    task: dict,
    mission: Optional[dict],
    *,
    objective=None,
    strategy=None,
    default_standard: str = "claude-sonnet",
    default_small: str = "claude-haiku",
    default_large: str = "claude-opus",
) -> str:
    """Return the model name to use for this turn."""
    del strategy
    task = task if isinstance(task, dict) else {}
    generation_mode = _objective_attr(objective, "generation_mode")
    risk_level = _objective_attr(objective, "risk_level")
    contract_kind = _contract_kind(task, objective)

    if risk_level == "escalated":
        model = default_large
    elif contract_kind == "external_side_effect":
        model = default_large
    elif risk_level == "high":
        model = default_standard
    elif generation_mode == "grounded":
        model = default_standard
    elif contract_kind in {"code_change", "file_artifact", "current_info", "operator_blocked"}:
        model = default_standard
    elif generation_mode in {"social", "creative", "persona"} and risk_level in {"low", "medium", ""}:
        model = default_small
    else:
        model = default_standard

    return _apply_model_cost_mode(
        model,
        mission=mission,
        default_standard=default_standard,
        default_large=default_large,
    )


def classify_task_tier(task: dict, mission: Optional[dict]) -> str:
    """Deprecated. Use classify_reasoning_depth / classify_context_depth /
    select_model directly. Retained for callers that still expect a single
    tier string; composed from the three axes for approximate compatibility.
    """
    reasoning_depth = classify_reasoning_depth(task, mission)
    context_depth = classify_context_depth(task, mission)
    model_capability = select_model(
        task,
        mission,
        default_small="small",
        default_standard="standard",
        default_large="large",
    )

    if (
        reasoning_depth in {"deliberative"}
        or context_depth in {"full"}
        or model_capability in {"large"}
    ):
        tier = "full"
    elif (
        reasoning_depth in {"reflective"}
        or context_depth in {"task-relevant"}
        or model_capability in {"standard"}
    ):
        tier = "standard"
    else:
        tier = "minimal"

    min_tier = mission.get("min_task_tier") if isinstance(mission, dict) else None
    if min_tier in TIER_ORDER:
        if TIER_ORDER[min_tier] > TIER_ORDER.get(tier, 0):
            tier = min_tier

    return tier


def expand_cost_mode(cost_mode: str) -> dict:
    """Return feature configuration defaults for a given cost_mode."""
    return COST_MODE_DEFAULTS.get(cost_mode, COST_MODE_DEFAULTS["balanced"])


def get_active_features(tier: str) -> dict:
    """Return the feature activation flags for a tier."""
    return TIER_FEATURES.get(tier, TIER_FEATURES["standard"])


def resolve_feature_config(mission: dict, feature_name: str, cost_mode_defaults: dict) -> dict:
    """Merge explicit mission config over cost_mode defaults."""
    defaults = cost_mode_defaults.get(feature_name, {})
    explicit = mission.get(feature_name, {})
    merged = dict(defaults)
    for k, v in explicit.items():
        if v is not None:
            merged[k] = v
    return merged
