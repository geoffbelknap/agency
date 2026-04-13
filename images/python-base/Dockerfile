FROM python:3.13-slim
ARG BUILD_ID=unknown
ARG SOURCE_HASH=unknown
LABEL agency.build.id="${BUILD_ID}"
LABEL agency.source.hash="${SOURCE_HASH}"
LABEL org.opencontainers.image.source="https://github.com/geoffbelknap/agency"
LABEL org.opencontainers.image.description="Shared Python base image for Agency Python services"

# Shared base image for Agency Python services.
# Common dependencies installed once; per-service images add only their extras.
# See docs/specs/container-build-standards.md for rationale.

RUN pip install --no-cache-dir \
    httpx==0.28.1 \
    aiohttp==3.13.3 \
    pyyaml==6.0.3 \
    pydantic==2.12.5
