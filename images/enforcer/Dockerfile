FROM --platform=$BUILDPLATFORM golang:1.26-bookworm@sha256:4f4ab2c90005e7e63cb631f0b4427f05422f241622ee3ec4727cc5febbf83e34 AS builder
WORKDIR /build
ARG TARGETOS
ARG TARGETARCH
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
COPY consent ./consent
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags="-s -w" -o /enforcer .

FROM debian:bookworm-slim@sha256:4724b8cc51e33e398f0e2e15e18d5ec2851ff0c2280647e1310bc1642182655d
ARG BUILD_ID=unknown
ARG SOURCE_HASH=unknown
RUN apt-get update && apt-get install -y --no-install-recommends curl ca-certificates && rm -rf /var/lib/apt/lists/*
COPY --from=builder /enforcer /usr/local/bin/enforcer
EXPOSE 3128 8081
LABEL agency.build.id=${BUILD_ID}
LABEL agency.source.hash=${SOURCE_HASH}
CMD ["/usr/local/bin/enforcer"]
