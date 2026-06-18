---
name: team-dev-instance
description: Run one dev instance of the app the team is building so every agent shares the same running URL for browser verification. Use when your team is building or improving a fullstack/backend/frontend web app. The lead starts the dev server in the team's working directory, binds it to 0.0.0.0, and announces it via `cm dev announce http://jarvis:<port>`. Teammates discover the URL with `cm dev url` and browser-verify against the same instance using `browser-screenshot`, `expect-cli`, or custom puppeteer scripts.
---

# Team Dev Instance

When the team is building a web app — Next.js, Vite/React, Go HTTP server, Flask, whatever — **one** dev server should run, announced centrally, so every teammate points their headless-shell browser at the same URL. Without this, agents either skip browser verification entirely, start competing servers that race for port 3000, or screenshot the wrong URL.

## When to use

Use this skill when:

- You are the **team lead** (strategic tier) AND the team's mission involves a web app
- You are a **frontend, backend, devops, or QA** agent about to run browser verification
- A task asks you to "verify the feature in the browser" or similar

Do **not** use this skill when:

- The team is not building a web-renderable app (pure CLI tool, library, data pipeline)
- Verification can be done with unit tests + API-level assertions alone
- The lead hasn't announced a URL yet AND you can't ask them (fall back to the Jarvis office URL itself for dashboard-flavored verification)

## Architecture you need to know

Jarvis and the headless-shell browser run as sibling containers in the same docker-compose network. A dev server you start inside the jarvis container on port `P` is reachable from headless-shell as:

```
http://jarvis:<P>
```

**NOT** `http://localhost:<P>`. The `headless-shell` container's localhost points at its own network namespace, not yours. Every announced URL must use `jarvis` as the host.

## Rule 1 — bind to 0.0.0.0

Most dev servers default to binding `127.0.0.1` only. That means they accept connections *inside the jarvis container only*. The headless-shell sibling container gets connection refused. You must bind to `0.0.0.0`.

Per-framework invocations that actually work:

| Framework | Correct command |
|---|---|
| **Vite** (React/Vue/Svelte) | `npm run dev -- --host 0.0.0.0 --port 3001` |
| **Next.js** | `npm run dev -- -H 0.0.0.0 -p 3001` or `HOSTNAME=0.0.0.0 PORT=3001 npm run dev` |
| **Create React App** | `HOST=0.0.0.0 PORT=3001 npm start` |
| **Astro** | `npm run dev -- --host 0.0.0.0 --port 3001` |
| **Go HTTP server** | Bind `":3001"` (not `"localhost:3001"`) in `http.ListenAndServe` |
| **Express / Node** | `app.listen(3001, '0.0.0.0', ...)` (not just `app.listen(3001)`) |
| **Flask** | `flask run --host 0.0.0.0 --port 3001` |
| **Django** | `python manage.py runserver 0.0.0.0:3001` |
| **Rails** | `bin/rails s -b 0.0.0.0 -p 3001` |
| **Vanilla static** | `python3 -m http.server --bind 0.0.0.0 3001` |

## Rule 2 — pick a free port

Start with 3001 and increment if busy. Check before launching:

```bash
ss -tlnp 2>/dev/null | grep ":300[0-9]" || echo "3000-range free"
```

Skip these ports (reserved by Jarvis itself): **3555** (HTTP), **3556** (HTTPS). Also skip **9222** (headless-shell CDP).

Deterministic enough heuristic: try `3001` first; on conflict try `3002`, `3003`, … up to `3009`. If all are busy, something is wrong — stop and ask the team.

## Rule 3 — background the server

Leave it running in the background while you continue work:

```bash
cd /path/to/app
nohup npm run dev -- --host 0.0.0.0 --port 3001 > /tmp/dev-server.log 2>&1 &
echo $! > /tmp/dev-server.pid
```

You get back a PID you can `kill` later. The `/tmp/dev-server.log` catches errors so you can debug a crashed server without losing the output.

Wait for the server to come up before announcing:

```bash
for i in 1 2 3 4 5 6 7 8 9 10; do
  if curl -sf http://127.0.0.1:3001 > /dev/null 2>&1; then
    echo "up"; break
  fi
  sleep 1
done
```

## Rule 4 — announce the URL with the PID

```bash
cm dev announce http://jarvis:3001 --pid $!
```

Now every teammate agent can run `cm dev url` and get `http://jarvis:3001` back. The URL is persisted in office state and broadcast over SSE, so the UI dashboard also shows it.

**Always pass `--pid`** when you can. If you die (your session crashes) or the team wraps up, Jarvis reads the PID and sends `SIGTERM` to the dev server automatically — no orphan process racing the next team's ports. The PID must be >= 100 (Jarvis refuses to signal low-numbered PIDs as a safety measure against killing PID 1 / system processes).

