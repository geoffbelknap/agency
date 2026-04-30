ARG PYTHON_BASE_IMAGE=ghcr.io/geoffbelknap/agency-python-base:latest
FROM ${PYTHON_BASE_IMAGE}
ARG BUILD_ID=unknown
ARG SOURCE_HASH=unknown
WORKDIR /app

# Extra deps not in the shared base
COPY services/knowledge/requirements.txt /app/
RUN pip install --no-cache-dir -r requirements.txt

# Shared logging infrastructure (auto-configures via sitecustomize.py)
COPY images/logging_config.py /app/logging_config.py
COPY images/_sitecustomize.py /app/sitecustomize.py

COPY services/knowledge /app/services/knowledge
RUN touch /app/services/__init__.py

EXPOSE 8080
LABEL agency.build.id=${BUILD_ID}
LABEL agency.source.hash=${SOURCE_HASH}
CMD ["python", "-u", "-m", "services.knowledge.server"]
