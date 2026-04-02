import json


class FallbackTracker:
    """Tracks tool call outcomes and matches failures against fallback policies."""

    def __init__(self, policies=None, default_policy=None):
        self.policies = policies or []
        self.default_policy = default_policy
        self._consecutive_errors = 0
        self._active_chains = {}  # trigger_key -> chain state

    def record_outcome(self, tool_name, success):
        """Record a tool call outcome. Returns a matched policy if a trigger fires, else None."""
        if success:
            self._consecutive_errors = 0
            return None
        self._consecutive_errors += 1

        # Check consecutive_errors trigger first
        consec_policy = self._check_consecutive_errors()
        if consec_policy:
            return consec_policy

        # Check tool_error trigger
        return self.match_trigger("tool_error", tool=tool_name)

    def match_trigger(self, trigger_type, tool=None, capability=None):
        """Find the highest-priority matching policy for a trigger."""
        # Priority: exact tool > wildcard > category > default
        exact_match = None
        wildcard_match = None
        category_match = None

        for p in self.policies:
            if p.get("trigger") != trigger_type:
                continue

            if trigger_type == "tool_error":
                if p.get("tool") == tool:
                    exact_match = p
                elif p.get("tool") == "*":
                    wildcard_match = p
            elif trigger_type == "capability_unavailable":
                if p.get("capability") == capability:
                    category_match = p
            else:
                # budget_warning, consecutive_errors, no_progress, timeout, etc.
                category_match = p

        match = exact_match or wildcard_match or category_match
        if match:
            return match
        if self.default_policy:
            return self.default_policy
        return None

    def _check_consecutive_errors(self):
        """Check if consecutive_errors trigger should fire."""
        for p in self.policies:
            if p.get("trigger") == "consecutive_errors":
                if self._consecutive_errors >= p.get("count", 3):
                    return p
        return None

    def check_budget_warning(self, budget_pct):
        """Check if budget_warning trigger should fire."""
        for p in self.policies:
            if p.get("trigger") == "budget_warning":
                if budget_pct >= p.get("threshold", 80):
                    return p
        return None

    def build_fallback_message(self, policy, context=None):
        """Build a user-role message presenting the fallback chain."""
        context = context or {}
        trigger = policy.get("trigger", "unknown")
        tool = context.get("tool", "")
        strategy = policy.get("strategy", [])

        header = f"## Fallback Policy Activated: {trigger}"
        if tool:
            header += f" ({tool})"

        lines = [header, ""]
        lines.append("Your mission's fallback policy defines this recovery chain:")
        lines.append("")

        for i, step in enumerate(strategy, 1):
            action = step.get("action", "").upper()
            parts = [f"{i}. {action}"]

            if step.get("action") == "retry":
                max_att = step.get("max_attempts", 1)
                parts.append(f" (up to {max_att} more attempts)")
                backoff = step.get("backoff", "none")
                if backoff != "none":
                    delay = step.get("delay_seconds", 5)
                    parts.append(f"\n   - Backoff: {backoff}, suggested wait ~{delay}s between attempts")

            elif step.get("action") == "alternative_tool":
                alt_tool = step.get("tool", "?")
                parts.append(f": {alt_tool}")
                if step.get("hint"):
                    parts.append(f'\n   - Hint: "{step["hint"]}"')

            elif step.get("action") == "escalate":
                sev = step.get("severity", "warning")
                parts.append(f" ({sev.upper()})")
                if step.get("message"):
                    msg = step["message"]
                    # Substitute template variables
                    for key, val in context.items():
                        msg = msg.replace(f"{{{key}}}", str(val))
                    parts.append(f'\n   - Message: "{msg}"')

            elif step.get("hint"):
                parts.append(f'\n   - Hint: "{step["hint"]}"')

            lines.append("".join(parts))

        lines.append("")
        lines.append("Follow this chain in order. Do not skip steps.")
        return "\n".join(lines)

    def reset_consecutive_errors(self):
        """Explicitly reset the consecutive error counter."""
        self._consecutive_errors = 0
