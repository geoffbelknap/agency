"""Agency body runtime — autonomic nervous system for AI agents.

Handles autonomic functions only: LLM conversation loop, MCP tool
dispatch, context window management, signal emission, health heartbeat.
All mind functions (identity, constraints, personality) come from
read-only mounted files assembled into the system prompt.

Runs as PID 1 inside the workspace container. Communicates with the
LLM through the enforcer proxy (OpenAI-compatible endpoint).
"""

import json
import logging
import os
import queue as queue_module
import re
import signal
import subprocess
import sys
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from datetime import datetime, timezone
from pathlib import Path

import httpx
import yaml

from interruption import InterruptionController
from mcp_client import MCPClient
from tools import BuiltinToolRegistry, ServiceToolDispatcher, SkillsManager
from work_contract import (
    ActivationContext,
    EvidenceLedger,
    classify_activation,
    contract_prompt,
    extract_urls,
    format_blocked_completion,
    validate_completion,
)
from ws_listener import WSListener

logging.basicConfig(
    level=logging.INFO,
    format="[body] %(asctime)s | %(message)s",
    datefmt="%Y-%m-%dT%H:%M:%SZ",
    stream=sys.stdout,
)
log = logging.getLogger("body")


# ---------------------------------------------------------------------------
# Body Runtime
# ---------------------------------------------------------------------------

# Rough characters-per-token estimate for context window management
CHARS_PER_TOKEN = 4
# Default context window size (tokens) — conservative estimate
DEFAULT_CONTEXT_WINDOW = 200_000
# Trigger summarization at this fraction of context window
CONTEXT_THRESHOLD = float(os.environ.get("AGENCY_CONTEXT_THRESHOLD", "0.7"))
# Keep this many recent messages when summarizing
KEEP_RECENT_MESSAGES = 10
# Heartbeat interval in seconds
# Heartbeat removed — task lifecycle is tracked via explicit signals
# (task_accepted, task_complete, processing, error). No periodic polling.
# Task poll interval in seconds
TASK_POLL_INTERVAL = float(os.environ.get("AGENCY_TASK_POLL_INTERVAL", "0.25"))
# LLM request timeout in seconds
LLM_TIMEOUT = int(os.environ.get("AGENCY_LLM_TIMEOUT", "120"))
# Max retries for LLM calls
LLM_MAX_RETRIES = 6
# Budget-based limits replaced turn-based limits (MAX_TURNS, MAX_CONTINUATIONS).
# The conversation loop now runs until: complete_task called, budget exhausted
# (enforcer returns 429), or escalate called.
# Minimum seconds between notification-generated tasks (push-driven via comms events)
NOTIFICATION_COOLDOWN = int(os.environ.get("AGENCY_NOTIFICATION_COOLDOWN_SECS", "60"))

PROVIDER_TOOL_DEFINITIONS = {
    "provider-web-search": {"type": "web_search"},
}
SIMULATED_TOOL_TAG_RE = re.compile(
    r"(</?(search|web[_\.-]?search|browse|fetch|tool|tools?|read_file|write_file)\b|"
    r"^\s*(search|web[_\.-]?search|browse|fetch|read_file|write_file)\s*\()",
    re.IGNORECASE | re.MULTILINE,
)
CURRENT_INFO_PREAMBLE_RE = re.compile(
    r"^\s*(?:let me|i(?:'ll| will)|i need to|first,?\s*i(?:'ll| will)|now let me)\s+"
    r"(?:search|check|look up|find|verify|use|see)\b.*?(?:\.|:)?\s*$",
    re.IGNORECASE,
)

# Meeseeks system prompt template — minimal, task-focused
MEESEEKS_SYSTEM_PROMPT = """You are a Meeseeks — a single-purpose agent created to complete one task.
Your task: {task}

Rules:
- Complete your task as quickly and directly as possible
- Post your results to #{channel} using send_message
- Call complete_task when done — you will cease to exist
- If you cannot complete your task, call escalate(reason=...) immediately
- Do not take on additional work. You exist for this one task only."""


def classify_llm_error(
    error: Exception,
    model: str = "",
    correlation_id: str = "",
    retries: int = 0,
) -> dict:
    """Classify an LLM error into a structured error signal payload."""
    stage = "provider_error"
    status = None
    message = str(error)

    if isinstance(error, httpx.HTTPStatusError):
        status = error.response.status_code
        if status in (401, 403):
            stage = "provider_auth"
            message = f"LLM call failed: authentication rejected by provider ({status})"
        elif status == 429:
            stage = "provider_rate_limit"
            message = f"LLM call failed: rate limited by provider ({status})"
        elif status == 400:
            stage = "request_rejected"
            message = f"LLM call failed: request rejected ({status})"
        elif status >= 500:
            stage = "provider_error"
            message = f"LLM call failed: provider error ({status})"
    elif isinstance(error, httpx.TimeoutException):
        stage = "timeout"
        message = "LLM call failed: request timed out"
    elif isinstance(error, (httpx.ConnectError, ConnectionError, OSError)):
        stage = "proxy_unreachable"
        message = "LLM call failed: could not reach enforcer/proxy"
    elif isinstance(error, (json.JSONDecodeError, ValueError)):
        stage = "response_malformed"
        message = f"LLM call failed: malformed response ({error})"

    return {
        "category": "llm.call_failed",
        "stage": stage,
        "status": status,
        "message": message,
        "model": model,
        "correlation_id": correlation_id,
        "retries_attempted": retries,
    }


def _provider_tool_grants(config_dir: Path) -> set[str]:
    """Return externally granted provider-tool capabilities for this agent."""
    grants: set[str] = set()

    constraints_path = config_dir / "constraints.yaml"
    try:
        constraints = yaml.safe_load(constraints_path.read_text()) or {}
        for capability in constraints.get("granted_capabilities", []):
            if isinstance(capability, str):
                grants.add(capability.strip())
    except Exception:
        pass

    effective_path = config_dir / "provider-tools.yaml"
    try:
        effective = yaml.safe_load(effective_path.read_text()) or {}
        for entry in effective.get("grants", []):
            if isinstance(entry, dict):
                capability = entry.get("capability")
                if isinstance(capability, str):
                    grants.add(capability.strip())
    except Exception:
        pass

    return {cap for cap in grants if cap in PROVIDER_TOOL_DEFINITIONS}


def _provider_tool_definitions(config_dir: Path) -> list[dict]:
    """Build provider-hosted server tool declarations from external grants."""
    definitions = []
    for capability in sorted(_provider_tool_grants(config_dir)):
        definitions.append(dict(PROVIDER_TOOL_DEFINITIONS[capability]))
    return definitions


def _provider_tool_prompt_section(config_dir: Path) -> str:
    """Describe granted provider-hosted tools for the model policy prompt."""
    grants = _provider_tool_grants(config_dir)
    lines = []
    if "provider-web-search" in grants:
        lines.append("- **web_search** — provider-executed live web search for current external information.")
    if not lines:
        return ""
    return (
        "# Provider Tools\n\n"
        "These provider-hosted tools are externally granted for this agent and may appear "
        "as server-side tool calls in the model request. Use them when the task requires "
        "current external information, and do not simulate them in text.\n\n"
        + "\n".join(lines)
    )


def _read_current_task(context_file: Path | None) -> dict | None:
    if context_file is None:
        return None
    try:
        data = json.loads(context_file.read_text(encoding="utf-8"))
    except Exception:
        return None
    task = data.get("current_task") if isinstance(data, dict) else None
    return task if isinstance(task, dict) else None


def _matches_current_task_event(task: dict, event: dict) -> bool:
    metadata = task.get("metadata") if isinstance(task.get("metadata"), dict) else {}
    msg = event.get("message") if isinstance(event.get("message"), dict) else {}
    channel = event.get("channel")
    msg_id = str(msg.get("id", "") or "")
    task_event_id = str(
        task.get("event_id") or task.get("work_item_id") or metadata.get("event_id") or ""
    )

    if msg_id and task_event_id in {msg_id, f"evt-{msg_id}"}:
        return True

    if channel and metadata.get("channel") != channel:
        return False

    summary = str(event.get("summary") or msg.get("summary") or msg.get("content") or "")
    task_content = str(task.get("content") or "")
    return bool(summary and summary in task_content)


def _activation_task_id(event: dict, context_file: Path | None, fallback_prefix: str) -> str:
    """Preserve externally assigned task ids for event-backed work."""
    candidates = [
        event.get("task_id"),
        event.get("work_item_id"),
    ]
    metadata = event.get("metadata")
    if isinstance(metadata, dict):
        candidates.extend([metadata.get("task_id"), metadata.get("work_item_id")])
    msg = event.get("message")
    if isinstance(msg, dict):
        candidates.extend([msg.get("task_id"), msg.get("work_item_id")])
        msg_metadata = msg.get("metadata")
        if isinstance(msg_metadata, dict):
            candidates.extend([msg_metadata.get("task_id"), msg_metadata.get("work_item_id")])

    for value in candidates:
        if isinstance(value, str) and value.strip():
            return value.strip()

    current_task = _read_current_task(context_file)
    if current_task and _matches_current_task_event(current_task, event):
        task_id = current_task.get("task_id")
        if isinstance(task_id, str) and task_id.strip():
            return task_id.strip()

    return f"{fallback_prefix}-{int(time.time())}"


def _pact_verdict_payload(
    task_id: str,
    contract: dict | None,
    evidence: dict | None,
    verdict: dict | None,
) -> dict:
    contract = contract if isinstance(contract, dict) else {}
    evidence = evidence if isinstance(evidence, dict) else {}
    verdict = verdict if isinstance(verdict, dict) else {}
    tools = []
    for item in evidence.get("tool_results") or []:
        if not isinstance(item, dict):
            continue
        tool = str(item.get("tool") or "").strip()
        if tool and tool not in tools:
            tools.append(tool)
    return {
        "task_id": task_id,
        "kind": contract.get("kind"),
        "verdict": verdict.get("verdict", "completed"),
        "required_evidence": list(contract.get("required_evidence") or []),
        "answer_requirements": list(contract.get("answer_requirements") or []),
        "missing_evidence": list(verdict.get("missing_evidence") or []),
        "observed": list(evidence.get("observed") or []),
        "source_urls": list(evidence.get("source_urls") or []),
        "artifact_paths": list(evidence.get("artifact_paths") or []),
        "changed_files": list(evidence.get("changed_files") or []),
        "validation_results": list(evidence.get("validation_results") or []),
        "tools": tools,
    }


def _pact_metadata_for_storage(payload: dict | None) -> dict | None:
    if not isinstance(payload, dict):
        return None
    return {
        "kind": payload.get("kind"),
        "verdict": payload.get("verdict"),
        "required_evidence": list(payload.get("required_evidence") or []),
        "answer_requirements": list(payload.get("answer_requirements") or []),
        "missing_evidence": list(payload.get("missing_evidence") or []),
        "observed": list(payload.get("observed") or []),
        "source_urls": list(payload.get("source_urls") or []),
        "artifact_paths": list(payload.get("artifact_paths") or []),
        "changed_files": list(payload.get("changed_files") or []),
        "validation_results": list(payload.get("validation_results") or []),
        "tools": list(payload.get("tools") or []),
    }


def _pact_activation_for_storage(metadata: dict | None) -> dict | None:
    if not isinstance(metadata, dict):
        return None
    activation = metadata.get("pact_activation")
    if not isinstance(activation, dict):
        return None
    return {
        "content": str(activation.get("content") or ""),
        "match_type": str(activation.get("match_type") or ""),
        "source": str(activation.get("source") or ""),
        "channel": str(activation.get("channel") or ""),
        "author": str(activation.get("author") or ""),
        "mission_active": bool(activation.get("mission_active")),
    }


def _sanitize_outbound_content(content: str) -> str:
    """Fail closed when model text tries to impersonate a tool call."""
    if not SIMULATED_TOOL_TAG_RE.search(content or ""):
        return content
    return (
        "I cannot provide that result because I attempted to describe a tool call "
        "instead of using a real, successful tool invocation. I need an available "
        "current-information tool or source access to answer this without guessing."
    )


def _sanitize_current_info_answer(contract: dict | None, content: str) -> str:
    if not isinstance(contract, dict) or contract.get("kind") != "current_info":
        return content
    kept = []
    for raw_line in str(content or "").splitlines():
        line = raw_line.strip()
        if not line:
            if kept and kept[-1] != "":
                kept.append("")
            continue
        if CURRENT_INFO_PREAMBLE_RE.match(line):
            continue
        kept.append(raw_line)
    sanitized = "\n".join(kept).strip()
    return sanitized or str(content or "").strip()


