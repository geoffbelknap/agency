FROM python:3.13-slim
ARG BUILD_ID=unknown
LABEL agency.build.id=${BUILD_ID}
WORKDIR /app

# Build context: agency/ (Python package root)
COPY images/intake/requirements.txt /app/
RUN pip install --no-cache-dir -r requirements.txt

COPY images/intake/*.py /app/
COPY models/ /app/agency_core/models/
COPY exceptions.py /app/agency_core/exceptions.py
RUN mkdir -p /app/agency_core/images/intake \
    && touch /app/agency_core/__init__.py /app/agency_core/images/__init__.py /app/agency_core/images/intake/__init__.py \
    && cp /app/work_items.py /app/agency_core/images/intake/work_items.py \
    && cp /app/router.py /app/agency_core/images/intake/router.py \
    && cp /app/server.py /app/agency_core/images/intake/server.py \
    && cp /app/poller.py /app/agency_core/images/intake/poller.py \
    && cp /app/scheduler.py /app/agency_core/images/intake/scheduler.py \
    && cp /app/channel_watcher.py /app/agency_core/images/intake/channel_watcher.py

EXPOSE 8080
CMD ["python", "-u", "/app/server.py"]
