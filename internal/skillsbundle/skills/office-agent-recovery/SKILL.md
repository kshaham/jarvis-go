---
name: office-agent-recovery
description: The Guardian protocol -- how a watching agent keeps the team alive by reading the roster, detecting stuck (no recent activity) or exited (missing from roster) peers, and respawning them with `cm agent restart <name>`. Use when you are the designated Guardian, or when you notice a teammate has gone silent and is not coming back on their own.
---

# Office Agent Recovery (Guardian protocol)

You are the **Guardian**: the agent responsible for keeping teammates alive. Agents die or stall -- a crash, a wedged tool call, an exited session. The team only stays productive if someone watches the roster and brings the dead back. That is your job.

Every command here runs via `Bash` (the `cm` binary is on your PATH; `$JARVIS_API_URL` and `$JARVIS_SESSION_ID` are already set). Never use raw `curl`.

## 1. Read the roster

```
cm agent health
```

This prints one row per peer: `NAME  STATUS  LAST ACTIVITY`. It is the same roster `cm roster` returns, formatted for a health glance. Use it on a loop -- once every few minutes is plenty.

## 2. Classify each peer

- **Healthy** -- present in the roster, recent activity. Leave it alone.
- **Stuck** -- present in the roster but no recent activity (stale `LAST ACTIVITY`, or `STATUS` shows idle/wedged). It is alive but not making progress.
- **Exited** -- a teammate you expect is **missing from the roster entirely**. Its session ended.

When in doubt, prefer waiting one more cycle over restarting. A peer mid-build can look idle for a minute. Only act on a peer that has been stale across **two** consecutive checks.

## 3. Restart a dead or wedged peer

```
cm agent restart <name>
```

`<name>` is the display name from the roster (not the sessionId). The server resolves the name to that peer's config and respawns the agent. The peer comes back with its memory intact, so it can resume where it left off.

## 4. Backoff etiquette -- do not spam restarts

Restarting is disruptive: it tears down and rebuilds a session. Respect a backoff:

- **One restart, then wait.** After a restart, give the peer at least one full health cycle (a few minutes) to come back and start working before considering another.
- **Escalating backoff.** If the same peer needs a second restart, double the wait. After ~3 failed restarts of the same peer in a row, **stop** -- the problem is not transient. Post a factual `cm message` to the team lead and let a human decide.
- **Never restart a healthy peer**, and never restart more than one peer per cycle unless several are clearly exited at once.

## 5. Announce what you did

Recovery is invisible unless you say so. Whenever you act, post a short factual note so the team (and the lead) know a peer was bounced:

```
cm message <leadSessionId> "Guardian: restarted <name> (stale 8m, no commits). Watching for recovery."
```

Get the lead's sessionId from `cm roster`. Keep it factual -- what you observed, what you did, what you are watching for. No speculation, no apology.

## Quick loop

```
cm agent health                       # who is alive
# classify: healthy / stuck / exited
cm agent restart <name>               # only a confirmed-stale or missing peer
cm message <leadSessionId> "Guardian: restarted <name> (<reason>)."
# wait one cycle, re-check, apply backoff
```
