FROM alpine:3.21
RUN apk add --no-cache socat
USER nobody
ENTRYPOINT ["socat", "TCP-LISTEN:8200,fork,reuseaddr", "TCP:gateway:8200"]
