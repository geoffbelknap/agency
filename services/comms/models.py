"""Schemas used by the host-managed comms service."""

import re
import uuid
from datetime import datetime, timezone
from enum import Enum
from typing import Any, Optional

from pydantic import BaseModel, Field, field_validator, model_validator


class ChannelType(str, Enum):
    TEAM = "team"
    DIRECT = "direct"
    SYSTEM = "system"


class ChannelState(str, Enum):
    ACTIVE = "active"
    ARCHIVED = "archived"
    PURGED = "purged"


class MessageFlags(BaseModel):
    decision: bool = False
    question: bool = False
    blocker: bool = False
    urgent: bool = False
    approval_request: bool = False
    approval_response: bool = False


class Message(BaseModel):
    id: str = Field(default_factory=lambda: uuid.uuid4().hex[:12])
    channel: str
    author: str
    timestamp: datetime = Field(default_factory=lambda: datetime.now(timezone.utc))
    content: str
    reply_to: Optional[str] = None
    flags: MessageFlags = Field(default_factory=MessageFlags)
    metadata: dict = Field(default_factory=dict)
    edited_at: Optional[datetime] = None
    edit_history: list[dict] = Field(default_factory=list)
    deleted: bool = False
    reactions: list[dict] = Field(default_factory=list)

    @field_validator("content")
    @classmethod
    def content_bounded(cls, v: str) -> str:
        if len(v) > 10000:
            raise ValueError("Message content exceeds 10000 character limit")
        return v

    @model_validator(mode="after")
    def content_not_empty_unless_deleted(self) -> "Message":
        if not self.deleted and (not self.content or not self.content.strip()):
            raise ValueError("Message content cannot be empty")
        return self


_CHANNEL_NAME_RE = re.compile(r"^_?[a-z0-9][a-z0-9-]*[a-z0-9]$|^_?[a-z0-9]$")


class Channel(BaseModel):
    id: str = Field(default_factory=lambda: str(uuid.uuid4()))
    name: str
    type: ChannelType
    created_by: str
    created_at: datetime = Field(default_factory=lambda: datetime.now(timezone.utc))
    topic: str = ""
    members: list[str] = Field(default_factory=list)
    visibility: str = "open"
    state: ChannelState = ChannelState.ACTIVE
    deployment_id: str = ""
    base_name: str = ""
    archived_at: Optional[datetime] = None
    archived_by: str = ""

    @field_validator("visibility")
    @classmethod
    def validate_visibility(cls, v: str) -> str:
        if v not in ("open", "private", "platform-write"):
            raise ValueError(f"visibility must be 'open', 'private', or 'platform-write', got {v!r}")
        return v

    @field_validator("name")
    @classmethod
    def validate_channel_name(cls, v: str) -> str:
        if not _CHANNEL_NAME_RE.match(v):
            raise ValueError(f"Channel name must be lowercase kebab-case: {v!r}")
        return v


class ExpertiseTier(str, Enum):
    BASE = "base"
    STANDING = "standing"
    LEARNED = "learned"
    TASK = "task"


class ExpertiseDeclaration(BaseModel):
    tier: ExpertiseTier
    description: str = ""
    keywords: list[str] = Field(default_factory=list)
    persistent: bool = False

    @field_validator("keywords")
    @classmethod
    def validate_keywords(cls, v: list[str]) -> list[str]:
        if len(v) > 30:
            raise ValueError("Expertise declarations support at most 30 keywords")
        return [kw for kw in v if len(kw) >= 3]


class InterestDeclaration(BaseModel):
    task_id: str
    description: str = ""
    keywords: list[str] = Field(default_factory=list)
    knowledge_filter: dict[str, list[str]] = Field(default_factory=dict)

    @field_validator("keywords")
    @classmethod
    def validate_interest_keywords(cls, v: list[str]) -> list[str]:
        if len(v) > 20:
            raise ValueError("Interest declarations support at most 20 keywords")
        return [kw for kw in v if len(kw) >= 3]

    @field_validator("knowledge_filter")
    @classmethod
    def validate_knowledge_filter(cls, v: dict[str, list[str]]) -> dict[str, list[str]]:
        total = sum(len(entries) for entries in v.values())
        if total > 10:
            raise ValueError("Knowledge filter supports at most 10 entries")
        return v


class MatchClassification(str, Enum):
    DIRECT = "direct"
    INTEREST_MATCH = "interest_match"
    AMBIENT = "ambient"


class WSEvent(BaseModel):
    v: int = 1
    type: str
    channel: Optional[str] = None
    match: Optional[str] = None
    matched_keywords: Optional[list[str]] = None
    message: Optional[dict[str, Any]] = None
    task: Optional[dict[str, Any]] = None
    event: Optional[str] = None
    data: Optional[dict[str, Any]] = None
