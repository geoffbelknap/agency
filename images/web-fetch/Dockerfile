FROM golang:1.26-alpine AS builder
ARG BUILD_ID=dev
WORKDIR /build
COPY go.mod ./
COPY go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 go build -ldflags "-X main.buildID=${BUILD_ID}" -o /web-fetch .

FROM alpine:3.23
ARG BUILD_ID=dev
LABEL agency.build.id=${BUILD_ID}
RUN apk add --no-cache ca-certificates wget
COPY --from=builder /web-fetch /usr/local/bin/web-fetch
EXPOSE 8080
CMD ["web-fetch"]
