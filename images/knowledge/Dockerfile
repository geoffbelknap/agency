FROM python:3.13-slim
ARG BUILD_ID=unknown
LABEL agency.build.id=${BUILD_ID}
WORKDIR /app

# Build context: repo root
COPY images/knowledge/requirements.txt /app/
RUN pip install --no-cache-dir -r requirements.txt

COPY images/knowledge/*.py /app/
RUN mkdir -p /app/images/knowledge \
    && touch /app/images/__init__.py /app/images/knowledge/__init__.py \
    && cp /app/*.py /app/images/knowledge/

EXPOSE 8080
CMD ["python", "-u", "/app/server.py"]
