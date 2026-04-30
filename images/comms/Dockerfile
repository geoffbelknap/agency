ARG PYTHON_BASE_IMAGE=ghcr.io/geoffbelknap/agency-python-base:latest
FROM ${PYTHON_BASE_IMAGE}
ARG BUILD_ID=unknown
ARG SOURCE_HASH=unknown
WORKDIR /app

# Shared logging infrastructure (auto-configures via sitecustomize.py)
COPY images/logging_config.py /app/logging_config.py
COPY images/_sitecustomize.py /app/sitecustomize.py

COPY services/comms /app/services/comms
RUN mkdir -p /app/images/models
COPY images/models/*.py /app/images/models/
COPY images/exceptions.py /app/images/exceptions.py
RUN touch /app/images/__init__.py /app/images/models/__init__.py \
    && touch /app/services/__init__.py

EXPOSE 8080
LABEL agency.build.id=${BUILD_ID}
LABEL agency.source.hash=${SOURCE_HASH}
CMD ["python", "-u", "-m", "services.comms.server"]
