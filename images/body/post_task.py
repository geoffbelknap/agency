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

CONVERSATION_MEMORY_PROMPT_TEMPLATE = """You are proposing durable memory records from the conversation above.

The session scratchpad is temporary working state. Only propose memory when the conversation contains information that should outlive this session.

Return valid JSON only:
{{
  "memories": [
    {{
      "memory_type": "semantic|episodic|procedural",
      "summary": "specific durable memory",
      "reason": "why this should be remembered",
      "confidence": "low|medium|high",
      "entities": ["specific entities"],
      "evidence_message_ids": ["message ids from the transcript when available"]
    }}
  ]
}}

Classification:
- semantic: stable facts, user preferences, durable entity/project facts
- episodic: notable events that should be remembered as something that happened
- procedural: reusable workflows, source preferences, or steps for future similar work

Rules:
- Return {{"memories": []}} for routine chatter, acknowledgments, or facts with no future value.
- Prefer low confidence when the memory is inferred rather than explicitly stated.
- Do not turn the scratchpad itself into memory.
- Do not propose secrets, credentials, or transient implementation details.
- Every proposed memory must be grounded in the transcript or task metadata."""


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


def build_conversation_memory_prompt(task_metadata):
    """Build a prompt for proposing durable conversation memories."""
    return CONVERSATION_MEMORY_PROMPT_TEMPLATE + (
        "\n\n## Task Metadata\n"
        f"- Agent: {task_metadata.get('agent', '')}\n"
        f"- Task ID: {task_metadata.get('task_id', '')}\n"
        f"- Channel: {task_metadata.get('channel', '')}\n"
        f"- Participant: {task_metadata.get('participant', '')}\n"
        f"- Source message ID: {task_metadata.get('message_id', '')}\n"
        f"- Timestamp: {task_metadata.get('timestamp', '')}\n"
    )


def parse_conversation_memory_response(response_text):
    """Parse proposed conversation memories from an LLM response."""
    parsed = _parse_json_object(response_text)
    if not isinstance(parsed, dict):
        return []
    memories = parsed.get("memories", [])
    if not isinstance(memories, list):
        return []
    result = []
    for item in memories:
        if not isinstance(item, dict):
            continue
        memory_type = str(item.get("memory_type", "")).lower()
        summary = str(item.get("summary", "")).strip()
        if memory_type not in {"semantic", "episodic", "procedural"} or not summary:
            continue
        confidence = str(item.get("confidence", "low")).lower()
        if confidence not in {"low", "medium", "high"}:
            confidence = "low"
        result.append({
            "memory_type": memory_type,
            "summary": summary,
            "reason": str(item.get("reason", "")).strip(),
            "confidence": confidence,
            "entities": _string_list(item.get("entities", [])),
            "evidence_message_ids": _string_list(item.get("evidence_message_ids", [])),
        })
    return result


def _parse_json_object(response_text):
    if not response_text:
        return None
    try:
        text = response_text.strip()
        start = text.find("{")
        if start < 0:
            return None
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
        return parsed if isinstance(parsed, dict) else None
    except (json.JSONDecodeError, ValueError):
        return None


def _string_list(value):
    if not isinstance(value, list):
        return []
    return [str(item).strip() for item in value if str(item).strip()]
