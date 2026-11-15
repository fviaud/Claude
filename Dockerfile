# syntax=docker/dockerfile:1

# --- Build stage -----------------------------------------------------------
FROM golang:1.25-alpine AS build
WORKDIR /src

# Download dependencies first so this layer stays cached when only the code
# changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Static binary (no libc needed), without debug symbols or local paths.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/claude ./cmd/claude

# --- Runtime stage ---------------------------------------------------------
# Distroless: no shell or package manager, runs as an unprivileged user.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/claude /claude

ENV ADDR=:8080
EXPOSE 8080
USER nonroot:nonroot

HEALTHCHECK --interval=10s --timeout=5s --start-period=5s --retries=3 \
    CMD ["/claude", "healthcheck"]

ENTRYPOINT ["/claude"]
