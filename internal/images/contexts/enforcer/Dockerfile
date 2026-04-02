FROM golang:1.26-bookworm AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG BUILD_ID=unknown
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w -X main.buildID=${BUILD_ID}" -o /enforcer .

FROM debian:bookworm-slim
ARG BUILD_ID=unknown
LABEL agency.build.id=${BUILD_ID}
RUN apt-get update && apt-get install -y --no-install-recommends curl ca-certificates && rm -rf /var/lib/apt/lists/*
COPY --from=builder /enforcer /usr/local/bin/enforcer
EXPOSE 3128 8081
CMD ["/usr/local/bin/enforcer"]
