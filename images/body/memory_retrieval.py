import json
import urllib.request
import urllib.parse
import logging

logger = logging.getLogger(__name__)


def fetch_procedural_memory(knowledge_url, mission_id, max_retrieved=5, include_failures=False):
    """Query knowledge graph for past procedures and format for prompt injection.

    Args:
        knowledge_url: Base URL for knowledge service (e.g. http://enforcer:8081/mediation/knowledge)
        mission_id: UUID of the current mission
        max_retrieved: Max procedures to return
        include_failures: Whether to include failed procedures

    Returns:
        str: Formatted prompt section, or empty string if no procedures found
    """
    if not mission_id:
        return ""

    try:
        query = f"entity_type:procedure mission_id:{mission_id} outcome:success"
        results = _query_knowledge(knowledge_url, query, limit=max_retrieved)

        failed_results = []
        if include_failures:
            fail_query = f"entity_type:procedure mission_id:{mission_id} outcome:failed"
            failed_results = _query_knowledge(knowledge_url, fail_query, limit=2)

        if not results and not failed_results:
            return ""

        lines = [
            "## Relevant Past Procedures",
            "",
            "Based on your previous experience with this mission, here are approaches that worked:",
            "",
        ]

        for p in results:
            attrs = p.get("attributes", p)  # handle both wrapped and flat formats
            ts = str(attrs.get("timestamp", "?"))[:10]
            dur = attrs.get("duration_minutes", "?")
            outcome = attrs.get("outcome", "?")
            approach = attrs.get("approach", "")
            lines.append(f"### Procedure from {ts} ({dur} min, {outcome})")
            lines.append(approach)
            lessons = attrs.get("lessons", [])
            if lessons:
                lines.append("Lessons: " + "; ".join(lessons))
            lines.append("")

        if failed_results:
            lines.append("### Approaches That Did NOT Work")
            lines.append("")
            for p in failed_results:
                attrs = p.get("attributes", p)
                ts = str(attrs.get("timestamp", "?"))[:10]
                approach = attrs.get("approach", "")
                lessons = attrs.get("lessons", [])
                lines.append(f"**Failed ({ts}):** {approach}")
                if lessons:
                    lines.append("Lesson: " + "; ".join(lessons))
                lines.append("")

        lines.append("Use these as reference — adapt to the current situation, don't follow blindly.")
        return "\n".join(lines)

    except Exception as e:
        logger.warning(f"Failed to fetch procedural memory: {e}")
        return ""


def fetch_episodic_memory(knowledge_url, agent_name, mission_id, max_retrieved=5):
    """Query knowledge graph for recent episodes and format for prompt injection.

    Args:
        knowledge_url: Base URL for knowledge service
        agent_name: Name of the current agent
        mission_id: UUID of the current mission
        max_retrieved: Max episodes to return

    Returns:
        str: Formatted prompt section, or empty string if no episodes found
    """
    if not mission_id:
        return ""

    try:
        query = f"entity_type:episode agent:{agent_name} mission_id:{mission_id}"
        results = _query_knowledge(knowledge_url, query, limit=max_retrieved)

        if not results:
            return ""

        lines = ["## Recent Episodes", ""]

        for ep in results:
            attrs = ep.get("attributes", ep)
            ts = str(attrs.get("timestamp", "?"))[:16].replace("T", " ")
            summary = attrs.get("summary", "")
            outcome = attrs.get("outcome", "?")
            dur = attrs.get("duration_minutes", "?")
            lines.append(f"### {ts} ({outcome}, {dur} min)")
            lines.append(summary)
            notable = attrs.get("notable_events", [])
            if notable:
                lines.append("Notable: " + "; ".join(str(n) for n in notable))
            lines.append("")

        return "\n".join(lines)

    except Exception as e:
        logger.warning(f"Failed to fetch episodic memory: {e}")
        return ""


def handle_recall_episodes(knowledge_url, agent_name, query, from_date=None, to_date=None,
                           entity=None, tag=None, outcome=None, mission=None, limit=10):
    """Handler for the recall_episodes tool.

    Args:
        knowledge_url: Base URL for knowledge service
        agent_name: Name of the requesting agent
        query: Semantic search query (required)
        from_date: ISO 8601 start date filter
        to_date: ISO 8601 end date filter
        entity: Entity name filter
        tag: Tag filter
        outcome: Outcome filter
        mission: Mission name filter
        limit: Max results

    Returns:
        str: JSON string with episodes list
    """
    try:
        parts = [f"entity_type:episode agent:{agent_name} {query}"]
        if from_date:
            parts.append(f"from:{from_date}")
        if to_date:
            parts.append(f"to:{to_date}")
        if entity:
            parts.append(f"entity:{entity}")
        if tag:
            parts.append(f"tag:{tag}")
        if outcome:
            parts.append(f"outcome:{outcome}")
        if mission:
            parts.append(f"mission:{mission}")

        full_query = " ".join(parts)
        results = _query_knowledge(knowledge_url, full_query, limit=limit)

        episodes = []
        for ep in results:
            attrs = ep.get("attributes", ep)
            episodes.append({
                "timestamp": attrs.get("timestamp", ""),
                "summary": attrs.get("summary", ""),
                "outcome": attrs.get("outcome", ""),
                "duration_minutes": attrs.get("duration_minutes", 0),
                "notable_events": attrs.get("notable_events", []),
                "operational_tone": attrs.get("operational_tone", ""),
                "tags": attrs.get("tags", []),
            })

        return json.dumps({"episodes": episodes, "count": len(episodes)})

    except Exception as e:
        return json.dumps({"error": str(e), "episodes": [], "count": 0})


def _query_knowledge(knowledge_url, query, limit=10):
    """Query the knowledge graph via the enforcer-mediated endpoint.

    Returns list of result dicts, or empty list on failure.
    """
    url = f"{knowledge_url}/query"
    payload = json.dumps({"query": query, "limit": limit}).encode()
    req = urllib.request.Request(url, data=payload, headers={"Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            data = json.loads(resp.read())
            return data.get("results", data.get("nodes", []))
    except Exception as e:
        logger.warning(f"Knowledge query failed: {e}")
        return []
