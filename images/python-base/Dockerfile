FROM python:3.13-slim
LABEL org.opencontainers.image.source="https://github.com/geoffbelknap/agency"
LABEL org.opencontainers.image.description="Shared Python base image for Agency Python services"

# Shared base image for Agency Python services.
# Common dependencies installed once; per-service images add only their extras.
# See docs/specs/container-build-standards.md for rationale.
# Keep the shared base free of volatile per-release metadata so downstream
# images can reuse it across tags when the actual base contents are unchanged.

RUN pip install --no-cache-dir \
    httpx==0.28.1 \
    aiohttp==3.13.3 \
    pyyaml==6.0.3 \
    pydantic==2.12.5
