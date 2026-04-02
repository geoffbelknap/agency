FROM python:3.13-slim
ARG BUILD_ID=unknown
LABEL agency.build.id=${BUILD_ID}
WORKDIR /app

# Build context: agency/ (Python package root)
COPY images/knowledge/requirements.txt /app/
RUN pip install --no-cache-dir -r requirements.txt

COPY images/knowledge/*.py /app/
RUN mkdir -p /app/agency_core/images/knowledge \
    && touch /app/agency_core/__init__.py /app/agency_core/images/__init__.py /app/agency_core/images/knowledge/__init__.py \
    && cp /app/store.py /app/agency_core/images/knowledge/store.py \
    && cp /app/ingester.py /app/agency_core/images/knowledge/ingester.py \
    && cp /app/synthesizer.py /app/agency_core/images/knowledge/synthesizer.py \
    && cp /app/curator.py /app/agency_core/images/knowledge/curator.py

EXPOSE 8080
CMD ["python", "-u", "/app/server.py"]
