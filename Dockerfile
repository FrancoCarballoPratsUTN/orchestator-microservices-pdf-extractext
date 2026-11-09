# syntax=docker/dockerfile:1

# ---- build stage: binario estático, sin cgo ----
FROM golang:1.26.10-bookworm AS build

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

# ---- runtime stage: imagen mínima, sin shell/gestor de paquetes ----
FROM gcr.io/distroless/static-debian13:nonroot AS production

WORKDIR /app

COPY --from=build /out/orchestrator /app/orchestrator

EXPOSE 8080

USER nonroot:nonroot

ENTRYPOINT ["/app/orchestrator"]
