"""Organization configuration schema."""

from datetime import datetime
from typing import Literal

from pydantic import BaseModel, ConfigDict, Field


class OrgConfig(BaseModel):
    """Schema for org.yaml."""

    model_config = ConfigDict(extra="forbid")

    version: str = "0.1"
    name: str
    operator: str
    created: str
    deployment_mode: Literal["standalone", "team", "enterprise"] = "standalone"