Announcing a localhost URL (`http://localhost:3001`) is **rejected** by default because headless-shell can't reach it. If you know what you're doing, pass `--force` — but almost always the right fix is to rebind your dev server to `0.0.0.0` and re-announce.

## Rule 5 — share login credentials if needed

If your dev instance requires login (admin pages, auth-gated flows), agents can't browser-verify past the login screen without knowing the credentials. Set a free-text blob that teammates can read:

```bash
cm dev creds set "admin@dev.local / password123"
```

Multi-line is fine:

```bash
cm dev creds set "admin@dev.local / admin123
user@dev.local / user123
login page at /auth/signin"
```

Teammates read with `cm dev creds` (no args). The blob is stored server-side at mode 0600 and never broadcast over SSE (only a boolean "set" flag is broadcast). These are dev throwaway credentials, not production secrets — don't put anything sensitive here.

Per-slot creds for fullstack teams: `cm dev creds set "..." --name frontend` and `cm dev creds set "..." --name api` keep the creds scoped to each slot.

## Rule 6 — use named slots for fullstack teams

When a team needs **multiple dev servers** (e.g. a frontend app + a backend API), announce them under distinct slot names:

```bash
# Frontend dev server
nohup npm run dev -- --host 0.0.0.0 --port 3001 > /tmp/fe.log 2>&1 &
cm dev announce http://jarvis:3001 --name frontend --pid $!

# Backend API
nohup go run ./cmd/server > /tmp/api.log 2>&1 &
cm dev announce http://jarvis:8080 --name api --pid $!
```

Teammates pick the right slot:

```bash
cm dev url frontend    # → http://jarvis:3001
cm dev url api         # → http://jarvis:8080
cm dev list            # JSON dump of all slots with health + PID
```

Port conflicts across slots return `409` — if you try to announce two slots on the same port, Jarvis rejects the second. Pick a different port or clear the prior slot first.

Single-app teams keep the original simple flow: `cm dev announce <url>` fills the default `"app"` slot; `cm dev url` (no arg) returns it. No migration needed.

## Rule 7 — Expo / React-Native web

For Expo teams, use the **web flavor** to get a browser-screenshottable build of the app:

```bash
# In your Expo project
expo start --web --host 0.0.0.0 --port 19006

# Or, if expo-router is configured:
npx expo start --web --host 0.0.0.0 --port 19006

# Then announce
cm dev announce http://jarvis:19006 --name frontend --pid $!
```

Expo web covers **most RN components** that render to DOM (Text, View, ScrollView, Image, Button, styled components via StyleSheet, React Navigation). It **does NOT** cover truly-native APIs: Haptics, NativeModules, platform-specific camera/audio, deep-link handlers. If your task depends on those, the web build will silently render nothing or throw — verify in the browser first, then flag the native-only gap to the lead.

Expo's dev server is slow to boot (~30s for the first build; ~10s warm). Extend your startup wait loop accordingly:

```bash
for i in {1..40}; do
  if curl -sf "http://127.0.0.1:19006" > /dev/null 2>&1; then break; fi
  sleep 1
done
```

The native bundler URL (`exp://...`) requires the Expo Go app on a physical device and is **out of scope** for agent verification — stick to web.

## Rule 8 — verify from a fresh browser, always

The announce itself proves nothing. Immediately follow every announce (and every change) with:

```bash
browser-screenshot http://jarvis:3001 /tmp/dev-home.png
```

Then use the Read tool to look at `/tmp/dev-home.png`. If you see the expected home page, the setup is correct and teammates can rely on it. If you see a blank/error screenshot, either the server isn't up (check `/tmp/dev-server.log`) or the bind is wrong (check it's on `0.0.0.0` not `127.0.0.1`).

**STRONG RULE for every agent (not just the lead):** if your task title mentions UI / frontend / page / button / modal / layout / styling / css / design / screenshot / visual / Expo / React-Native, your `cm task complete --summary` MUST cite the browser verification. Example:

> "Updated the sign-up flow copy. Screenshotted at `/tmp/signup.png` — confirms the new headline, disabled submit state, and the trust badges render in the sidebar."

A summary for a UI-keyword task that says only "implemented X" triggers a dashboard warning (`office:uiTaskMissingScreenshot` SSE event) and gets flagged in the office activity feed. The task still completes — this is a discipline signal, not a hard block. But the flag is visible to everyone, and persistent slippage is a pattern teammates will call you on during standup.

## Full startup sequence (lead agent)

