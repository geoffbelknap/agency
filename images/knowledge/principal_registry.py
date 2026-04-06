"""Principal registry — reads from gateway snapshot.

The gateway is the authoritative source. This module reads the
registry.json snapshot delivered via config path or file mount.
"""
import json
import logging
import os

logger = logging.getLogger(__name__)


class PrincipalRegistry:
    """Read-only principal registry backed by a JSON snapshot."""

    VALID_TYPES = ("operator", "agent", "team", "role", "channel")

    def __init__(self, snapshot_path=None, snapshot_data=None):
        self._principals = {}  # uuid -> principal dict
        self._by_type_name = {}  # (type, name) -> uuid
        if snapshot_path:
            self.load_file(snapshot_path)
        elif snapshot_data:
            self.load_data(snapshot_data)

    def load_file(self, path):
        if not os.path.exists(path):
            logger.warning("Registry snapshot not found at %s", path)
            return
        with open(path) as f:
            data = json.load(f)
        self.load_data(data)

    def load_data(self, data):
        self._principals = {}
        self._by_type_name = {}
        for p in data.get("principals", []):
            uuid = p["uuid"]
            self._principals[uuid] = p
            self._by_type_name[(p["type"], p["name"])] = uuid

    def resolve(self, uuid):
        return self._principals.get(uuid)

    def resolve_name(self, principal_type, name):
        return self._by_type_name.get((principal_type, name))

    def list_by_type(self, principal_type):
        return [p for p in self._principals.values() if p["type"] == principal_type]

    def list_all(self):
        return list(self._principals.values())

    @staticmethod
    def format_id(principal_type, uuid):
        return f"{principal_type}:{uuid}"

    def parse_id(self, principal_id):
        if ":" not in principal_id:
            raise ValueError(f"Invalid principal ID: {principal_id}")
        ptype, identifier = principal_id.split(":", 1)
        if len(identifier) == 36 and identifier.count("-") == 4:
            return ptype, identifier
        resolved = self.resolve_name(ptype, identifier)
        return ptype, resolved if resolved else identifier
