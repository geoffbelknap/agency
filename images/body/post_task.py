import json

CAPTURE_PROMPT_TEMPLATE = """You are extracting memory records from the conversation above.

## Task Metadata
- Mission: {mission_name}
- Task ID: {task_id}
- Tools used: {tools_used}
- Duration: {duration_minutes} minutes
- Outcome: {outcome}

{sections}

Return valid JSON only. No text outside the JSON.
"""

PROCEDURE_SECTION = """## Procedure Extract
Return a "procedure" key with:
{{
  "approach": "Numbered steps of what was DONE (not planned). Be specific.",
  "tools_used": ["actual_tool_names_used"],
  "outcome": "success|partial|failed",
  "lessons": ["specific takeaways worth remembering, if any — empty list if routine"]
}}

Rules for procedure:
- Describe what was done, not what was available
- List actual tools used, not all tools
- Lessons should be actionable for future similar tasks"""

EPISODE_SECTION = """## Episode Extract
Return an "episode" key with:
{{
  "summary": "2-5 sentence narrative of what happened. Be specific: names, numbers, timelines.",
  "notable_events": [
    "Things that were surprising, unusual, or worth remembering.",
    "Include connections to past events, unexpected outcomes.",
    "Empty list if entirely routine."
  ],
  "entities_mentioned": [
    {{"type": "person|system|incident|decision|process", "name": "..."}}
  ],
  "operational_tone": "routine|notable|problematic",
  "tags": ["specific", "searchable", "labels"]
}}

Rules for episode:
- Summary should stand alone — reader should understand without seeing the conversation
- For notable_events, focus on what would be useful to recall in a future similar situation
- For entities_mentioned, only include entities that played a meaningful role
- Tags should be specific: prefer "config-error" over "error", "deploy-related" over "deployment"
- operational_tone: routine = smooth/unremarkable, notable = interesting but not problematic, problematic = errors/stress/degraded"""


def build_capture_prompt(task_metadata, procedural_enabled=True, episodic_enabled=True):
    """Build the post-task capture prompt for procedure and/or episode extraction.

    Args:
        task_metadata: dict with keys: mission_name, task_id, tools_used (list),
                      duration_minutes (int), outcome (str)
        procedural_enabled: whether to extract a procedure record
        episodic_enabled: whether to extract an episode record

    Returns:
        str: the capture prompt to send to the LLM
    """
    sections = []
    if procedural_enabled:
        sections.append(PROCEDURE_SECTION)
    if episodic_enabled:
        sections.append(EPISODE_SECTION)

    if not sections:
        return ""

    return CAPTURE_PROMPT_TEMPLATE.format(
        mission_name=task_metadata.get("mission_name", ""),
        task_id=task_metadata.get("task_id", ""),
        tools_used=", ".join(task_metadata.get("tools_used", [])),
        duration_minutes=task_metadata.get("duration_minutes", 0),
        outcome=task_metadata.get("outcome", "unknown"),
        sections="\n\n".join(sections),
    )


def parse_capture_response(response_text):
    """Parse the LLM response into procedure and/or episode dicts.

    Returns:
        dict with optional "procedure" and "episode" keys, or None on parse failure.
    """
    if not response_text:
        return None
    try:
        text = response_text.strip()
        start = text.find("{")
        if start < 0:
            return None
        # Find the matching closing brace (handle nested braces)
        depth = 0
        end = -1
        for i in range(start, len(text)):
            if text[i] == "{":
                depth += 1
            elif text[i] == "}":
                depth -= 1
                if depth == 0:
                    end = i + 1
                    break
        if end <= start:
            return None
        parsed = json.loads(text[start:end])
        if not isinstance(parsed, dict):
            return None
        # Validate expected keys
        result = {}
        if "procedure" in parsed and isinstance(parsed["procedure"], dict):
            result["procedure"] = parsed["procedure"]
        if "episode" in parsed and isinstance(parsed["episode"], dict):
            result["episode"] = parsed["episode"]
        return result if result else None
    except (json.JSONDecodeError, ValueError):
        return None


def enrich_procedure(procedure, task_metadata):
    """Add task metadata fields to a procedure record before storage."""
    procedure["agent"] = task_metadata.get("agent", "")
    procedure["mission_id"] = task_metadata.get("mission_id", "")
    procedure["mission_name"] = task_metadata.get("mission_name", "")
    procedure["task_id"] = task_metadata.get("task_id", "")
    procedure["timestamp"] = task_metadata.get("timestamp", "")
    procedure["duration_minutes"] = task_metadata.get("duration_minutes", 0)
    if "reflection_notes" not in procedure:
        procedure["reflection_notes"] = ""
    if "lessons" not in procedure:
        procedure["lessons"] = []
    return procedure


def enrich_episode(episode, task_metadata):
    """Add task metadata fields to an episode record before storage."""
    episode["agent"] = task_metadata.get("agent", "")
    episode["mission_id"] = task_metadata.get("mission_id", "")
    episode["mission_name"] = task_metadata.get("mission_name", "")
    episode["task_id"] = task_metadata.get("task_id", "")
    episode["timestamp"] = task_metadata.get("timestamp", "")
    episode["duration_minutes"] = task_metadata.get("duration_minutes", 0)
    episode["outcome"] = task_metadata.get("outcome", "unknown")
    if "notable_events" not in episode:
        episode["notable_events"] = []
    if "entities_mentioned" not in episode:
        episode["entities_mentioned"] = []
    if "operational_tone" not in episode:
        episode["operational_tone"] = "routine"
    if "tags" not in episode:
        episode["tags"] = []
    return episode
