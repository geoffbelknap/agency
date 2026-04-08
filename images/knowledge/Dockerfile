FROM agency-python-base:latest
ARG BUILD_ID=unknown
LABEL agency.build.id=${BUILD_ID}
WORKDIR /app

# Extra deps not in the shared base
COPY images/knowledge/requirements.txt /app/
RUN pip install --no-cache-dir -r requirements.txt

# Shared logging infrastructure (auto-configures via sitecustomize.py)
COPY images/logging_config.py /app/logging_config.py
COPY images/_sitecustomize.py /app/sitecustomize.py

COPY images/knowledge/*.py /app/
RUN mkdir -p /app/images/knowledge \
    && touch /app/images/__init__.py /app/images/knowledge/__init__.py \
    && cp /app/*.py /app/images/knowledge/

EXPOSE 8080
CMD ["python", "-u", "/app/server.py"]
