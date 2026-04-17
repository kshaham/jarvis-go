# Deploying jarvis-go

Guidance for running jarvis in real environments. Three supported paths, in
order of complexity: **Docker Compose → Docker → bare-metal systemd**. All
three land you at the same place: a running `jarvis` on port 3555 with
persistent state under `~/.jarvis/` (or a bind-mounted equivalent).

---

## 1. Docker Compose (fastest)

```bash
git clone <your-fork> && cd jarvis-go
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

## 2. Docker standalone

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

## 3. Bare-metal systemd

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

---

## 4. Reverse proxy

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

## 5. Prometheus scrape config

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
- `jarvis_cost_usd_total` — derivative over 1h to detect runaway spending.
- `jarvis_sse_clients` — unusually high/low may indicate connection issues.
- `jarvis_http_requests_total{status=~"5.."}` — 5xx rate.

---

## 6. Backup & restore

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

## 7. Upgrades

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

## 8. Log levels at runtime

`JARVIS_LOG_LEVEL` (`debug`, `info`, `warn`, `error`) and `JARVIS_LOG_FORMAT`
(`text`, `json`) are read at startup. Change them and restart — runtime
toggling is a follow-up feature.

## 9. Troubleshooting

- **`X-Request-ID` header appears in every response** — use this to correlate
  a frontend error to a specific log line. `grep` the journal for the ID.
- **`jarvis_sessions_stopped_total{reason="cost_cap"}` is climbing** — the
  per-session cost cap is firing. Raise `MaxCostUSD` or investigate looping
  agents.
- **SSE disconnects constantly** — check reverse proxy `proxy_read_timeout`
  (must be 24h or larger). Browsers reconnect automatically; the
  `Last-Event-ID` header flushes missed events from the 200-event replay ring.
- **`/metrics` returns 404** — you're on a jarvis build before this commit.
  Upgrade.
- **`/debug/pprof/*` returns 401** — that's correct. Log in first (session
  cookie), then hit the endpoint with the same cookie.
