"""Authorization scope model for knowledge graph filtering.

A Scope defines the visibility boundary for knowledge nodes and edges.
Agents with overlapping scopes can share knowledge; disjoint scopes
isolate knowledge between agents.

Empty scopes (no channels AND no principals) are unrestricted and
overlap with everything — this preserves backward compatibility with
agents that predate scope-based filtering.
"""

from dataclasses import dataclass, field
from typing import Optional


@dataclass
class Scope:
    """Authorization scope for knowledge graph access."""

    channels: list = field(default_factory=list)
    principals: list = field(default_factory=list)
    classification: Optional[str] = None

    def to_dict(self) -> dict:
        """Serialize for JSON storage."""
        d = {
            "channels": sorted(self.channels),
            "principals": sorted(self.principals),
        }
        if self.classification is not None:
            d["classification"] = self.classification
        return d

    @classmethod
    def from_dict(cls, data: dict) -> "Scope":
        """Deserialize from a dict (e.g. parsed JSON)."""
        return cls(
            channels=data.get("channels", []),
            principals=data.get("principals", []),
            classification=data.get("classification"),
        )

    @classmethod
    def from_source_channels(cls, channels: list) -> "Scope":
        """Create a Scope from a legacy source_channels list."""
        return cls(channels=list(channels))

    def _is_empty(self) -> bool:
        return not self.channels and not self.principals

    def overlaps(self, other: "Scope") -> bool:
        """True if any channel or principal overlaps.

        Empty scopes (no channels AND no principals) are unrestricted
        and overlap with everything.
        """
        if self._is_empty() or other._is_empty():
            return True
        channel_overlap = bool(set(self.channels) & set(other.channels))
        principal_overlap = bool(set(self.principals) & set(other.principals))
        return channel_overlap or principal_overlap

    def intersection(self, other: "Scope") -> "Scope":
        """Return a new Scope with only shared channels and principals."""
        shared_channels = sorted(set(self.channels) & set(other.channels))
        shared_principals = sorted(set(self.principals) & set(other.principals))
        return Scope(channels=shared_channels, principals=shared_principals)

    def is_narrower_than(self, other: "Scope") -> bool:
        """True if self is a subset of other (self's scope fits within other's)."""
        channels_subset = set(self.channels) <= set(other.channels)
        principals_subset = set(self.principals) <= set(other.principals)
        return channels_subset and principals_subset
