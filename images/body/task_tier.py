"""Task tier classification and cost_mode expansion for the body runtime.

Classifies incoming tasks into tiers (minimal/standard/full) based on observable
signals, expands cost_mode shortcuts into feature defaults, and defines the
feature activation matrix per tier.
"""

from typing import Optional

TIER_ORDER = {"minimal": 0, "standard": 1, "full": 2}

COST_MODE_DEFAULTS = {
    "frugal": {
        "reflection": {"enabled": False},
        "success_criteria_eval_mode": "checklist_only",
        "procedural_memory": {"capture": False, "retrieve": False},
        "episodic_memory": {"capture": False, "retrieve": False, "tool_enabled": False},
    },
    "balanced": {
        "reflection": {"enabled": False},
        "success_criteria_eval_mode": "checklist_only",
        "procedural_memory": {"capture": True, "retrieve": True, "max_retrieved": 3, "include_failures": False},
        "episodic_memory": {"capture": True, "retrieve": True, "max_retrieved": 3, "tool_enabled": True},
    },
    "thorough": {
        "reflection": {"enabled": True, "max_rounds": 2},
        "success_criteria_eval_mode": "llm",
        "procedural_memory": {"capture": True, "retrieve": True, "max_retrieved": 5, "include_failures": True},
        "episodic_memory": {"capture": True, "retrieve": True, "max_retrieved": 5, "tool_enabled": True},
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

_DIRECT_SOURCES = {"dm", "mention", "idle_direct", "context_fallback"}
_ASYNC_SOURCES = {"connector", "schedule", "webhook", "channel_trigger"}


def classify_task_tier(task: dict, mission: Optional[dict]) -> str:
    """Classify a task into a tier: minimal, standard, or full."""
    if mission is None:
        return "minimal"

    cost_mode = mission.get("cost_mode", "balanced")

    if cost_mode == "frugal":
        tier = "minimal"
    elif cost_mode == "thorough":
        tier = "full"
    else:
        source = task.get("source", "")
        content = task.get("content", "")
        content_len = len(content)

        if source in _DIRECT_SOURCES:
            tier = "minimal" if content_len < 100 else "standard"
        elif source in _ASYNC_SOURCES:
            tier = "standard"
        else:
            tier = "standard"

    min_tier = mission.get("min_task_tier")
    if min_tier and min_tier in TIER_ORDER:
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
