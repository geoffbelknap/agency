FROM python:3.13-slim
ARG BUILD_ID=unknown
LABEL agency.build.id=${BUILD_ID}
WORKDIR /app

# Build context: repo root
COPY images/intake/requirements.txt /app/
RUN pip install --no-cache-dir -r requirements.txt

# Shared logging infrastructure (auto-configures via sitecustomize.py)
COPY images/logging_config.py /app/logging_config.py
COPY images/_sitecustomize.py /app/sitecustomize.py

COPY images/intake/*.py /app/
COPY images/models/ /app/images/models/
COPY images/exceptions.py /app/images/exceptions.py
RUN touch /app/images/__init__.py /app/images/models/__init__.py \
    && mkdir -p /app/images/intake \
    && touch /app/images/intake/__init__.py \
    && cp /app/*.py /app/images/intake/

EXPOSE 8080
CMD ["python", "-u", "/app/server.py"]
