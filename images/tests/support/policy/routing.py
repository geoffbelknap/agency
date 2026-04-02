"""Exception routing — routes two-key exceptions to appropriate principals.

When an agent needs an exception to a policy parameter, the exception
request is routed to the principals responsible for that domain.
Privacy exceptions go to the privacy officer, security exceptions to
the security lead, etc.

Route definitions live in principals.yaml under exception_routes.
"""

import logging
from datetime import datetime, timezone
from pathlib import Path

import yaml

from typing import Optional
from images.models.principal import PrincipalsConfig

log = logging.getLogger(__name__)


# Default domain mappings for parameters when no explicit route exists
_PARAMETER_DOMAINS = {
    "risk_tolerance": "security",
    "max_concurrent_tasks": "operations",
    "max_task_duration": "operations",
    "autonomous_interrupt_threshold": "security",
    "network_mediation": "security",
    "logging": "compliance",
    "constraints_readonly": "security",
    "llm_credentials_isolated": "security",
}


class ExceptionRequest:
    """A pending exception request awaiting routing and approval."""

    def __init__(
        self,
        request_id: str,
        agent_name: str,
        parameter: str,
        requested_value: str,
        reason: str,
        domain: Optional[str] = None,
    ):
        self.request_id = request_id
        self.agent_name = agent_name
        self.parameter = parameter
        self.requested_value = requested_value
        self.reason = reason
        self.domain = domain or _PARAMETER_DOMAINS.get(parameter, "general")
        self.status = "pending"  # pending, routed, approved, denied
        self.routed_to: list[str] = []
        self.approvals: list[dict] = []  # {principal_id, timestamp}
        self.denials: list[dict] = []
        self.recommendations: list[dict] = []  # {agent, action, reasoning, timestamp}
        self.requires_dual_approval = False
        self.created_at = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
        self.resolved_at: Optional[str] = None

    def to_dict(self) -> dict:
        return {
            "request_id": self.request_id,
            "agent_name": self.agent_name,
            "parameter": self.parameter,
            "requested_value": self.requested_value,
            "reason": self.reason,
            "domain": self.domain,
            "status": self.status,
            "routed_to": self.routed_to,
            "approvals": self.approvals,
            "denials": self.denials,
            "recommendations": self.recommendations,
            "requires_dual_approval": self.requires_dual_approval,
            "created_at": self.created_at,
            "resolved_at": self.resolved_at,
        }

    @classmethod
    def from_dict(cls, data: dict) -> "ExceptionRequest":
        req = cls(
            request_id=data["request_id"],
            agent_name=data["agent_name"],
            parameter=data["parameter"],
            requested_value=str(data["requested_value"]),
            reason=data.get("reason", ""),
            domain=data.get("domain"),
        )
        req.status = data.get("status", "pending")
        req.routed_to = data.get("routed_to", [])
        req.approvals = data.get("approvals", [])
        req.denials = data.get("denials", [])
        req.recommendations = data.get("recommendations", [])
        req.requires_dual_approval = data.get("requires_dual_approval", False)
        req.created_at = data.get("created_at", "")
        req.resolved_at = data.get("resolved_at")
        return req


