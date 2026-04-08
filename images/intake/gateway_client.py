"""Gateway HTTP client for the intake service.

Routes inter-service calls through the gateway (hub-and-spoke)
instead of direct HTTP to comms/knowledge containers.
"""
import os
import logging
import aiohttp

logger = logging.getLogger("agency.intake.gateway")


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

    async def publish_event(
        self,
        source_name: str,
        event_type: str,
        data: dict,
        metadata: dict | None = None,
    ) -> None:
        """Publish an event to the gateway event bus."""
        payload = {
            "source_type": "platform",
            "source_name": source_name,
            "event_type": event_type,
            "data": data,
        }
        if metadata:
            payload["metadata"] = metadata

        url = f"{self.base_url}/api/v1/events/publish"
        try:
            async with aiohttp.ClientSession() as session:
                resp = await session.post(
                    url,
                    json=payload,
                    headers=self._headers(),
                    timeout=aiohttp.ClientTimeout(total=10),
                )
                if resp.status >= 400:
                    body = await resp.text()
                    logger.warning("event publish failed: %d %s", resp.status, body)
        except Exception as e:
            logger.warning("event publish error: %s", e)

    async def graph_ingest(
        self,
        content: str,
        filename: str = "",
        content_type: str = "application/json",
    ) -> dict | None:
        """Ingest content into the knowledge graph via the gateway."""
        payload = {
            "content": content,
            "filename": filename,
            "content_type": content_type,
        }
        url = f"{self.base_url}/api/v1/graph/ingest"
        try:
            async with aiohttp.ClientSession() as session:
                resp = await session.post(
                    url,
                    json=payload,
                    headers=self._headers(),
                    timeout=aiohttp.ClientTimeout(total=30),
                )
                if resp.status < 400:
                    return await resp.json()
                body = await resp.text()
                logger.warning("graph ingest failed: %d %s", resp.status, body)
        except Exception as e:
            logger.warning("graph ingest error: %s", e)
        return None

    async def post_channel_message(
        self,
        channel_name: str,
        content: str,
        author: str,
    ) -> dict | None:
        """Post a message to a comms channel via the gateway."""
        url = f"{self.base_url}/api/v1/comms/channels/{channel_name}/messages"
        payload = {"content": content, "author": author}
        try:
            async with aiohttp.ClientSession() as session:
                resp = await session.post(
                    url,
                    json=payload,
                    headers=self._headers(),
                    timeout=aiohttp.ClientTimeout(total=10),
                )
                if resp.status < 400:
                    return await resp.json()
                body = await resp.text()
                logger.warning("channel message failed: %d %s", resp.status, body)
        except Exception as e:
            logger.warning("channel message error: %s", e)
        return None

    async def get_channel_messages(
        self,
        channel_name: str,
        since: str | None = None,
        limit: int = 100,
    ) -> list:
        """Fetch messages from a comms channel via the gateway."""
        url = f"{self.base_url}/api/v1/comms/channels/{channel_name}/messages"
        params = {"limit": str(limit)}
        if since:
            params["since"] = since
        try:
            async with aiohttp.ClientSession() as session:
                resp = await session.get(
                    url,
                    params=params,
                    headers=self._headers(),
                    timeout=aiohttp.ClientTimeout(total=10),
                )
                if resp.status < 400:
                    return await resp.json()
                body = await resp.text()
                logger.warning(
                    "channel messages fetch failed: %d %s", resp.status, body
                )
        except Exception as e:
            logger.warning("channel messages error: %s", e)
        return []
