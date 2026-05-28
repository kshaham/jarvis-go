---
name: docker-compose-debug
description: Troubleshooting the jarvis + headless-shell compose stack -- docker compose exec, logs, restart, port, volume inspection, rebuild workflow, network inspection, and the "headless-shell is only reachable from inside the compose network" gotcha. Read when the stack misbehaves or when you need to peek inside a running container.
---

# Docker Compose Debug (jarvis stack)

The jarvis docker-compose stack has exactly two services: `jarvis` (the Go server) and `headless-shell` (chromedp/headless-shell for browser automation). When something goes wrong -- a flaky login, a browser-screenshot failure, a stuck container -- this skill is the debugging playbook.

## Cardinal rule

> Don't guess. Use `docker compose logs -f`, `docker compose exec`, and `docker network inspect` to see what's actually happening. Every assumption about the network, volumes, or env vars can be verified in under 30 seconds.

## The stack shape

Two services in `docker-compose.yml`:
- **`jarvis`**: built from local Dockerfile, exposes 3555 (HTTP) + 3556 (HTTPS) on the host, mounts `~/.jarvis` + `./workspace`, env vars `JARVIS_HTTPS=1`, `HEADLESS_BROWSER_HOST=headless-shell`.
- **`headless-shell`**: `chromedp/headless-shell:stable`, `shm_size: 2gb`, **no host port** -- reachable only from inside the compose network at `headless-shell:9222`.

Two critical facts:
1. **`headless-shell` has no host port.** Curl from your laptop fails. Curl from `docker compose exec jarvis` works.
2. **`jarvis depends_on headless-shell`.** If headless-shell is stopped, jarvis's `browser-screenshot` calls fail. Restart both together, not just one.

## The 6 commands you actually need

All commands run from wherever the `docker-compose.yml` lives.

### 1. `docker compose ps` -- what's actually running

```bash
docker compose ps
```

Columns: name, state (`Up`, `Exit`, `Restarting`), ports, health. If a service says `Restarting`, it's crashing in a loop -- `logs` next.

### 2. `docker compose logs` -- what happened

```bash
docker compose logs -f jarvis            # follow live
docker compose logs --tail=200 jarvis    # last 200 lines
docker compose logs --since=10m jarvis   # last 10 minutes
```

Use `-f` for live tailing, `--tail` for forensics, `--since` for "what happened around when the user reported the issue."

For multi-service debugging, drop the service name to tail everything:
```bash
docker compose logs -f
```

Prefix each line tells you which service it came from.

### 3. `docker compose exec` -- go inside the container

```bash
docker compose exec jarvis bash
```

This is the single most useful command. From inside:

```bash
ls /home/jarvis/.claude/skills   # confirm new skills are installed
cat /home/jarvis/.jarvis/state.json | jq .    # inspect live state
getent hosts headless-shell        # confirm DNS resolves
curl -s http://$(getent hosts headless-shell | awk '{print $1}'):9222/json/version | jq  # verify CDP
jarvis-go version
cm --help
ps aux | grep claude               # are agent processes running?
```

`exec` drops you into a new shell **inside the running container** -- no restart, no side effects. It's the fastest path from "something's broken" to "I can see what's wrong."

For one-off commands without a full shell:

```bash
docker compose exec jarvis cat /home/jarvis/.jarvis/state.json
docker compose exec jarvis ls /home/jarvis/.claude/skills/
```

### 4. `docker compose restart` -- kick it

```bash
docker compose restart jarvis                    # restart just jarvis
docker compose restart jarvis headless-shell     # restart both (order matters)
```

`restart` is faster than `up -d` because it doesn't re-read the compose file or rebuild images -- it just stops and starts the existing containers. Use this when:

- A config file in a mounted volume changed and you want to pick it up.
- A transient network hiccup left a service confused.
- You need to flush in-memory state without losing persistent data.

Do NOT use `restart` to pick up code changes -- that needs a rebuild.

### 5. `docker compose build` + `up -d` -- rebuild and redeploy

```bash
docker compose build jarvis
docker compose up -d jarvis
```

