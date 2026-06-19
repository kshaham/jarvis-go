# Multi-stage Dockerfile for jarvis-go.
#
# The default "runtime" stage is a minimal debian:12-slim image with `screen`
# installed, which is required for PTY-mode sessions (the common case).
# For SDK-only deployments that never spawn PTY sessions, build with
# --target=runtime-scratch to get a ~20MB scratch image.
#
# Build:   docker build -t jarvis .
# Run:     docker run --rm -v ~/.jarvis:/home/jarvis/.jarvis -p 3555:3555 jarvis

# --- Stage 0: build the React SPA (/office-v2) so go:embed picks up the
# react bundle. internal/web/static/public/react/ is gitignored, so it is NOT
# in the build context and must be produced here before the Go compile.
FROM node:22-alpine AS spa-build
WORKDIR /spa
COPY internal/web/spa/package.json internal/web/spa/package-lock.json ./
RUN npm ci
COPY internal/web/spa/ ./
# vite outDir is ../static/public/react (relative to /spa) -> /static/public/react
RUN npm run build

# --- Stage 1: builder ---------------------------------------------------------
FROM golang:1.25.11-alpine AS builder
WORKDIR /src

# Cache module downloads separately from source changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Graft the built SPA into the embed path so `//go:embed all:public` includes
# the /react bundle (the dir is gitignored, hence absent from `COPY . .`).
COPY --from=spa-build /static/public/react ./internal/web/static/public/react

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

# Headroom context-compression CLI (https://github.com/chopratejas/headroom).
# Installed into an isolated venv (avoids debian-12 PEP-668 externally-managed
# breakage) and symlinked onto PATH so `headroom` is visible to the jarvis user.
# Only the [mcp,code] extras are bundled (MCP server + AST-aware code/JSON
# structural compressors). The heavy [ml] extra (torch + the Kompress text
# model, ~GB) is intentionally omitted to keep the image small; the code is
# feature-gated on `headroom` presence (internal/office/headroom.go), so the
# integration degrades gracefully if an operator drops this layer.
RUN apt-get update && \
    apt-get install -y --no-install-recommends python3 python3-venv && \
    python3 -m venv /opt/headroom && \
    /opt/headroom/bin/pip install --no-cache-dir "headroom-ai[mcp,code]" && \
    ln -s /opt/headroom/bin/headroom /usr/local/bin/headroom && \
    rm -rf /var/lib/apt/lists/* && \
    /usr/local/bin/headroom --version

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

# --- Stage 3: runtime-browser (runtime + Node + scripts for visual verification) --
# Extends the default runtime with Node.js + puppeteer-core + the
# repo's browser-screenshot.mjs helper so agents can take screenshots
# against a sibling chromedp/headless-shell container. Build with
# --target=runtime-browser and pair with the `headless-shell` service
# in docker-compose.yml (commented-out profile). R34 · Aligns the
# repo's default image with what the production jarvis container
# provides; without this stage `browser-screenshot` is a phantom
# command in a clone.
FROM runtime AS runtime-browser
USER root
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        ca-certificates curl gnupg && \
    curl -fsSL https://deb.nodesource.com/setup_22.x | bash - && \
    apt-get install -y --no-install-recommends nodejs && \
    rm -rf /var/lib/apt/lists/*
# Install puppeteer-core globally so browser-screenshot.mjs can
# resolve it. Pinned to a major for reproducibility; update on
# review.
RUN npm install -g puppeteer-core@23.0.0 2>&1 | tail -20
# Copy the repo script + wrapper to a canonical location on PATH.
COPY scripts/browser-screenshot.mjs /usr/local/bin/browser-screenshot.mjs
RUN printf '#!/bin/sh\nexec /usr/bin/node /usr/local/bin/browser-screenshot.mjs "$@"\n' > /usr/local/bin/browser-screenshot && \
    chmod +x /usr/local/bin/browser-screenshot && \
    printf '#!/bin/sh\necho "expect-cli is not installed in this image. The production jarvis container ships a custom expect-cli binary; this stage only ships browser-screenshot. See DEPLOYMENT.md." >&2\nexit 2\n' > /usr/local/bin/expect-cli && \
    chmod +x /usr/local/bin/expect-cli
# Default env vars for the sibling browser container -- override
# in compose if your headless-shell lives elsewhere.
ENV EXPECT_BROWSER_WS_ENDPOINT=ws://headless-shell:9222 \
    HEADLESS_BROWSER_HOST=headless-shell \
    HEADLESS_BROWSER_PORT=9222
USER jarvis
ENTRYPOINT ["/usr/local/bin/jarvis"]
CMD ["web"]

# --- Stage 4: runtime-scratch (SDK-only, no PTY support) ---------------------
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
