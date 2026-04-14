FROM debian:bookworm-slim@sha256:4724b8cc51e33e398f0e2e15e18d5ec2851ff0c2280647e1310bc1642182655d AS builder

ARG NEBULA_VERSION=1.10.0
ARG TARGETARCH

RUN apt-get update && apt-get install -y --no-install-recommends curl ca-certificates && \
    curl -fsSL "https://github.com/slackhq/nebula/releases/download/v${NEBULA_VERSION}/nebula-linux-${TARGETARCH}.tar.gz" \
    | tar -xz -C /usr/local/bin/ nebula nebula-cert && \
    chmod +x /usr/local/bin/nebula /usr/local/bin/nebula-cert

FROM debian:bookworm-slim@sha256:4724b8cc51e33e398f0e2e15e18d5ec2851ff0c2280647e1310bc1642182655d
COPY --from=builder /usr/local/bin/nebula /usr/local/bin/nebula
COPY --from=builder /usr/local/bin/nebula-cert /usr/local/bin/nebula-cert
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
ENTRYPOINT ["/entrypoint.sh"]
