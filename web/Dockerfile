# Stage 1: Build the React app
FROM node:22-alpine AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --ignore-scripts
COPY . .
RUN npm run build

# Stage 2: Serve with nginx (plain alpine to avoid inherited EXPOSE 80)
FROM alpine:3.21

ARG BUILD_ID=unknown
ARG SOURCE_HASH=unknown
LABEL agency.build.id="${BUILD_ID}"
LABEL agency.source.hash="${SOURCE_HASH}"

RUN apk add --no-cache nginx \
    && addgroup -g 101 -S nginx 2>/dev/null || true \
    && adduser -S -D -H -u 101 -h /var/cache/nginx -s /sbin/nologin -G nginx nginx 2>/dev/null || true \
    && mkdir -p /etc/nginx/conf.d /var/cache/nginx \
    && rm -f /etc/nginx/http.d/default.conf

RUN printf 'worker_processes auto;\n\
pid /tmp/nginx.pid;\n\
error_log /dev/stderr warn;\n\
events { worker_connections 1024; }\n\
http {\n\
    include /etc/nginx/mime.types;\n\
    default_type application/octet-stream;\n\
    log_format agency escape=json '"'"'{"ts":"$time_iso8601","level":"info","component":"web","msg":"access","method":"$request_method","path":"$uri","status":$status,"duration_s":$request_time,"bytes":$bytes_sent,"remote":"$remote_addr"}'"'"';\n\
    access_log /dev/stdout agency;\n\
    sendfile on;\n\
    keepalive_timeout 65;\n\
    client_body_temp_path /tmp/client_temp;\n\
    proxy_temp_path /tmp/proxy_temp;\n\
    fastcgi_temp_path /tmp/fastcgi_temp;\n\
    uwsgi_temp_path /tmp/uwsgi_temp;\n\
    scgi_temp_path /tmp/scgi_temp;\n\
    include /etc/nginx/conf.d/*.conf;\n\
}\n' > /etc/nginx/nginx.conf

COPY nginx.conf /etc/nginx/conf.d/agency-web.conf
COPY --from=build /app/dist /usr/share/nginx/html
COPY agency-entrypoint.sh /agency-entrypoint.sh

USER nginx
EXPOSE 8280

HEALTHCHECK --interval=5s --timeout=2s --start-period=2s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:8280/health || exit 1

ENTRYPOINT ["/agency-entrypoint.sh"]
