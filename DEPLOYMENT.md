# Deploying jarvis-go

Guidance for running jarvis in real environments. Three supported paths, in
order of complexity: **Docker Compose → Docker → bare-metal systemd**. All
three land you at the same place: a running `jarvis` on port 3555 with
persistent state under `~/.jarvis/` (or a bind-mounted equivalent).

---

## ✅ 0. Prerequisites + install bundled skills

### Runtime prerequisites

| Required | Used for |
|---|---|
| Claude Code CLI on PATH | every agent session (SDK + PTY modes both shell to `claude`) |
| `screen` binary | PTY-mode sessions only — install with `apt install screen` / `brew install screen` |
| Writable `~/.jarvis/` (or bind-mounted equivalent) | persistent state |
| Writable `~/.claude/skills/` (or `JARVIS_SKILLS_DIR`) | shared skill library |

| Optional | Used for |
|---|---|
| Node 18+ (`npx` on PATH) | ECC MCP servers (context7, sequential-thinking, shadcn). Skip if you set `disableEcc: true` per team. |
| `codegraph` binary on PATH | extra symbol-graph MCP per agent. Skip if you set `disableCodegraph: true` per team. |
| `chromedp`-compatible Chrome | visual verification flow (`browserEnabled: true` per team). |

### Install bundled skills (one-time, all deployment modes)

The team templates and agent onboarding reference ~54 curated skills
that ship in `./skills/`. Install them into `~/.claude/skills/` (the
location agents resolve at runtime) before starting jarvis:

```bash
./scripts/install-skills.sh              # symlink, skip existing
./scripts/install-skills.sh --copy       # copy (use if jarvis runs in a container that can't follow host symlinks)
./scripts/install-skills.sh --force      # overwrite stale installs
./scripts/install-skills.sh --target /opt/claude/skills   # custom location
```

For Docker / systemd installs run this on the host (or inside the
container) before bringing jarvis up. Skipping is non-fatal but
degrades agent prompt quality.

## 🐳 1. Docker Compose (fastest)

```bash
git clone <your-fork> && cd jarvis-go
./scripts/install-skills.sh
docker compose up -d
open http://127.0.0.1:3555
```

State persists to `./jarvis-data/` in the repo. Back this directory up for
disaster recovery — it holds `state.json`, `scheduler.json`,
`team-templates.json`, `notification-routes.json`, agent memory, and the
SSE event replay buffer isn't persistent so restarts lose a few seconds of
in-flight events (this is expected).

Edit `docker-compose.yml` to:

- Bind-mount your project directories under `/home/jarvis/...` so agents can
  work on them. Match these paths in the `workingDirAllowlist` setting.
- Uncomment the Prometheus sidecar to scrape `/metrics` locally.
- Change the host port binding from `127.0.0.1:3555` to `0.0.0.0:3555` if
  you're going to front it with a reverse proxy (see §4).

---

## 📦 2. Docker standalone

Build once:

```bash
docker build -t jarvis .
```

Two variants are available:

- `--target=runtime` (default) — Debian slim + `screen`, supports PTY-mode sessions.
- `--target=runtime-scratch` — distroless/static, ~20MB, **SDK-only** (no PTY).

Run:

```bash
mkdir -p ~/jarvis-data
docker run --rm -d \
  --name jarvis \
  -p 127.0.0.1:3555:3555 \
  -v ~/jarvis-data:/home/jarvis/.jarvis \
  -v ~/Code:/home/jarvis/Code \
  -e JARVIS_LOG_LEVEL=info \
  -e JARVIS_LOG_FORMAT=json \
  jarvis
```

---

## 🐧 3. Bare-metal systemd

```bash
# 1. Build or fetch the binary
make build                              # produces ./jarvis
sudo install -m 755 ./jarvis /usr/local/bin/jarvis

# Optional: install the companion CLI + MCP server inside agent sessions
make build-cm && make build-mcp
sudo install -m 755 ./cm         /usr/local/bin/cm
sudo install -m 755 ./jarvis-mcp /usr/local/bin/jarvis-mcp

# 2. Create the dedicated user
sudo useradd --system --home /home/jarvis --create-home --shell /usr/sbin/nologin jarvis

# 3. Install the systemd unit
sudo install -m 644 deploy/systemd/jarvis.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now jarvis

# 4. Verify
sudo systemctl status jarvis
curl http://127.0.0.1:3555/api/health
```

Logs live in the journal: `journalctl -u jarvis -f`.

`deploy/systemd/jarvis.service` applies aggressive sandboxing
(`ProtectSystem=strict`, `NoNewPrivileges`, `PrivateTmp`, etc.). If your
workflow needs looser isolation (e.g., agents compiling against system
libraries), selectively relax those directives and document why.

### Optional: build the React island

