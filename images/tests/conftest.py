"""Shared test fixtures for knowledge server tests."""


PLATFORM_HEADERS = {"X-Agency-Platform": "true"}


class PlatformClient:
    """Wraps an aiohttp test client to inject the X-Agency-Platform header.

    The knowledge server's ``_require_platform()`` check requires this header
    on platform-privileged endpoints (/pending, /review, /org-context, etc.).
    Test clients represent platform callers, so the header should always be set.
    """

    def __init__(self, client):
        self._client = client

    # Proxy the ``app`` attribute so tests can still reach the store via ``c.app["store"]``.
    @property
    def app(self):
        return self._client.app

    def _merge_headers(self, kwargs):
        headers = kwargs.pop("headers", {})
        headers.setdefault("X-Agency-Platform", "true")
        kwargs["headers"] = headers

    async def get(self, path, **kwargs):
        self._merge_headers(kwargs)
        return await self._client.get(path, **kwargs)

    async def post(self, path, **kwargs):
        self._merge_headers(kwargs)
        return await self._client.post(path, **kwargs)

    async def put(self, path, **kwargs):
        self._merge_headers(kwargs)
        return await self._client.put(path, **kwargs)

    async def delete(self, path, **kwargs):
        self._merge_headers(kwargs)
        return await self._client.delete(path, **kwargs)

    async def patch(self, path, **kwargs):
        self._merge_headers(kwargs)
        return await self._client.patch(path, **kwargs)
