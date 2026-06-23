#!/bin/bash
# headless-guard: headless-shell (Chrome) leaks renderer processes over long
# browser-heavy runs, eating RAM AND CPU. Restart it when EITHER memory gets low
# OR the renderer count runs away (catches the CPU-pegging leak that the
# memory-only check missed). Memory + count gated to minimise disruption.
COMPOSE=~/jarvis-go-deploy/docker-compose.cutover.yml
DC=~/docker-compose
LOG=~/jarvis-go-deploy/headless-guard.log
THRESHOLD_MB=2500      # restart if available memory < this
MAX_HS_PROCS=10        # restart if headless-shell process count > this (renderer leak)
CHECK_EVERY=180        # seconds
COOLDOWN=900           # min seconds between restarts
last=0
while true; do
  avail=$(free -m 2>/dev/null | awk '/^Mem:/{print $7}')
  hs=$(ps -eo comm 2>/dev/null | awk '$1=="headless-shell"{n++} END{print n+0}')
  now=$(date +%s)
  reason=""
  [ -n "$avail" ] && [ "$avail" -lt "$THRESHOLD_MB" ] && reason="low memory (${avail}MB)"
  [ "$hs" -gt "$MAX_HS_PROCS" ] && reason="${reason:+$reason + }renderer leak (${hs} procs)"
  if [ -n "$reason" ] && [ $((now-last)) -ge "$COOLDOWN" ]; then
    echo "$(date '+%F %T') restart headless-shell: $reason" >> "$LOG"
    "$DC" -f "$COMPOSE" restart headless-shell >> "$LOG" 2>&1
    last=$now
    echo "$(date '+%F %T') done; avail=$(free -m|awk '/^Mem:/{print $7}')MB hs=$(ps -eo comm|awk '\$1==\"headless-shell\"{n++}END{print n+0}')" >> "$LOG"
  fi
  sleep "$CHECK_EVERY"
done
