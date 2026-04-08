"""Gateway client for the knowledge service.

Routes curator notifications through the gateway event bus
instead of direct HTTP to the comms container.
"""
import os
import logging
import httpx

logger = logging.getLogger("agency.knowledge.gateway")


class GatewayClient:
    def __init__(self, base_url: str = "http://gateway:8200", token: str = ""):
        self.base_url = base_url.rstrip("/")
        self.token = token

    def _headers(self) -> dict:
        h = {"Content-Type": "application/json"}
        if self.token:
            h["Authorization"] = f"Bearer {self.token}"
        caller = os.environ.get("AGENCY_CALLER", "")
        if caller:
            h["X-Agency-Caller"] = caller
        return h

    def publish_knowledge_update(self, node_summary: str, metadata: dict) -> None:
        """Publish a knowledge update event via the gateway event bus."""
        payload = {
            "source_type": "platform",
            "source_name": "knowledge",
            "event_type": "knowledge_update",
            "data": {
                "summary": node_summary,
                "channel": "_knowledge-updates",
                **metadata,
            },
        }
        try:
            client = httpx.Client(timeout=5)
            resp = client.post(
                f"{self.base_url}/api/v1/events/publish",
                json=payload,
                headers=self._headers(),
            )
            if resp.status_code >= 400:
                logger.warning("knowledge update publish failed: %d", resp.status_code)
        except Exception as e:
            logger.warning("knowledge update publish error: %s", e)
