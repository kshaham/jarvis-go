#!/bin/bash
# Guarded deploy for the standalone jarvis-go cutover.
# - Aborts on build failure (NEVER silently ships a stale image).
# - Drains the live team before recreate (best-effort) so the container stop
#   reaps cleanly instead of orphaning agent processes.
# - Health-checks the new container; reports failure loudly.
set -euo pipefail

DIR=~/jarvis-go-deploy
COMPOSE="$DIR/docker-compose.cutover.yml"
ENVFILE=~/jarvis-deploy/.env
DC=~/docker-compose
B=http://localhost:3555

cd "$DIR"
echo "[deploy] pulling..."
git pull -q origin main
echo "[deploy] at $(git log --oneline -1)"

# 1) Build FIRST. set -e aborts the whole script if this fails, so a broken
#    build never reaches the recreate step (the stale-image bug).
echo "[deploy] building image..."
"$DC" -f "$COMPOSE" build jarvis
echo "[deploy] build OK"

# 2) Best-effort drain: ask the live team to shut down before we kill the
#    container, so its agent subprocesses are reaped via the app path. Failure
#    here is non-fatal (no team / not logged in); the in-process reaper +
#    stop_grace_period are the backstops.
if [ -f ~/.jarvis_op ]; then
  echo "[deploy] draining team (best-effort)..."
  CJ=$(mktemp)
  PW=$(cat ~/.jarvis_op)
  LB=$(PW="$PW" python3 -c 'import json,os;print(json.dumps({"password":os.environ["PW"]}))')
  curl -s -m 8 -c "$CJ" -X POST "$B/api/auth/login" -H 'Content-Type: application/json' -d "$LB" -o /dev/null || true
  CSRF=$(curl -s -m 8 -b "$CJ" "$B/api/csrf-token" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("token",""))' 2>/dev/null || echo "")
  curl -s -m 15 -b "$CJ" -X POST "$B/api/office/shutdown-team" -H "X-CSRF-Token: $CSRF" -o /dev/null || true
  rm -f "$CJ"
  sleep 4
fi

# 3) Recreate with the freshly-built image.
echo "[deploy] recreating jarvis..."
"$DC" --env-file "$ENVFILE" -f "$COMPOSE" up -d --force-recreate jarvis

# 4) Health gate: poll office-v2 for up to 90s. Fail loudly if it never comes up.
echo "[deploy] health check..."
for i in $(seq 1 30); do
  code=$(curl -s -m 5 -o /dev/null -w "%{http_code}" "$B/office-v2/" 2>/dev/null || echo 000)
  if [ "$code" = "200" ]; then
    echo "[deploy] HEALTHY (office-v2 200 after ${i} checks)"
    exit 0
  fi
  sleep 3
done
echo "[deploy] FAILED: office-v2 never returned 200. Check: $DC -f $COMPOSE logs jarvis" >&2
exit 1
