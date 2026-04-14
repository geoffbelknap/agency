FROM alpine:3.21@sha256:c3f8e73fdb79deaebaa2037150150191b9dcbfba68b4a46d70103204c53f4709
ARG BUILD_ID=unknown
ARG SOURCE_HASH=unknown
RUN apk add --no-cache socat
COPY entrypoint.sh /entrypoint.sh
RUN chmod 0755 /entrypoint.sh
USER nobody
LABEL agency.build.id=${BUILD_ID}
LABEL agency.source.hash=${SOURCE_HASH}
ENTRYPOINT ["/entrypoint.sh"]