The classic dashboard at `/office` runs on every build. The newer React
dashboard at `/office-v2` is opt-in — without a build, the route serves
a styled placeholder pointing operators back here.

```bash
cd internal/web/spa
npm install          # one-time
npm run build        # → ../static/public/react/
```

The build output is embedded automatically via the existing
`//go:embed all:public` directive — restart jarvis after building and
`/office-v2` will serve the React shell instead of the placeholder. See
`internal/web/spa/README.md` for the full dev workflow.

### Independent frontend deploys (`OFFICE_SPA_DIR`)

The static asset root is configurable. By default the server serves the
repo-relative `internal/web/static/public/` in dev and the embedded build in
prod. Set `OFFICE_SPA_DIR=/path/to/public` to serve the SPA (and the classic
UI) from a directory **at runtime** instead — this lets the frontend be
rebuilt and redeployed without recompiling or relinking the Go binary.

The container deployment uses this to decouple the two build artifacts:

- The `frontend-dist` Docker stage assembles the full `public/` tree
  (committed classic UI + the freshly built Vite SPA) into a tiny data image.
- `scripts/deploy.sh` copies that tree into the `office-spa` named volume
  (pinned name `jarvis_office_spa`) under a timestamped `rel-*` dir, then
  atomically flips a `current` symlink. The backend mounts the volume
  read-only at `/app/office-public` with `OFFICE_SPA_DIR=/app/office-public/current`.
- The Go `go-build` stage no longer embeds the SPA, so a **frontend-only**
  change rebuilds only the small `frontend-dist` image and repopulates the
  volume — the Go binary's layers stay cached, no relink, no backend restart
  (the server re-reads the dir per request). The binary still embeds the
  **classic** UI as an offline fallback; if the volume is ever missing,
  `/office-v2` degrades to the "bundle not built" placeholder.

`scripts/rollback.sh` flips `current` back to the previous `rel-*` release.
This is a single-origin design: no reverse proxy, no CORS — the Go server
still serves both `/api` and the SPA.

---

## 🔀 4. Reverse proxy

SSE connections are long-lived and need special proxy care.

### Caddy (recommended)

```caddy
jarvis.example.com {
    reverse_proxy 127.0.0.1:3555 {
        flush_interval -1
        transport http {
            read_timeout 24h
        }
    }
}
```

Caddy auto-provisions TLS. `flush_interval -1` disables response buffering so
SSE frames reach browsers immediately. `read_timeout 24h` lets long-lived
connections stay open; without this Caddy closes them hourly.

### nginx

```nginx
server {
    listen 443 ssl http2;
    server_name jarvis.example.com;

    ssl_certificate     /etc/letsencrypt/live/jarvis.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/jarvis.example.com/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:3555;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # SSE-specific settings: disable buffering, extend timeouts.
    location /api/events {
        proxy_pass http://127.0.0.1:3555;
        proxy_http_version 1.1;
        proxy_buffering off;
        proxy_cache off;
        proxy_read_timeout 24h;
        proxy_set_header Connection "";
        proxy_set_header Host $host;
    }

    # Optional: restrict metrics + pprof to trusted networks only.
    location /metrics        { allow 10.0.0.0/8; deny all; proxy_pass http://127.0.0.1:3555; }
    location /debug/pprof/   { allow 10.0.0.0/8; deny all; proxy_pass http://127.0.0.1:3555; }
}
```

### Trusting the proxy

Once you have a reverse proxy in front, configure jarvis to honor
`X-Forwarded-For` from it. Via the settings API (authenticated):

```bash
curl -X PUT http://127.0.0.1:3555/api/settings/accounts \
  -H 'Content-Type: application/json' \
  -H 'Cookie: jarvis_session=...' \
  -d '{"trustedProxyCIDRs":["127.0.0.1/32","::1/128"]}'
```

Restart jarvis after changing this setting. Without it, `X-Forwarded-For`
is ignored and rate-limit identities are the proxy's IP.

---

## 📈 5. Prometheus scrape config

```yaml
scrape_configs:
  - job_name: jarvis
    static_configs:
      - targets: ['127.0.0.1:3555']
    metrics_path: /metrics
    scrape_interval: 15s
```

Relevant series to alert on:

- `jarvis_sessions_active` — operator-defined baseline; alert on unexpected zero.
- `jarvis_sessions_stopped_total{reason="stuck"}` — rate of auto-kill events.
- `jarvis_cost_usd_total` — informational only. Dollar/token cost caps were removed (subscription plans report cost as $0), so this series is for visibility, not enforcement. The binding runaway guardrails are the per-session turn cap, the wedged-session stop, and the perpetual-loop iteration/time hard stops — alert on `jarvis_sessions_stopped_total` instead.
- `jarvis_sse_clients` — unusually high/low may indicate connection issues.
- `jarvis_http_requests_total{status=~"5.."}` — 5xx rate.

