# --------------------------
# Stage 1: Build the Go binary
# --------------------------
FROM golang:1.25 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -v -trimpath -ldflags="-s -w" -o app ./cmd/reisearch-http

# --------------------------
# Stage 2: Minimal runtime image
# --------------------------
FROM alpine:latest

RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S appgroup && adduser -S appuser -G appgroup

WORKDIR /app

COPY --from=builder --chown=appuser:appgroup /app/app .

USER appuser

EXPOSE 4479
CMD ["./app"]
