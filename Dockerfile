# Multi-stage Dockerfile for jarvis-go.
#
# The default "runtime" stage is a minimal debian:12-slim image with `screen`
# installed, which is required for PTY-mode sessions (the common case).
# For SDK-only deployments that never spawn PTY sessions, build with
# --target=runtime-scratch to get a ~20MB scratch image.
#
# Build:   docker build -t jarvis .
# Run:     docker run --rm -v ~/.jarvis:/home/jarvis/.jarvis -p 3555:3555 jarvis

# --- Stage 1: builder ---------------------------------------------------------
FROM golang:1.25-alpine AS builder
WORKDIR /src

# Cache module downloads separately from source changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ENV CGO_ENABLED=0
RUN go build -ldflags="-s -w -X main.version=${VERSION}" -o /out/jarvis      ./cmd/jarvis/      && \
    go build -ldflags="-s -w"                           -o /out/cm          ./cmd/cm/          && \
    go build -ldflags="-s -w"                           -o /out/jarvis-mcp  ./cmd/jarvis-mcp/  && \
    go build -ldflags="-s -w"                           -o /out/ptyserver   ./cmd/ptyserver/

# --- Stage 2: runtime (default -- includes `screen` for PTY mode) ------------
FROM debian:12-slim AS runtime
RUN apt-get update && \
    apt-get install -y --no-install-recommends screen ca-certificates && \
    rm -rf /var/lib/apt/lists/* && \
    groupadd --system --gid 10001 jarvis && \
    useradd  --system --uid 10001 --gid jarvis --home-dir /home/jarvis --create-home jarvis

COPY --from=builder /out/jarvis      /usr/local/bin/jarvis
COPY --from=builder /out/cm          /usr/local/bin/cm
COPY --from=builder /out/jarvis-mcp  /usr/local/bin/jarvis-mcp
COPY --from=builder /out/ptyserver   /usr/local/bin/ptyserver

USER jarvis
WORKDIR /home/jarvis
VOLUME ["/home/jarvis/.jarvis"]
EXPOSE 3555
ENV JARVIS_LOG_LEVEL=info \
    JARVIS_LOG_FORMAT=json
ENTRYPOINT ["/usr/local/bin/jarvis"]
CMD ["web"]

# --- Stage 3: runtime-scratch (SDK-only, no PTY support) ---------------------
FROM gcr.io/distroless/static-debian12:nonroot AS runtime-scratch
COPY --from=builder /out/jarvis      /usr/local/bin/jarvis
COPY --from=builder /out/cm          /usr/local/bin/cm
COPY --from=builder /out/jarvis-mcp  /usr/local/bin/jarvis-mcp
COPY --from=builder /out/ptyserver   /usr/local/bin/ptyserver
USER nonroot:nonroot
WORKDIR /home/nonroot
VOLUME ["/home/nonroot/.jarvis"]
EXPOSE 3555
ENV JARVIS_LOG_LEVEL=info \
    JARVIS_LOG_FORMAT=json
ENTRYPOINT ["/usr/local/bin/jarvis"]
CMD ["web"]
