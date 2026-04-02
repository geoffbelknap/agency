import json

DEFAULT_REFLECTION_CRITERIA = [
    "The output is complete — it addresses the full scope of the task, not just part of it.",
    "The output is accurate — claims are supported, no hallucinated details.",
    "The output is actionable — the recipient can act on it without needing to ask follow-up questions.",
]

REFLECTION_PROMPT_TEMPLATE = """You just called complete_task with the following summary:

---
{summary}
---

Before this task is marked complete, evaluate your output honestly against these criteria:

{criteria_block}

Respond with JSON only. No text outside the JSON.

{{
  "verdict": "APPROVED or REVISION_NEEDED",
  "criteria_results": [
    {{
      "criterion": "<the criterion text>",
      "met": true or false,
      "justification": "<one sentence>"
    }}
  ],
  "issues": ["<specific issue to address>"]
}}

Set verdict to APPROVED only if all criteria are met. Set issues to an empty list if APPROVED.
Do not hedge. Do not add caveats about limitations. Evaluate the actual output you produced."""


def build_reflection_criteria(mission):
    reflection = mission.get("reflection", {}) if mission else {}
    if reflection.get("criteria"):
        return reflection["criteria"]
    success = mission.get("success_criteria", {}) if mission else {}
    checklist = success.get("checklist", [])
    if checklist:
        return [item["description"] for item in checklist if item.get("description")]
    return list(DEFAULT_REFLECTION_CRITERIA)


def build_reflection_prompt(summary, mission):
    criteria = build_reflection_criteria(mission)
    criteria_block = "\n".join(f"{i+1}. {c}" for i, c in enumerate(criteria))
    return REFLECTION_PROMPT_TEMPLATE.format(summary=summary, criteria_block=criteria_block)


def parse_reflection_verdict(response_text):
    try:
        text = response_text.strip()
        start = text.find("{")
        if start < 0:
            raise ValueError("No JSON found")
        end = text.rfind("}") + 1
        if end <= start:
            raise ValueError("No closing brace")
        parsed = json.loads(text[start:end])
        if parsed.get("verdict") in ("APPROVED", "REVISION_NEEDED"):
            return {
                "verdict": parsed["verdict"],
                "criteria_results": parsed.get("criteria_results", []),
                "issues": parsed.get("issues", []),
            }
    except (json.JSONDecodeError, ValueError, KeyError):
        pass
    return {
        "verdict": "REVISION_NEEDED",
        "criteria_results": [],
        "issues": ["Reflection verdict was unparseable — treating as revision needed."],
    }


class ReflectionState:
    def __init__(self, max_rounds=3):
        self.max_rounds = max_rounds
        self.round = 0
        self.pending = False
        self.summary = None
        self.forced = False
        self.budget_exhausted = False

    def intercept_completion(self, summary):
        """Called when agent calls complete_task. Returns True if intercepted."""
        self.pending = True
        self.summary = summary
        return True

    def record_round(self):
        """Record a reflection round. Returns True if max rounds reached."""
        self.round += 1
        self.pending = False
        return self.round >= self.max_rounds

    def force_completion(self, budget_exhausted=False):
        """Force completion (max rounds or budget exhaustion)."""
        self.forced = True
        self.budget_exhausted = budget_exhausted
        self.pending = False

    def get_signal_data(self, task_id):
        """Return data dict for task_complete signal."""
        data = {"task_id": task_id, "result": self.summary or ""}
        if self.round > 0:
            data["reflection_rounds"] = self.round
            data["reflection_forced"] = self.forced
            if self.budget_exhausted:
                data["reflection_budget_exhausted"] = True
        return data

    def get_cycle_signal_data(self, task_id, verdict):
        """Return data dict for reflection_cycle signal."""
        return {
            "task_id": task_id,
            "round": self.round,
            "verdict": "REVISION_NEEDED",
            "issues": verdict.get("issues", []),
        }
