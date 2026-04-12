"""Service credential models.

Defines the schema for service definitions (what services exist and how they
authenticate) and service grants (which agents have access to which services).
"""

from datetime import datetime, timezone

from pydantic import BaseModel, ConfigDict, field_validator
from typing import Optional


class ServiceCredentialConfig(BaseModel):
    """How a service authenticates API requests."""

    model_config = ConfigDict(extra="forbid")

    env_var: str
    header: str
    format: Optional[str] = None
    scoped_prefix: str

    @field_validator("scoped_prefix")
    @classmethod
    def prefix_must_start_with_agency(cls, v: str) -> str:
        if not v.startswith("agency-scoped-"):
            raise ValueError("scoped_prefix must start with 'agency-scoped-'")
        return v


class ServiceToolParameter(BaseModel):
    """A parameter for a service tool."""

    model_config = ConfigDict(extra="forbid")

    name: str
    type: str = "string"
    description: str
    required: bool = True
    default: Optional[str] = None


class ConsentRequirement(BaseModel):
    model_config = ConfigDict(extra="forbid")

    operation_kind: str
    token_input_field: str
    target_input_field: str
    min_witnesses: int = 1


class ServiceTool(BaseModel):
    """An MCP-exposed tool for a granted service."""

    model_config = ConfigDict(extra="forbid")

    name: str
    description: str
    parameters: list[ServiceToolParameter] = []
    method: str = "GET"
    path: str
    query_params: Optional[dict[str, str]] = None
    body_template: Optional[dict] = None
    response_path: Optional[str] = None
    requires_consent_token: Optional[ConsentRequirement] = None


class ServiceDefinition(BaseModel):
    """A service that can be granted to agents.

    Loaded from YAML files in ~/.agency/services/.
    """

    model_config = ConfigDict(extra="forbid")

    service: str
    display_name: str
    api_base: str
    description: str = ""
    credential: ServiceCredentialConfig
    usage_example: Optional[str] = None
    tools: list[ServiceTool] = []

    @field_validator("service")
    @classmethod
    def service_name_valid(cls, v: str) -> str:
        if not v.replace("-", "").replace("_", "").isalnum():
            raise ValueError(
                "Service name must be alphanumeric with hyphens or underscores"
            )
        return v


class ServiceGrant(BaseModel):
    """Record that a service has been granted to an agent."""

    model_config = ConfigDict(extra="forbid")

    service: str
    granted_at: str
    granted_by: str

    @field_validator("granted_at")
    @classmethod
    def timestamp_not_empty(cls, v: str) -> str:
        if not v:
            raise ValueError("granted_at must not be empty")
        return v


class AgentServiceGrants(BaseModel):
    """All service grants for an agent.

    Written to ~/.agency/agents/{name}/services.yaml.
    """

    model_config = ConfigDict(extra="forbid")

    agent: str
    grants: list[ServiceGrant] = []
