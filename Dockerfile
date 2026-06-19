# Multi-stage Dockerfile for jarvis-go.
#
# Targets:
#   runtime-prod    (DEFAULT) full production image -- Go binaries + Node + the
#                   mockup-render / browser-screenshot / deliverable-save
#                   wrappers + screen (PTY) + headroom + expect-cli. This is what
#                   the deployed jarvis container runs.
#   runtime-scratch SDK-only ~90MB distroless image (no PTY/node/mockups).
#
# BuildKit cache mounts make rebuilds incremental: the Go build cache, Go module
# cache, and npm cache all persist across `docker build` invocations on the same
# daemon, so a warm rebuild only recompiles changed packages.
#
# Build:   docker build -t jarvis .                 # -> runtime-prod
#          docker build --target runtime-scratch -t jarvis-sdk .
# Run:     docker run --rm -v ~/.jarvis:/home/jarvis/.jarvis -p 3555:3555 jarvis

# --- Stage 0: build the React SPA (/office-v2) so go:embed picks up the
# react bundle. internal/web/static/public/react/ is gitignored, so it is NOT
# in the build context and must be produced here before the Go compile.
FROM node:22-alpine AS spa-build
WORKDIR /spa
COPY internal/web/spa/package.json internal/web/spa/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci
COPY internal/web/spa/ ./
# vite outDir is ../static/public/react (relative to /spa) -> /static/public/react
RUN npm run build

# --- Stage 1: builder ---------------------------------------------------------
FROM golang:1.25.11-alpine AS builder
WORKDIR /src

# Cache module downloads separately from source changes.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
# Graft the built SPA into the embed path so `//go:embed all:public` includes
# the /react bundle (the dir is gitignored, hence absent from `COPY . .`).
COPY --from=spa-build /static/public/react ./internal/web/static/public/react

ARG VERSION=dev
ENV CGO_ENABLED=0
# Cache mounts: /root/.cache/go-build (compile cache) + /go/pkg/mod (modules)
# persist across builds, so a warm rebuild only recompiles changed packages.
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    go build -ldflags="-s -w -X main.version=${VERSION}" -o /out/jarvis      ./cmd/jarvis/      && \
    go build -ldflags="-s -w"                           -o /out/cm          ./cmd/cm/          && \
    go build -ldflags="-s -w"                           -o /out/jarvis-mcp  ./cmd/jarvis-mcp/  && \
    go build -ldflags="-s -w"                           -o /out/ptyserver   ./cmd/ptyserver/

# --- Stage 2: runtime-base ----------------------------------------------------
# All heavy, code-INDEPENDENT system installs live here in one cached layer set,
# so a code change never re-runs apt/npm/pip. Binaries + scripts are grafted on
# top in runtime-prod (the cheap, frequently-rebuilt layers).
FROM debian:12-slim AS runtime-base
ENV DEBIAN_FRONTEND=noninteractive

