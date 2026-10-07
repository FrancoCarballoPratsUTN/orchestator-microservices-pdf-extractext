# syntax=docker/dockerfile:1

# ---- build stage: compila un binario estático, mínimo y sin cgo ----
FROM golang:1.26.8-alpine AS build

WORKDIR /src

ARG GOARCH=amd64

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=${GOARCH} \
    GOFLAGS=-buildvcs=false \
    go build -trimpath -ldflags="-s -w" -o /out/orchestrator ./cmd/orchestrator

# ---- runtime stage: imagen mínima, no-root, estática (sin shell/librerías extra) ----
FROM alpine:3.20

# ca-certificates no es requerido al compilar sin cgo, pero se mantiene para
# cualquier salida HTTPS futura y para un healthcheck sin dependencias extra.
RUN apk add --no-cache ca-certificates \
    && addgroup -S -g 10001 app \
    && adduser -S -D -H -u 10001 -G app app \
    && mkdir -p /app \
    && chown -R app:app /app

WORKDIR /app

COPY --from=build --chown=app:app /out/orchestrator /app/orchestrator

USER app:app

EXPOSE 8080

LABEL org.opencontainers.image.title="pdf-extractext orchestrator" \
      org.opencontainers.image.description="Orquestador/validador: orquesta Extract, Persistence y AuditLog (chi v5)" \
      org.opencontainers.image.source="https://github.com/universidad/pdf-extractext" \
      org.opencontainers.image.vendor="Universidad" \
      org.opencontainers.image.licenses="MIT"

ENTRYPOINT ["/app/orchestrator"]