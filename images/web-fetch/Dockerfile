FROM golang:1.26-alpine@sha256:c2a1f7b2095d046ae14b286b18413a05bb82c9bca9b25fe7ff5efef0f0826166 AS builder
WORKDIR /build
COPY go.mod ./
COPY go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 go build -o /web-fetch .

FROM alpine:3.23@sha256:25109184c71bdad752c8312a8623239686a9a2071e8825f20acb8f2198c3f659
ARG BUILD_ID=dev
ARG SOURCE_HASH=unknown
RUN apk add --no-cache ca-certificates wget
COPY --from=builder /web-fetch /usr/local/bin/web-fetch
EXPOSE 8080
LABEL agency.build.id=${BUILD_ID}
LABEL agency.source.hash=${SOURCE_HASH}
CMD ["web-fetch"]
