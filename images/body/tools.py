"""Tool registries and dispatchers for the body runtime.

Contains:
- ServiceToolDispatcher: HTTP service tools through the enforcer proxy
- BuiltinToolRegistry: Workspace operations (read, write, list, execute, search)
- SkillsManager: Agent skill activation (agentskills.io)
"""

import json
import logging
import os
import subprocess
from pathlib import Path

import httpx
from typing import Optional, Union

log = logging.getLogger("body")


# ---------------------------------------------------------------------------
# Service Tool Dispatcher (HTTP through enforcer)
# ---------------------------------------------------------------------------

class ServiceToolDispatcher:
    """Translates services-manifest.json into tool definitions and
    dispatches HTTP calls through the enforcer proxy."""

    def __init__(self, manifest_path: Union[str, Path]):
        self.manifest_path = Path(manifest_path)
        self._manifest: Optional[dict] = None
        self._tools: list[dict] = []
        self._last_mtime: float = 0.0

    def load(self) -> None:
        """Load and parse the services manifest."""
        if not self.manifest_path.exists():
            self._manifest = {"services": []}
            self._last_mtime = 0.0
            return
        self._last_mtime = self.manifest_path.stat().st_mtime
        content = self.manifest_path.read_text()
        self._manifest = json.loads(content)
        self._build_tools()

    def load_from_url(self, url: str) -> None:
        """Load manifest from the enforcer config endpoint, falling back to file."""
        try:
            resp = httpx.Client(timeout=5).get(url)
            if resp.status_code == 200:
                self._manifest = json.loads(resp.text)
                self._build_tools()
                return
        except Exception as e:
            log.warning("Manifest fetch failed: %s", e)
        # Fallback to file
        self.load()

    def check_reload(self) -> bool:
        """Reload manifest if the file changed. Returns True if reloaded."""
        try:
            if not self.manifest_path.exists():
                if self._manifest and self._manifest.get("services"):
                    self._manifest = {"services": []}
                    self._tools = []
                    return True
                return False
            mtime = self.manifest_path.stat().st_mtime
            if mtime != self._last_mtime:
                log.info("Services manifest changed, reloading")
                self.load()
                return True
        except OSError:
            pass
        return False

    def _build_tools(self) -> None:
        """Build tool definitions from the manifest."""
        self._tools = []
        for service in self._manifest.get("services", []):
            for tool in service.get("tools", []):
                # Build parameters schema from tool definition
                properties = {}
                required = []
                passthrough = bool(tool.get("passthrough"))
                for param in tool.get("parameters", []):
                    properties[param["name"]] = {
                        "type": param.get("type", "string"),
                        "description": param.get("description", ""),
                    }
                    if param.get("required", True):
                        required.append(param["name"])

                # Expand ${VAR} env var references in api_base and endpoint paths.
                # Service definitions use ${LC_ORG_ID} etc. which must be resolved
                # from the container environment at load time.
                api_base = os.path.expandvars(service["api_base"])
                endpoint = os.path.expandvars(tool.get("path", tool.get("endpoint", "")))

                self._tools.append({
                    "type": "function",
                    "function": {
                        "name": tool["name"],
                        "description": tool.get("description", ""),
                        "parameters": {
                            "type": "object",
                            "properties": properties,
                            "required": required,
                            "additionalProperties": passthrough,
                        },
                    },
                    "_service": service["service"],
                    "_api_base": api_base,
                    "_scoped_token": service["scoped_token"],
                    "_tool_name": tool["name"],
                    "_endpoint": endpoint,
                    "_method": tool.get("method", "GET"),
                    "_query_params": tool.get("query_params", {}),
                })

    def get_tool_definitions(self) -> list[dict]:
        """Return tools in OpenAI function calling format."""
        return [
            {"type": t["type"], "function": t["function"]}
            for t in self._tools
        ]

    def has_tool(self, name: str) -> bool:
        """Check if a tool name belongs to this dispatcher."""
        return any(t["function"]["name"] == name for t in self._tools)

    def call_tool(self, name: str, arguments: dict, http_client: httpx.Client) -> str:
        """Execute an HTTP service tool call through the enforcer.

        Service requests use http:// scheme so httpx sends them as regular
        proxy requests (absolute-form) instead of CONNECT tunneling.  The
        enforcer's http_proxy_handler does the credential swap and upgrades
        back to https:// before forwarding through the egress proxy.
        """
        tool = None
        for t in self._tools:
            if t["function"]["name"] == name:
                tool = t
                break
        if tool is None:
            return json.dumps({"error": f"Unknown service tool: {name}"})

        url = tool["_api_base"].rstrip("/")
        endpoint = tool.get("_endpoint", "")
        if endpoint:
            # Substitute path parameters
            for key, value in arguments.items():
                endpoint = endpoint.replace(f"{{{key}}}", str(value))
            url = f"{url}/{endpoint.lstrip('/')}"

        # Downgrade https:// to http:// so httpx routes through the enforcer
        # as a regular proxy request (not CONNECT tunnel).  The enforcer
        # upgrades back to https:// before forwarding through egress.
        url = url.replace("https://", "http://", 1)

        method = tool.get("_method", "GET").upper()
        headers = {
            "X-Agency-Service": tool["_service"],
            "X-Agency-Tool": tool.get("_tool_name", name),
            "Authorization": f"Bearer {tool['_scoped_token']}",
            "Accept": "application/json",
        }

        # Map parameter names using query_params config (e.g. "query" -> "q")
        query_map = tool.get("_query_params", {})
        if query_map:
            mapped = {}
            for param_name, value in arguments.items():
                api_name = query_map.get(param_name, param_name)
                mapped[api_name] = value
            arguments = mapped

        try:
            if method == "GET":
                resp = http_client.get(url, headers=headers, params=arguments)
            else:
                resp = http_client.request(method, url, headers=headers, json=arguments)
            if resp.status_code >= 400:
                log.warning("Service tool %s returned %d: %s",
                            name, resp.status_code, resp.text[:200])
            return resp.text
        except httpx.HTTPError as e:
            log.warning("Service tool %s HTTP error: %s", name, e)
            return json.dumps({"error": f"HTTP error calling {name}: {e}"})


