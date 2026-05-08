# syntax=docker/dockerfile:1
# Multi-stage build: builder stage compiles the app, runtime stage runs it.

ARG GO_VERSION=1.22
ARG APP_VERSION=latest

FROM golang:${GO_VERSION} AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/server ./cmd/server

FROM ubuntu:22.04 AS runtime
LABEL org.opencontainers.image.version="1.0.0"
LABEL org.opencontainers.image.source="https://github.com/example/app"
ARG TARGETPLATFORM
ENV APP_HOME=/app
ENV LOG_LEVEL=info
COPY --from=builder /app/server /usr/local/bin/server
EXPOSE 8080
EXPOSE 9090
HEALTHCHECK --interval=30s --timeout=10s CMD curl -f http://localhost:8080/health || exit 1
USER nobody
VOLUME ["/data"]
WORKDIR /app
STOPSIGNAL SIGTERM
ENTRYPOINT ["/usr/local/bin/server"]
CMD ["--config", "/app/config.yaml"]
