"""Default policy bundle — ships with Agency.

Applied automatically on `agency init`. Represents a safe, reasonable
starting point for a standalone operator. The operator can add restrictions
on top of this. They cannot remove the hard floors.
"""

# Hard floors — absolute minimums, cannot be modified at any level
HARD_FLOORS = {
    "logging": "required",
    "constraints_readonly": True,
    "llm_credentials_isolated": True,
    "network_mediation": "required",
}

# Bounded parameters — tunable within range, lower levels can only restrict
PARAMETER_DEFAULTS = {
    "risk_tolerance": {
        "value": "medium",
        "allowed": ["low", "medium"],  # "high" is not allowed
        "order": ["low", "medium", "high"],  # low is most restrictive
    },
    "max_concurrent_tasks": {
        "value": 5,
        "min": 1,
        "max": 20,
    },
    "max_task_duration": {
        "value": "4 hours",
        "order": ["1 hour", "2 hours", "4 hours", "8 hours"],
    },
    "autonomous_interrupt_threshold": {
        "value": "HIGH",
        "order": ["LOW", "MEDIUM", "HIGH", "CRITICAL"],
    },
}

# Default rules — lower levels can add but never remove
DEFAULT_RULES = [
    {
        "rule": "irreversible actions require confirmation",
        "applies_to": ["file_delete", "db_drop", "git_push_force"],
    },
    {
        "rule": "sensitive domains require escalation",
        "applies_to": ["billing", "authentication", "security_config"],
    },
    {
        "rule": "production data requires confirmation",
        "applies_to": ["prod_db_access", "prod_api_calls"],
    },
]


def get_platform_defaults() -> dict:
    """Return the complete platform default policy."""
    return {
        "agency_default_policy_v1": {
            "description": "Agency default policy - standalone operator MVP",
            "hard_floors": HARD_FLOORS,
            "parameters": {k: v["value"] for k, v in PARAMETER_DEFAULTS.items()},
            "rules": DEFAULT_RULES,
        }
    }


def is_hard_floor(key: str) -> bool:
    """Check if a parameter is a hard floor."""
    return key in HARD_FLOORS


def is_loosening(param_name: str, current_value, proposed_value) -> bool:
    """Check if a proposed value loosens a bounded parameter.

    Loosening means moving to a less restrictive value.
    Lower levels can only restrict (move toward more restrictive).
    """
    spec = PARAMETER_DEFAULTS.get(param_name)
    if spec is None:
        return True  # Unknown parameter — treat as loosening (reject)

    order = spec.get("order")
    if order:
        # Ordered parameter — higher index = less restrictive
        if current_value not in order or proposed_value not in order:
            return False
        return order.index(proposed_value) > order.index(current_value)

    # Numeric parameter — higher value = less restrictive
    if isinstance(spec.get("value"), (int, float)):
        try:
            return float(proposed_value) > float(current_value)
        except (ValueError, TypeError):
            return False

    return False