# ---------------------------------------------------------------------------
# Built-in Tools (workspace operations)
# ---------------------------------------------------------------------------

class BuiltinToolRegistry:
    """Registration-based registry for built-in tools the LLM calls directly.

    Provides workspace operations (read, write, list, execute, search)
    with path traversal enforcement. All paths are resolved against the
    workspace boundary.
    """

    VISIBILITY_DIR = Path("/visibility")

    def __init__(self, workspace_dir: Union[str, Path] = "/workspace",
                 extra_allowed_dirs: Optional[list[str]] = None):
        self.workspace_dir = Path(workspace_dir).resolve()
        self._extra_allowed_dirs = [
            Path(d).resolve() for d in (extra_allowed_dirs or [])
        ]
        self._tools: dict[str, dict] = {}  # name -> {handler, definition}
        self._register_defaults()

    def _register_defaults(self) -> None:
        """Register the five default built-in tools."""
        self.register_tool(
            name="read_file",
            description="Read the contents of a file. Returns the file text.",
            parameters={
                "type": "object",
                "properties": {
                    "path": {"type": "string", "description": "Path to the file (relative to /workspace)"},
                    "offset": {"type": "integer", "description": "Line number to start reading from (1-based)"},
                    "limit": {"type": "integer", "description": "Maximum number of lines to read"},
                },
                "required": ["path"],
            },
            handler=self._read_file,
        )
        self.register_tool(
            name="write_file",
            description="Write content to a file. Creates the file and parent directories if they don't exist.",
            parameters={
                "type": "object",
                "properties": {
                    "path": {"type": "string", "description": "Path to the file (relative to /workspace)"},
                    "content": {"type": "string", "description": "Content to write to the file"},
                },
                "required": ["path", "content"],
            },
            handler=self._write_file,
        )
        self.register_tool(
            name="list_directory",
            description="List the contents of a directory. Returns JSON array of entries with name, type, and size.",
            parameters={
                "type": "object",
                "properties": {
                    "path": {"type": "string", "description": "Directory path (relative to /workspace). Defaults to /workspace."},
                },
                "required": [],
            },
            handler=self._list_directory,
        )
        self.register_tool(
            name="execute_command",
            description="Execute a shell command in /workspace. Returns stdout, stderr, and exit code.",
            parameters={
                "type": "object",
                "properties": {
                    "command": {"type": "string", "description": "Shell command to execute"},
                    "timeout": {"type": "integer", "description": "Timeout in seconds (default 300, max 300)"},
                },
                "required": ["command"],
            },
            handler=self._execute_command,
        )
        self.register_tool(
            name="search_files",
            description="Search for a pattern in files using grep. Returns matching lines.",
            parameters={
                "type": "object",
                "properties": {
                    "pattern": {"type": "string", "description": "Search pattern (regex)"},
                    "path": {"type": "string", "description": "Directory to search in (relative to /workspace)"},
                    "include": {"type": "string", "description": "Glob pattern for files to include (e.g. '*.py')"},
                },
                "required": ["pattern"],
            },
            handler=self._search_files,
        )

    def register_tool(
        self,
        name: str,
        description: str,
        parameters: dict,
        handler,
    ) -> None:
        """Register a tool with its definition and handler."""
        self._tools[name] = {
            "handler": handler,
            "definition": {
                "type": "function",
                "function": {
                    "name": name,
                    "description": description,
                    "parameters": parameters,
                },
            },
        }

    def get_tool_definitions(self) -> list[dict]:
        """Return all tool definitions in OpenAI function-calling format."""
        return [t["definition"] for t in self._tools.values()]

    def has_tool(self, name: str) -> bool:
        return name in self._tools

    def call_tool(self, name: str, arguments: dict) -> str:
        """Call a registered tool by name. Returns JSON string."""
        if name not in self._tools:
            return json.dumps({"error": f"Unknown built-in tool: {name}"})
        try:
            return self._tools[name]["handler"](arguments)
        except Exception as e:
            return json.dumps({"error": f"Tool {name} failed: {e}"})

    def _resolve_path(self, path_str: str) -> Path:
        """Resolve a path against the workspace boundary.

        Blocks directory traversal via .. and symlink escapes.
        Raises ValueError if the resolved path is outside the workspace.
        """
        # Handle both absolute paths starting with /workspace and relative paths
        p = Path(path_str)
        if p.is_absolute():
            resolved = p.resolve()
        else:
            resolved = (self.workspace_dir / p).resolve()

        # Ensure the resolved path is within an allowed boundary
        allowed = False
        for allowed_dir in [self.workspace_dir, self.VISIBILITY_DIR.resolve(),
                            *self._extra_allowed_dirs]:
            try:
                resolved.relative_to(allowed_dir)
                allowed = True
                break
            except ValueError:
                continue
        if not allowed:
            raise ValueError(
                f"Path '{path_str}' resolves to '{resolved}' which is outside "
                f"the workspace boundary '{self.workspace_dir}'"
            )
        return resolved

    def _read_file(self, args: dict) -> str:
        path = self._resolve_path(args["path"])
        if not path.exists():
            return json.dumps({"error": f"File not found: {args['path']}"})
        if not path.is_file():
            return json.dumps({"error": f"Not a file: {args['path']}"})

        content = path.read_text(errors="replace")
        lines = content.splitlines(keepends=True)

        offset = args.get("offset")
        limit = args.get("limit")
        if offset is not None:
            lines = lines[max(0, offset - 1):]
        if limit is not None:
            lines = lines[:limit]

        return "".join(lines)

    def _write_file(self, args: dict) -> str:
        path = self._resolve_path(args["path"])
        # Block writes to read-only directories
        for ro_dir in [self.VISIBILITY_DIR.resolve(), *self._extra_allowed_dirs]:
            try:
                path.relative_to(ro_dir)
                return json.dumps({"error": f"Path under {ro_dir} is read-only"})
            except ValueError:
                continue
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(args["content"])
        return json.dumps({"status": "ok", "path": str(path), "bytes": len(args["content"])})

    def _list_directory(self, args: dict) -> str:
        path_str = args.get("path", ".")
        path = self._resolve_path(path_str)
        if not path.exists():
            return json.dumps({"error": f"Directory not found: {path_str}"})
        if not path.is_dir():
            return json.dumps({"error": f"Not a directory: {path_str}"})

        entries = []
        for entry in sorted(path.iterdir()):
            try:
                stat = entry.stat()
                entries.append({
                    "name": entry.name,
                    "type": "directory" if entry.is_dir() else "file",
                    "size": stat.st_size,
                })
            except OSError:
                entries.append({"name": entry.name, "type": "unknown", "size": 0})
        return json.dumps(entries)

    # Environment variables stripped from subprocess execution to prevent
    # credential leakage via agent-directed commands like `env` or `echo $KEY`.
    _SENSITIVE_ENV_VARS = frozenset({
        "OPENAI_API_KEY", "ANTHROPIC_API_KEY", "GOOGLE_API_KEY",
        "LLM_PROXY_KEY",
        "API_KEY", "SECRET_KEY", "AWS_SECRET_ACCESS_KEY",
        "AGENCY_LLM_API_KEY",
    })

    def _safe_env(self) -> dict[str, str]:
        """Return environment with sensitive variables removed."""
        return {
            k: v for k, v in os.environ.items()
            if k not in self._SENSITIVE_ENV_VARS
            and not k.endswith("_SECRET") and not k.endswith("_TOKEN")
        }

    def _execute_command(self, args: dict) -> str:
        command = args["command"]
        timeout = min(args.get("timeout", 300), 300)

        try:
            result = subprocess.run(
                ["bash", "-c", command],
                capture_output=True,
                timeout=timeout,
                cwd=str(self.workspace_dir),
                env=self._safe_env(),
            )
            return json.dumps({
                "stdout": result.stdout.decode(errors="replace"),
                "stderr": result.stderr.decode(errors="replace"),
                "exit_code": result.returncode,
            })
        except subprocess.TimeoutExpired:
            return json.dumps({"error": f"Command timed out after {timeout}s"})

    def _search_files(self, args: dict) -> str:
        pattern = args["pattern"]
        path_str = args.get("path", ".")
        include = args.get("include")

        search_path = self._resolve_path(path_str)
        if not search_path.exists():
            return json.dumps({"error": f"Path not found: {path_str}"})

        cmd = ["grep", "-rn", pattern, str(search_path)]
        if include:
            cmd = ["grep", "-rn", "--include", include, pattern, str(search_path)]

        try:
            result = subprocess.run(
                cmd,
                capture_output=True,
                timeout=30,
                cwd=str(self.workspace_dir),
                env=self._safe_env(),
            )
            output = result.stdout.decode(errors="replace")
            # Truncate large output
            if len(output) > 50_000:
                output = output[:50_000] + "\n... (output truncated)"
            return output if output else "No matches found."
        except subprocess.TimeoutExpired:
            return json.dumps({"error": "Search timed out"})