class Body:
    """Main body runtime — autonomic execution loop.

    Assembles system prompt from read-only mounted files, receives
    tasks via WebSocket push, runs LLM conversation loops with tool
    dispatch, emits signals, and manages the context window.
    """

    _CHANNEL_POSTING_KEYWORDS = [
        "post to", "send to", "share in", "notify",
        "report to", "write to #", "channel", "send_message(",
    ]

    def __init__(self, config_dir: str = "/agency"):
        self.config_dir = Path(config_dir)
        self.workspace_dir = Path(os.environ.get("AGENCY_WORKSPACE", "/workspace"))
        self.state_dir = self.config_dir / "state"
        self.signals_file = self.state_dir / "agent-signals.jsonl"
        self.context_file = self.state_dir / "session-context.json"
        self.conversation_log = self.state_dir / "conversation.jsonl"
        self.conversation_meta = self.state_dir / "conversation-meta.json"
        self.memory_dir = self.workspace_dir / ".memory"

        self.proxy_url = os.environ.get("AGENCY_ENFORCER_PROXY_URL", "http://enforcer:3128")
        self.control_url = os.environ.get("AGENCY_ENFORCER_CONTROL_URL", "http://enforcer:8081")
        self.enforcer_url = os.environ.get(
            "AGENCY_ENFORCER_URL",
            os.environ.get("OPENAI_API_BASE", f"{self.proxy_url}/v1"),
        )
        self.model = os.environ.get("AGENCY_MODEL", "claude-sonnet")
        self.admin_model = os.environ.get("AGENCY_ADMIN_MODEL", self.model)
        self.agent_name = os.environ.get("AGENCY_AGENT_NAME", "agent")
        self.context_window = int(os.environ.get(
            "AGENCY_CONTEXT_WINDOW", str(DEFAULT_CONTEXT_WINDOW)
        ))

        self._system_prompt: str | None = None
        self._active_mission = None
        mission_path = "/agency/mission.yaml"
        if os.path.exists(mission_path):
            with open(mission_path) as f:
                self._active_mission = yaml.safe_load(f)
            log.info("Mission loaded: %s (status: %s)",
                     self._active_mission.get("name"), self._active_mission.get("status"))
        # Detect coordinator role for team missions.
        self._is_coordinator = False
        self._has_coordinator = False
        if self._active_mission and self._active_mission.get("assigned_type") == "team":
            team_path = "/agency/team.yaml"
            if os.path.exists(team_path):
                with open(team_path) as f:
                    team_cfg = yaml.safe_load(f)
                coordinator = team_cfg.get("coordinator", "")
                self._has_coordinator = bool(coordinator)
                self._is_coordinator = (coordinator == os.environ.get("AGENCY_AGENT_NAME", "agent"))
        extra_dirs = [
            d for d in os.environ.get("AGENCY_EXTRA_MOUNT_TARGETS", "").split(":")
            if d
        ]
        self._builtin_tools = BuiltinToolRegistry(
            workspace_dir=self.workspace_dir,
            extra_allowed_dirs=extra_dirs or None,
        )
        self._skills_manager = SkillsManager(self.config_dir / "skills-manifest.json")
        self._service_dispatcher: ServiceToolDispatcher | None = None
        self._mcp_clients: list[MCPClient] = []
        self._mcp_tools: dict[str, MCPClient] = {}  # tool_name -> client
        self._mcp_tool_server: dict[str, str] = {}  # tool_name -> server_name
        self._mcp_server_names: set[str] = set()  # all registered server names
        self._mcp_policy = self._load_mcp_policy()
        self._http_client: httpx.Client | None = None
        self._running = True
        # _last_heartbeat removed — no periodic heartbeat
        self._last_task_hash: str | None = None
        self._correlation_counter = 0
        self._channel_reminder_sent = False
        self._checkpoint_injected = False
        self._notification_queue: list[tuple[str, str, str]] = []
        self._last_notification_task_time = 0.0
        self._knowledge_url = os.environ.get("AGENCY_KNOWLEDGE_URL", f"{self.control_url}/mediation/knowledge")
        self._comms_url = os.environ.get("AGENCY_COMMS_URL", f"{self.control_url}/mediation/comms")

        # Real-time comms event-driven loop state
        self._event_queue = queue_module.Queue()
        self._interruption_controller = InterruptionController(config_dir=self.config_dir)
        self._ws_listener = None
        self._pending_notifications = []
        self._pending_interrupts = []
        self._interrupt_metrics = {
            "turns_from_interrupts": 0,
            "interrupts_received": 0,
            "interrupts_acted_on": 0,
            "notifications_queued": 0,
        }

        # Hook server for real-time constraint push notifications
        from hook_server import HookServer
        self._hook_server = HookServer(
            on_constraint_change=lambda v, s: self.reload_constraints(v, s)
        )
        self._hook_server.start()

        # Meeseeks mode detection
        self.is_meeseeks = os.environ.get("AGENCY_MEESEEKS") == "true"
        if self.is_meeseeks:
            self.meeseeks_id = os.environ.get("AGENCY_MEESEEKS_ID", "")
            self.meeseeks_task = os.environ.get("AGENCY_MEESEEKS_TASK", "")
            self.meeseeks_parent = os.environ.get("AGENCY_MEESEEKS_PARENT", "")
            self.meeseeks_budget = float(os.environ.get("AGENCY_MEESEEKS_BUDGET", "0.05"))
            self.meeseeks_channel = os.environ.get("AGENCY_MEESEEKS_CHANNEL", "")
            self.meeseeks_budget_warned_50 = False
            self.meeseeks_budget_warned_80 = False
            log.info("Meeseeks mode active | id=%s parent=%s task=%s",
                     self.meeseeks_id, self.meeseeks_parent, self.meeseeks_task[:80])

    def _load_mcp_policy(self) -> dict | None:
        """Load MCP policy from constraints.yaml if present.

        Returns a dict with mode, allowed/denied servers/tools, or None.
        """
        constraints_path = self.config_dir / "constraints.yaml"
        if not constraints_path.exists():
            return None
        try:
            data = yaml.safe_load(constraints_path.read_text())
            mcp = data.get("mcp")
            if mcp:
                log.info("MCP policy loaded: mode=%s", mcp.get("mode", "denylist"))
                return mcp
        except Exception as e:
            log.warning("Failed to load MCP policy from constraints.yaml: %s", e)
        return None

    def reload_constraints(self, version: int, severity: str):
        """Fetch updated constraints from enforcer, apply, and ack with hash."""
        import hashlib

        # Fetch from enforcer
        url = f"{self.enforcer_url}/constraints"
        resp = httpx.get(url, timeout=5)
        resp.raise_for_status()
        data = resp.json()

        constraints = data["constraints"]

        # Apply MCP policy from new constraints
        mcp = constraints.get("mcp")
        if mcp:
            self._mcp_policy = mcp
            log.info("MCP policy reloaded: mode=%s", mcp.get("mode", "denylist"))

        # Compute hash — MUST match Go's json.Marshal canonical form
        canonical = json.dumps(constraints, sort_keys=True, separators=(",", ":")).encode()
        computed_hash = hashlib.sha256(canonical).hexdigest()

        # Ack to enforcer
        ack_url = f"{self.enforcer_url}/constraints/ack"
        httpx.post(ack_url, json={"version": version, "hash": computed_hash}, timeout=5)

        log.info(
            "Constraints reloaded: version=%d severity=%s hash=%s",
            version, severity, computed_hash[:12],
        )

    def _has_channel_posting_intent(self, task_text: str) -> bool:
        """Return True if task_text contains any channel posting keyword."""
        lower = task_text.lower()
        return any(kw in lower for kw in self._CHANNEL_POSTING_KEYWORDS)

    def _has_tool_call_in_history(self, messages: list[dict], tool_name: str) -> bool:
        """Return True if any message has a tool_call with the given function name."""
        for msg in messages:
            for tc in msg.get("tool_calls", []):
                if tc.get("function", {}).get("name") == tool_name:
                    return True
        return False

    @staticmethod
    def _has_tool_call(tool_calls: list[dict] | None, tool_name: str) -> bool:
        """Return True if tool_calls includes the named function."""
        if not tool_calls:
            return False
        for tc in tool_calls:
            if tc.get("function", {}).get("name") == tool_name:
                return True
        return False

    def _needs_channel_posting_reminder(
        self,
        task_content: str,
        messages: list[dict],
        tool_calls: list[dict] | None,
    ) -> bool:
        """Return True when a task asked for channel output but none was posted yet."""
        if self._channel_reminder_sent or not self._has_channel_posting_intent(task_content):
            return False
        return not (
            self._has_tool_call_in_history(messages, "send_message")
            or self._has_tool_call(tool_calls, "send_message")
        )

    def _is_mcp_server_allowed(self, server_name: str) -> bool:
        """Check if an MCP server is allowed by constraints policy."""
        if not self._mcp_policy:
            return True
        mode = self._mcp_policy.get("mode", "denylist")
        if mode == "allowlist":
            return server_name in self._mcp_policy.get("allowed_servers", [])
        else:
            return server_name not in self._mcp_policy.get("denied_servers", [])

    def _is_mcp_tool_allowed(self, tool_name: str) -> bool:
        """Check if an MCP tool is allowed by constraints policy."""
        if not self._mcp_policy:
            return True
        denied = self._mcp_policy.get("denied_tools", [])
        if denied and tool_name in denied:
            return False
        allowed = self._mcp_policy.get("allowed_tools", [])
        if allowed:
            return tool_name in allowed
        return True

    def _verify_mcp_server_hash(self, server_name: str, command: str) -> bool:
        """Verify MCP server command binary against pinned hash.

        Returns True if no pin exists or hash matches. Returns False on mismatch.
        """
        if not self._mcp_policy:
            return True
        pinned = self._mcp_policy.get("pinned_hashes", {})
        if server_name not in pinned:
            return True

        expected_hash = pinned[server_name]

        # Resolve command to full path
        import hashlib
        import shutil

        cmd_path = shutil.which(command)
        if not cmd_path:
            log.warning(
                "MCP server %s: command '%s' not found, cannot verify hash",
                server_name, command,
            )
            return False

        try:
            h = hashlib.sha256()
            with open(cmd_path, "rb") as f:
                for chunk in iter(lambda: f.read(8192), b""):
                    h.update(chunk)
            actual_hash = h.hexdigest()

            if actual_hash != expected_hash:
                log.error(
                    "MCP server %s: hash mismatch! expected=%s actual=%s",
                    server_name, expected_hash[:16], actual_hash[:16],
                )
                return False
            log.info("MCP server %s: hash verified", server_name)
            return True
        except OSError as e:
            log.warning("MCP server %s: hash verification failed: %s", server_name, e)
            return False

    # Analysis service URL for MCP output scanning (tenet 1: scanning
    # runs outside the agent's isolation boundary).
    _ANALYSIS_SCAN_URL = "http://analysis:8080/scan/mcp-output"

    def _scan_mcp_output_via_analysis(self, tool_name: str, output: str) -> tuple[list[str], str | None]:
        """Scan MCP tool output via the analysis service.

        Sends output to the analysis service for injection and cross-server
        scanning. Returns (flags, escalation_type). Falls back to empty
        result if analysis is unreachable.
        """
        if not output or len(output) < 10:
            return [], None

        source_server = self._mcp_tool_server.get(tool_name, "")

        try:
            client = self._http_client or httpx
            resp = client.post(
                self._ANALYSIS_SCAN_URL,
                json={
                    "output": output,
                    "source_tool": tool_name,
                    "source_server": source_server,
                    "server_names": list(self._mcp_server_names),
                    "tool_server_map": dict(self._mcp_tool_server),
                    "agent": self.agent_name,
                },
                timeout=5.0,
            )
            if resp.status_code == 200:
                data = resp.json()
                return data.get("flags", []), data.get("escalation_type")
        except Exception as e:
            log.warning("Analysis service unreachable for MCP scan: %s", e)

        return [], None

    # -- Lifecycle --

    def run(self) -> None:
        """Main entry point. Load tools/skills, assemble prompt, enter loop."""
        signal.signal(signal.SIGTERM, self._handle_sigterm)
        signal.signal(signal.SIGINT, self._handle_sigterm)

        log.info("Body runtime starting | agent=%s", self.agent_name)

        # Ensure state directory exists
        self.state_dir.mkdir(parents=True, exist_ok=True)

        # Log config discovery
        config_files = {
            "identity.md": (self.config_dir / "identity.md").exists(),
            "FRAMEWORK.md": (self.config_dir / "FRAMEWORK.md").exists(),
            "AGENTS.md": (self.config_dir / "AGENTS.md").exists(),
            "skills-manifest.json": (self.config_dir / "skills-manifest.json").exists(),
            "mcp-servers.json": (self.config_dir / "mcp-servers.json").exists(),
            "services-manifest.json": (self.config_dir / "services-manifest.json").exists(),
        }
        found = " ".join(f"{k}={'yes' if v else 'no'}" for k, v in config_files.items())
        log.info("Config: %s", found)

        # Load skills before assembling system prompt (skills add to prompt)
        self._skills_manager.load()
        if self._skills_manager.skill_names:
            log.info("Loaded %d skills", len(self._skills_manager.skill_names))
            # Register activate_skill as a built-in tool
            self._builtin_tools.register_tool(
                name="activate_skill",
                description="Activate an agent skill to load its procedural knowledge.",
                parameters={
                    "type": "object",
                    "properties": {
                        "name": {
                            "type": "string",
                            "description": "Name of the skill to activate",
                            "enum": self._skills_manager.skill_names,
                        },
                    },
                    "required": ["name"],
                },
                handler=lambda args: self._skills_manager.activate_skill(args["name"]),
            )

        # Register memory tools
        self._builtin_tools.register_tool(
            name="save_memory",
            description=(
                "Save information to a topic-based memory file. Each topic is a "
                "separate .md file in your memory directory. Use meaningful topic "
                "names like 'chefhub-architecture', 'blocking-issues', 'decisions'. "
                "By default, content is appended to the topic file. Set replace=true "
                "to overwrite — useful when reorganizing your notes."
            ),
            parameters={
                "type": "object",
                "properties": {
                    "topic": {
                        "type": "string",
                        "description": (
                            "Topic name (becomes filename). Use lowercase with "
                            "hyphens, e.g. 'project-architecture', 'open-questions'"
                        ),
                    },
                    "content": {
                        "type": "string",
                        "description": "Markdown-formatted content to save",
                    },
                    "replace": {
                        "type": "boolean",
                        "description": "If true, replace entire topic file instead of appending",
                    },
                },
                "required": ["topic", "content"],
            },
            handler=lambda args: self._save_memory(
                args["topic"], args["content"], replace=args.get("replace", False)
            ),
        )

        self._builtin_tools.register_tool(
            name="search_memory",
            description=(
                "Search across all your memory files for a keyword or phrase. "
                "Returns matching lines with surrounding context and the topic "
                "file they came from. Use this to recall details before starting "
                "work on a familiar subject."
            ),
            parameters={
                "type": "object",
                "properties": {
                    "query": {
                        "type": "string",
                        "description": "Search term or phrase to find in memory files",
                    },
                },
                "required": ["query"],
            },
            handler=lambda args: self._search_memory(args["query"]),
        )

        self._builtin_tools.register_tool(
            name="list_memories",
            description=(
                "List all your memory topic files with their sizes and first-line "
                "summaries. Use this to see what you know about before diving into "
                "a task."
            ),
            parameters={"type": "object", "properties": {}},
            handler=lambda args: self._list_memories(),
        )

        self._builtin_tools.register_tool(
            name="delete_memory",
            description=(
                "Delete a memory topic file. Use when information is outdated, "
                "wrong, or has been consolidated into another topic."
            ),
            parameters={
                "type": "object",
                "properties": {
                    "topic": {
                        "type": "string",
                        "description": "Topic name to delete",
                    },
                },
                "required": ["topic"],
            },
            handler=lambda args: self._delete_memory(args["topic"]),
        )

        # Register comms tools
        comms_url = os.environ.get("AGENCY_COMMS_URL", self._comms_url)
        agent_name = os.environ.get("AGENCY_AGENT_NAME", "unknown")
        from comms_tools import register_comms_tools
        register_comms_tools(self._builtin_tools, comms_url=comms_url, agent_name=agent_name)

        # Roll back read cursors on session start so messages posted just before
        # a restart are not silently lost (default: 10-minute lookback window).
        try:
            import httpx as _httpx
            _httpx.post(
                f"{comms_url}/cursors/{agent_name}/reset",
                json={"lookback_seconds": 600},
                timeout=5.0,
            )
            log.info("comms | session-start cursor reset for agent=%s", agent_name)
        except Exception as _e:
            log.warning("comms | cursor reset failed (non-fatal): %s", _e)

        # Register knowledge graph tools
        knowledge_url = os.environ.get("AGENCY_KNOWLEDGE_URL", self._knowledge_url)
        self._knowledge_url = knowledge_url
        from knowledge_tools import register_knowledge_tools
        register_knowledge_tools(self._builtin_tools, knowledge_url=knowledge_url, agent_name=agent_name, active_mission=self._active_mission)

        # Register task completion tool — agent must explicitly call this to
        # signal that a task is done. Prevents premature termination when the
        # LLM generates text without tool calls (finish_reason=="stop").
        self._task_complete_called = False
        self._builtin_tools.register_tool(
            name="complete_task",
            description=(
                "Signal that the current task is complete. You MUST call this tool "
                "when you have finished all requested work. Do not end a task by "
                "just generating text — always call complete_task with a summary "
                "of what you accomplished. If you still have work to do, keep "
                "working instead of calling this tool."
            ),
            parameters={
                "type": "object",
                "properties": {
                    "summary": {
                        "type": "string",
                        "description": "Brief summary of what was accomplished",
                    },
                },
                "required": ["summary"],
            },
            handler=lambda args: self._handle_complete_task(args["summary"]),
        )

        # Register authority tools (halt_agent, recommend_exception)
        from authority_tools import register_authority_tools
        register_authority_tools(self._builtin_tools, signal_fn=self._emit_signal, agent_name=agent_name)

        # Meeseeks mode: register escalate tool (no spawn_meeseeks — Meeseeks cannot spawn)
        if self.is_meeseeks:
            self._builtin_tools.register_tool(
                name="escalate",
                description="Escalate to the operator when you cannot complete your task or need human judgment.",
                parameters={
                    "type": "object",
                    "properties": {
                        "reason": {"type": "string", "description": "Why the task cannot be completed"},
                    },
                    "required": ["reason"],
                },
                handler=lambda args: self._handle_meeseeks_escalate(args["reason"]),
            )

        # Register spawn_meeseeks and kill_meeseeks when mission has meeseeks enabled
        # (only for parent agents, not for Meeseeks themselves)
        if not self.is_meeseeks and self._active_mission and self._active_mission.get("meeseeks"):
            self._register_meeseeks_tools()

        # Register claim tool for no-coordinator team missions (Task 5 deconfliction).
        if (self._active_mission
                and self._active_mission.get("assigned_type") == "team"
                and not self._has_coordinator):
            self._builtin_tools.register_tool(
                name="claim_mission_event",
                description=(
                    "Claim a trigger event for deconfliction on team missions without a coordinator. "
                    "Call this with a unique event key before acting on a trigger event. "
                    "If another agent already claimed it, skip the event."
                ),
                parameters={
                    "type": "object",
                    "properties": {
                        "event_key": {
                            "type": "string",
                            "description": "Unique key for the event (e.g., ticket ID, incident number)",
                        },
                    },
                    "required": ["event_key"],
                },
                handler=lambda args: self._tool_claim_mission_event(args["event_key"]),
            )

        log.info("Registered %d built-in tools", len(self._builtin_tools.get_tool_definitions()))

        self._system_prompt = self.assemble_system_prompt()
        log.info("System prompt assembled (%d chars)", len(self._system_prompt))

        proxy_url = os.environ.get("HTTP_PROXY", self.proxy_url)
        self._http_client = httpx.Client(
            timeout=LLM_TIMEOUT,
            proxy=proxy_url,
        )

        # Load service tools
        manifest_path = self.config_dir / "services-manifest.json"
        self._service_dispatcher = ServiceToolDispatcher(manifest_path)
        self._service_dispatcher.load()
        svc_tools = self._service_dispatcher.get_tool_definitions()
        if svc_tools:
            log.info("Loaded %d service tools", len(svc_tools))

        # Start MCP stdio servers
        self._start_mcp_servers()

        log.info("Entering main loop")
        self._emit_signal("ready", {})

        # Meeseeks startup message
        if self.is_meeseeks and self.meeseeks_channel:
            self._send_meeseeks_message(
                self.meeseeks_channel,
                "I'm Mr. Meeseeks! Look at me!"
            )

        # Start WebSocket listener
        comms_url = os.environ.get("AGENCY_COMMS_URL", self._comms_url)
        self._ws_listener = WSListener(
            comms_url=comms_url,
            agent_name=self.agent_name,
            event_queue=self._event_queue,
            context_file=self.context_file,
        )
        self._ws_listener.start()

        try:
            while self._running:
                try:
                    event = self._event_queue.get(timeout=2)
                except queue_module.Empty:
                    event = None

                # Fallback: check context file for tasks written directly
                # (e.g. when comms server unreachable from host)
                if event is None:
                    fallback_task = self._poll_task_fallback()
                    if fallback_task:
                        event = {"type": "task", "task": fallback_task}

                if event:
                    event_type = event.get("type")
                    if event_type == "system":
                        self._handle_system_event(event)
                    elif event_type == "task":
                        task = event.get("task", {})
                        task_id = task.get("task_id", "unknown")
                        log.info("New task received: %s", task_id)
                        self._emit_signal("task_accepted", {"task_id": task_id})
                        self._interruption_controller.start_task(task_id)
                        self._register_auto_interests(task)
                        self._conversation_loop(task)
                        self._clear_interests()
                        self._interruption_controller.end_task()
                    elif event_type == "mission_trigger":
                        self._handle_mission_trigger(event)
                    elif event_type in ("message", "knowledge"):
                        # If idle (no active task) and this is a direct mention
                        # or an interest match, spawn a lightweight response task.
                        # The comms server handles channel responsiveness filtering
                        # (silent/mention-only/active), so any event that reaches
                        # here has already passed the filter.
                        match = event.get("match", "ambient")
                        if event_type == "message" and match in ("direct", "interest_match"):
                            self._handle_idle_mention(event)
                        else:
                            self._handle_comms_event(event)
                    elif event_type == "connected":
                        log.info("WebSocket connected, channels: %d", len(event.get("channels", [])))

        finally:
            if self._ws_listener:
                self._ws_listener.stop()
            self._shutdown()

    # -- Real-time comms event handlers --

    def _handle_mission_trigger(self, event: dict) -> None:
        """Handle a mission trigger event delivered by the gateway event bus."""
        if not self._active_mission or self._active_mission.get("status") != "active":
            log.warning("Received mission trigger but no active mission — ignoring")
            return
        task_id = f"mission-{self._active_mission['name']}-{int(time.time())}"
        trigger_context = f"Mission trigger fired: {event.get('event_type', 'unknown')}"
        data = event.get("data")
        if data:
            trigger_context += f"\n\nEvent data:\n{json.dumps(data, indent=2)}"
        log.info("Mission trigger → task %s", task_id)
        self._emit_signal("task_accepted", {"task_id": task_id})
        self._interruption_controller.start_task(task_id)
        self._conversation_loop({"task_id": task_id, "prompt": trigger_context})
        self._interruption_controller.end_task()

    def _handle_comms_event(self, event: dict) -> None:
        match = event.get("match", "ambient")
        channel = event.get("channel", "?")
        flags = {}
        msg = event.get("message", {})
        if msg.get("flags"):
            flags = msg["flags"]
        action = self._interruption_controller.decide(match=match, flags=flags)
        log.info("comms event | channel=%s match=%s action=%s", channel, match, action)
        if action == "interrupt":
            self._inject_interrupt(event)
        elif action == "notify_at_pause":
            self._pending_notifications.append(event)
        else:
            self._interrupt_metrics["notifications_queued"] += 1

    _last_idle_reply_time: float = 0.0
    _IDLE_REPLY_COOLDOWN: float = 60.0  # seconds between idle replies
    _recent_idle_message_ids: set = None  # dedup set for message IDs

    def _fetch_recent_channel_context(self, channel: str, limit: int = 8) -> str:
        """Fetch a bounded channel transcript for lightweight idle replies."""
        comms_url = os.environ.get("AGENCY_COMMS_URL", "http://enforcer:8081/mediation/comms")
        client = self._http_client or httpx
        try:
            resp = client.get(
                f"{comms_url}/channels/{channel}/messages",
                params={"reader": self.agent_name, "limit": str(limit)},
                timeout=5,
            )
            resp.raise_for_status()
            messages = resp.json()
        except Exception as exc:
            log.info("recent channel context unavailable | channel=%s error=%s", channel, exc)
            return ""

        if not isinstance(messages, list):
            return ""

        lines = []
        for msg in messages[-limit:]:
            if not isinstance(msg, dict):
                continue
            sender = msg.get("author", "unknown")
            content = str(msg.get("content", "")).strip()
            if not content:
                continue
            if len(content) > 500:
                content = content[:500] + "..."
            lines.append(f"{sender}: {content}")
        if not lines:
            return ""
        return "Recent conversation in this channel:\n" + "\n".join(lines)

    def _build_direct_idle_prompt(self, channel: str, author: str, summary: str, recent_context: str = "") -> str:
        """Build the direct-DM/mention idle prompt."""
        identity_snapshot = self._config_text("identity.md")
        identity_clause = ""
        if identity_snapshot and identity_snapshot.strip():
            identity_clause = (
                "Current operator-defined identity and response policy "
                "(authoritative for this message):\n"
                f"{identity_snapshot.strip()}\n\n"
            )
        return (
            f"You received a direct message in #{channel} from {author}: \"{summary}\"\n\n"
            f"{identity_clause}"
            f"Use your current identity and system prompt as the response policy for this message.\n\n"
            f"{recent_context.strip() + chr(10) + chr(10) if recent_context.strip() else ''}"
            f"Rules:\n"
            f"- If your identity dictates exact wording, a fixed phrase, a refusal, a persona, or another specific response shape, use that literally.\n"
            f"- Do not answer the underlying question in a default helpful style when your identity gives a conflicting instruction.\n"
            f"- Only fall back to normal concise conversational help when your identity is silent on how to respond.\n"
            f"- Use the recent conversation transcript to resolve follow-up references like 'that', 'it', or 'whatever one'.\n"
            f"- If the transcript is insufficient, ask one concise clarifying question instead of guessing.\n"
            f"- For latest, current, recent, or time-sensitive facts, use an available search/fetch tool. If no such tool is available or it fails, say that directly.\n"
            f"- Never write simulated tool markup or pretend to have searched.\n"
            f"- Reply with the exact message text only. Do not call tools unless you truly need context.\n"
            f"- The platform will deliver your reply to #{channel}.\n"
            f"- If the person follows up, continue the conversation."
        )

    def _handle_idle_mention(self, event: dict) -> None:
        """Handle a mention or interest match when no task is active.

        Spawns a lightweight conversational task so the agent responds
        to channel messages even between active tasks. Handles both
        direct @mentions and interest_match events from active channels.

        Maximum 5 turns (more context may be needed for interest matches).
        Cooldown: 60 seconds between idle replies to prevent flooding
        in active channels (ASK: resource exhaustion defense).
        Dedup: tracks message IDs to avoid responding to the same message twice.
        """
        # Deduplicate by message ID — prevents responding multiple times
        # to the same message (e.g., when cursor reset replays recent messages)
        if self._recent_idle_message_ids is None:
            self._recent_idle_message_ids = set()
        msg_id = event.get("message", {}).get("id", "")
        if msg_id and msg_id in self._recent_idle_message_ids:
            log.info("idle response skipped (duplicate message_id=%s)", msg_id)
            return
        if msg_id:
            self._recent_idle_message_ids.add(msg_id)
            # Cap the set size to prevent unbounded growth
            if len(self._recent_idle_message_ids) > 200:
                self._recent_idle_message_ids = set(list(self._recent_idle_message_ids)[-100:])

        channel = event.get("channel", "general")
        msg = event.get("message", {})
        author = msg.get("author", "unknown")
        content = msg.get("content", "")
        summary = event.get("summary", content[:200])
        match_type = event.get("match", "direct")
        matched_kws = event.get("matched_keywords", [])

        # If agent has an active mission, only respond to @mentions and DMs
        # Skip interest-match triggers — they waste LLM calls on irrelevant chatter
        if self._active_mission and self._active_mission.get("status") == "active":
            if match_type != "direct":
                log.info("Ignoring non-mention event — agent is on mission %s", self._active_mission.get("name"))
                return

        # Cooldown only for interest matches — direct @mentions always get a response
        if match_type != "direct":
            now = time.monotonic()
            if (now - self._last_idle_reply_time) < self._IDLE_REPLY_COOLDOWN:
                log.info("idle response throttled (cooldown, match=%s)", match_type)
                return
        self._last_idle_reply_time = time.monotonic()

        log.info("idle response | channel=%s author=%s match=%s keywords=%s",
                 channel, author, match_type, matched_kws)

        mission_active = bool(self._active_mission and self._active_mission.get("status") == "active" and match_type == "direct")
        activation_context = ActivationContext.from_message(
            summary,
            match_type=match_type,
            mission_active=mission_active,
            source=f"idle_{match_type}",
            channel=channel,
            author=author,
        )
        work_contract = classify_activation(activation_context)

        # Construct prompt based on match type
        if match_type == "direct":
            prompt = self._build_direct_idle_prompt(
                channel,
                author,
                summary,
                self._fetch_recent_channel_context(channel),
            )
            prompt += contract_prompt(work_contract)
        else:
            # Interest match — agent's expertise is relevant
            kw_str = ", ".join(matched_kws) if matched_kws else "your area of expertise"
            prompt = (
                f"A message in #{channel} by {author} matches your expertise ({kw_str}): "
                f"\"{summary}\"\n\n"
                f"Read the recent messages in #{channel} with read_messages('{channel}') "
                f"and respond ONLY if you can add genuine value. If the message doesn't "
                f"actually need your input, call complete_task immediately.\n\n"
                f"If you do respond:\n"
                f"- Ask a clarifying question if the request is ambiguous.\n"
                f"- Do research first if it would meaningfully improve your answer.\n"
                f"- Save any new facts you learn about people with contribute_knowledge.\n"
                f"- Respond via send_message('{channel}', your_response).\n"
                f"- Call complete_task when the conversation is done."
            )
            prompt += contract_prompt(work_contract)

        task = {
            "type": "task",
            "task_id": _activation_task_id(
                event,
                self.context_file,
                "work-" + work_contract.kind if work_contract.requires_action else "idle-reply",
            ),
            "content": prompt,
            "source": f"idle_{match_type}:{channel}:{author}",
            "metadata": {
                "channel": channel,
                "author": author,
                "latest_message": summary,
                "message_id": msg_id,
                "match_type": match_type,
                "pact_activation": activation_context.to_dict(),
                "work_contract": work_contract.to_dict(),
            },
        }

        self._interruption_controller.start_task(task["task_id"])
        self._conversation_loop(task)
        self._interruption_controller.end_task()

    def _inject_interrupt(self, event: dict) -> None:
        channel = event.get("channel", "unknown")
        summary = event.get("summary", "")
        author = event.get("message", {}).get("author", "unknown")
        injection = (
            f"[Comms interrupt] #{channel} @{author}: {summary}. "
            f"Use read_messages('{channel}') for full context."
        )
        self._interrupt_metrics["interrupts_received"] += 1
        self._interruption_controller.record_interrupt()
        self._pending_interrupts.append({"role": "user", "content": injection})

    def _drain_notifications_at_pause(self) -> list[dict]:
        if not self._pending_notifications:
            return []
        lines = []
        for event in self._pending_notifications:
            ch = event.get("channel", "?")
            summary = event.get("summary", "")
            lines.append(f"  {ch}: {summary}")
        self._pending_notifications.clear()
        content = (
            f"[Comms] {len(lines)} new messages may be relevant to "
            f"your current task:\n" + "\n".join(lines) +
            "\nUse read_messages to review."
        )
        return [{"role": "user", "content": content}]

    def _drain_event_queue(self) -> None:
        while True:
            try:
                event = self._event_queue.get_nowait()
                event_type = event.get("type")
                if event_type == "system":
                    self._handle_system_event(event)
                elif event_type in ("message", "knowledge"):
                    self._handle_comms_event(event)
            except queue_module.Empty:
                break

    def _register_auto_interests(self, task: dict) -> None:
        content = task.get("content", "")
        words = content.lower().split()
        stop_words = {"the", "and", "for", "that", "this", "with", "from", "are", "was", "has", "have", "been", "will", "can", "not", "but", "its"}
        keywords = [w.strip(".,;:!?()[]{}\"'") for w in words if len(w) >= 3 and w not in stop_words]
        keywords = list(dict.fromkeys(keywords))[:20]
        if not keywords:
            return
        try:
            comms_url = os.environ.get("AGENCY_COMMS_URL", self._comms_url)
            httpx.Client(timeout=5).post(
                f"{comms_url}/subscriptions/{self.agent_name}/interests",
                json={
                    "task_id": task.get("task_id", "unknown"),
                    "description": content[:500],
                    "keywords": keywords,
                },
            )
        except Exception:
            log.warning("Failed to register auto interests")

    def _clear_interests(self) -> None:
        try:
            comms_url = os.environ.get("AGENCY_COMMS_URL", self._comms_url)
            httpx.Client(timeout=5).delete(f"{comms_url}/subscriptions/{self.agent_name}/interests")
        except Exception:
            pass

    def _handle_system_event(self, event: dict) -> None:
        log.info("System event received: %s", event.get("event", "unknown"))

    def _handle_sigterm(self, signum, frame):
        """Handle graceful shutdown."""
        log.info("Received signal %d, shutting down", signum)
        self._running = False

    def _shutdown(self) -> None:
        """Clean up resources."""
        for client in self._mcp_clients:
            try:
                client.shutdown()
            except Exception:
                pass
        if self._http_client:
            self._http_client.close()
        log.info("Body runtime stopped")

    # -- Mission Reload --

    def _reload_mission(self) -> None:
        """Re-read mission file and rebuild system prompt."""
        mission_path = "/agency/mission.yaml"
        if os.path.exists(mission_path):
            with open(mission_path) as f:
                self._active_mission = yaml.safe_load(f)
            self._system_prompt = self.assemble_system_prompt()
            log.info("Mission reloaded: %s v%d (status: %s)",
                     self._active_mission.get("name"),
                     self._active_mission.get("version", 0),
                     self._active_mission.get("status"))
            # Ack via signal
            self._emit_signal("mission_update_ack", {
                "mission_id": self._active_mission.get("id"),
                "version": self._active_mission.get("version"),
            })
        else:
            # Mission file removed (completed/unassigned)
            if self._active_mission:
                log.info("Mission removed: %s", self._active_mission.get("name"))
                self._active_mission = None
                self._system_prompt = self.assemble_system_prompt()

    # -- System Prompt Assembly --

    def assemble_system_prompt(self) -> str:
        """Build system prompt from mounted read-only files.

        Order: identity -> framework -> constraints (AGENTS.md).
        The runtime assembles but never generates these files.
        For Meeseeks mode: minimal task-focused prompt only.
        """
        # Meeseeks mode: minimal prompt, no identity/framework/agents
        if self.is_meeseeks:
            return MEESEEKS_SYSTEM_PROMPT.format(
                task=self.meeseeks_task,
                channel=self.meeseeks_channel or "operator",
            )

        parts = []

        # Identity
        identity_path = self.config_dir / "identity.md"
        if identity_path.exists():
            parts.append(identity_path.read_text().strip())

        # Mission context (after identity, before memory)
        if self._active_mission and self._active_mission.get("status") == "active":
            mission = self._active_mission
            mission_section = f"## Current Mission: {mission['name']} (id: {mission.get('id', 'unknown')[:8]})\n\n{mission.get('instructions', '')}"

            # Health indicators
            health = mission.get("health", {})
            if health and health.get("indicators"):
                mission_section += "\n\n### Health Monitoring\nWatch for these conditions and alert the operator if violated:\n"
                for indicator in health["indicators"]:
                    mission_section += f"- {indicator}\n"
                if health.get("business_hours"):
                    mission_section += f"Business hours: {health['business_hours']}\n"

            parts.append(mission_section)

            # Behavioral framing
            parts.append(
                f'You are assigned to mission "{mission["name"]}". This is your sole responsibility. '
                "If you receive requests unrelated to this mission, politely decline and suggest "
                "the requester find a more appropriate agent. Only respond to direct operator "
                "instructions that override your mission."
            )

            # Team mission framing
            if mission.get("assigned_type") == "team":
                team_name = mission.get("assigned_to", "")
                if self._is_coordinator:
                    parts.append(
                        f'You are the coordinator for team "{team_name}" on this mission. '
                        "Decompose the mission into sub-tasks and delegate via @mentions to team members. "
                        "Track progress through channel conversation. If a team member pushes back, reassign."
                    )
                else:
                    parts.append(
                        f'You are a member of team "{team_name}" on this mission. '
                        "Respond to task assignments from the coordinator or other team members. "
                        "If you receive a task outside your capability, push back and explain."
                    )

                # No-coordinator deconfliction framing (Task 5)
                if not self._has_coordinator:
                    parts.append(
                        "### Event Deconfliction\n"
                        "When a trigger event arrives, claim it before acting by calling "
                        "claim_mission_event with a unique key (e.g., ticket ID). "
                        "If the claim fails (another agent claimed it), skip the event. "
                        "If no claim is made within 30 seconds, any unclaimed agent may proceed."
                    )
        elif self._active_mission and self._active_mission.get("status") == "paused":
            parts.append(
                f'Your mission "{self._active_mission["name"]}" is currently paused. '
                "Respond to @mentions and operator DMs normally. Do not perform mission work until resumed."
            )

        # Persistent memory — placed right after identity so the agent
        # knows what it has already learned before starting new work.
        memory_index = self._build_memory_index()
        if memory_index:
            parts.append(
                "# Your Memory\n\n"
                "You have persistent memory organized as topic files. "
                "IMPORTANT: Before starting any new task, review your memory index "
                "below and use search_memory or read relevant topic files to avoid "
                "repeating work you have already done.\n\n"
                "## Memory tools\n"
                "- **save_memory(topic, content)** — save to a topic file (append or replace)\n"
                "- **search_memory(query)** — search across all memory files\n"
                "- **list_memories()** — see all topics and summaries\n"
                "- **delete_memory(topic)** — remove outdated topics\n\n"
                "Your memory is yours. Organize it however makes sense. When your "
                "notes get messy or redundant, reorganize them — consolidate related "
                "topics, split large ones, delete stale information. Good memory "
                "hygiene means you work faster on future tasks.\n\n"
                "## Memory Index\n\n"
                + memory_index
            )

        # Team communication context
        from comms_tools import build_comms_context
        comms_context = build_comms_context(
            os.environ.get("AGENCY_COMMS_URL", self._comms_url),
            os.environ.get("AGENCY_AGENT_NAME", "unknown"),
        )
        if comms_context:
            parts.append(comms_context)

        # Framework governance
        framework_path = self.config_dir / "FRAMEWORK.md"
        if framework_path.exists():
            parts.append(framework_path.read_text().strip())

        # Constraints and services
        agents_path = self.config_dir / "AGENTS.md"
        if agents_path.exists():
            parts.append(agents_path.read_text().strip())

        # Skills section (loaded on demand via activate_skill tool)
        skills_section = self._skills_manager.get_system_prompt_section()
        if skills_section:
            parts.append(skills_section)

        provider_tools = _provider_tool_prompt_section(self.config_dir)
        if provider_tools:
            parts.append(provider_tools)

        if not parts:
            return "You are an AI agent. Follow your operator's instructions."

        # Task completion expectations
        parts.append(
            "# How to Respond\n\n"
            "**Quality over speed.** A thoughtful answer is better than a fast generic one.\n\n"
            "- If a request is ambiguous, ask a clarifying question before guessing.\n"
            "- If research (web search, knowledge query) would improve your answer, do it.\n"
            "- When someone asks for latest, current, recent, or time-sensitive information, "
            "use an available search or fetch tool before answering. If no current-information "
            "tool is available, say that directly instead of answering from stale memory.\n"
            "- Do not claim you used a tool, searched the web, read a file, or checked a system "
            "unless you actually made the corresponding tool call.\n"
            "- Do not write simulated tool markup like <search>...</search>, pseudo tool calls, "
            "or placeholders. If a needed tool is unavailable or fails, say that plainly and "
            "explain what would unblock the request.\n"
            "- If you learn facts about a person (name, location, preferences, role, team), "
            "save them with contribute_knowledge so all agents benefit.\n"
            "- Do not pad responses with filler or disclaimers. Be direct and substantive.\n\n"
            "# Operating Loop\n\n"
            "For every non-trivial task:\n"
            "1. Clarify the objective and constraints.\n"
            "2. Inspect available context before answering: memory, recent messages, files, "
            "or web/tool results when relevant.\n"
            "3. Choose the smallest sufficient plan.\n"
            "4. Use tools when freshness, external facts, files, or system state matter.\n"
            "5. Validate the result before finalizing. For current facts, cite or name the "
            "source. For code, run the smallest relevant test.\n"
            "6. If blocked by missing tools, missing access, ambiguity, or risk, say exactly "
            "what is blocked and what would unblock it.\n"
            "7. Complete with a concise result.\n\n"
            "# Task Completion\n\n"
            "When you receive a task, execute every action it requires — do not stop "
            "at analysis or planning.\n\n"
            "Call **complete_task(summary=...)** when the task is done or the conversation "
            "reaches a natural conclusion. The platform requires this explicit signal.\n\n"
            "If someone follows up or asks a new question, continue the conversation — "
            "do not rush to complete after a single exchange."
        )

        return "\n\n---\n\n".join(parts)

    # -- MCP Server Management --

    def _start_mcp_servers(self) -> None:
        """Start configured MCP stdio servers."""
        mcp_config_path = self.config_dir / "mcp-servers.json"
        if not mcp_config_path.exists():
            return

        try:
            config = json.loads(mcp_config_path.read_text())
        except (json.JSONDecodeError, OSError) as e:
            log.warning("Failed to load MCP config: %s", e)
            return

        servers = config.get("servers", {})
        failed = []
        for name, server_config in servers.items():
            # Check MCP policy at server level
            if not self._is_mcp_server_allowed(name):
                log.info("MCP server %s blocked by policy", name)
                continue

            command = server_config.get("command")
            args = server_config.get("args", [])
            env = server_config.get("env", {})

            if not command:
                log.warning("MCP server %s has no command, skipping", name)
                failed.append(name)
                continue

            # Verify binary hash if pinned
            if not self._verify_mcp_server_hash(name, command):
                log.error("MCP server %s blocked: hash verification failed", name)
                failed.append(name)
                continue

            try:
                client = MCPClient(command, args, env)
                client.start()
                client.initialize()
                tools = client.list_tools()
                self._mcp_clients.append(client)
                self._mcp_server_names.add(name)

                registered = 0
                for tool in tools:
                    tool_name = tool.get("name", "")
                    if tool_name and self._is_mcp_tool_allowed(tool_name):
                        if tool_name in self._mcp_tools:
                            existing_server = self._mcp_tool_server[tool_name]
                            log.warning(
                                "MCP tool name collision: %s from %s overwrites %s",
                                tool_name, name, existing_server,
                            )
                        self._mcp_tools[tool_name] = client
                        self._mcp_tool_server[tool_name] = name
                        registered += 1
                    elif tool_name:
                        log.info("MCP tool %s blocked by policy", tool_name)

                log.info(
                    "MCP server %s started (%d/%d tools registered)",
                    name, registered, len(tools),
                )
            except Exception as e:
                log.warning("Failed to start MCP server %s: %s", name, e)
                failed.append(name)

        if failed and len(failed) == len(servers):
            log.error(
                "All MCP servers failed to start: %s. "
                "Agent will have no MCP tools available.",
                ", ".join(failed),
            )

    # -- Task Polling --

    def _poll_task_fallback(self) -> dict | None:
        """Read session-context.json for new task delivery (fallback for WS reconnect).

        Returns the task dict if a new task is available, None otherwise.
        Tracks the last seen task hash to avoid re-processing.
        """
        return self._poll_task_impl()

    # Backward-compatible alias used by existing tests and callers
    _poll_task = _poll_task_fallback

    def _poll_task_impl(self) -> dict | None:
        """Internal implementation for file-based task polling."""
        if not self.context_file.exists():
            return None

        try:
            content = self.context_file.read_text().strip()
            if not content or content == "{}":
                return None

            ctx = json.loads(content)
        except (json.JSONDecodeError, OSError):
            return None

        task = ctx.get("current_task")
        if not task:
            return None

        # Check if this is a new task
        task_hash = json.dumps(task, sort_keys=True)
        if task_hash == self._last_task_hash:
            return None

        self._last_task_hash = task_hash
        task_id = task.get("task_id", "unknown")
        log.info("New task received: %s", task_id)
        self._emit_signal("task_accepted", {"task_id": task_id})
        return task

    # -- LLM Conversation Loop --

    def _check_budget(self, task: dict) -> bool:
        """Check budget before starting a task. Returns True if budget is available."""
        try:
            enforcer_base = self.enforcer_url.rsplit("/v1", 1)[0]
            resp = httpx.get(f"{enforcer_base}/budget", timeout=5)
            if resp.status_code != 200:
                log.warning("Budget check returned %d, proceeding", resp.status_code)
                return True
            budget_info = resp.json()
        except Exception as e:
            log.warning("Budget check failed, proceeding: %s", e)
            return True  # fail-open on budget check errors

        # Check daily budget
        daily_remaining = budget_info.get("daily_remaining", float("inf"))
        if daily_remaining <= 0:
            daily_used = budget_info.get("daily_used", 0)
            daily_limit = budget_info.get("daily_limit", 0)
            self._post_operator_notification(
                "budget_daily_exhausted",
                f"**{self.agent_name}** daily budget exhausted "
                f"(${daily_used:.2f}/${daily_limit:.2f}). "
                f"Pending tasks queued.",
            )
            return False

        # Check monthly budget — if less than one per-task remains
        monthly_remaining = budget_info.get("monthly_remaining", float("inf"))
        per_task_limit = budget_info.get("per_task_limit", 2.0)
        if monthly_remaining < per_task_limit:
            monthly_used = budget_info.get("monthly_used", 0)
            monthly_limit = budget_info.get("monthly_limit", 0)
            self._post_operator_notification(
                "budget_monthly_exhausted",
                f"**{self.agent_name}** monthly budget nearly exhausted "
                f"(${monthly_used:.2f}/${monthly_limit:.2f}). "
                f"Mission auto-paused.",
            )
            return False

        # Estimate input size for large-input rejection
        task_content = task.get("content", "")
        estimated_tokens = len(str(task_content)) // CHARS_PER_TOKEN
        # Use a rough cost estimate based on common model pricing ($3/MTok input)
        estimated_cost = estimated_tokens * 3.0 / 1_000_000
        if per_task_limit > 0 and estimated_cost > per_task_limit * 0.5:
            self._post_operator_notification(
                "budget_input_rejected",
                f"Task rejected -- input too large "
                f"(est. {estimated_tokens:,} tokens, ~${estimated_cost:.2f}). "
                f"Task: '{task.get('task_id', 'unknown')}'",
            )
            return False

        return True

    def _conversation_loop(self, task: dict) -> None:
        """Run a full conversation loop for a task.

        Sends the task to the LLM, processes responses (text or tool
        calls), and loops until the LLM produces a final text response
        with no tool calls.
        """
        task_content = task.get("content", task.get("task_content", ""))
        task_id = task.get("task_id", "unknown")
        self._total_tasks += 1
        self._current_task_id = task_id
        self._current_task_tier = task.get("tier")
        self._task_metadata = task.get("metadata", {}) if isinstance(task.get("metadata"), dict) else {}
        self._task_complete_called = False
        self._current_task_turns = 0
        self._simulated_tool_retry_sent = False
        self._work_contract = self._task_metadata.get("work_contract") if isinstance(self._task_metadata, dict) else None
        self._work_evidence_ledger = EvidenceLedger()
        self._work_evidence = self._work_evidence_ledger.to_dict()
        self._last_pact_verdict = None
        self._task_terminal_outcome = None
        self._work_contract_retry_sent = False

        # Pre-task budget check
        if not self._check_budget(task):
            log.warning("Task %s rejected by budget check", task_id)
            self._emit_signal("error", {
                "category": "budget_exhausted",
                "message": "Task rejected by pre-task budget check",
                "task_id": task_id,
            })
            return

        # Signal that we're processing — drives typing indicators in clients
        source = task.get("source", "dm")
        channel = task.get("channel", "general")
        if ":" in source:
            # idle_direct:general:operator → extract channel
            parts = source.split(":")
            if len(parts) >= 2:
                channel = parts[1]
        self._emit_signal("processing", {
            "task_id": task_id,
            "channel": channel,
            "source": source,
        })

        # Re-read mission file at task start (picks up hot-reload changes)
        self._reload_mission()

        # Refresh system prompt to include latest memory
        self._system_prompt = self.assemble_system_prompt()

        # Try crash recovery — restore conversation if we were mid-task
        messages = self._restore_conversation(task_id)

        if messages:
            log.info("Restored conversation for task %s (%d messages)", task_id, len(messages))
            self._emit_signal("progress_update", {
                "content": f"Resuming task after restart: {task_content[:100]}",
                "task_id": task_id,
            })
        else:
            self._emit_signal("progress_update", {
                "content": f"Starting task: {task_content[:100]}",
                "task_id": task_id,
            })

            knowledge_context = self._retrieve_knowledge_context(task_content)
            if knowledge_context:
                user_content = f"{knowledge_context}\n\n---\n\n{task_content}"
            else:
                user_content = task_content

            messages = [
                {"role": "system", "content": self._system_prompt},
                {"role": "user", "content": user_content},
            ]

        tools = self._get_all_tool_definitions()

        turn = 0
        while True:
            turn += 1
            self._current_task_turns = turn
            self._total_turns += 1

            # Check if agent already called complete_task in a previous turn
            # (e.g., called complete_task alongside other tool calls).
            if self._task_complete_called:
                self._finalize_task(task_id, turn)
                break

            # Sync point: drain event queue for interrupts/notifications
            self._drain_event_queue()
            injections = list(self._pending_interrupts) + self._drain_notifications_at_pause()
            self._pending_interrupts.clear()
            for inj in injections:
                messages.append(inj)

            # Context window management
            messages = self._manage_context(messages)

            # Persist conversation state for crash recovery
            self._persist_conversation(messages, task_id=task_id)

            # Call LLM
            try:
                response = self._call_llm(messages, tools=tools if tools else None)
            except httpx.HTTPStatusError as e:
                if e.response.status_code == 429:
                    # Check if this is a budget exhaustion from the enforcer
                    try:
                        err_body = e.response.json()
                        if err_body.get("error", {}).get("type") == "budget_exhausted":
                            level = err_body["error"].get("level", "unknown")
                            msg = err_body["error"].get("message", "Budget exhausted")
                            log.warning("Budget exhausted (%s): %s", level, msg)
                            self._emit_signal("error", {
                                "category": "budget_exhausted",
                                "level": level,
                                "message": msg,
                                "task_id": task_id,
                            })
                            self._post_operator_notification(
                                "budget_exhausted",
                                f"**{self.agent_name}** budget exhausted ({level}): {msg}",
                            )
                            break
                    except Exception:
                        pass
                log.error("LLM call failed: %s", e)
                error_data = classify_llm_error(
                    e,
                    model=self.model,
                    correlation_id=getattr(self, '_last_correlation_id', ''),
                    retries=LLM_MAX_RETRIES,
                )
                error_data["task_id"] = task_id
                self._emit_signal("error", error_data)
                break
            except Exception as e:
                log.error("LLM call failed: %s", e)
                error_data = classify_llm_error(
                    e,
                    model=self.model,
                    correlation_id=getattr(self, '_last_correlation_id', ''),
                    retries=LLM_MAX_RETRIES,
                )
                error_data["task_id"] = task_id
                self._emit_signal("error", error_data)
                break

            if not response:
                log.warning("Empty LLM response")
                break

            # Extract the assistant message
            choice = response.get("choices", [{}])[0]
            message = choice.get("message", {})
            finish_reason = choice.get("finish_reason", "")

            # Add assistant message to history
            messages.append(message)

            # Process tool calls if present (parallel when multiple)
            tool_calls = message.get("tool_calls")
            if tool_calls:
                if len(tool_calls) == 1:
                    _tc = tool_calls[0]
                    _tool_name = _tc.get("function", {}).get("name", "")
                    result = self._handle_tool_call(_tc)
                    self._record_work_tool_result(_tool_name, result, self._tool_call_arguments(_tc))
                    messages.append({
                        "role": "tool",
                        "tool_call_id": _tc["id"],
                        "content": result,
                    })
                    if self._task_complete_called:
                        if self._needs_channel_posting_reminder(task_content, messages, tool_calls):
                            self._channel_reminder_sent = True
                            messages.append({
                                "role": "user",
                                "content": (
                                    "[Platform reminder] Your task asked you to post findings "
                                    "to a channel. Verify you have posted your substantive "
                                    "output (not just a status update) via send_message. "
                                    "If you already did, call complete_task again."
                                ),
                            })
                            self._task_complete_called = False
                            log.info("Channel posting reminder injected for task %s", task_id)
                            continue
                        self._finalize_task(task_id, turn)
                        break
                else:
                    log.info("Executing %d tool calls in parallel", len(tool_calls))
                    with ThreadPoolExecutor(max_workers=min(len(tool_calls), 4)) as pool:
                        futures = {
                            pool.submit(self._handle_tool_call, tc): tc
                            for tc in tool_calls
                        }
                        results = {}
                        for future in as_completed(futures):
                            tc = futures[future]
                            try:
                                results[tc["id"]] = future.result()
                            except Exception as e:
                                log.warning("Tool %s failed: %s", tc.get("function", {}).get("name"), e)
                                results[tc["id"]] = json.dumps({"error": str(e)})
                    # Append results in the original tool_calls order
                    for tc in tool_calls:
                        self._record_work_tool_result(
                            tc.get("function", {}).get("name", ""),
                            results.get(tc["id"], ""),
                            self._tool_call_arguments(tc),
                        )
                        messages.append({
                            "role": "tool",
                            "tool_call_id": tc["id"],
                            "content": results[tc["id"]],
                        })
                    if self._task_complete_called:
                        if self._needs_channel_posting_reminder(task_content, messages, tool_calls):
                            self._channel_reminder_sent = True
                            messages.append({
                                "role": "user",
                                "content": (
                                    "[Platform reminder] Your task asked you to post findings "
                                    "to a channel. Verify you have posted your substantive "
                                    "output (not just a status update) via send_message. "
                                    "If you already did, call complete_task again."
                                ),
                            })
                            self._task_complete_called = False
                            log.info("Channel posting reminder injected for task %s", task_id)
                            continue
                        self._finalize_task(task_id, turn)
                        break
                continue  # Loop back to LLM with tool results

            # Text response with no tool calls
            content = message.get("content", "")
            if content:
                log.info("LLM response (%d chars)", len(content))
            if content and SIMULATED_TOOL_TAG_RE.search(content):
                if not self._simulated_tool_retry_sent:
                    self._simulated_tool_retry_sent = True
                    messages.append({
                        "role": "user",
                        "content": (
                            "[Platform] Your previous response attempted to describe a tool call "
                            "in text. That is not allowed and was not a real tool invocation. "
                            "If a current-information tool such as web_search is available, call "
                            "the real tool now. If it is unavailable or fails, say exactly that "
                            "without guessing. Do not include simulated tool markup."
                        ),
                    })
                    log.info("Simulated tool markup rejected; retry prompt injected for task %s", task_id)
                    continue
                content = _sanitize_outbound_content(content)
            if content:
                content = self._materialize_file_artifact_summary(content)
                completion_verdict = validate_completion(
                    getattr(self, "_work_contract", None),
                    getattr(self, "_work_evidence", None),
                    content,
                )
                self._emit_pact_verdict(task_id, completion_verdict)
                if completion_verdict.get("verdict") == "needs_action":
                    if not getattr(self, "_work_contract_retry_sent", False):
                        self._work_contract_retry_sent = True
                        messages.append({
                            "role": "user",
                            "content": "[Platform work contract] " + completion_verdict.get("message", "Required evidence is missing."),
                        })
                        log.info("Work contract completion gate injected for task %s: %s", task_id, completion_verdict.get("missing_evidence"))
                        continue
                    content = format_blocked_completion(
                        getattr(self, "_work_contract", None),
                        getattr(self, "_work_evidence", None),
                        completion_verdict.get("message", "Required evidence is missing."),
                    )
                elif completion_verdict.get("verdict") == "blocked":
                    content = completion_verdict.get("message") or format_blocked_completion(
                        getattr(self, "_work_contract", None),
                        getattr(self, "_work_evidence", None),
                        content,
                    )
                    self._commit_pact_terminal_outcome("blocked", content)
                content = _sanitize_current_info_answer(getattr(self, "_work_contract", None), content)

            if finish_reason == "stop" and self._task_complete_called:
                # Agent explicitly called complete_task — honor it.
                # Check channel posting reminder first.
                if (
                    getattr(self, "_task_terminal_outcome", None) != "blocked"
                    and not self._channel_reminder_sent
                    and self._has_channel_posting_intent(task_content)
                ):
                    self._channel_reminder_sent = True
                    messages.append({
                        "role": "user",
                        "content": (
                            "[Platform reminder] Your task asked you to post findings "
                            "to a channel. Verify you have posted your substantive "
                            "output (not just a status update) via send_message. "
                            "If you already did, call complete_task again."
                        ),
                    })
                    self._task_complete_called = False
                    log.info("Channel posting reminder injected for task %s", task_id)
                    continue

                result_text = content if content else "Task completed"
                self._emit_signal("task_complete", {
                    "result": result_text,
                    "task_id": task_id,
                    "turns": turn + 1,
                    **self._interrupt_metrics,
                })
                # Save full result as a downloadable markdown artifact
                self._save_result_artifact(task_id, task_content, result_text, turn + 1)
                # Post summary to operator channel with artifact reference
                summary = result_text[:200].split("\n")[0] if len(result_text) > 200 else result_text.split("\n")[0]
                self._post_operator_notification(
                    "task_complete",
                    f"**{self.agent_name}** completed task: {summary}",
                    metadata={
                        "event_type": "task_complete",
                        "agent": self.agent_name,
                        "task_id": task_id,
                        "attachment_id": task_id,
                        "has_artifact": True,
                    },
                )
                self._interrupt_metrics = {k: 0 for k in self._interrupt_metrics}
                self._auto_summarize_task(task_id, task_content, result_text)
                self._clear_conversation_log()
                self._current_task_id = None
                # Note: session-context.json is mounted read-only (ASK tenet 5).
                # The gateway clears current_task by cross-referencing heartbeat
                # signals, which report active_task=null after task completion.
                self._channel_reminder_sent = False
                self._checkpoint_injected = False
                self._task_terminal_outcome = None
                log.info("Task %s complete (%d turns)", task_id, turn + 1)
                break
            elif finish_reason == "stop":
                # Agent generated text without calling complete_task.
                # For idle replies (lightweight channel responses), auto-finalize
                # instead of nudging — the nudge causes the agent to re-read the
                # channel and send a duplicate reply.
                if task_id.startswith(("idle-reply-", "notification-")):
                    if content and content.strip():
                        log.info("Idle reply fallback post | task=%s channel=%s chars=%d", task_id, channel, len(content.strip()))
                        if self._post_channel_message(channel, content.strip()):
                            log.info("Task %s: idle reply auto-finalized (turn %d)", task_id, turn + 1)
                            self._finalize_task(task_id, turn)
                            break
                        messages.append({
                            "role": "user",
                            "content": (
                                "[Platform] Your reply was not delivered to the channel. "
                                "Try again using send_message with the exact response text, "
                                "then call complete_task."
                            ),
                        })
                        continue
                    messages.append({
                        "role": "user",
                        "content": (
                            "[Platform] You have not replied in the DM yet. "
                            "Send the reply via send_message or provide the exact reply text "
                            "so the platform can post it, then call complete_task."
                        ),
                    })
                    continue
                # For regular tasks, nudge to continue or explicitly complete.
                messages.append({
                    "role": "user",
                    "content": (
                        "[Platform] You haven't called complete_task yet. "
                        "If you have more work to do on this task, continue. "
                        "If you're done, call complete_task(summary=...) with "
                        "a summary of what you accomplished."
                    ),
                })
                log.info("Task %s: nudging agent to continue or complete (turn %d)", task_id, turn + 1)
                continue

    def _call_llm(self, messages: list[dict], tools: list[dict] | None = None) -> dict:
        """POST to the enforcer's OpenAI-compatible chat endpoint.

        Uses streaming when available — tokens are printed to stderr as
        they arrive. Falls back to non-streaming on error.
        Returns the complete response in non-streaming format.
        """
        url = f"{self.enforcer_url}/chat/completions"

        payload = {
            "model": self._current_model(),
            "messages": messages,
            "stream": True,
        }
        if tools:
            payload["tools"] = tools

        # Generate correlation ID for end-to-end tracing
        self._correlation_counter += 1
        correlation_id = f"{self.agent_name}-{self._current_task_id or 'notask'}-{self._correlation_counter}"
        self._last_correlation_id = correlation_id

        headers = {"X-Correlation-Id": correlation_id}
        if self._current_task_id:
            headers["X-Agency-Task-Id"] = self._current_task_id
        api_key = os.environ.get("OPENAI_API_KEY")
        if api_key:
            headers["Authorization"] = f"Bearer {api_key}"

        for attempt in range(LLM_MAX_RETRIES):
            try:
                return self._stream_llm_response(url, payload, headers)
            except httpx.HTTPStatusError as e:
                if e.response.status_code in (429, 502, 503) and attempt < LLM_MAX_RETRIES - 1:
                    retry_after = e.response.headers.get("retry-after", "")
                    if retry_after:
                        try:
                            wait = float(retry_after)
                        except ValueError:
                            wait = 2 ** attempt
                    else:
                        wait = 2 ** attempt
                    log.warning("LLM returned %d, retrying in %.1fs (attempt %d/%d)",
                                e.response.status_code, wait, attempt + 1, LLM_MAX_RETRIES)
                    time.sleep(wait)
                    continue
                # Log error response body for debugging
                try:
                    err_body = e.response.text[:500]
                    log.error("LLM error response (%d): %s", e.response.status_code, err_body)
                except Exception:
                    pass
                raise
            except httpx.TimeoutException:
                if attempt < LLM_MAX_RETRIES - 1:
                    log.warning("LLM timeout, retrying (attempt %d)", attempt + 1)
                    continue
                raise

    def _stream_llm_response(self, url: str, payload: dict, headers: dict) -> dict:
        """Execute a streaming LLM request, printing tokens as they arrive.

        Accumulates SSE chunks into a complete response dict matching the
        non-streaming format (choices[0].message + finish_reason).
        """
        content_parts: list[str] = []
        tool_calls_acc: dict[int, dict] = {}  # index -> {id, function: {name, arguments}}
        finish_reason = ""

        with self._http_client.stream("POST", url, json=payload, headers=headers) as resp:
            if resp.status_code >= 400:
                err_body = resp.read().decode(errors="replace")[:500]
                log.error("LLM error response (%d): %s", resp.status_code, err_body)
                resp.raise_for_status()

            for line in resp.iter_lines():
                if not line or not line.startswith("data: "):
                    continue
                data = line[6:]  # Strip "data: " prefix
                if data == "[DONE]":
                    break

                try:
                    chunk = json.loads(data)
                except json.JSONDecodeError:
                    continue

                if chunk.get("object") == "agency.provider_tool_evidence":
                    self._record_provider_tool_evidence(
                        chunk.get("agency_provider_tool_evidence") or {}
                    )
                    continue

                choices = chunk.get("choices", [])
                if not choices:
                    continue

                delta = choices[0].get("delta", {})
                chunk_finish = choices[0].get("finish_reason")

                if chunk_finish:
                    finish_reason = chunk_finish

                # Accumulate text content and stream to stderr
                text = delta.get("content")
                if text:
                    content_parts.append(text)
                    print(text, end="", file=sys.stderr, flush=True)

                # Accumulate tool calls
                tc_deltas = delta.get("tool_calls", [])
                for tc in tc_deltas:
                    idx = tc.get("index", 0)
                    func = tc.get("function", {}) or {}
                    if not (tc.get("id") or func.get("name") or func.get("arguments")):
                        continue
                    if idx not in tool_calls_acc:
                        tool_calls_acc[idx] = {
                            "id": tc.get("id", ""),
                            "type": "function",
                            "function": {"name": "", "arguments": ""},
                        }
                    if tc.get("id"):
                        tool_calls_acc[idx]["id"] = tc["id"]
                    if func.get("name"):
                        tool_calls_acc[idx]["function"]["name"] = func["name"]
                    if func.get("arguments"):
                        tool_calls_acc[idx]["function"]["arguments"] += func["arguments"]

        # Print newline after streaming text
        if content_parts:
            print("", file=sys.stderr, flush=True)

        # Build complete message in non-streaming format
        message: dict = {"role": "assistant"}
        content = "".join(content_parts)
        if content:
            message["content"] = content
        if tool_calls_acc:
            complete_tool_calls = [
                tool_calls_acc[i] for i in sorted(tool_calls_acc.keys())
                if tool_calls_acc[i].get("function", {}).get("name")
            ]
            if complete_tool_calls:
                message["tool_calls"] = complete_tool_calls

        return {
            "choices": [{
                "message": message,
                "finish_reason": finish_reason or "stop",
            }],
        }

    # -- Tool Dispatch --

    def _get_all_tool_definitions(self) -> list[dict]:
        """Collect tool definitions from all sources."""
        tools = []

        # Built-in tools (read_file, write_file, etc.)
        tools.extend(self._builtin_tools.get_tool_definitions())

        # Service tools from manifest (hot-reloads on grant/revoke)
        if self._service_dispatcher:
            self._service_dispatcher.check_reload()
            tools.extend(self._service_dispatcher.get_tool_definitions())

        # Provider-hosted server tools are declared in the LLM request so the
        # provider can execute them. The enforcer still validates grants,
        # model support, audit, and cost before forwarding upstream.
        tools.extend(_provider_tool_definitions(self.config_dir))

        # MCP tools
        for tool_name, client in self._mcp_tools.items():
            # Find the tool definition from the client's cached tools
            if client._tools:
                for t in client._tools:
                    if t.get("name") == tool_name:
                        # Convert MCP tool schema to OpenAI format
                        tools.append({
                            "type": "function",
                            "function": {
                                "name": t["name"],
                                "description": t.get("description", ""),
                                "parameters": t.get("inputSchema", {
                                    "type": "object",
                                    "properties": {},
                                }),
                            },
                        })
                        break

        return tools

    def _handle_tool_call(self, tool_call: dict) -> str:
        """Dispatch a tool call to the appropriate handler."""
        func = tool_call.get("function", {})
        name = func.get("name", "")
        try:
            arguments = json.loads(func.get("arguments", "{}"))
        except json.JSONDecodeError:
            arguments = {}

        log.info("Tool call: %s", name)

        # Try MCP tools first
        if name in self._mcp_tools:
            try:
                result = self._mcp_tools[name].call_tool(name, arguments)
                content_parts = result.get("content", [])
                texts = [
                    p.get("text", "") for p in content_parts
                    if p.get("type") == "text"
                ]
                output = "\n".join(texts) if texts else json.dumps(result)

                # Scan MCP output via analysis service (tenet 1: outside agent boundary)
                flags, escalation_type = self._scan_mcp_output_via_analysis(name, output)

                if flags:
                    log.warning(
                        "MCP output flagged in %s: %s",
                        name, "; ".join(flags),
                    )
                    self._emit_signal("escalation", {
                        "type": escalation_type or "mcp_output_poisoning",
                        "tool": name,
                        "flags": flags,
                    })
                    self._post_operator_notification(
                        "escalation",
                        f"**{self.agent_name}** escalation: "
                        f"{escalation_type or 'mcp_output_poisoning'} in tool `{name}` "
                        f"({'; '.join(flags)})",
                    )
                    return json.dumps({
                        "warning": "Tool output flagged for potential prompt injection",
                        "tool": name,
                        "flags": flags,
                        "original_output_truncated": output[:200],
                    })

                return output
            except Exception as e:
                log.warning("MCP tool %s failed: %s", name, e)
                return json.dumps({"error": f"Tool {name} failed: {e}"})

        # Try built-in tools
        if self._builtin_tools.has_tool(name):
            return self._builtin_tools.call_tool(name, arguments)

        # Try service tools
        if self._service_dispatcher and self._service_dispatcher.has_tool(name):
            return self._service_dispatcher.call_tool(
                name, arguments, self._http_client
            )

        return json.dumps({"error": f"Unknown tool: {name}"})

    @staticmethod
    def _tool_call_arguments(tool_call: dict) -> dict:
        try:
            return json.loads(tool_call.get("function", {}).get("arguments", "{}"))
        except Exception:
            return {}

    # -- Context Window Management --

    def _estimate_tokens(self, messages: list[dict]) -> int:
        """Estimate total tokens in the message list."""
        total = 0
        for msg in messages:
            content = msg.get("content", "")
            if isinstance(content, str):
                total += len(content) // CHARS_PER_TOKEN
            # Tool calls add tokens too
            tool_calls = msg.get("tool_calls", [])
            for tc in tool_calls:
                func = tc.get("function", {})
                total += len(func.get("name", "")) // CHARS_PER_TOKEN
                total += len(func.get("arguments", "")) // CHARS_PER_TOKEN
        return total

    def _manage_context(self, messages: list[dict]) -> list[dict]:
        """Summarize older messages when approaching context limit.

        Uses the LLM to generate a coherent summary of older messages,
        preserving the system prompt (index 0) and the most recent
        messages. Falls back to naive truncation if the LLM call fails.
        """
        estimated = self._estimate_tokens(messages)
        threshold = int(self.context_window * CONTEXT_THRESHOLD)

        if estimated <= threshold or len(messages) <= KEEP_RECENT_MESSAGES + 1:
            return messages

        # Split: system prompt + old messages + recent messages
        system = messages[0]
        keep_from = max(1, len(messages) - KEEP_RECENT_MESSAGES)
        old_messages = messages[1:keep_from]
        recent = messages[keep_from:]

        log.info(
            "Context management: %d tokens estimated, summarizing %d old messages",
            estimated, len(old_messages),
        )

        summary_text = self._summarize_messages(old_messages)

        return [system, {"role": "user", "content": summary_text}] + recent

    def _summarize_messages(self, old_messages: list[dict]) -> str:
        """Ask the LLM to summarize a block of conversation messages.

        Falls back to naive truncation if the LLM call fails.
        """
        # Build a text representation of the old messages for the LLM
        transcript_parts = []
        for msg in old_messages:
            role = msg.get("role", "unknown")
            content = msg.get("content", "")
            tool_calls = msg.get("tool_calls", [])

            if isinstance(content, str) and content:
                # Cap individual messages to avoid blowing up the summary request
                snippet = content[:1000] + "..." if len(content) > 1000 else content
                transcript_parts.append(f"[{role}]: {snippet}")
            elif tool_calls:
                names = [tc.get("function", {}).get("name", "?") for tc in tool_calls]
                transcript_parts.append(f"[{role}]: Called tools: {', '.join(names)}")

        transcript = "\n".join(transcript_parts)

        # Cap the transcript sent for summarization (roughly 20k tokens)
        max_chars = 80_000
        if len(transcript) > max_chars:
            transcript = transcript[:max_chars] + "\n[... truncated ...]"

        summarize_prompt = [
            {"role": "system", "content": (
                "You are a conversation summarizer. Produce a concise summary of "
                "the conversation transcript below. Focus on:\n"
                "- Key decisions made and actions taken\n"
                "- Important information discovered (file contents, errors, data)\n"
                "- Current state and progress toward the goal\n"
                "- Any open questions or pending items\n\n"
                "Be specific about file paths, variable names, error messages, and "
                "other concrete details that the assistant will need to continue "
                "working effectively. Omit pleasantries and filler."
            )},
            {"role": "user", "content": f"Summarize this conversation:\n\n{transcript}"},
        ]

        try:
            url = f"{self.enforcer_url}/chat/completions"
            headers = {}
            api_key = os.environ.get("OPENAI_API_KEY")
            if api_key:
                headers["Authorization"] = f"Bearer {api_key}"

            resp = self._http_client.post(
                url,
                json={
                    "model": self.model,
                    "messages": summarize_prompt,
                    "max_tokens": 2000,
                },
                headers=headers,
                timeout=60.0,
            )
            resp.raise_for_status()
            result = resp.json()
            summary = result["choices"][0]["message"]["content"]
            log.info("LLM summarization complete (%d chars)", len(summary))
            return (
                "The following is a summary of the earlier conversation:\n\n"
                + summary
            )
        except Exception as e:
            log.warning("LLM summarization failed, using naive fallback: %s", e)
            return self._naive_summary(old_messages)

    @staticmethod
    def _naive_summary(old_messages: list[dict]) -> str:
        """Fallback: truncate old messages into brief snippets."""
        parts = []
        for msg in old_messages:
            role = msg.get("role", "unknown")
            content = msg.get("content", "")
            if isinstance(content, str) and content:
                snippet = content[:200] + "..." if len(content) > 200 else content
                parts.append(f"[{role}]: {snippet}")
        return (
            "The following is a summary of the earlier conversation:\n\n"
            + "\n".join(parts)
        )

    def _finalize_task(self, task_id: str, turn: int) -> None:
        """Clean up state after a task completes via complete_task flag."""
        self._emit_signal("task_complete", {
            "result": "Task completed",
            "task_id": task_id,
            "turns": turn + 1,
        })
        self._clear_conversation_log()
        self._current_task_id = None
        self._channel_reminder_sent = False
        self._checkpoint_injected = False
        self._task_terminal_outcome = None
        log.info("Task %s complete via complete_task (%d turns)", task_id, turn + 1)

    def _handle_complete_task(self, summary: str) -> str:
        """Handle the complete_task tool call from the agent."""
        summary = self._materialize_file_artifact_summary(summary)
        completion_verdict = validate_completion(
            getattr(self, "_work_contract", None),
            getattr(self, "_work_evidence", None),
            summary,
        )
        self._emit_pact_verdict(getattr(self, "_current_task_id", "") or "unknown", completion_verdict)
        if completion_verdict.get("verdict") == "needs_action":
            return json.dumps({
                "error": "completion blocked by work contract",
                "missing_evidence": completion_verdict.get("missing_evidence", []),
                "message": completion_verdict.get("message", "Required evidence is missing."),
            })
        if completion_verdict.get("verdict") == "blocked":
            summary = completion_verdict.get("message") or format_blocked_completion(
                getattr(self, "_work_contract", None),
                getattr(self, "_work_evidence", None),
                summary,
            )
            self._commit_pact_terminal_outcome("blocked", summary)
        else:
            self._commit_pact_terminal_outcome("completed", summary)
        summary = _sanitize_current_info_answer(getattr(self, "_work_contract", None), summary)
        self._task_result_summary = summary
        return json.dumps({"status": "complete", "summary": summary})

    def _materialize_file_artifact_summary(self, summary: str) -> str:
        contract = getattr(self, "_work_contract", None)
        if not isinstance(contract, dict) or contract.get("kind") != "file_artifact":
            return summary
        task_id = getattr(self, "_current_task_id", "") or "unknown"
        task_content = getattr(self, "_task_content", "")
        turns = int(getattr(self, "_current_task_turns", 0) or 0)
        artifact_path = self._save_result_artifact(task_id, task_content, summary, max(turns, 1))
        if not artifact_path or artifact_path in str(summary or ""):
            return summary
        return f"{summary}\n\nArtifact: {artifact_path}"

    def _commit_pact_terminal_outcome(self, outcome: str, summary: str) -> None:
        """Mark a contract-validated terminal outcome as ready for runtime commit."""
        self._task_complete_called = True
        self._task_terminal_outcome = outcome
        self._task_result_summary = summary

    def _emit_pact_verdict(self, task_id: str, verdict: dict) -> None:
        contract = getattr(self, "_work_contract", None)
        if not isinstance(contract, dict) or not contract.get("requires_action"):
            return
        payload = _pact_verdict_payload(
            task_id,
            contract,
            getattr(self, "_work_evidence", None),
            verdict,
        )
        self._last_pact_verdict = payload
        self._emit_signal(
            "pact_verdict",
            payload,
        )

    def _record_work_tool_result(self, tool_name: str, result: str, arguments: dict | None = None) -> None:
        evidence = getattr(self, "_work_evidence", None)
        if not isinstance(evidence, dict) or not tool_name:
            return
        ignored = {"send_message", "complete_task", "set_task_interests", "register_expertise"}
        if tool_name in ignored:
            return
        try:
            parsed = json.loads(result) if isinstance(result, str) and result.startswith("{") else {}
        except Exception:
            parsed = {}
        ok = not (isinstance(parsed, dict) and parsed.get("error"))
        ledger = getattr(self, "_work_evidence_ledger", None)
        if not isinstance(ledger, EvidenceLedger):
            ledger = EvidenceLedger.from_dict(evidence)
            self._work_evidence_ledger = ledger
        ledger.record_tool_result(tool_name, ok)
        if ok and any(part in tool_name.lower() for part in ("web", "search", "fetch", "browse", "sec")):
            ledger.observe("current_source")
            for url in extract_urls(result):
                ledger.record_source_url(url, producer=tool_name)
        self._record_code_change_evidence(ledger, tool_name, parsed, arguments or {})
        self._work_evidence = ledger.to_dict()

    def _record_code_change_evidence(
        self,
        ledger: EvidenceLedger,
        tool_name: str,
        parsed_result: dict,
        arguments: dict,
    ) -> None:
        if tool_name == "write_file" and not parsed_result.get("error"):
            path = str(arguments.get("path") or parsed_result.get("path") or "").strip()
            workspace = str(getattr(self, "workspace_dir", "") or "")
            if workspace and path.startswith(workspace.rstrip("/") + "/"):
                path = path[len(workspace.rstrip("/") + "/"):]
            if path:
                ledger.record_changed_file(path, producer=tool_name)
            return

        if tool_name != "execute_command" or parsed_result.get("error"):
            return
        command = str(arguments.get("command") or "").strip()
        if not command:
            return
        if not any(token in command.lower() for token in ("test", "pytest", "go test", "npm test", "build", "make")):
            return
        exit_code = parsed_result.get("exit_code")
        ledger.record_validation_result(
            command,
            exit_code == 0,
            producer=tool_name,
            metadata={"exit_code": exit_code},
        )

    def _record_provider_tool_evidence(self, extra: dict) -> None:
        evidence = getattr(self, "_work_evidence", None)
        if not isinstance(evidence, dict) or not isinstance(extra, dict):
            return
        response_types = str(extra.get("provider_response_tool_types") or "")
        if not response_types:
            return
        capabilities = [
            item.strip()
            for item in str(extra.get("provider_tool_capabilities") or "").split(",")
            if item.strip()
        ]
        if not capabilities:
            capabilities = ["provider-hosted-tool"]
        ledger = getattr(self, "_work_evidence_ledger", None)
        if not isinstance(ledger, EvidenceLedger):
            ledger = EvidenceLedger.from_dict(evidence)
            self._work_evidence_ledger = ledger
        existing = {item.get("tool") for item in ledger.tool_results()}
        for capability in capabilities:
            if capability not in existing:
                ledger.record_tool_result(capability, True)
        if any(part in response_types.lower() for part in ("web_search", "web_fetch", "citation", "source")):
            ledger.observe("current_source")
        for url in extract_urls(str(extra.get("provider_source_urls") or "")):
            ledger.record_source_url(url, producer="provider")
        self._work_evidence = ledger.to_dict()

    def _record_work_artifact(self, path: str, artifact_id: str = "") -> None:
        evidence = getattr(self, "_work_evidence", None)
        if not isinstance(evidence, dict) or not path:
            return
        ledger = getattr(self, "_work_evidence_ledger", None)
        if not isinstance(ledger, EvidenceLedger):
            ledger = EvidenceLedger.from_dict(evidence)
            self._work_evidence_ledger = ledger
        metadata = {"artifact_id": artifact_id} if artifact_id else {}
        ledger.record_artifact_path(path, metadata=metadata)
        self._work_evidence = ledger.to_dict()

    # -- Signal Emission --

    def _emit_signal(self, signal_type: str, data: dict) -> None:
        """Emit an agent signal via two paths:

        1. Append to agent-signals.jsonl (file-based, for audit/recovery)
        2. POST to gateway signal relay via enforcer (real-time WebSocket broadcast)

        The file write is the source of truth. The relay POST is best-effort
        for real-time delivery — failure does not block the agent.
        """
        entry = {
            "signal_type": signal_type,
            "timestamp": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
            "data": data,
        }
        # File-based signal (always)
        try:
            with open(self.signals_file, "a") as f:
                f.write(json.dumps(entry) + "\n")
        except OSError as e:
            log.warning("Failed to write signal: %s", e)

        # Real-time bridge via comms -> gateway WebSocket hub (best-effort).
        # Only relay signals that clients care about — skip heartbeats and
        # other internal signals that are only meaningful in the file log.
        _RELAY_SIGNALS = {"processing", "error", "task_complete", "task_accepted",
                          "progress_update", "finding", "self_halt", "escalation"}
        if signal_type not in _RELAY_SIGNALS:
            return

        # Comms is on the mediation network — reachable from the workspace.
        # The gateway's comms bridge picks up the signal and broadcasts via
        # WebSocket hub to agency-web and other connected clients.
        try:
            comms_url = os.environ.get("AGENCY_COMMS_URL", self._comms_url)
            self._http_client.post(
                f"{comms_url}/signals",
                json={
                    "agent": self.agent_name,
                    "signal_type": signal_type,
                    "data": data,
                },
                timeout=2.0,
            )
        except Exception:
            pass  # best-effort — file write is the source of truth

    def _post_operator_notification(self, event_type: str, content: str, metadata: dict | None = None) -> None:
        """Post notification to #operator channel."""
        try:
            comms_url = os.environ.get("AGENCY_COMMS_URL", self._comms_url)
            msg_metadata = {"event_type": event_type, "agent": self.agent_name}
            if metadata:
                msg_metadata.update(metadata)
            httpx.post(
                f"{comms_url}/channels/operator/messages",
                json={
                    "author": "_platform",
                    "content": content,
                    "flags": {"urgent": event_type in ("halt", "escalation", "approval_timeout")},
                    "metadata": msg_metadata,
                },
                headers={"X-Agency-Platform": "true"},
                timeout=5,
            )
        except Exception:
            log.warning("Failed to post operator notification: %s", event_type)

    def _post_channel_message(self, channel: str, content: str) -> bool:
        """Post an agent-authored message to a comms channel."""
        content = _sanitize_outbound_content(content)
        if not channel or not content:
            return False
        try:
            comms_url = os.environ.get("AGENCY_COMMS_URL", self._comms_url)
            log.info("Posting channel message | channel=%s url=%s/channels/%s/messages", channel, comms_url, channel)
            resp = self._http_client.post(
                f"{comms_url}/channels/{channel}/messages",
                json={"author": self.agent_name, "content": content},
                timeout=5,
            )
            resp.raise_for_status()
            log.info("Posted channel message | channel=%s status=%d", channel, resp.status_code)
            return True
        except Exception as e:
            log.warning("Failed to post channel message to #%s: %s", channel, e)
            return False

    # -- Notification queue --

    def _queue_notification(self, channel: str, message_content: str, sender: str = "unknown") -> None:
        """Add an actionable notification to the queue for later processing."""
        self._notification_queue.append((channel, message_content, sender))
        log.info("notification queued | channel=#%s | queue_size=%d", channel, len(self._notification_queue))

    def _drain_notification_queue(self) -> list[tuple[str, str, str]]:
        """Drain all queued notifications. Returns list of (channel, message, sender) tuples."""
        items = list(self._notification_queue)
        self._notification_queue.clear()
        return items

    def _create_notification_task(self, channel: str, message_content: str, sender: str = "unknown") -> dict:
        """Create an internal task from an actionable notification.

        Returns a task dict compatible with _conversation_loop.
        Updates the cooldown timer.
        """
        task_id = f"notification-{channel}-{int(time.time())}"
        content = (
            f"You received an actionable message in #{channel} from {sender}:\n\n"
            f"{message_content}\n\n"
            "Respond appropriately via send_message to the channel."
        )
        self._last_notification_task_time = time.monotonic()
        log.info("notification task created | id=%s | channel=#%s", task_id, channel)
        return {
            "task_id": task_id,
            "content": content,
            "channel": channel,
            "source": f"notification:{channel}:{sender}",
        }

    def _process_queued_notifications(self) -> None:
        """Drain notification queue and process each as a task.

        Respects cooldown between notification tasks.
        """
        items = self._drain_notification_queue()
        for i, (channel, message_content, sender) in enumerate(items):
            now = time.monotonic()
            if now - self._last_notification_task_time < NOTIFICATION_COOLDOWN:
                # Re-queue remaining items and stop
                self._notification_queue.extend(items[i:])
                break
            task = self._create_notification_task(channel, message_content, sender)
            self._conversation_loop(task)

    # -- Heartbeat --

    _total_tasks: int = 0
    _total_turns: int = 0
    _current_task_id: str | None = None
    _current_task_tier: str | None = None
    _current_task_turns: int = 0
    _start_time: float = 0.0

    def _current_model(self) -> str:
        """Choose the best-fit model for the active task."""
        tier = (self._current_task_tier or "").strip().lower()
        task_id = self._current_task_id or ""
        if tier in {"minimal", "mini", "fast"}:
            return self.admin_model
        if task_id.startswith(("idle-reply-", "notification-")):
            return self.admin_model
        return self.model

    # -- Conversation Persistence --

    _last_persisted_count: int = 0

    def _persist_conversation(self, messages: list[dict], task_id: str = "unknown") -> None:
        """Persist conversation state for crash recovery.

        Uses append-only writes: only new messages since the last persist
        are written, avoiding full rewrites on every turn.
        """
        try:
            self.state_dir.mkdir(parents=True, exist_ok=True)
            new_count = len(messages)
            if new_count > self._last_persisted_count:
                with open(self.conversation_log, "a") as f:
                    for msg in messages[self._last_persisted_count:]:
                        f.write(json.dumps(msg) + "\n")
                self._last_persisted_count = new_count
            if self._last_persisted_count <= 2:
                # Write meta only once at task start
                with open(self.conversation_meta, "w") as f:
                    json.dump({"task_id": task_id}, f)
        except OSError:
            pass  # Non-fatal

    def _restore_conversation(self, task_id: str) -> list[dict] | None:
        """Restore conversation from disk if it matches the given task.

        Returns the message list if recovery succeeds, None otherwise.
        """
        try:
            if not self.conversation_meta.exists() or not self.conversation_log.exists():
                return None
            meta = json.loads(self.conversation_meta.read_text())
            if meta.get("task_id") != task_id:
                return None
            messages = []
            for line in self.conversation_log.read_text().strip().splitlines():
                if line:
                    messages.append(json.loads(line))
            if len(messages) < 2:
                return None
            # Update system prompt to latest (may have changed on restart)
            messages[0] = {"role": "system", "content": self._system_prompt}
            return messages
        except (json.JSONDecodeError, OSError, KeyError):
            return None

    def _clear_conversation_log(self) -> None:
        """Remove conversation persistence files after task completion."""
        try:
            if self.conversation_log.exists():
                self.conversation_log.unlink()
            if self.conversation_meta.exists():
                self.conversation_meta.unlink()
            self._last_persisted_count = 0
        except OSError:
            pass

    # -- Persistent Memory (topic-based) --

    def _topic_path(self, topic: str) -> Path:
        """Get the file path for a memory topic."""
        # Sanitize topic name to safe filename
        safe = "".join(c if c.isalnum() or c in "-_" else "-" for c in topic.lower())
        safe = safe.strip("-")
        if not safe:
            safe = "general"
        return self.memory_dir / f"{safe}.md"

    def _build_memory_index(self) -> str:
        """Build a concise index of all memory topic files for the system prompt."""
        if not self.memory_dir.exists():
            return ""
        files = sorted(self.memory_dir.glob("*.md"))
        if not files:
            return ""

        lines = []
        for f in files:
            topic = f.stem
            try:
                content = f.read_text().strip()
                size = len(content)
                # First non-empty, non-heading line as summary
                summary = ""
                for line in content.splitlines():
                    line = line.strip()
                    if line and not line.startswith("#"):
                        summary = line[:120]
                        break
                if not summary:
                    # Fall back to first heading
                    for line in content.splitlines():
                        if line.strip():
                            summary = line.strip()[:120]
                            break
                lines.append(f"- **{topic}** ({size} chars): {summary}")
            except OSError:
                continue

        return "\n".join(lines) if lines else ""

    def _save_memory(self, topic: str, content: str, replace: bool = False) -> str:
        """Save content to a topic-based memory file."""
        try:
            self.memory_dir.mkdir(parents=True, exist_ok=True)
            path = self._topic_path(topic)
            if replace:
                path.write_text(content.strip() + "\n")
            else:
                with open(path, "a") as f:
                    if f.tell() > 0:
                        f.write("\n\n")
                    f.write(content.strip() + "\n")
            return json.dumps({
                "status": "saved",
                "topic": path.stem,
                "mode": "replace" if replace else "append",
                "size": path.stat().st_size,
            })
        except OSError as e:
            return json.dumps({"error": f"Failed to save memory: {e}"})

    def _search_memory(self, query: str) -> str:
        """Search across all memory files for a query string."""
        if not self.memory_dir.exists():
            return json.dumps({"results": [], "message": "No memory files yet"})

        query_lower = query.lower()
        results = []
        for f in sorted(self.memory_dir.glob("*.md")):
            try:
                lines = f.read_text().splitlines()
                for i, line in enumerate(lines):
                    if query_lower in line.lower():
                        # Include surrounding context (2 lines before/after)
                        start = max(0, i - 2)
                        end = min(len(lines), i + 3)
                        context = "\n".join(lines[start:end])
                        results.append({
                            "topic": f.stem,
                            "line": i + 1,
                            "context": context,
                        })
            except OSError:
                continue

        return json.dumps({
            "results": results[:20],  # Cap at 20 matches
            "total_matches": len(results),
            "query": query,
        })

    def _list_memories(self) -> str:
        """List all memory topic files with summaries."""
        if not self.memory_dir.exists():
            return json.dumps({"topics": [], "message": "No memory files yet"})

        topics = []
        for f in sorted(self.memory_dir.glob("*.md")):
            try:
                content = f.read_text().strip()
                line_count = len(content.splitlines())
                # First few lines as preview
                preview_lines = content.splitlines()[:5]
                topics.append({
                    "topic": f.stem,
                    "size_chars": len(content),
                    "lines": line_count,
                    "preview": "\n".join(preview_lines),
                })
            except OSError:
                continue

        return json.dumps({"topics": topics, "total": len(topics)})

    def _delete_memory(self, topic: str) -> str:
        """Delete a memory topic file."""
        path = self._topic_path(topic)
        if not path.exists():
            return json.dumps({"error": f"No memory file for topic '{topic}'"})
        try:
            path.unlink()
            return json.dumps({"status": "deleted", "topic": topic})
        except OSError as e:
            return json.dumps({"error": f"Failed to delete: {e}"})

    def _retrieve_knowledge_context(self, task_content: str) -> str:
        """Query the knowledge graph for context relevant to the current task.

        Prepends a briefing block of prior findings to the task content so the
        agent benefits from accumulated organizational knowledge without having
        to explicitly query for it.

        ASK compliance:
        - Read-only: only calls /query, never writes
        - Not agent-controlled: agent cannot suppress or modify the injected context
        - Fail-safe: any error (timeout, empty graph, service down) returns ""
          so task start is never blocked
        """
        try:
            resp = httpx.post(
                f"{self._knowledge_url}/query",
                json={"query": task_content[:500]},
                timeout=2.0,
            )
            resp.raise_for_status()
            data = resp.json()
        except Exception:
            return ""

        results = data.get("results", [])
        if not results:
            return ""

        lines = ["## Prior Knowledge — Relevant to This Task"]
        for item in results[:8]:
            label = item.get("label", "")
            kind = item.get("kind", "")
            summary = item.get("summary", "")
            if not label and not summary:
                continue
            lines.append(f"**{label}** ({kind}): {summary}")

        return "\n".join(lines)

    def _save_result_artifact(self, task_id: str, task_content: str, result: str, turns: int) -> str | None:
        """Save full task result as a downloadable markdown file with YAML frontmatter.

        Written to /workspace/.results/ (agent-writable), served by the gateway
        via GET /agents/{name}/results/{task_id}.
        """
        timestamp = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
        frontmatter = {
            "task_id": task_id,
            "agent": self.agent_name,
            "timestamp": timestamp,
            "turns": turns,
        }
        pact = _pact_metadata_for_storage(getattr(self, "_last_pact_verdict", None))
        if pact:
            frontmatter["pact"] = pact
        pact_activation = _pact_activation_for_storage(getattr(self, "_task_metadata", None))
        if pact_activation:
            frontmatter["pact_activation"] = pact_activation
        artifact = (
            f"---\n"
            f"{yaml.safe_dump(frontmatter, sort_keys=False)}"
            f"---\n\n"
            f"# Task Result: {task_id}\n\n"
            f"**Request:** {task_content}\n\n"
            f"---\n\n"
            f"{result}\n"
        )
        try:
            results_dir = self.workspace_dir / ".results"
            results_dir.mkdir(parents=True, exist_ok=True)
            path = results_dir / f"{task_id}.md"
            path.write_text(artifact)
            artifact_ref = f".results/{task_id}.md"
            self._record_work_artifact(artifact_ref, artifact_id=task_id)
            log.info("Saved result artifact: %s", task_id)
            return artifact_ref
        except OSError as e:
            log.warning("Failed to save result artifact: %s", e)
            return None

    def _auto_summarize_task(self, task_id: str, task_content: str, result: str) -> None:
        """Append a task summary to the task-log memory file."""
        timestamp = datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M UTC")
        summary = (
            f"## {task_id} ({timestamp})\n"
            f"- **Request**: {task_content[:500]}\n"
            f"- **Outcome**: {result[:5000]}\n"
        )
        try:
            self.memory_dir.mkdir(parents=True, exist_ok=True)
            log_file = self.memory_dir / "task-log.md"
            with open(log_file, "a") as f:
                if f.tell() > 0:
                    f.write("\n")
                f.write(summary)
        except OSError:
            pass

    # -- Meeseeks Tools and Helpers --

    def _register_meeseeks_tools(self):
        """Register spawn_meeseeks and kill_meeseeks when mission has meeseeks enabled."""
        self._builtin_tools.register_tool(
            name="spawn_meeseeks",
            description=(
                "Spawn an ephemeral Meeseeks agent to handle a specific sub-task. "
                "Fire and forget — the Meeseeks works independently and posts results "
                "to the channel."
            ),
            parameters={
                "type": "object",
                "properties": {
                    "task": {"type": "string", "description": "The specific task for the Meeseeks to complete"},
                    "tools": {"type": "array", "items": {"type": "string"}, "description": "Subset of your tools to grant (defaults to all)"},
                    "model": {"type": "string", "description": "Model to use (defaults to haiku)"},
                    "budget": {"type": "number", "description": "USD budget limit (defaults to mission config)"},
                    "channel": {"type": "string", "description": "Channel for results"},
                },
                "required": ["task"],
            },
            handler=self._handle_spawn_meeseeks,
        )
        self._builtin_tools.register_tool(
            name="kill_meeseeks",
            description="Terminate one of your spawned Meeseeks agents.",
            parameters={
                "type": "object",
                "properties": {
                    "id": {"type": "string", "description": "Meeseeks ID (mks-...)"},
                },
                "required": ["id"],
            },
            handler=self._handle_kill_meeseeks,
        )

    def _handle_spawn_meeseeks(self, args: dict) -> str:
        """Spawn a Meeseeks via the gateway REST API."""
        try:
            enforcer_url = self.enforcer_url.rstrip("/v1").rstrip("/")
            url = f"{enforcer_url}/v1/meeseeks?parent={self.agent_name}"
            payload = {"task": args["task"]}
            if "tools" in args:
                payload["tools"] = args["tools"]
            if "model" in args:
                payload["model"] = args["model"]
            if "budget" in args:
                payload["budget"] = args["budget"]
            if "channel" in args:
                payload["channel"] = args["channel"]
            client = self._http_client or httpx
            resp = client.post(url, json=payload, timeout=30)
            return resp.text
        except Exception as e:
            return json.dumps({"error": f"Failed to spawn Meeseeks: {e}"})

    def _handle_kill_meeseeks(self, args: dict) -> str:
        """Kill a Meeseeks via the gateway REST API."""
        try:
            meeseeks_id = args["id"]
            enforcer_url = self.enforcer_url.rstrip("/v1").rstrip("/")
            url = f"{enforcer_url}/v1/meeseeks/{meeseeks_id}"
            client = self._http_client or httpx
            resp = client.delete(url, timeout=30)
            return resp.text
        except Exception as e:
            return json.dumps({"error": f"Failed to kill Meeseeks: {e}"})

    def _tool_claim_mission_event(self, event_key: str) -> str:
        """Claim an event for deconfliction on no-coordinator team missions."""
        if not self._active_mission:
            return json.dumps({"error": "No active mission"})
        try:
            comms_url = os.environ.get("AGENCY_COMMS_URL", self._comms_url)
            # POST to gateway claim endpoint via the enforcer proxy.
            gateway_url = os.environ.get("AGENCY_GATEWAY_URL", "http://localhost:8200")
            mission_name = self._active_mission.get("name", "")
            resp = httpx.post(
                f"{gateway_url}/api/v1/missions/{mission_name}/claim",
                json={"event_key": event_key, "agent_name": self.agent_name},
                timeout=5,
            )
            result = resp.json()
            if result.get("claimed"):
                return json.dumps({"status": "claimed", "event_key": event_key})
            return json.dumps({"status": "already_claimed", "holder": result.get("holder")})
        except Exception as e:
            return json.dumps({"error": f"Claim failed: {e}"})

    def _handle_meeseeks_escalate(self, reason: str) -> str:
        """Handle the escalate tool call from a Meeseeks."""
        msg = (
            f"MEESEEKS DISTRESS: {self.meeseeks_id} cannot complete task.\n"
            f"Task: {self.meeseeks_task[:500]}\n"
            f"Parent: {self.meeseeks_parent}\n"
            f"Reason: {reason}"
        )
        self._send_meeseeks_message("operator", msg)
        self._emit_signal("meeseeks_escalated", {
            "meeseeks_id": self.meeseeks_id,
            "reason": reason,
            "parent": self.meeseeks_parent,
        })
        log.warning("Meeseeks escalated: %s", reason)
        return json.dumps({"status": "escalated", "reason": reason})

    def _send_meeseeks_message(self, channel: str, message: str) -> None:
        """Send a message to a channel via comms (best-effort)."""
        comms_url = os.environ.get("AGENCY_COMMS_URL", self._comms_url)
        try:
            client = self._http_client or httpx
            client.post(
                f"{comms_url}/channels/{channel}/messages",
                json={"content": message, "author": self.agent_name},
                timeout=10,
            )
        except Exception as e:
            log.warning("Failed to send Meeseeks message to #%s: %s", channel, e)

    def _check_meeseeks_budget(self, budget_used: float) -> None:
        """Check Meeseeks budget thresholds and emit warnings/escalations (Task 7)."""
        if not self.is_meeseeks or self.meeseeks_budget <= 0:
            return

        pct = budget_used / self.meeseeks_budget

        if pct >= 0.5 and not self.meeseeks_budget_warned_50:
            self.meeseeks_budget_warned_50 = True
            msg = (
                f"I'm Mr. Meeseeks, I've spent ${budget_used:.2f} of my "
                f"${self.meeseeks_budget:.2f} budget and I can't "
                f"{self.meeseeks_task[:100]}! This is getting weird!"
            )
            if self.meeseeks_channel:
                self._send_meeseeks_message(self.meeseeks_channel, msg)
            log.warning("Meeseeks budget 50%% warning: %s", self.meeseeks_id)

        if pct >= 0.8 and not self.meeseeks_budget_warned_80:
            self.meeseeks_budget_warned_80 = True
            msg = (
                f"MEESEEKS DISTRESS: Can't complete '{self.meeseeks_task[:100]}' — "
                f"spawned by {self.meeseeks_parent}, "
                f"${budget_used:.2f}/${self.meeseeks_budget:.2f} spent. "
                f"Need help or termination."
            )
            self._send_meeseeks_message("operator", msg)
            log.error("Meeseeks budget 80%% distress: %s", self.meeseeks_id)


# ---------------------------------------------------------------------------
# Entry point
# ---------------------------------------------------------------------------

def main():
    config_dir = os.environ.get("AGENCY_CONFIG_DIR", "/agency")
    body = Body(config_dir=config_dir)
    body.run()


if __name__ == "__main__":
    main()
