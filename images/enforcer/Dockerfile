FROM --platform=$BUILDPLATFORM golang:1.26-bookworm AS builder
WORKDIR /build
ARG TARGETOS
ARG TARGETARCH
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
COPY consent ./consent
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags="-s -w" -o /enforcer .

FROM debian:bookworm-slim
ARG BUILD_ID=unknown
ARG SOURCE_HASH=unknown
RUN apt-get update && apt-get install -y --no-install-recommends curl ca-certificates && rm -rf /var/lib/apt/lists/*
COPY --from=builder /enforcer /usr/local/bin/enforcer
EXPOSE 3128 8081
LABEL agency.build.id=${BUILD_ID}
LABEL agency.source.hash=${SOURCE_HASH}
CMD ["/usr/local/bin/enforcer"]