# ---------------------------------------------------------------------------
# Skills Manager (agentskills.io)
# ---------------------------------------------------------------------------

class SkillsManager:
    """Loads and manages agent skills from a pre-generated manifest.

    Skills follow the agentskills.io open standard. Each skill provides
    procedural knowledge via a SKILL.md file. Descriptions are loaded
    into the system prompt; full content is loaded on demand via the
    activate_skill tool.
    """

    def __init__(self, manifest_path: Union[str, Path]):
        self.manifest_path = Path(manifest_path)
        self._skills: dict[str, dict] = {}  # name -> {description, path, ...}
        self._activated: set[str] = set()

    def load(self) -> None:
        """Load skills from the manifest file."""
        if not self.manifest_path.exists():
            return
        try:
            content = self.manifest_path.read_text()
            manifest = json.loads(content)
        except (json.JSONDecodeError, OSError) as e:
            log.warning("Failed to load skills manifest: %s", e)
            return

        for skill in manifest.get("skills", []):
            name = skill.get("name", "")
            if name:
                self._skills[name] = skill

    @property
    def skill_names(self) -> list[str]:
        return list(self._skills.keys())

    def get_system_prompt_section(self) -> str:
        """Return a system prompt section listing available skills."""
        if not self._skills:
            return ""

        lines = ["<available-skills>"]
        for name, skill in self._skills.items():
            desc = skill.get("description", "No description")
            lines.append(f"  <skill name=\"{name}\">{desc}</skill>")
        lines.append("</available-skills>")
        lines.append("")
        lines.append(
            "To use a skill, call the activate_skill tool with the skill name. "
            "This will load detailed procedural knowledge for that skill."
        )
        return "\n".join(lines)

    def activate_skill(self, name: str) -> str:
        """Activate a skill and return its SKILL.md content."""
        if name not in self._skills:
            return json.dumps({"error": f"Unknown skill: {name}. Available: {', '.join(self._skills.keys())}"})

        skill = self._skills[name]
        skill_path = Path(skill.get("path", ""))

        if not skill_path.exists():
            return json.dumps({"error": f"Skill file not found: {skill_path}"})

        try:
            content = skill_path.read_text()
            self._activated.add(name)
            return content
        except OSError as e:
            return json.dumps({"error": f"Failed to read skill {name}: {e}"})

    @property
    def activated(self) -> set[str]:
        return set(self._activated)