# Base tools: screen (PTY sessions), ca-certificates, plus curl/gnupg for the
# NodeSource setup. Node 22 is required by the mockup-render / browser-screenshot
# / deliverable-save agent helpers.
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        screen ca-certificates curl gnupg \
        python3 python3-venv && \
    curl -fsSL https://deb.nodesource.com/setup_22.x | bash - && \
    apt-get install -y --no-install-recommends nodejs && \
    rm -rf /var/lib/apt/lists/* && \
    groupadd --system --gid 10001 jarvis && \
    useradd  --system --uid 10001 --gid jarvis --home-dir /home/jarvis --create-home jarvis

# Headroom context-compression CLI (https://github.com/chopratejas/headroom) in
# an isolated venv (avoids debian-12 PEP-668). Only [mcp,code] extras; the heavy
# [ml] extra is omitted. Code is feature-gated on `headroom` presence
# (internal/office/headroom.go), so it degrades gracefully if removed.
RUN python3 -m venv /opt/headroom && \
    /opt/headroom/bin/pip install --no-cache-dir "headroom-ai[mcp,code]" && \
    ln -s /opt/headroom/bin/headroom /usr/local/bin/headroom && \
    /usr/local/bin/headroom --version

# Global Node deps for the agent helpers: puppeteer-core (browser-screenshot
# connects to a sibling chromedp/headless-shell over CDP) and expect-cli
# (terminal-automation helper invoked by the office).
RUN npm install -g puppeteer-core@23.0.0 expect-cli@latest 2>&1 | tail -20

# Agent backends the office SPAWNS. Without these the office cannot create ANY
# agent session (the SDK + PTY paths both exec `claude`; Codex teams exec
# `codex`). claude installs to /root/.local/bin; copy it onto the system PATH so
# any runtime uid finds it. Pin >= 2.1.154 via CLAUDE_VERSION for Opus 4.8 +
# Dynamic Workflows.
ARG CLAUDE_VERSION=latest
RUN curl -fsSL https://claude.ai/install.sh | bash -s -- "$CLAUDE_VERSION" && \
    cp /root/.local/bin/claude /usr/local/bin/claude && \
    /usr/local/bin/claude --version
ARG CODEX_VERSION=latest
RUN npm install -g @openai/codex@"$CODEX_VERSION" && codex --version

# The deployed container runs under a compose `user:` override (uid != the image
# jarvis uid), so make the home writable for the agent CLIs' dotfiles
# (~/.claude.json, ~/.config) and seed Claude's onboarding bypass so headless
# `claude -p` never hits the interactive first-run gate.
RUN chmod 777 /home/jarvis && \
    python3 -c "import json; json.dump({'hasCompletedOnboarding': True, 'lastOnboardingVersion': '2.1.74', 'installMethod': 'native', 'numStartups': 10, 'projects': {}}, open('/home/jarvis/.claude.json', 'w'), indent=2)" && \
    chmod 666 /home/jarvis/.claude.json

# --- Stage 3: runtime-scratch (SDK-only, no PTY/node) -------------------------
# Kept for SDK-only deployments that never spawn PTY sessions / mockups.
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

# --- Stage 4: runtime-prod (DEFAULT) -----------------------------------------
# Full production image. Only the cheap binary + script COPY layers rebuild on a
# code change; the heavy runtime-base layers stay cached.
FROM runtime-base AS runtime-prod
COPY --from=builder /out/jarvis      /usr/local/bin/jarvis
COPY --from=builder /out/cm          /usr/local/bin/cm
COPY --from=builder /out/jarvis-mcp  /usr/local/bin/jarvis-mcp
COPY --from=builder /out/ptyserver   /usr/local/bin/ptyserver

# Agent node helpers + PATH wrappers. NODE_PATH lets the wrappers resolve the
# globally-installed puppeteer-core (browser-screenshot requires it; the others
# use only built-ins but the export is harmless).
COPY scripts/mockup-render.mjs      /usr/local/bin/mockup-render.mjs
COPY scripts/browser-screenshot.mjs /usr/local/bin/browser-screenshot.mjs
COPY scripts/deliverable-save.mjs   /usr/local/bin/deliverable-save.mjs
RUN for s in mockup-render browser-screenshot deliverable-save; do \
        printf '#!/bin/sh\nexport NODE_PATH="$(npm root -g)"\nexec node /usr/local/bin/%s.mjs "$@"\n' "$s" > /usr/local/bin/$s && \
        chmod +x /usr/local/bin/$s; \
    done

USER jarvis
WORKDIR /home/jarvis
VOLUME ["/home/jarvis/.jarvis"]
EXPOSE 3555 3556
ENV JARVIS_LOG_LEVEL=info \
    JARVIS_LOG_FORMAT=json \
    EXPECT_BROWSER_WS_ENDPOINT=ws://headless-shell:9222 \
    HEADLESS_BROWSER_HOST=headless-shell \
    HEADLESS_BROWSER_PORT=9222
ENTRYPOINT ["/usr/local/bin/jarvis"]
CMD ["web"]