class ExceptionRouter:
    """Routes exception requests to appropriate principals for approval.

    Uses principals.yaml exception_routes to determine who reviews what.
    Falls back to the operator if no route is defined.
    """

    def __init__(self, agency_home: Optional[Path] = None):
        self.home = agency_home or Path.home() / ".agency"
        self.requests_dir = self.home / "exception-requests"

    def _load_principals(self) -> Optional[PrincipalsConfig]:
        principals_file = self.home / "principals.yaml"
        if not principals_file.exists():
            return None
        try:
            data = yaml.safe_load(principals_file.read_text())
            return PrincipalsConfig.model_validate(data)
        except Exception as e:
            log.warning("Failed to load principals: %s", e)
            return None

    def route(self, request: ExceptionRequest) -> ExceptionRequest:
        """Route an exception request to the appropriate approvers.

        Looks up the domain in principals.yaml exception_routes.
        Falls back to operator if no route is defined.
        """
        principals = self._load_principals()

        if principals:
            for route in principals.exception_routes:
                if route.domain == request.domain:
                    request.routed_to = list(route.approvers)
                    request.requires_dual_approval = route.requires_dual_approval
                    request.status = "routed"
                    self._save(request)
                    return request

            # Check if any human has this domain in exception_domains
            domain_principals = [
                h.id for h in principals.humans
                if request.domain in h.exception_domains
                and h.status == "active"
            ]
            if domain_principals:
                request.routed_to = domain_principals
                request.status = "routed"
                self._save(request)
                return request

        # Fallback: route to operator
        request.routed_to = ["operator"]
        request.status = "routed"
        self._save(request)
        return request

    def approve(
        self, request_id: str, principal_id: str
    ) -> Optional[ExceptionRequest]:
        """Record an approval from a principal.

        If dual approval is required, the request stays routed until
        two approvals are received.
        """
        request = self.get(request_id)
        if not request:
            return None

        if principal_id not in request.routed_to:
            return None

        request.approvals.append({
            "principal_id": principal_id,
            "timestamp": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        })

        if request.requires_dual_approval:
            if len(request.approvals) >= 2:
                request.status = "approved"
                request.resolved_at = datetime.now(timezone.utc).strftime(
                    "%Y-%m-%dT%H:%M:%SZ"
                )
        else:
            request.status = "approved"
            request.resolved_at = datetime.now(timezone.utc).strftime(
                "%Y-%m-%dT%H:%M:%SZ"
            )

        self._save(request)
        return request

    def deny(
        self, request_id: str, principal_id: str, reason: str = ""
    ) -> Optional[ExceptionRequest]:
        """Deny an exception request."""
        request = self.get(request_id)
        if not request:
            return None

        request.denials.append({
            "principal_id": principal_id,
            "reason": reason,
            "timestamp": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        })
        request.status = "denied"
        request.resolved_at = datetime.now(timezone.utc).strftime(
            "%Y-%m-%dT%H:%M:%SZ"
        )
        self._save(request)
        return request

    def recommend(
        self,
        request_id: str,
        agent_name: str,
        action: str,
        reasoning: str,
    ) -> Optional[ExceptionRequest]:
        """Record a function agent's recommendation on an exception request.

        Function agents can recommend 'approve' or 'deny' with reasoning.
        Recommendations are advisory — they don't change the request status.
        Human approvers see them when reviewing.
        """
        if action not in ("approve", "deny"):
            return None

        request = self.get(request_id)
        if not request:
            return None

        # Validate the recommender is a function agent in a relevant team
        valid = self._validate_recommender(agent_name, request.domain)
        if not valid:
            return None

        request.recommendations.append({
            "agent": agent_name,
            "action": action,
            "reasoning": reasoning,
            "timestamp": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        })

        self._save(request)
        return request

    def _validate_recommender(self, agent_name: str, domain: str) -> bool:
        """Check if an agent is a function agent that can recommend for this domain."""
        try:
            return False  # coordination module removed; function agent validation unavailable
            team = find_agent_team(agent_name, agency_home=self.home)
            if not team:
                return False
            member = team.get_member(agent_name)
            if not member or member.agent_type != "function":
                return False
            return True
        except Exception:
            return False

    def get(self, request_id: str) -> Optional[ExceptionRequest]:
        """Load an exception request by ID."""
        req_file = self.requests_dir / f"{request_id}.yaml"
        if not req_file.exists():
            return None
        try:
            data = yaml.safe_load(req_file.read_text())
            return ExceptionRequest.from_dict(data)
        except Exception:
            return None

    def list_pending(self) -> list[ExceptionRequest]:
        """List all pending/routed exception requests."""
        if not self.requests_dir.exists():
            return []
        results = []
        for f in sorted(self.requests_dir.glob("*.yaml")):
            try:
                data = yaml.safe_load(f.read_text())
                req = ExceptionRequest.from_dict(data)
                if req.status in ("pending", "routed"):
                    results.append(req)
            except Exception:
                continue
        return results

    def list_for_principal(self, principal_id: str) -> list[ExceptionRequest]:
        """List exception requests routed to a specific principal."""
        if not self.requests_dir.exists():
            return []
        results = []
        for f in sorted(self.requests_dir.glob("*.yaml")):
            try:
                data = yaml.safe_load(f.read_text())
                req = ExceptionRequest.from_dict(data)
                if principal_id in req.routed_to and req.status == "routed":
                    results.append(req)
            except Exception:
                continue
        return results

    def _save(self, request: ExceptionRequest) -> None:
        self.requests_dir.mkdir(parents=True, exist_ok=True)
        (self.requests_dir / f"{request.request_id}.yaml").write_text(
            yaml.dump(request.to_dict(), default_flow_style=False)
        )
