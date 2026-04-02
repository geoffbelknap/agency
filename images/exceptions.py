"""Agency exception hierarchy."""


from typing import Optional
class AgencyError(Exception):
    """Base exception. Always includes: what failed, why, how to fix."""

    fix: Optional[str] = None

    def __init__(self, message: str, fix: Optional[str] = None):
        super().__init__(message)
        if fix:
            self.fix = fix


class DockerNotAvailable(AgencyError):
    """Docker daemon not running or not accessible."""

    fix = "Start Docker Desktop, or on Linux: sudo systemctl start docker"


class ValidationError(AgencyError):
    """File failed schema validation."""


class AgentExistsError(AgencyError):
    """Agent with this name already exists."""


class AgentNotFoundError(AgencyError):
    """Agent not found."""


class InitExistsError(AgencyError):
    """Agency home directory already exists."""


class InfrastructureStartFailed(AgencyError):
    """Shared infrastructure failed to start."""

    def __init__(self, component: str, reason: str, logs: str = ""):
        self.component = component
        self.logs = logs
        message = (
            f"Infrastructure component '{component}' failed to start.\n"
            f"  Reason: {reason}"
        )
        if logs:
            message += f"\n  Logs: {logs[:500]}"
        fix = f"Check Docker logs: docker logs agency-{component}"
        super().__init__(message, fix=fix)


class IntegrityCheckFailed(AgencyError):
    """File integrity check failed."""

    def __init__(self, agent_name: str, filename: str, detail: str):
        self.agent_name = agent_name
        self.filename = filename
        message = (
            f"Integrity check failed for {agent_name}/{filename}.\n"
            f"  {detail}\n\n"
            f"  This could indicate:\n"
            f"    - Accidental edit to {filename}\n"
            f"    - Unauthorized modification attempt"
        )
        fix = (
            f"agency agent verify {agent_name}    -> detailed integrity report\n"
            f"  agency agent start {agent_name} --skip-integrity-check  -> override (logged)"
        )
        super().__init__(message, fix=fix)


class ConstraintLoadFailed(AgencyError):
    """constraints.yaml cannot be loaded or validated."""

    def __init__(self, agent_name: str, reason: str):
        message = (
            f"Failed to load constraints for {agent_name}.\n"
            f"  Reason: {reason}"
        )
        fix = f"Check constraints file: ~/.agency/agents/{agent_name}/constraints.yaml"
        super().__init__(message, fix=fix)


class WorkspaceCompatibilityFailed(AgencyError):
    """Agent requires tools workspace doesn't provide."""

    def __init__(self, agent_name: str, missing_tools: list[str]):
        self.missing_tools = missing_tools
        message = (
            f"Workspace missing required tools for {agent_name}.\n"
            f"  Missing: {', '.join(missing_tools)}"
        )
        fix = "Add missing tools to workspace template or agent image"
        super().__init__(message, fix=fix)


class ASKViolation(AgencyError):
    """Raised when an operation would violate an ASK framework tenet."""

    def __init__(self, tenet: int, explanation: str):
        self.tenet = tenet
        self.explanation = explanation
        fix = "This is an enforcement decision. The ASK framework requires this constraint."
        super().__init__(f"ASK Tenet {tenet}: {explanation}", fix=fix)


class HubError(AgencyError):
    """Hub operation failed (git clone, component resolution, etc.)."""