---

## 💾 6. Backup & restore

Everything durable lives under `~/.jarvis/` (or your bind-mount equivalent):

- `state.json` + `.backup` — settings, account config, session metadata
- `scheduler.json` + `.backup` — cron jobs
- `team-templates.json` + `.backup` — custom templates
- `notification-routes.json` + `.backup` — notification routes
- `agent-token` — the `cm` CLI token (regenerated if deleted)
- `auth-secret.key` — HMAC signing key for sessions/hooks
- `office-memory/` — per-agent memory
- `screens.json` — session-to-screen mapping

Nightly tar is sufficient:

```bash
tar czf jarvis-backup-$(date +%F).tgz -C ~/ .jarvis
```

Restoration: stop jarvis, replace the directory, start jarvis. The
`.backup` rotation within each file provides an extra safety net against
mid-write corruption.

---

## ⬆️ 7. Upgrades

State files are forward-compatible: running a newer `jarvis` against an
existing `.jarvis/` directory Just Works. Process:

```bash
# systemd
sudo systemctl stop jarvis
sudo install -m 755 ./jarvis.new /usr/local/bin/jarvis
sudo systemctl start jarvis

# docker compose
docker compose pull   # or rebuild
docker compose up -d
```

If you ever need to downgrade, the `.backup` files from before the upgrade
provide a safety net. Read them by setting `mv state.json.backup state.json`
and restarting. This is rarely needed but documented so operators can.

---

## 📜 8. Log levels at runtime

`JARVIS_LOG_LEVEL` (`debug`, `info`, `warn`, `error`) and `JARVIS_LOG_FORMAT`
(`text`, `json`) are read at startup. Change them and restart — runtime
toggling is a follow-up feature.

## 📱 9. Mobile app access

The companion Expo app (`jarvis-mobile`, published as a sibling repo)
authenticates via per-device bearer tokens. The flow:

1. The operator (you) sets the server URL in the app — `http://192.168.x.x:3555`
   for LAN or `https://jarvis.your-domain.com` behind your reverse proxy.
2. The app POSTs the operator password to `/api/auth/mobile/login` with a
   `deviceName`. The server returns a 64-hex token + `deviceId` + `expiresAt`
   (default 90 days). Same per-IP rate limiting as the browser login.
3. The token lives in iOS Keychain / Android Keystore. Every subsequent
   request carries `Authorization: Bearer <token>`. Bearer-auth requests
   are accepted on every existing `/api/office/*` route alongside session
   cookies, and skip CSRF.
4. Revoke a token from the device with `POST /api/auth/mobile/logout`, or
   server-side with `DELETE /api/auth/mobile/tokens/{deviceId}` (e.g. lost
   phone). `GET /api/auth/mobile/tokens` lists registered devices.

Tokens are persisted to `~/.jarvis/mobile-tokens.json` (`0600`, atomic write).
Only the SHA-256 hash of each raw token is stored; the raw value is shown
to the client exactly once at issue time.

When publishing the server to the public internet, terminate TLS at your
reverse proxy (see §4) — mobile clients should never connect over plain
HTTP outside the LAN.

## 🩺 10. Troubleshooting

- **`X-Request-ID` header appears in every response** — use this to correlate
  a frontend error to a specific log line. `grep` the journal for the ID.
- **`jarvis_sessions_stopped_total{reason="stuck"}` is climbing** — sessions
  are hitting the per-session turn cap or the wedged-session hard stop.
  Investigate looping agents, or raise the per-agent turn ceiling at spawn time
  if the work legitimately needs more turns. (Dollar/token cost caps and the
  `reason="cost_cap"` stop were removed — subscription plans report $0 cost, so
  iteration and time hard stops are the guardrail now.)
- **SSE disconnects constantly** — check reverse proxy `proxy_read_timeout`
  (must be 24h or larger). Browsers reconnect automatically; the
  `Last-Event-ID` header flushes missed events from the 200-event replay ring.
- **`/metrics` returns 404** — you're on a jarvis build before this commit.
  Upgrade.
- **`/debug/pprof/*` returns 401** — that's correct. Log in first (session
  cookie), then hit the endpoint with the same cookie.
- **ECC MCPs (context7, sequential-thinking, shadcn) missing from agent sessions** —
  check `which npx`. The Node detection is cached at boot; if you install
  Node after starting jarvis, restart the daemon to pick it up. See
  `docs/ECC.md` for the activation log lines to grep.
- **`/office-v2` shows the "React bundle not built" placeholder** — run
  `cd internal/web/spa && npm install && npm run build`, then restart
  jarvis. The classic UI at `/office` keeps working in the meantime.