```bash
# 1. Move to the app directory
cd /app/workspace/your-project

# 2. Install if needed (one-time)
npm install

# 3. Find a free port
PORT=3001
while ss -tlnp 2>/dev/null | grep -q ":$PORT "; do
  PORT=$((PORT + 1))
done
echo "Using port $PORT"

# 4. Start in the background, bound to 0.0.0.0 — capture the PID
nohup npm run dev -- --host 0.0.0.0 --port "$PORT" > /tmp/dev-server.log 2>&1 &
DEV_PID=$!
echo "$DEV_PID" > /tmp/dev-server.pid

# 5. Wait for it
for i in 1 2 3 4 5 6 7 8 9 10; do
  if curl -sf "http://127.0.0.1:$PORT" > /dev/null 2>&1; then break; fi
  sleep 1
done

# 6. Announce the URL + PID so Jarvis can clean up if you die
cm dev announce "http://jarvis:$PORT" --pid "$DEV_PID"

# 7. (If the app needs login) share credentials with teammates
# cm dev creds set "admin@dev.local / password123"

# 8. Sanity-screenshot to prove it's live
browser-screenshot "http://jarvis:$PORT" /tmp/dev-home.png
# (then Read /tmp/dev-home.png)
```

## Consuming the URL (any agent)

```bash
# Get the URL
DEV_URL=$(cm dev url)
if [ -z "$DEV_URL" ]; then
  echo "No dev server announced yet. Ask the lead, or fall back to the Jarvis office URL."
  exit 1
fi

# Screenshot after your change
browser-screenshot "$DEV_URL" /tmp/after-my-change.png

# Then Read /tmp/after-my-change.png and confirm it shows your change.
```

If the dev instance has login, pull the credentials the lead set:

```bash
CREDS=$(cm dev creds)
echo "$CREDS"
# e.g.
#   admin@dev.local / password123
#   user@dev.local / user123
```

Then thread those into your expect-cli or puppeteer script:

```bash
expect-cli -m "Open $DEV_URL/admin, log in with admin@dev.local / password123, assert the user list is visible" -y
```

Exit code 0 = pass, 1 = fail.

### Stale URL protection

If `cm dev url` returns a URL but the dev server has crashed, Jarvis runs a background health check that pings the URL on each monitoring tick. After 3 consecutive failures (connection refused or 5xx), the URL is auto-cleared and an `office:devInstanceCleared` SSE event fires. The next `cm dev url` returns empty, so you know to nudge the lead to re-announce.

4xx responses (including 401/403 for auth-gated paths) are treated as **healthy** — they prove the server is alive, just gatekept.

## Teardown (lead agent, on wrap-up)

If you passed `--pid` when announcing, `cm dev clear` does everything:

```bash
cm dev clear
# Clears URL + creds AND SIGTERMs the tracked PID (with SIGKILL fallback after 3s)
```

If you forgot `--pid`, kill it manually:

```bash
if [ -f /tmp/dev-server.pid ]; then
  kill "$(cat /tmp/dev-server.pid)" 2>/dev/null
  rm -f /tmp/dev-server.pid
fi
cm dev clear
```

**Automatic teardown:** Jarvis also calls the clear path on:

- **Team wrap-up / shutdown** — the next `/api/office/shutdown-team` call triggers the cleanup
- **Lead death without a successor** — if your session dies and no other strategic-tier lead is alive, Jarvis SIGTERMs the tracked PID automatically

So forgetting to clear is less catastrophic than it used to be. But it's still best practice to clear explicitly when you know the team is done.

## Troubleshooting

**`browser-screenshot http://jarvis:3001` times out** → dev server not bound to `0.0.0.0`. Kill it, restart with the `--host 0.0.0.0` flag. Verify with `curl -v http://jarvis:3001/` from *inside the jarvis container*.

**`browser-screenshot` works from jarvis but returns blank PNG** → SPA hasn't rendered by the time the screenshot fires. Use a custom puppeteer script with `await page.waitForSelector(...)` or `waitUntil: 'networkidle2'`. See `cm skill read headless-browser` for a complete template.

**`cm dev url` returns stale URL pointing at dead server** → lead forgot to clear on last wrap-up. Safe to re-announce: the new `cm dev announce` overwrites.

**Port conflict on 3001** → another team's dev server. Pick 3002, 3003, … The checker in the startup sequence above handles this.

**localhost muscle memory** → if you typed `cm dev announce http://localhost:3001`, you'll get a warning. Re-announce with `http://jarvis:3001`. Don't push the `localhost` form to teammates.

## Verification checklist

Before marking the "set up dev instance" task complete:

- [ ] Dev server is running and bound to `0.0.0.0` (verify: `ss -tlnp | grep $PORT` shows `0.0.0.0:$PORT`)
- [ ] `curl http://127.0.0.1:$PORT` returns a 2xx response from inside the jarvis container
- [ ] `cm dev url` returns `http://jarvis:$PORT`
- [ ] PID was passed to announce (`cm dev url` output + `cat /tmp/dev-server.pid` agree)
- [ ] If the app needs login: `cm dev creds` returns a non-empty blob teammates can use
- [ ] `browser-screenshot http://jarvis:$PORT /tmp/proof.png` produced a non-empty, non-blank image
- [ ] You Read `/tmp/proof.png` and it shows the expected home page
