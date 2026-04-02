FROM python:3.13-slim
ARG BUILD_ID=unknown
LABEL agency.build.id=${BUILD_ID}
WORKDIR /app

# Build context: agency/ (Python package root)
COPY images/comms/requirements.txt /app/
RUN pip install --no-cache-dir -r requirements.txt

COPY images/comms/*.py /app/
COPY models/ /app/agency_core/models/
COPY exceptions.py /app/agency_core/exceptions.py
RUN mkdir -p /app/agency_core/images/comms \
    && touch /app/agency_core/__init__.py /app/agency_core/images/__init__.py /app/agency_core/images/comms/__init__.py \
    && cp /app/store.py /app/agency_core/images/comms/store.py \
    && cp /app/matcher.py /app/agency_core/images/comms/matcher.py \
    && cp /app/subscriptions.py /app/agency_core/images/comms/subscriptions.py \
    && cp /app/websocket.py /app/agency_core/images/comms/websocket.py

EXPOSE 8080
CMD ["python", "-u", "/app/server.py"]