When to use:
- You pulled new Go code from git.
- You changed the Dockerfile.
- You added a new skill file (to pick it up without waiting for the scheduled deploy).

The multi-stage Dockerfile caches the apt, npm, and Go layers. A skill-only rebuild hits the `COPY skills/` layer and everything after it, which is fast (<30s typically).

`up -d` creates/starts the container in detached mode. Existing running containers with the same image don't restart; changed ones do.

### 6. `docker network inspect` -- "why can't X reach Y?"

```bash
docker network ls                        # find the compose network name
docker network inspect claudeman_default # or whatever it's called
```

This shows you which containers are on the compose network, their IPs, and the network driver. Useful when:

- Compose hostnames aren't resolving (`headless-shell` can't be found).
- Two containers can't talk to each other and you suspect a network partition.
- You need to confirm which IP `headless-shell` is actually on.

## Volume inspection

The jarvis service mounts several host directories into the container:

```bash
docker compose exec jarvis ls -la /home/jarvis/.jarvis/
docker compose exec jarvis ls -la /app/workspace/
docker compose exec jarvis ls -la /home/jarvis/.claude/skills/ | head -20
```

If changes on the host aren't visible in the container, the mount may have failed silently. Check `docker compose ps` -- a container in `Restarting` because of a broken volume often shows a clear error in `logs`.

## The headless-shell gotcha

**From your laptop:** `curl http://localhost:9222/json/version` -> connection refused. There's no host port.

**From inside the jarvis container:** `curl http://headless-shell:9222/json/version` -> **also fails**, because chromedp/headless-shell has DNS-rebinding protection that rejects Host headers that aren't IP or "localhost."

**The working recipe** (already used by `browser-screenshot`):
```bash
ip=$(getent hosts headless-shell | awk '{print $1}')
curl -s http://$ip:9222/json/version | jq
```

Resolve the hostname to an IP, then hit the IP. The `headless-browser` skill covers the full pattern for puppeteer scripts.

## The first-look checklist

When something's broken, run these in order until you find the problem:

```bash
# 1. Is the service even running?
docker compose ps

# 2. Is it crashing or restarting?
docker compose logs --tail=100 jarvis | grep -iE 'error|panic|failed'

# 3. Can you reach it from inside?
docker compose exec jarvis curl -s http://localhost:3555/api/health

# 4. Can jarvis reach headless-shell?
docker compose exec jarvis bash -c 'ip=$(getent hosts headless-shell | awk "{print \$1}"); curl -s http://$ip:9222/json/version | head -c 200'

# 5. Are the mounted volumes populated as expected?
docker compose exec jarvis ls /home/jarvis/.jarvis/
docker compose exec jarvis ls /home/jarvis/.claude/skills/ | head

# 6. Does a fresh restart help?
docker compose restart jarvis
```

90% of real issues turn up in the first three steps.

## Anti-patterns

- **`docker compose down`** as a first step. It stops AND removes containers, losing in-memory state. Use `restart` instead.
- **`docker kill`** outside of compose. Compose won't clean up the state and you'll get "container already exists" on the next up.
- **`docker compose up` without `-d`**. Runs in foreground, blocks your shell, and exits take the container down.
- **Reading logs with `docker logs`** directly. Fine, but loses the compose service prefix -- prefer `docker compose logs`.
- **`docker compose exec jarvis vi file`** to edit files live. Those edits don't survive a rebuild. Edit on the host or in git, then rebuild.
- **Running `docker compose build` without `--no-cache`** when the cache is stale. Usually fine, but if a dependency install is mysteriously not picking up, `--no-cache` is the escape hatch.
- **Poking at `headless-shell`** from outside the compose network. You can't. That's by design.

## Verification

1. `docker compose ps` -- both services `Up`.
2. `docker compose exec jarvis curl -s http://localhost:3555/api/health | jq` -- returns `{"ok":true}` or equivalent.
3. `docker compose exec jarvis browser-screenshot http://jarvis:3555/login.html /tmp/t.png && ls -l /tmp/t.png` -- non-zero file, exit 0.
4. `docker compose logs --tail=50 jarvis` -- no panics or repeating errors.

If all four pass, the stack is healthy.
