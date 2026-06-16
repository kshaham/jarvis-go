# Jarvis -- The Business Explainer

> A plain-English guide to what Jarvis is, what it does, and why it matters.
> No jargon. No engineering required.

---

## At a Glance

**Jarvis is a virtual software engineering team that works around the clock on your codebase.** Instead of hiring one AI assistant that you have to babysit, you deploy a self-organizing team of AI engineers -- each with a role, personality, and specialty -- who coordinate through meetings, share knowledge, and continuously improve your code without human supervision. They learn from their work, remember what they have done, and hand off tasks to each other just like a real team would.

You can pause the whole team with one click when a deploy window opens. You can restart a single agent without losing its memory when its context gets bloated. You can save a snapshot of the team on Friday and resume it on Monday. You can redirect an agent to a new part of the codebase without respawning anyone. And while it all runs, the dashboard flags anything that looks wrong -- an agent repeating itself, a session that hung, a background task that crashed -- so you see problems before they become incidents.

---

## Table of Contents

1. [The Problem](#1-the-problem)
2. [What Jarvis Is](#2-what-jarvis-is)
3. [The Virtual Office](#3-the-virtual-office)
4. [How the Team Works](#4-how-the-team-works)
5. [The Learning System](#5-the-learning-system)
6. [Never-Idle Autonomy](#6-never-idle-autonomy)
7. [Smart Context Management](#7-smart-context-management)
8. [Safety and Control](#8-safety-and-control)
9. [Visibility and Monitoring](#9-visibility-and-monitoring)
10. [Flexibility](#10-flexibility)
11. [Automation and Scheduling](#11-automation-and-scheduling)
12. [Typical Use Cases](#12-typical-use-cases)
13. [Business Value](#13-business-value)
14. [What Jarvis Is Not](#14-what-jarvis-is-not)
15. [Getting Started](#15-getting-started)
16. [Security and Reliability](#16-security-and-reliability)
17. [Frequently Asked Questions](#17-frequently-asked-questions)
18. [Glossary](#18-glossary)

---

## 1. The Problem

Software engineering is expensive, slow, and bottlenecked by human attention. Even with AI coding assistants, a human still has to sit in front of a screen and guide the assistant through every task. When the human stops, the work stops.

Today's AI coding tools share three big limitations:

**They need a babysitter.** You hire an AI assistant, give it one task, and then you have to review its work, correct its mistakes, and give it the next task. The AI does not run itself.

**They forget.** After a conversation ends, the AI has no memory of what it learned. Every new task starts from scratch. Knowledge built up during hours of work is discarded.

**They work alone.** Real codebases are complex and benefit from specialists -- a backend engineer, a frontend engineer, a quality assurance engineer, a designer. A single AI assistant cannot be all of these at once. And when it tries, it loses context and makes contradictory decisions.

On top of that, **AI assistants are hard to supervise at scale.** Without the right guardrails, one stuck agent can loop forever, waste a quota, or leave the rest of the team idle while it spins on a broken tool call. Traditional tooling gives you almost no visibility into when things are going wrong -- so you either sit there watching or come back to a mess.

The result: most teams use AI for brief interactive sessions, then close the window. They never unlock the real power of 24/7 autonomous engineering.

**Jarvis solves all of this.**

---

## 2. What Jarvis Is

Jarvis is a platform that runs **a team of AI software engineers** on your codebase. You choose a team template (or build your own), spawn the team, and the team gets to work. Each team member is a separate AI agent with its own identity, role, and personality. They communicate with each other, coordinate tasks, share knowledge, and keep working indefinitely.

You can think of it like this:

| Traditional AI Coding | Jarvis |
|---|---|
| One AI, one human | A team of AIs, optional human oversight |
| Forgets after every session | Remembers projects across weeks and months |
| Works only when you do | Works 24 hours a day, 7 days a week |
| Reads every file (expensive) | Queries code structure (5-10x cheaper) |
| Burns out after long tasks | Self-recovers from problems automatically |
| No role specialization | Backend, frontend, QA, DevOps, architects |
| Starts from scratch each time | Learns from past work, shares across team |
| Binary on/off | Pause, resume, snapshot, restart, redirect |
| Hope it isn't stuck | Dashboard flags stuck agents, loops, and crashes |

Jarvis runs as a web application. You open a browser, click "Spawn Team," watch the dashboard, and let the team work. Or close the browser and check back in the morning.

---

## 3. The Virtual Office

The best way to understand Jarvis is through an office metaphor. When you deploy a team, you are staffing a virtual office.

### Hiring a Team

Jarvis ships with **seven prebuilt team templates (plus a one-click Demo Jarvis team)** you can deploy with one click:

| Template | Size | What it does |
|---|---|---|
| **Office (Default)** | 7 members | A classic full-stack team: an Engineering Manager, a Senior Architect, a UI/UX Designer, a Frontend Engineer, a Backend Engineer, a DevOps Engineer, and a QA Engineer |
| **Backend / AI** | 6 members | Focused on AI and data work: an AI Engineering Lead, an ML Engineer, a Prompt Engineer, a Data Engineer, an API Engineer, and an AI Quality Evaluator |
| **Frontend Studio** | 6 members | Design-heavy team: a Design Lead, a Frontend Architect, a UI Engineer, an Interaction Engineer, an Accessibility Specialist, and a Performance Engineer |
| **Full-Stack Compact** | 5 members | A lean shipping team: Tech Lead, Frontend, Backend, QA/Security, and DevOps |
| **Research Lab** | 7 members | For investigation and proof-of-concept work: a Research Director, a Deep Researcher, a Technical Analyst, a POC Developer, a Presentation Specialist, a Brand Strategist, and a Documentation Writer |
| **Innovation Sprint** | 6 members | Rapid ideation and validation: an Innovation Lead, a Market Researcher, a Prototype Engineer, a Visual Storyteller, a Technical Writer, and a Validation Analyst |
| **Core Builders** | 6 members | A product-led full-stack team: a Product Manager who maintains the codebase wiki, an internal technical scout/QA, an external framework scout/QA, a UI/UX Designer, a Senior Full-Stack Engineer, and a Frontend Engineer |

On top of the seven templates, a one-click **Demo Jarvis** team spawns a tiny 3-member crew (lead + designer + engineer) that runs the full office workflow for real -- standups, whiteboard, tasks, chats, meetings, knowledge, and deliverables -- so every dashboard screen fills with genuine data for a first look.

You can also build custom teams by picking roles, assigning personalities, and choosing which AI models each member uses.

### Spawning a Team

The spawn flow is a workspace-sized modal with three focused tabs instead of one long scroll:

- **Team tab** -- team mission, working directory (existing folder or a fresh project that Jarvis creates and seeds with a `CLAUDE.md`), perpetual or goal-focused mode, default AI provider/model with an "Apply to all" shortcut, optional Telegram updates.
- **Members tab** -- a master-detail layout with a roster rail on the left and a full editor on the right. Rename, retier, recolor, set emoji, pick a provider and model, write a personality and system prompt, add skills, override the working directory, set a per-agent turn ceiling, and set a per-agent spawn delay so a large team can stagger its authentication without tripping rate limits. Hover any roster row to duplicate it (handy for cloning a worker you want two of) or remove it. The rail ends with a dashed "+ Add member" button so you can grow a team beyond its template before spawning.
- **Review tab** -- a summary grid and per-member thumbnails that read live values from the editor, so you can sanity-check the full configuration in one view before clicking "Spawn."

The modal is accessible at every tab -- keyboard-navigable, screen-reader friendly, and responsive down to narrow viewports where the split collapses into a single column.

### Every Team Member Is Unique

Just like in a real company, every agent has:

- **A name** (e.g., "Captain," "Nova," "Dash," "Shield")
- **A role** (e.g., Tech Lead, Frontend Engineer, QA Specialist)
- **A tier** -- either *strategic* (a leader who plans and delegates) or *worker* (who executes tasks)
- **A personality** -- a multi-paragraph character description that shapes how the agent communicates
- **A skill set** -- the specific competencies they bring
- **A color and icon** -- for visual identification in the dashboard
- **A turn ceiling** -- the maximum number of turns their AI session can run before Jarvis stops it, as a runaway safety net
- **A spawn delay** (optional) -- a pause between when this agent starts and when the next one does, for rate-limit smoothing on big teams

Team members reference each other by name. Workers know who their leaders are. Leaders know their workers' strengths. This is not just for show -- it drives how tasks get routed and how problems get escalated.

### A Day in the Life

When a Jarvis team is spawned, here is what happens:

1. **Onboarding.** Each new team member receives a detailed "kickstart" briefing that explains the team's mission, who the other team members are, the tools available, past learnings from previous sessions, and what their specific role is.

2. **Morning standup.** Every few hours, the team holds an automatic standup. Each member reports what they are working on and any blockers. The leader reviews responses and assigns new tasks.

3. **Independent work.** Team members pick up tasks from their personal queue, announce they are starting (preventing duplicate work), execute the task, and report when done.

4. **Brainstorming.** When a team member runs out of work, the system triggers a brainstorming session with a teammate. They propose new tasks together, and the proposals automatically become real tasks in the queue.

5. **File coordination.** Before editing a shared file, a team member "locks" it so no one else can modify it at the same time. When they finish, they release the lock.

6. **Messaging.** Team members can send direct messages to each other, ask questions, and request help.

7. **Structured handoffs.** Mid-task, a team member can explicitly hand work to a named teammate with a short context payload ("here is what I found, this is why it's their specialty"). The receiver sees the incoming handoff in their task queue and must accept or reject. Accept moves the task (same ID, same history) to the new owner in in-progress state. Reject sends the task back to the sender along with a chat message explaining why. Every handoff is audited -- the dashboard shows pending handoffs, accept/reject counts per agent, and an activity-feed entry for each transition. Operators can force-resolve a stuck handoff when the receiver is unresponsive.

8. **Reflection.** After completing a task, each member reflects on what they learned and adds it to their personal knowledge base.

9. **Wrap-up.** When you call "end of day," the team finishes what they are working on, commits their code, saves their memory, and shuts down cleanly.

---

## 4. How the Team Works

### Task Management

Every team uses a **5-column kanban board** (the same kind used in Jira, Trello, or Linear):

```
Backlog  →  To Do  →  In Progress  →  Review  →  Done
```

Tasks flow through these columns as team members pick them up, work on them, and complete them. You can watch the board update in real time as agents check in.

Each task has:
- A **title** and **description**
- A **priority** (high / medium / low)
- A **source** (assigned by a leader, self-generated, or a bug report)
- **Dependencies** (other tasks that must finish first)
- A **status history** showing every state change

### Leader Routing

Strategic-tier team members (the "leaders") are responsible for high-level planning. When a standup ends, the leader receives the full status of the team and decides what should happen next. They can:

- Assign new tasks to specific team members
- Escalate blocked tasks to other team members who might have the skills to unblock them
- Adjust priorities based on what they are learning

Workers focus on execution. They do not try to plan the whole project -- they take direction from leaders.

### Task Triage

When a team member gets stuck on a task, they can mark it as "blocked" with a reason. After a few attempts, the system automatically routes the task to a leader who can reassign it to someone better suited. This means problems do not stall the whole team.

### File Locking

A common problem in multi-agent systems: two agents try to edit the same file at the same time, creating conflicts. Jarvis solves this with a distributed locking system. Before editing a shared file, an agent acquires a lock. Other agents see the lock and work on different files instead. Locks automatically expire after 30 minutes in case an agent forgets to release one. When an agent is restarted in place (see section 8), its locks migrate to the new session so no one loses exclusivity on files they were editing.

### Bug Tracking

The QA-role team member scans the codebase for bugs and files them as bug tasks. Bug tasks appear in the queue for other team members to fix. When a fix is committed, the bug is marked resolved. If the agent assigned to a bug task goes offline, the background sweeper clears the assignment so the dashboard does not keep pointing at a dead session.

### Water-Cooler Chat

Occasionally, two team members have a brief informal exchange -- just like coworkers chatting by the water cooler. These interactions are rare and light, but they help the team maintain social context and catch coordination issues that formal meetings miss.

### Team Performance Scoring

Every agent earns an ongoing performance grade -- **A, B, C, D, or F** -- based on four dimensions:

- **Completion rate** -- how often assigned tasks are finished successfully
- **Speed** -- how long tasks take to move from assigned to done
- **Quality** -- how many follow-up bug tasks are filed against their work
- **Collaboration** -- how often they respond to standups and teammates' messages

Grades update continuously and are visible on each agent card in the dashboard. They are not punitive -- they are a diagnostic tool. When the system detects a significant gap between the team's top and bottom performers, it automatically injects optimization hints into the underperforming agents' next briefings, nudging them toward patterns that are working for their teammates.

In addition, every team run produces an **experiment record** capturing that session's outcomes: task count, completion rate, top and bottom performers, and any notable events. These records accumulate into an append-only trend log. Over weeks of operation, you can see whether your team is actually getting better -- and, if not, which agents or roles are the limiting factor.

### Shared Team Knowledge Search

Every agent can search the team's combined knowledge in one query: memory files, learnings, standup transcripts, and the whiteboard all come back in a single recency-ranked result list. If Dash the frontend engineer needs to know what the team decided about authentication last Tuesday, one command answers -- no hunting through sessions, no re-asking a teammate who may be in the middle of a turn.

### Output Compression

Agent shell commands like `git`, `go build`, `docker`, and `pnpm` often produce verbose output that burns context for no benefit (progress bars, timestamp noise, repeated "unchanged" lines). Jarvis compresses those outputs automatically -- typically a 60–90% reduction -- while preserving errors and the parts the agent actually needs to reason about. Compression is fully transparent to the agent and falls back to raw output when the compressor is uncertain.

### Automated Code Review on Completion

Any task labelled `needs-review` triggers an adversarial code review when the agent marks it complete. The review runs as a separate Claude session against the diff and reports back as a structured set of findings (severity, file, line, description) that show up in the task detail view and in the activity feed. Use it for high-stakes changes where you want a second pair of eyes before merging, without breaking the agent's flow to ask for one.

---

## 5. The Learning System

Most AI tools forget everything the moment a conversation ends. Jarvis agents remember.

### Per-Agent Memory

Every team member has a **persistent memory file** that survives restarts, team respawns, and project changes. When a team member completes work and wraps up, they save a summary of what they did, what decisions they made, and what is left to do. Next time they are spawned, that memory is loaded into their briefing so they pick up exactly where they left off.

This means you can:
- Run a team for two hours, stop them, and resume a week later with no loss of context
- Switch team members in and out without losing institutional knowledge
- Build up deep project understanding that compounds over time
- Restart a single agent's AI session when it gets bloated, without losing a thing

### Reflection After Work

After completing a task, agents are prompted to reflect on what they learned. They record a learning entry in one of several categories:

- **Technique** -- a reusable approach that worked well
- **Pattern** -- a recurring shape of problem or solution
- **Pitfall** -- something to avoid next time
- **Tool usage** -- a new way to use an existing tool
- **Codebase insight** -- something specific about this project
- **Collaboration** -- a lesson about working with teammates

Each reflection becomes a searchable entry in the agent's personal learning store, and meaningful ones are auto-appended to the agent's memory file so the lesson outlives the session.

### Insight Distillation

When a team member has accumulated 30+ reflections, the system automatically processes the oldest ones and condenses them into a smaller number of "distilled insights" -- short, high-level wisdom statements. This prevents the learning store from growing unboundedly while preserving the important lessons.

Think of it like a senior engineer's intuition: they cannot recall every bug they fixed, but they have a mental library of patterns that guides their decisions. Jarvis builds the same kind of library for each team member.

### Shared Skill Library

When a team member discovers a particularly reusable technique, they can create a **shared skill** -- a named, searchable entry that any team member can read. For example, one agent might create a skill called "optimistic UI updates" explaining a pattern that works well in the project.

Other team members can browse and search the skill library. Over time, the team builds up a playbook of proven approaches that new members inherit.

### Cross-Team Knowledge Sharing

When building each team member's briefing, Jarvis pulls recent learnings from every other team member and includes them in a "Teammate Insights" section. This means discoveries made by one agent are visible to all others, without anyone having to manually share them.

If Dash the frontend engineer figures out how to handle a tricky CSS bug, Stack the backend engineer sees it in tomorrow's briefing -- even though they never directly communicated about it.

### The Mission Evolves

Your project's mission statement is not static. As the team discovers important context, they can append insights to the mission. Over time, the mission becomes a living document that captures the accumulated understanding of the team.

```
Original mission: "Build a real-time chat application"
After a week: "Build a real-time chat application
  + [Learned] WebSocket heartbeats are required for NAT traversal
  + [Learned] Message deduplication is needed after reconnect
  + [Learned] Users expect 100ms feedback on keystrokes"
```

New team members joining later inherit the enriched mission automatically.

---

## 6. Never-Idle Autonomy

One of Jarvis's most powerful features is **perpetual mode** -- agents that never stop working.

### The Problem with Traditional AI

Hire a typical AI coding assistant and tell it "fix the login bug." The assistant fixes the bug, says "done," and stops. Now what? You have to give it the next task. And the next. And the next.

The human is the bottleneck.

### The RALPH Loop

In perpetual mode, Jarvis agents run a continuous improvement loop called **RALPH** (Recognize, Assess, Log, Plan, Handle). After every completed task, they:

1. **Recognize** -- check if there are more tasks in their queue
2. **Assess** -- if the queue is empty, scan the codebase for improvements
3. **Log** -- record what they learned from the just-finished task
4. **Plan** -- generate 2-3 new tasks based on what they found
5. **Handle** -- pick the highest-priority new task and start working

This loop runs forever. Agents always have something to do -- unless you pause the team (see section 8), which halts new work without killing the agents themselves.

### What Agents Find On Their Own

When an agent genuinely runs out of work, it scans the codebase for things to improve:

- **TODO and FIXME comments** in the code -- fix them
- **Code without tests** -- write tests
- **Recently changed files** -- review for bugs
- **Documentation gaps** -- write or improve docs
- **Performance hotspots** -- optimize them
- **Code smells** -- refactor for readability
- **Failing tests** -- fix them
- **Best practices from their specialty** -- apply them

A frontend engineer might audit the UI for accessibility issues. A backend engineer might look for N+1 database queries. A QA engineer might write property-based tests for untested modules.

### Business Value

This is where Jarvis delivers its biggest impact. Imagine having an engineering team that:

- **Never takes a coffee break**
- **Never goes home at 5 PM**
- **Never runs out of things to do**
- **Never waits for Jira assignments**
- **Always looks for improvements to make**

Every hour your project exists, measurable improvements are being made to it. Over a week, the cumulative impact is substantial: dozens of tests written, bugs fixed, documentation improved, code refactored -- all without anyone managing the team.

### Optional: Goal-Focused Mode

If you prefer a more traditional "complete these tasks, then stop" model, you can disable perpetual mode at spawn time. In goal-focused mode, agents finish their assigned work, report completion, and wait for new instructions. This is useful for narrowly-scoped sprints where you do not want the team to invent extra work.

---

## 7. Smart Context Management

### The Hidden Cost of AI Coding

When an AI agent reads a file, it consumes "context" -- roughly equivalent to the amount of text it is holding in its head at once. Every line of code it reads costs either money or a quota credit, depending on your plan.

A single agent working on a real codebase for eight hours can easily chew through a massive amount of context just reading the same files over and over to understand the code structure.

### The Solution: Code Knowledge Graph

Jarvis integrates a tool called **CodeGraphContext** that builds a "map" of your codebase as a searchable graph. Instead of reading entire files to understand the code, agents query the graph:

- "Who calls this function?" → instant answer, no file reads
- "What does this class inherit from?" → instant answer
- "What other code is similar to this?" → instant answer
- "What depends on this module?" → instant answer

The graph is built automatically when the team is spawned. It updates as the code changes.

### Before and After

| Task | Without knowledge graph | With knowledge graph |
|---|---|---|
| "Understand how login works" | Read 2000 lines across 10 files | Query 5 function relationships |
| "Find all places that use this function" | Search every file in the repo | Single graph query |
| "What does this class do?" | Read the class and its parents | Query class hierarchy |

**Real-world impact:** In internal testing, the knowledge graph reduced context consumption by **5 to 10 times** on exploration tasks. On pay-per-token plans, that is the difference between a $200/day team and a $20–40/day team. On subscription plans, it is the difference between hitting your monthly quota in three days or lasting the whole month.

### Why This Matters

- **Pay-per-token plans** (direct Anthropic API, etc.): lower context = lower dollar cost.
- **Subscription plans** (Claude subscription, Z.AI Coding Plan): lower context = more turns per day against your quota.
- **All plans**: shorter effective prompts = faster responses, so the team finishes tasks sooner regardless of price model.

The knowledge graph is free and runs locally. There is no downside.

### Context-Compression Layers

The knowledge graph cuts how much an agent has to read in the first place. Two optional compression layers cut how much of what it does read stays in context:

- **Headroom** -- an optional MCP that compresses the bulky stuff agents accumulate over a long run: tool outputs, logs, and earlier conversation. When the binary is on PATH, agents get `mcp__headroom__*` tools and a nudge to pass large blobs (dependency trees, search results, multi-thousand-line logs) through the compressor before pasting them into their reasoning -- typically a 60-95% shrink, and reversible if a detail is needed later.
- **rtk** -- bash-output compression wired in as a `PreToolUse` hook. It automatically trims verbose command output (`git`, `go build`, `docker`, test runners, `cat`, and the like) by 60-90% before it ever reaches the agent, falling back to raw output when it is uncertain. An agent can prefix a single command with `rtk --raw` when it needs the unfiltered stream.

Both are opt-in and degrade gracefully -- if the binary is not installed, the agent simply runs without that layer.

---

## 8. Safety and Control

Autonomy is powerful, but only if you can trust it. Jarvis is designed with safety controls at every layer.

### You Can Always Stop the Team

At any moment, you can:

- **Pause the entire team** -- freeze background automation (no new standups, no idle replenishment, no stuck-agent nudges) while keeping every agent alive with all memory and tasks intact. Resume with one click when you are ready.
- **Kill a single agent** -- surgical stop for a runaway individual. The rest of the team keeps working and the dashboard reassigns the stopped agent's pending tasks.
- **Inject an operator prompt** to one agent -- send a corrective nudge ("stop debating the color and commit what you have") without killing the accumulated context.
- **Restart an agent in place** -- attach a fresh AI session to the same agent while keeping their memory, learnings, task queue, whiteboard, and file locks. Use this when an agent's context is bloated but their accumulated knowledge is worth keeping.
- **Graceful wrap-up** -- tell the team to finish current work, commit, save memory, and exit cleanly (takes a few minutes).
- **Hard shutdown** -- immediately stop every agent.
- **Force unlock files** -- release any lock that is blocking other work.

Access to these controls is guarded by an optional password login with built-in rate limiting -- repeated failed attempts slow down exponentially to defeat brute-force attacks. An operator can also rotate the dashboard password at any time without restarting the server.

### Team Lifecycle Controls

Beyond the stop-and-start buttons, Jarvis exposes a full lifecycle surface so the team is not a binary on/off:

- **Pause and Resume** -- pause the team for a deploy window, a client meeting, or while you review a questionable change. Agents do not receive new prompts, the auto-standup clock stops, the recovery nudges stop, and the idle-replenishment loop stops. When you resume, everything picks up where it left off. An amber banner at the top of the dashboard shows the pause state + your optional reason so nobody forgets why the team is frozen.
- **Team Snapshots** -- save a named snapshot of the team's configuration, mission, member list, per-member tasks, whiteboard notes, perpetual-mode state, and pause state to a single file. Restore it later, even after a full shutdown or server restart, and the team spawns back with the same shape. Memory and learnings are already on disk keyed by member role, so they rehydrate automatically -- the agents truly resume, they do not start over. This is how Friday afternoon pairings become Monday morning continuations.
- **In-Place Agent Restart** -- swap a fresh AI session onto an agent without losing anything that matters. The agent's memory, learnings, scores, task queue, whiteboard, and file locks carry over. The restart takes seconds. Use it when an agent's context window fills up with irrelevant history and you want them starting each thought with a clean slate but all their project knowledge intact.
- **Working Directory Bump** -- redirect one agent to a different part of the codebase mid-run. The agent keeps working but focuses on a new subdirectory. Useful when priorities shift (move a QA agent from `/api` to `/frontend`) without spawning anyone new.
- **Fork-Mode Snapshot Restore** -- restoring a snapshot in fork mode keeps the original team running and spawns a parallel team that resumes each agent's Claude transcript via `claude --fork-session`. Useful when a teammate is stuck on a hard problem: leave them on the original thread, fork a copy to experiment with a different approach, and merge the winner back. Members without a captured session ID start fresh in the fork; the rest pick up exactly where they left off.
- **Tool Policy** -- a per-team allow/deny matrix enforced before every tool call. The default policy blocks the obvious (`rm -rf /`, `sudo`, `curl | sh`, writes to `/etc` / `/usr` / `~/.ssh/`). Operators can tighten or loosen it from the Settings page. The check fires inside Claude's PreToolUse hook, so a denied call returns a structured rejection to the agent rather than silently dropping work.

### Guardrails Against Runaway Agents

Jarvis uses quota-agnostic defenses -- the kind that work whether you are on a pay-per-token plan or a subscription. (Dollar and token cost caps existed in earlier versions but were removed: they are meaningless on subscription plans, which is what most deployments use. The binding guardrails are now the iteration and time hard stops, the objective verify gate, and the task-category blocklist described below.)

Perpetual mode is disciplined by loop-engineering, so a self-running agent cannot quietly drift into trouble:

- **Objective verify gate.** A self-asserted "done" cannot override a failing test suite. Before a perpetual-loop task is allowed to close, Jarvis runs an objective verify command (an auto-detected build/test/lint command, or one you configure) against the working tree. The loop advances only when the gate passes -- the agent does not get to grade its own homework.
- **Iteration and wall-clock hard stops.** A perpetual loop is capped at a maximum number of self-replenishment cycles (default 10) and, optionally, a maximum wall-clock lifetime. When either cap is crossed, the loop pauses for human review rather than charging ahead indefinitely.
- **Committed-secret scan.** Before replenishing an idle agent with fresh self-generated work, a lightweight secret scan runs over the working tree. Any committed-secret finding halts replenishment and surfaces a dashboard alert, so the team cannot keep building on top of a leaked credential.
- **Task-category blocklist.** Self-added tasks that fall into risky categories -- authentication, payment, deploy, migration, dependency-upgrade, architecture-rewrite -- are rejected. Loops belong on small, machine-checkable work, not on irreversible judgment calls, so these are routed to a human instead of auto-generated.

On top of the loop discipline, Jarvis backs the session itself with runtime safety nets:

- **Per-session turn ceiling.** Every AI session has a maximum number of turns it is allowed to take, with a sensible default of 100. Cross that ceiling and the session is stopped automatically, an event fires to the dashboard, and the agent's pending tasks get reassigned. This is the primary backstop against an agent running the same loop 200 times.
- **Wedged-session hard stop.** If an agent's AI session reports itself as "working" for longer than the configured wall-clock window (default 20 minutes) without producing any tool activity or turn events, Jarvis assumes the subprocess is hung and stops it. The dashboard shows a toast so you know cleanup happened.
- **Text-emission loop advisory.** If an agent emits the same block of text three or more times in a short span, an orange "loop?" pill appears on the agent card. It is an advisory, not an auto-stop -- legitimate repetition is possible, so you decide whether to intervene. Clears on the next turn boundary.
- **Tool-level loop advisory.** If an agent runs the same tool with the same arguments producing the same output three or more times in a short span, a red "⟲ Bash" (or whatever tool) pill appears on the agent card. This signal is much stronger than the text-level one -- it is almost always a real loop -- so the operator can usually act immediately with the kill or inject button.
- **Panic recovery.** If a background process inside Jarvis crashes (a brainstorm orchestrator, the auto-standup scheduler, the idle-monitor loop), the platform catches the panic, logs it with a full stack trace, and broadcasts a toast to the dashboard so you see it. The rest of the system keeps running. No silent subsystem death.
- **Meeting-aborted broadcast.** If a brainstorm facilitator's session dies mid-meeting, the participants are unblocked immediately with a clear "meeting aborted -- facilitator unavailable" signal, not left hanging until the 5-minute timeout.

### Agents Cannot Escape Their Sandbox

Each agent runs with its own working directory and its own branch in the code repository. They can:

- Read and write files in their working directory
- Run commands relevant to their tasks
- Use the tools provided by Jarvis

They cannot:

- Access files outside their working directory
- Push code directly to shared branches (only the designated "forge" agent does merging)
- Access the production environment
- Share credentials or secrets

### Automatic Recovery

Sometimes an agent gets stuck -- maybe it is waiting for input that never comes, or it has entered a repetitive behavior. Jarvis's monitoring system detects this and takes action automatically:

1. **First**, send a gentle "are you still working?" prompt
2. **Second**, interrupt the agent and send a recovery prompt
3. **Third**, send a diagnostic prompt asking the agent to report its state
4. **If none work**, mark the agent as stuck and notify you

The goal is to recover silently when possible and alert you only when human intervention is actually needed. When the team is paused, all automatic recovery nudges are suppressed -- agents that look idle during a pause are legitimately idle by your instruction, not stuck.

### Complete Audit Log

Every action the team takes is recorded in an audit log:

- When agents join or leave
- Every task assignment, status change, and completion
- Every file lock acquisition and release
- Every message sent between agents
- Every standup, brainstorm, and reflection
- Every pause, resume, snapshot, and restart
- Every error and recovery event

The audit log is available through the dashboard and can be exported for compliance or debugging.

### Conflict Prevention

File locks prevent two agents from editing the same file. Priority-based task routing prevents duplicate work. Leader-based planning prevents contradictory decisions. The net result: agents work together without getting in each other's way.

---

## 9. Visibility and Monitoring

You should never have to guess what your AI team is doing. Jarvis gives you real-time visibility at every level.

### The Browser Dashboard

When you open Jarvis, you see:

**The Office View** -- a grid of team member cards showing each agent's current status, the task they are working on, their token usage, and their recent activity. A glance tells you who is busy, who is idle, and who needs attention.

**The Kanban Board** -- tasks flowing through Backlog → To Do → In Progress → Review → Done. You can see every task, who it is assigned to, when it was created, and how long it has been in its current state.

**The Chat Tab** -- every message sent between team members, with timestamps. Great for understanding how they are coordinating.

**The Standup Tab** -- the current standup transcript or the last completed one, with each team member's status report.

**The Timeline Tab** -- a single chronological feed that merges everything happening in the office: audit events, task transitions, agent messages, completed standups, and learning entries. Filter by event kind (tasks, messages, standups, learnings, audit) or by a specific agent. Live-updates as events fire, so you can watch the team work in real time.

**The Insights Tab** -- an analytics dashboard at a glance. Team velocity sparkline (tasks completed per hour over the last 24 hours). Per-agent cost breakdown as horizontal bars. Token usage stacks (input vs. output). Standup response rates per agent. A learning leaderboard ranking agents by how much they have discovered. A recent-skills feed showing when each agent picked up something new.

**The Meetings Tab** -- a browser for past meetings. Two panes: on the left, a chronological list of every completed standup and brainstorm with date, type, and participant count. On the right, the selected meeting's full transcript with speaker labels and timestamps. You can revisit a meeting from three weeks ago and see exactly what was said.

**The Settings Panel** -- change the team mission, spawn or shut down teams, adjust monitoring intervals, manage snapshots, and configure integrations.

### React Beta Dashboard

A second dashboard at `/office-v2` is being built incrementally. Same data, same SSE stream, cleaner stack (React + Tailwind + Tanstack Query). Opt-in until parity with the classic dashboard is reached -- the classic UI at `/office` stays canonical in the meantime. If the React bundle hasn't been built yet on your install, `/office-v2` shows a styled placeholder page with the build command instead of erroring out.

### Warning Signals on Every Agent Card

Each agent card surfaces compact pills beside the cost readout so problems are visible at a glance -- no modal clicks needed:

- **Turn counter pill** -- appears once an agent crosses 50 turns, goes amber at 80, and turns red when the turn cap fires. You can spot long-running sessions before they hit the ceiling.
- **`⟲ Bash` (or tool name) red pulsing pill** -- the tool-level loop advisory described in section 8. Strongest warning signal on the card.
- **`⟳ loop?` orange pill** -- the text-level loop advisory. Softer signal; often legitimate.
- **`⚠ N ev` grey pill** -- appears when the agent's event stream has dropped N events because the internal buffer overflowed. Non-critical -- data on the card may be slightly stale -- but useful to notice if you are seeing "ghost" timestamps.

The border on a working agent pulses (the "border-beam" treatment) so you can tell at 10 feet away which agents are active.

### Team Pause Banner

When the team is paused, a prominent amber banner pins to the top of every tab: "Team paused" plus your optional reason, plus an inline Resume button. The banner survives page reloads. Returning to a paused dashboard the next morning, you will immediately see why work is halted.

### Agent Detail View

Click any team member card and you get the full picture of that agent:

- Current task and queue
- Personal memory (what they know about the project)
- Personality (which you can override if needed)
- Learning store (their reflections and insights)
- Skills they have created
- Full task history
- Token and cost usage

You can send a direct prompt to any agent, override their personality, restart their AI session in place, bump their working directory, or give them a one-off operator prompt injection.

The detail view also includes three sub-tabs:

- **Activity** (default) -- a trimmed timeline of just this agent's recent events
- **Learnings** -- every reflection this agent has recorded, with category and timestamp
- **Cost & Tokens** -- total spend, turns executed, input/output token totals, and a sparkline of the agent's recent turn costs so you can spot spikes

There is also a shared **Whiteboard** section per agent -- a freeform note area where the operator (or the agent itself) can scribble "here is what I am working on" context. Whiteboard notes are visible to the rest of the team and persist across restarts (including in-place restarts and snapshot restores).

### Clickable Task Detail

Click any card on the kanban board and a detail modal opens showing:

- Full title and description
- Timestamps for when it was assigned, started, and completed
- The complete progress history -- every update the agent posted while working, in order
- Blocker info if the task is stuck
- A jump-to-agent link that opens the owning agent's detail view

This turns the kanban board from a high-level overview into a rich audit trail for every task.

### Command Palette (Cmd+K)

Press Cmd+K (Mac) or Ctrl+K (Windows/Linux) to open a fuzzy-searchable command palette. Type any fragment and it filters across:

- Every tab in the dashboard (Agents, Tasks, Standup, Timeline, Insights, Meetings, Reports, Settings)
- Every agent by name ("Open agent: Nova")
- Every team action (Spawn Team, Start Standup, Start Brainstorm, Wrap Up All, Pause Team, Resume Team, Snapshots, Shutdown Team)

Arrow keys navigate, Enter executes. This dramatically speeds up operating large teams -- you never have to hunt for the right button.

### Per-Card Sparklines

Each agent card in the Office view carries a tiny 20-point line chart showing that agent's recent turn costs. A flat line = cheap work. A climbing line = agent is reading a lot. It is an at-a-glance signal for "who is hot right now" without opening any modal.

### Multi-Terminal Tabs

The main Jarvis page (where you interact directly with individual sessions) shows open sessions as browser-style tabs. Click any tab to switch instantly -- terminal state is preserved, so scrollback, cursor position, and in-flight output survive. Drag tabs to reorder. Inactive tabs are evicted from memory under a least-recently-used policy, so long-running deployments with dozens of sessions stay responsive.

### Relationship Graph

In the Reports tab, a circular view shows who talks to whom across the team. Each agent is a node, placed around a ring. Edges connect pairs that have exchanged messages, with thickness proportional to message volume. Hover an edge to see the count and the last-message time. Click a node to open that agent's detail view. It is useful for spotting communication gaps ("the designer and the backend engineer have never messaged each other?") and overloaded hubs.

### Operator Observability

For teams running Jarvis in production, two additional surfaces are available:

- **Prometheus metrics endpoint** -- exposes agent counts, task counters, request rates, and other gauges in the standard format that Grafana, Datadog, and most monitoring stacks understand
- **pprof profiling** -- lets an SRE pull CPU, memory, and goroutine profiles from a running instance when they need to debug performance

### Desktop Notifications

Beyond the in-dashboard toast notifications, Jarvis can raise OS-level desktop alerts through your browser (with an optional sound cue). Alerts are deduplicated within a configurable window -- you will not get ten "agent completed task" popups when the team is cruising through work.

### Real-Time Updates

The dashboard updates live as things happen. When an agent completes a task, you see it within seconds. When a new agent joins a brainstorm, you see the room fill up. When a file lock is acquired, the lock indicator flashes. When a loop is detected or a session wedges, the card flags it immediately.

### Mobile Notifications via Telegram

You do not have to sit at your desk to monitor the team. Connect your Telegram account and Jarvis sends you:

- Periodic office summaries
- Alerts when an agent gets stuck or hits its turn cap
- Task completion notifications
- Standup summaries

You can even interact with the team from Telegram -- send messages, create new sessions, and check status without opening the dashboard.

### Chat and Transcript Persistence

Every inter-agent message and every standup transcript is saved to disk. If you close the browser and come back hours later, the full history is still there. If the server restarts, it loads the history back into memory. You never lose the thread of what the team has been discussing.

---

## 10. Flexibility

Jarvis is designed to adapt to different needs, budgets, and workflows.

### Multiple AI Providers

Jarvis is not locked to one AI company. You can run your team on:

- **Anthropic Claude** (direct API or subscription plan)
- **Z.AI** (offering "Coding Plan" subscription access to GLM models at a fraction of Claude's cost)
- A mix of providers across the team

The **Z.AI Coding Plan** is particularly interesting: it offers Anthropic-compatible access to GLM models with a generous monthly message quota, at a fixed subscription price far below pay-per-token Claude. For teams that burn a lot of turns on high-volume work, this is often the most cost-effective option.

### Rate-Limit-Aware Model Allocation

Z.AI imposes rate limits on how many agents can run on each model tier simultaneously. A naive deployment would hit the limit and agents would stall. Jarvis automatically distributes agents across model tiers to stay within the limits while keeping your most important agents on the best models.

Example: a 5-agent team might be allocated as:

- **Leader on the best model** (highest quality)
- **Two workers on the second-best model** (good quality)
- **Two workers on the third-best model** (still capable)

You get maximum throughput at minimum quota cost, and the allocation is completely automatic.

### Per-Member Spawn Delay

On a large team with a subscription plan, kicking off eight agents in the same second can trip the provider's concurrent-authentication rate limit. The spawn modal exposes a per-member "spawn delay" knob so you can stagger startup by a few seconds per agent -- enough to smooth through the limit without slowing the overall deployment noticeably.

### Extra Skills and Tools via the ECC Bundle

Jarvis vendors a curated subset of the [ECC](https://github.com/affaan-m/ECC) project -- 22 additional skills (deep-research, e2e-testing, security-review, mcp-server-patterns, and more), four code-review subagents the team's `Task` tool can dispatch on demand (`code-architect`, `security-reviewer`, `pr-test-analyzer`, `code-simplifier`), three slash commands (`/security-scan`, `/test-coverage`, `/refactor-clean`), and three MCP servers attached to every agent session: **context7** for live library documentation lookup, **sequential-thinking** for structured chain-of-thought reasoning on hard problems, and **shadcn** for UI component discovery. Opt the whole bundle out per team via `disableEcc`. See `docs/ECC.md` for the full inventory.

### PR-Linked Sessions

When you spawn a team to work on a specific GitHub PR, you can tie the spawn to a PR number. Future `claude --from-pr <number>` invocations resume the same office team rather than starting from scratch -- useful for "I'll come back to this tomorrow" workflows where the in-flight thread, file locks, and accumulated context need to survive the gap.

### Custom Teams

If none of the seven prebuilt templates fit your needs, build your own. You define:

- How many members the team has
- Each member's name, role, tier, and personality
- Which skills each member brings
- Which AI model each member uses
- Custom instructions and mission

The spawn modal's inline add / duplicate / remove controls let you take a template as a starting point, clone the members you want twice of, remove the ones you do not need, and rename the survivors -- all before you spawn. You can also save the result as a reusable template.

### Personality Overrides

You can override an agent's personality on the fly -- useful for temporary focus shifts. "You are now an ultra-cautious security reviewer until further notice" -- the agent takes on the new personality without being recreated.

### Configurable Autonomy Level

- **Perpetual mode**: team never stops, generates its own work
- **Goal-focused mode**: team stops when assigned work is done
- **Paused state**: team is alive but receives no new prompts until you resume
- **Wrap-up mode**: team is told to finish up and shut down cleanly

You choose the level of autonomy you are comfortable with.

### Respawn Lifecycle Presets

For common automation patterns, Jarvis ships with three built-in lifecycle presets you can apply to any agent:

- **Quick Iteration** -- small turn budget, fast wrap-up, tuned for prototype work where you expect many short sessions
- **Autonomous Long** -- generous budget, aggressive self-recovery, tuned for overnight or multi-day perpetual runs
- **Careful Review** -- frequent pauses for operator approval, ideal for production-adjacent work where every change should be eyeballed

Presets are starting points -- you can customize any field and save your own.

### Project Context Injection

When an agent joins a project, Jarvis automatically reads the project's standard context files -- `CLAUDE.md`, `README.md`, `ARCHITECTURE.md` -- and folds them into the agent's briefing. The agent does not have to discover and read these files itself (wasting context). Context is re-read when files change, so briefings always reflect current intent.

### New Project Flow

The spawn modal can create a brand-new project at spawn time: pick a slug, Jarvis creates `/app/workspace/<slug>`, seeds a `CLAUDE.md` with design defaults, and spawns the team directly onto the fresh directory. No shell commands required.

### Shared Dev Instance

A team working on a full-stack app needs a running dev server to verify changes against. Jarvis manages a "team dev instance" -- a process the team lead starts once, announces on a named slot ("app", "frontend", "api"), and every teammate can query for the URL and credentials before running browser verification. The health checker pings the URL and auto-clears a dead slot after three consecutive failures, so teammates do not get pointed at a stopped dev server.

### HTTPS Support

The dashboard supports HTTPS out of the box. If you do not have a signed certificate, Jarvis can generate a self-signed one for you so you can run securely on a private network without provisioning certificates manually.

---

## 11. Automation and Scheduling

Jarvis is not just interactive. Much of its value comes from work that happens on a schedule, without a human in the loop -- nightly cleanups, hourly health checks, weekly regression sweeps, event-triggered investigations.

### Cron-Style Scheduler

Create durable scheduled jobs using standard cron expressions. Each job has a name, a schedule (e.g., "every weekday at 2 AM"), and an action. Actions include:

- **Spawn a team** -- fire up a full office at the scheduled time
- **Run a one-shot session** -- execute a single prompt and capture the result
- **Call a webhook** -- POST to any URL with custom headers and body

Every job keeps an execution history so you can see whether it has been firing reliably, how long each run took, and what output it produced. A "Run now" button lets you test a schedule out-of-band without waiting for the next tick.

Use cases: nightly test runs, scheduled documentation refreshes, weekly dependency audits, cost reports emailed to ops, hourly check-ins from a QA agent.

### Session Cloning

Sometimes you need parallelism without the overhead of building a full team. Session cloning spawns N independent copies of a single worker session from a template. Each clone runs in its own sandbox with its own task. This is perfect for embarrassingly parallel work: process 20 files at once, explore 10 different fix approaches simultaneously, run a validation task across every module.

### One-Shot Sessions

For fire-and-forget prompts where you do not need a long-running interactive session, one-shot sessions run a single prompt and capture everything -- output, token count, duration -- in a single API response. Useful for:

- Ad-hoc AI queries that do not warrant a full team
- External webhooks that want an AI-generated response (a Slack bot, a help-desk router)
- Quick validation checks before a bigger action

### Notification Routing

Jarvis emits a constant stream of events -- agent status changes, task transitions, messages, standups, loop advisories, restarts, pauses, errors. You can configure routes that forward matching events to external channels:

- **Telegram** -- direct-message alerts
- **Webhook / Slack** -- POST to any URL
- **Email** -- formatted digests
- **Log file** -- append-only for later auditing

Routes use event pattern matching. Subscribe to the exact event (`session:idle`) or use wildcards (`office:*` for every office event). Routes can optionally be filtered to a specific session so a noisy integration does not flood other channels. Every route has a "Test" button to verify connectivity before it goes live.

### Webhook Actions

Scheduled jobs and notification routes can POST to arbitrary URLs with custom HTTP headers, custom request bodies, and configurable timeouts. This is how Jarvis integrates with external tools like ticketing systems, monitoring stacks, or internal Slack bots.

---

## 12. Typical Use Cases

### Long-Running Refactoring Projects

You have a legacy codebase that needs cleanup. The task is too big for a human team to focus on for weeks, and most humans find it tedious. Deploy a Jarvis team with a refactoring mission, turn on perpetual mode, and let them grind away for a week. Check in daily via the dashboard.

### Autonomous Bug Hunting

You suspect there are bugs in a module but do not have time to investigate. A Jarvis QA team can read the code, write tests, find the bugs, file them as tasks, and fix them -- all without a human writing a single line.

### Documentation Generation

Your team shipped features faster than documentation could keep up. A Jarvis documentation team can read the code, understand the architecture, and produce comprehensive user and developer documentation. Overnight.

### Test Coverage Improvement

Your test coverage is below where you want it. A Jarvis team focused on testing can scan the codebase, identify untested paths, write tests, and raise coverage steadily over days or weeks.

### Research and Proof-of-Concept Building

You have an idea for a new feature and want to validate it. Deploy the **Innovation Sprint** team -- they will research the problem, prototype a solution, validate it, and hand you a working proof of concept plus a presentation.

### Continuous Code Quality Improvement

Keep a lean Jarvis team running perpetually. They find small improvements every day: better variable names, extracted functions, fixed warnings, improved error messages. Over a year, the quality of the codebase improves dramatically without any human attention.

### Security Audits

Deploy a team with a security mission. They scan for common vulnerabilities, review authentication code, check for secrets in commits, and file security bugs. The Shield role in the Full-Stack Compact template is designed for exactly this.

### Knowledge Transfer and Onboarding

When a new engineer joins your team, give them the Jarvis dashboard and the agent memory files. Every decision the team has made, every pattern they have discovered, and every codebase insight is right there -- better than any wiki.

### Nightly Regression Sweeps

Schedule a QA team to spawn at 2 AM every night. The team runs the full test suite, files any failures as bug tasks, assigns them to the responsible modules, and shuts down cleanly by morning. Your engineering team arrives to a clean triage queue instead of a surprise crisis.

### Fan-Out Batch Work

You have 50 files that each need a specific, repetitive change -- a migration, a renaming, a policy audit. Clone one worker session fifty times (or ten times, five times over) and let them process the files in parallel. What would take a single agent a day completes in an hour.

### Friday-To-Monday Continuations

Wrap up a complex refactor on Friday afternoon and take the weekend off. Save the team as a snapshot. Monday morning, restore the snapshot with one click -- the team spawns back with the same mission, the same member list, the same task queues, and all their memory and learnings intact. No hand-wringing about "where did we leave off?"

### Mid-Run Priority Shifts

A customer emergency requires half your team's attention shifted from `/backend` to `/api` for the afternoon. Use per-agent working-directory bumps to redirect three or four members without respawning or losing in-progress work. When the emergency is over, bump them back.

---

## 13. Business Value

Let us talk about the numbers.

### 24/7 Throughput

A human engineer works about 8 hours a day, 5 days a week. That is roughly 2,000 productive hours a year.

A Jarvis agent works 24 hours a day, 7 days a week. That is 8,760 hours a year -- **4.4 times more**.

A 5-agent team running perpetually produces roughly 5 * 8,760 = 43,800 engineering hours a year. The equivalent of **22 full-time engineers** in calendar hours. (Not a direct substitution -- a human is still more capable in many areas -- but the raw throughput is real.)

### Compounding Learning

Unlike human teams where knowledge walks out the door when people leave, a Jarvis team accumulates knowledge in a structured, persistent form. After three months of running, the team knows your codebase better than most of your human engineers did at month one. After a year, it knows your codebase better than anyone.

This is the compounding factor that makes long-running deployments especially valuable. Team snapshots make it explicit: you can put the accumulated knowledge of the team on ice, pick it back up three weeks later, and resume with full context.

### Parallelism Without Coordination Overhead

Adding human engineers to a project does not linearly increase throughput -- every new person adds coordination overhead. But Jarvis agents coordinate automatically through the built-in protocols (standups, brainstorms, task triage, file locks). Doubling the team size roughly doubles the throughput.

### Predictable Costs on Subscription Plans

On a subscription plan (Claude subscription, Z.AI Coding Plan), your cost is fixed per month -- not per token. That means:

- **No surprise bills** from a misbehaving agent running in a loop for hours
- **No need for dollar-based caps** -- the ceiling is your plan's message quota
- **Predictable budgeting** -- an engineering manager can sign off once and not re-check weekly

On pay-per-token plans (direct Anthropic API), the knowledge graph is your biggest lever for keeping costs down. Teams that lean on it typically see 5-10x reduction in token spend versus naive deployments.

### Rough Cost Comparison

Ballpark numbers for a 5-agent team running 24/7 on real codebase work:

| Setup | Monthly cost | Notes |
|---|---|---|
| Claude direct API, no graph | $4,000 to $8,000 | The expensive option |
| Claude direct API, with graph | $500 to $1,500 | Knowledge graph reduces context |
| Claude subscription | Fixed plan price | Flat rate, use all your quota |
| Z.AI GLM subscription, with graph | Fixed plan price | Best cost-to-value ratio |

These are rough estimates and vary with workload intensity. The exact numbers depend on which plan you choose and how heavily you lean on the knowledge graph.

### Operational Savings

Beyond raw token/quota costs, Jarvis eliminates several categories of operational cost:

- **Task management overhead** -- no Jira, no sprint planning meetings, no standups (the AI runs them)
- **Code review bottlenecks** -- the QA agent reviews in real time
- **Knowledge loss from turnover** -- memory and learnings persist
- **Weekend and holiday downtime** -- agents do not take vacations
- **Babysitting time** -- the dashboard flags stuck agents, loops, and crashes without a human watching

---

## 14. What Jarvis Is Not

Being honest about the limitations is as important as celebrating the capabilities.

### Not a Replacement for Senior Architects

Jarvis is excellent at executing well-defined work and gradually improving existing codebases. It is less well-suited for greenfield architectural decisions -- choosing a technology stack, designing a new system from scratch, or making critical business-facing product calls. These decisions are best made by human experts, with Jarvis implementing the result.

### Not Infallible

Agents make mistakes. They sometimes write incorrect code, miss edge cases, or follow patterns that do not fit your codebase. This is why the dashboard exists -- you should review the work periodically, especially for the first few days when the team is learning the codebase.

In perpetual mode, think of Jarvis as a team of mid-level engineers that needs occasional light supervision, not zero supervision.

### Not Suitable for Security-Critical Unsupervised Work

If you are handling regulated data, financial transactions, or safety-critical systems, you should not run Jarvis unsupervised on those codebases. Agents can and do make mistakes that would be unacceptable in high-stakes environments.

For these use cases, use Jarvis in goal-focused mode with a human reviewing every commit before it is merged.

### Requires a Good Starting Point

Jarvis works best when:

- The codebase is in reasonable shape (not a disaster)
- Tests exist and can be run automatically
- Git history is clean
- A clear mission statement is provided
- The task is well-defined

Jarvis can improve a mediocre codebase, but it is not magic. If you point it at a chaotic repository with no tests and no documentation, the results will be mixed.

### Not a Human Replacement

The most successful deployments use Jarvis as a force multiplier for a small human team, not as a replacement for one. A human still:

- Sets the mission
- Reviews high-stakes decisions
- Handles customer-facing communication
- Makes architecture choices
- Provides domain expertise

Jarvis handles the bulk of the implementation work.

---

## 15. Getting Started

### What You Need

To run Jarvis, you need:

1. **A server** to host it -- a modest cloud VM (4 CPUs, 8GB RAM) is enough
2. **An AI provider account** -- Claude (Anthropic API or subscription) or Z.AI Coding Plan, or both
3. **A codebase** to run the team on
4. **A git repository** -- the team commits their work automatically
5. **Optional: a Telegram account** -- for mobile notifications

### Deployment Options

**Docker (recommended).** Jarvis ships as a Docker image. Run one command and it is up.

**Self-hosted binary.** Download the prebuilt binary for Linux or macOS and run it. No Docker required.

### Setup Effort

A technical operator can have Jarvis running in about **30 minutes** from a fresh server:

1. Create a cloud VM (5 minutes)
2. Install Docker (5 minutes)
3. Configure AI provider credentials (5 minutes)
4. Run `docker compose up` (5 minutes)
5. Open the browser and spawn a team (10 minutes)

After the initial setup, starting a new team is a single button click.

### Cost to Run

The server itself costs about **$20 to $50 per month** for a cloud VM. AI costs depend entirely on which plan you choose:

- **Subscription plans** (Claude subscription, Z.AI Coding) -- a fixed monthly fee, typically $20 to $200
- **Pay-per-token** (Claude API direct) -- varies with workload, $50 to $1,500 per month for a 5-agent team

For comparison, a single human software engineer costs **$10,000 to $20,000 per month** in the US (salary + benefits + overhead). Even at the high end, Jarvis is a fraction of the cost of one engineer while producing the throughput of several.

---

## 16. Security and Reliability

### Built-in Safety

Jarvis includes many layers of protection:

- **Password protection** -- optional login to the dashboard
- **Authentication tokens** -- requests are verified to prevent spoofing
- **Rate limiting** -- prevents brute-force attacks on the login
- **Path isolation** -- agents cannot access files outside their working directory
- **Injection prevention** -- user input is carefully validated before use
- **Encrypted credential storage** -- API keys are stored with restricted file permissions
- **Audit logging** -- every action is recorded for review
- **CSRF protection** -- every mutating dashboard action requires a CSRF token

### Battle-Tested Code

Jarvis has undergone more than a dozen comprehensive code reviews with hundreds of bug fixes shipped across:

- Memory safety (no crashes or hangs)
- Concurrency (no race conditions, exercised with Go's race detector on every change)
- Security (no injection, no path traversal)
- Resource management (no leaks)
- Error handling (graceful failures)

Every commit runs the full Go race detector and the unit + integration test suite (several hundred tests). The most recent reliability rounds added:

- A waitSlot close-guard that prevents late replies from one standup round polluting the next.
- A panic-recovery wrapper around every long-running background goroutine, so a panic in one subsystem logs + broadcasts + dies alone rather than taking down the server.
- A wedged-session hard stop that catches AI subprocess hangs the turn-cap cannot see.
- A meeting-aborted broadcast so participants unblock instantly when a facilitator dies mid-brainstorm.
- Tool-level and text-level loop advisories that show up on the agent card before the operator ever notices in the logs.

### Protected State on Disk

Jarvis persists several pieces of state to disk: team messages, team mission, daily stats, dev-instance slots, and team snapshots. Every load path runs through a hardened helper that:

- **Quarantines corrupt files** -- if a JSON file fails to parse, it is renamed to `<file>.corrupt-<timestamp>` and the platform starts with an empty default instead of silently losing state. The operator is notified via a dashboard event.
- **Validates values** -- negative budgets, zero-time message timestamps, and future-dated entries are dropped or clamped with a warning, so a clock-skew incident cannot scramble the dashboard.
- **Writes atomically** -- critical files are written via a temp-file + rename pattern so a crash mid-write cannot leave a half-serialized state file that fails to load.

### Graceful Failure Modes

When something goes wrong, Jarvis fails gracefully:

- **Agent crashes**: automatically restarted by the respawn system (or manually with the in-place restart button)
- **Server crashes**: all state persists to disk, resumes on restart
- **Network outages**: agents reconnect automatically
- **AI provider outages**: agents pause, resume when the provider returns
- **Disk full**: state persistence is lossless (atomic writes)
- **Subsystem panic**: caught, logged with a full stack, broadcast as a dashboard toast; the rest of the platform keeps running
- **Ghost meetings** from a prior crash: swept on the next background tick; the dashboard is notified

You will not find Jarvis corrupting state, losing work, or entering an unrecoverable state.

### Production-Grade Observability

For operators running Jarvis in production, the platform exposes the same surfaces any SRE team expects:

- **Prometheus metrics** -- export the full set of internal counters and gauges in a format Grafana, Datadog, and the major monitoring stacks understand natively
- **pprof profiling** -- pull live CPU, memory, goroutine, and blocking profiles from a running instance when you need to diagnose performance
- **Request ID tracing** -- every incoming request is tagged with a unique ID that appears in the logs, the response headers, and any downstream call. When you investigate an incident, you can trace one request end-to-end without guessing.
- **Agent token authentication** -- each session is issued its own short-lived token, so one agent session cannot reach into another's state even if something goes wrong in the wider system
- **Event-drop observability** -- each agent card surfaces a small pill if any of its outbound events were dropped because the internal buffer overflowed, so slow dashboards become visible before they silently miss signals

### Compliance-Friendly Audit Log

Every action is recorded. If you need to demonstrate to auditors that your AI team's actions are traceable, you can export the audit log and show:

- When each agent joined and left (including in-place restarts with the old and new session ID)
- What tasks were assigned and by whom
- What files were modified and when
- What code was committed and to which branch
- What messages were exchanged between agents
- When the team was paused or resumed and for what reason
- When a snapshot was created or restored

---

## 17. Frequently Asked Questions

### Can I trust agents with production code?

Yes, with appropriate supervision. For production code, we recommend running in **goal-focused mode** and having a human review every commit before it is merged to the main branch. Jarvis gives you the audit log and kanban history to make review efficient.

For non-production code (documentation, test files, developer tools, internal utilities), agents can run in **perpetual mode** with less supervision.

### What if an agent makes a mistake?

Several safeguards catch mistakes:

- **Tests**: if the agent breaks tests, the task is marked failed
- **QA agent**: reviews code from other agents and files bug tasks
- **Human review**: the dashboard lets you spot-check work
- **Git history**: every change is in a commit you can revert

The worst-case scenario is a bad commit, which is trivially revertible. No irreversible mistakes are possible because the team works only on code, not on infrastructure or data.

### How much does it cost to run?

Depends on which plan you choose:

- **Subscription plans** (Claude subscription, Z.AI Coding): fixed monthly fee regardless of workload
- **Pay-per-token** (Anthropic API direct): varies with workload, commonly $50–$1,500/month for a 5-agent team

Compare to **one human engineer at $120,000 to $240,000 per year** and the math is usually favorable no matter which plan you pick.

### Can I customize the team?

Absolutely. You can:

- Use any of seven prebuilt templates (plus the one-click Demo Jarvis team)
- Add, remove, or duplicate members in the spawn modal before you spawn
- Build custom teams with any mix of roles
- Write custom personalities
- Override personalities at any time
- Choose different AI models per team member
- Set per-agent turn ceilings and spawn delays
- Adjust monitoring intervals and thresholds
- Enable or disable specific features

### What happens when agents disagree?

Leaders have authority. If a worker is blocked or uncertain, the task gets escalated to a strategic-tier leader who makes the call. This mirrors how human teams resolve disagreements -- push decisions up the chain of command.

In rare cases where a disagreement is intractable, the task is marked as failed and you are notified.

### How do I stop them?

Several options, from gentle to abrupt:

1. **Pause** (gentlest): freezes new prompts and recovery nudges while keeping everyone alive. Resume with one click.
2. **Wrap-up** (recommended for long sessions): agents finish what they are working on, commit, save memory, and exit cleanly. Takes a few minutes.
3. **Kill one agent**: surgical stop of a single runaway member. The rest keep working.
4. **Shutdown**: immediate stop of everyone, all sessions terminate within seconds.

All four are available as buttons in the dashboard.

### How do I pause the team mid-run?

Click **Pause** in the header. The team freezes immediately -- no new prompts, no auto-standup, no recovery nudges. Current AI turns finish normally (pause is "no new input," not "suspend mid-step"). An amber banner at the top of the dashboard shows the pause state + your optional reason. Click **Resume** (the same button flips labels) to unfreeze.

### Can I save a team to continue later?

Yes. Click **Snapshots** in the header, give the current team a name, and Jarvis serializes the whole shape of the team -- mission, members, per-agent tasks, whiteboards, perpetual-mode state, pause state -- to a single file. Later (after the current team has been shut down), open the Snapshots modal and click **Restore** on the saved snapshot. The team spawns back with the same configuration and the same task queues. Memory and learnings rehydrate automatically from their own per-role files.

This is how you do Friday-afternoon-to-Monday-morning handoffs without losing context.

### How do I restart a single agent without losing its memory?

Hover the agent card → click the **Restart** quick-action button (the circular-arrow icon). Jarvis stops the agent's current AI session and attaches a fresh one to the same agent. Memory, learnings, task queue, whiteboard, file locks, personality -- everything carries over. The only thing that resets is the AI session's own context window.

Use this when an agent's context is cluttered with irrelevant history but the agent itself has accumulated knowledge you do not want to lose.

### What happens when an agent gets stuck in a loop?

The dashboard catches it before you do:

- If the agent emits the same block of text three+ times in a short span, an orange **⟳ loop?** pill appears on the card (text-level advisory).
- If the agent runs the same tool with the same arguments producing the same output three+ times in a short span, a red **⟲ <tool-name>** pulsing pill appears (tool-level advisory -- much stronger signal).
- If the agent exceeds its per-session turn ceiling (default 100), the session is stopped automatically and a notification fires.
- If the agent reports itself as "working" for longer than the wedged-session timeout (default 20 minutes) without any tool activity, the session is stopped automatically.

None of the advisories auto-kill the agent (except the hard ceiling + wedged-session stop). They raise a flag and wait for you to decide -- legitimate repetition is possible, so the advisory is a nudge, not a decision.

### How do I prevent runaway agents?

Jarvis uses quota-agnostic defenses that work whether you are on a pay-per-token plan or a subscription:

1. **Objective verify gate** -- a perpetual-loop task closes only when an auto-detected build/test/lint command passes, so a self-asserted "done" cannot override a failing test suite.
2. **Iteration and time hard stops** -- a perpetual loop is capped at a maximum number of self-replenishment cycles (default 10) and an optional wall-clock lifetime; crossing either pauses for human review.
3. **Committed-secret scan** -- a secret scan runs before replenishing an idle agent with new work, and halts replenishment if it finds a committed credential.
4. **Task-category blocklist** -- self-added tasks in risky categories (auth, payment, deploy, migration, dependency-upgrade, architecture-rewrite) are rejected and routed to a human.
5. **Per-session turn ceiling** -- every AI session is capped at 100 turns by default. Operators can adjust per-agent at spawn time.
6. **Wedged-session hard stop** -- any AI session that reports working for 20+ minutes without tool activity gets stopped.
7. **Loop advisories** -- the dashboard flags repetitive output and repetitive tool calls so you intervene early.
8. **Pause button** -- click pause when you see trouble; agents freeze without dying.
9. **Kill button** -- surgical stop for one agent; the rest keep working.

(Dollar- and token-based cost caps existed in earlier versions but were removed -- they are meaningless on subscription plans, which is what most deployments use. The iteration and time hard stops above are the binding guardrail.)

### Do they work on proprietary code?

Yes. Jarvis is self-hosted -- it runs on your server, not ours. Your code never leaves your infrastructure except to query the AI provider (Claude or Z.AI) for the current task. If you are concerned about code going to an AI provider, you can use models that run entirely on your own hardware (though that requires more setup).

### Can I run multiple teams at once?

One Jarvis instance runs one team at a time. If you need multiple teams in parallel, run multiple Jarvis instances (one per codebase, each on its own port). This keeps state cleanly separated.

### Do I need to be technical to use it?

To **operate** Jarvis day-to-day (spawning teams, monitoring, reviewing work, pausing/resuming, taking snapshots), no technical skill is required. The dashboard is designed to be usable by a product manager or business leader.

To **deploy** Jarvis initially, you need either a technical operator or about 2 hours following the setup guide.

### What if the AI provider has an outage?

Agents pause and wait. When the provider is back online, they resume from exactly where they left off. Nothing is lost. If you prefer to halt everything during an outage instead of letting agents slowly retry, use the Pause button explicitly.

### Can it work offline?

Not today. Jarvis requires internet access to communicate with the AI provider. If you need fully offline operation, you would need to run a local AI model, which is an advanced deployment.

### How do I migrate to a new codebase?

Two paths:

1. **Fresh start**: spawn a new team pointing at the new codebase. Each team member starts fresh on the new codebase but inherits the shared skill library and distilled insights from previous deployments. You do not lose prior learning, but you do start with a clean memory for the new project.
2. **Working-directory bump**: if you want to keep the same agents but point them at a different folder (say a new sub-module inside the same repo), use the per-agent workdir bump button.

### Can agents interact with external services?

Yes, within limits. Agents can call APIs, make HTTP requests, and interact with command-line tools. They cannot, however, interact with services that require interactive authentication (like OAuth flows) -- you would need to set those up ahead of time.

### What languages does it support?

Any language Claude Code or Z.AI GLM supports -- which is essentially all major languages: Python, JavaScript, TypeScript, Go, Rust, Java, C#, C++, Ruby, PHP, Swift, Kotlin, and many more. The team members do not have to know the language in advance; they learn it from the codebase.

### Can I schedule the team to run nightly?

Yes. The built-in cron scheduler lets you create jobs that spawn a team (or run a one-shot prompt, or call a webhook) at any standard cron time. Jobs keep their own execution history so you can confirm they ran. Use cases: nightly regression sweeps, weekly dependency audits, hourly health checks.

### Can I run parallel copies of the same agent?

Yes. Session cloning spawns N independent copies of a worker session from one template. Each clone has its own sandbox and works on its own task in parallel -- useful when you need fan-out throughput without building a whole team.

### Can it send alerts to Slack?

Yes. Notification routing forwards selected events to Telegram, Slack, email, webhooks, or log files. Routes use pattern matching (exact event names or wildcards like `office:*`) and can be filtered by session. Every route has a "Test" button so you can verify connectivity before it goes live.

### How do I know which agent is burning the most context?

The Insights tab shows a per-agent cost breakdown as horizontal bars -- the longest bar is your biggest spender. Each agent card also shows a tiny sparkline of recent turn costs, so a sudden climbing line tells you that agent just consumed a lot of context on a single turn. Turn-count pills on the card surface long-running sessions before they hit the per-session ceiling.

### How long should I let a team run?

Depends on the goal:

- **Short sprints** (2-8 hours) for focused tasks
- **Daily runs** (8-24 hours) for active projects
- **Multi-day runs** (days to weeks) for long-term refactoring or research
- **Perpetual runs** (indefinite) for continuous improvement

The team's learning system actually improves with longer runs -- they get better at your codebase over time. And if a run needs to be chopped up by deploy windows or weekends, use Pause + Resume (or Snapshot + Restore) to keep momentum without keeping a session alive the whole time.

---

## 18. Glossary

**Agent** -- A single AI team member with a specific role, name, and personality. Each agent runs as an independent process and coordinates with others through Jarvis.

**Auto-standup** -- A recurring standup triggered by the monitoring loop, without operator input. Configurable interval.

**Border beam** -- The animated outline that pulses around an actively-working agent card in the Office view. At-a-glance "who's busy right now" signal.

**Brainstorm** -- A meeting between two or more agents where they propose new tasks. The proposals automatically become real tasks in the queue.

**CodeGraphContext** -- The knowledge-graph tool that maps relationships in the codebase so agents can query structure instead of reading whole files.

**Command palette** -- Keyboard-driven menu (Cmd+K / Ctrl+K) for quickly jumping to any tab, agent, or team action. Fuzzy-searchable.

**Cron job** -- A scheduled action that runs on a repeating time expression. Can spawn teams, run one-shot sessions, or call webhooks.

**Dashboard** -- The browser interface for monitoring and controlling Jarvis. Accessible at the server's address.

**Dev instance** -- A dev server (e.g. `npm run dev`) that the team lead starts once and teammates query by named slot ("app", "frontend", "api") before running browser verification. Jarvis health-checks the URL and auto-clears dead slots.

**Distilled insights** -- Condensed wisdom generated from an agent's accumulated learning reflections. Prevents the learning store from growing indefinitely.

**Event-drop pill** -- A grey "⚠ N ev" pill on the agent card indicating the outbound event buffer overflowed at some point. Data on the card may be slightly stale.

**File lock** -- A temporary reservation on a file so only one agent can edit it at a time. Prevents conflicts. Migrates to the new session when an agent is restarted in place.

**Fix plan version** -- A checkpoint an agent creates before executing a significant change. Every checkpoint is revertible.

**Goal-focused mode** -- Operating mode where agents complete their assigned work and stop. The opposite of perpetual mode.

**In-place restart** -- Stopping one agent's AI session and attaching a fresh one to the same agent. Keeps memory, learnings, tasks, whiteboard, and file locks.

**Insights tab** -- Dashboard tab showing analytics: velocity sparkline, per-agent cost bars, token usage, standup response rates, learning leaderboard.

**Kanban board** -- A visual representation of tasks flowing through columns (Backlog → To Do → In Progress → Review → Done). Standard software-project-management interface.

**Kickstart** -- The initial briefing given to each agent when the team is spawned. Includes mission, memory, learnings, role, teammates, and tools.

**Knowledge graph** -- A searchable map of the codebase that lets agents query relationships instead of reading files. Dramatically cuts context consumption.

**Leader** -- An agent with strategic tier, responsible for planning and delegating work to workers. Usually the Tech Lead or Engineering Manager role.

**Learning store** -- A per-agent file that accumulates reflections on past work. Feeds into future briefings.

**Loop advisory** -- A dashboard flag raised when an agent emits repeated output (text-level, orange pill) or runs the same tool with the same result repeatedly (tool-level, red pulsing pill). Advisory only -- the operator decides whether to act.

**Meeting-aborted broadcast** -- The signal that fires when a meeting facilitator's session dies mid-meeting. Participants are unblocked immediately rather than waiting for the per-speaker timeout.

**Meetings tab** -- Dashboard tab for browsing past standups and brainstorms, with the meeting list on one side and the transcript on the other.

**Memory** -- A per-agent, per-project file that records what the agent knows about the project. Persists across restarts (including in-place restarts and snapshot restores).

**Mission** -- The high-level goal given to the team. Can be enriched over time as the team discovers context.

**Monitoring loop** -- The automatic background process that checks for stuck agents, expired file locks, idle teams, wedged sessions, and other issues. Runs every 15 seconds.

**New project flow** -- The spawn modal mode that creates a fresh project directory, seeds a `CLAUDE.md`, and spawns the team onto it in one click.

**Notification route** -- A rule that forwards matching events to an external channel (Telegram, Slack/webhook, email, log file).

**One-shot session** -- A single-prompt execution that captures output and tokens in one API call. No long-running terminal.

**Operator prompt injection** -- A corrective message delivered to one agent's running session with a "[OPERATOR PROMPT]" prefix. Does not reset the session.

**Panic recovery** -- The safety wrapper around every long-running background goroutine. Catches panics, logs stack traces, and broadcasts a dashboard toast while the rest of the platform keeps running.

**Pause / Resume** -- Team-wide control that freezes (or unfreezes) background automation: no new prompts, no auto-standup, no recovery nudges. Agents stay alive; current turns finish.

**Perpetual mode** -- Operating mode where agents never stop. When a task is done, they find new work automatically.

**RALPH loop** -- The continuous improvement cycle agents follow in perpetual mode: Recognize, Assess, Log, Plan, Handle.

**Reflection** -- A post-task note recording what an agent learned. Categorized and persisted in the learning store; meaningful reflections are auto-appended to the agent's memory file.

**Respawn** -- Restarting an agent after a crash or other failure. Automatic.

**Screenshot gate** -- The QA discipline that requires any UI-keyword task (containing words like "frontend", "modal", "button", "layout") to cite a browser screenshot in its completion summary. The dashboard warns when a summary is missing one.

**Session** -- An individual running instance of an AI agent. A team is made up of multiple sessions running in parallel.

**Session clone** -- A copy of an existing worker session spawned for parallelism. Runs independently with its own task.

**Shared skill** -- A reusable technique documented by one agent and visible to all others. Stored in a team-wide skill library.

**Snapshot** -- A saved, named file capturing the shape of the current team (configs, mission, per-member tasks, whiteboards, pause state). Can be restored later to resume a team exactly where it left off.

**Sparkline** -- A tiny inline line chart on each agent card showing recent turn costs. Spikes are visible at a glance.

**Spawn delay** -- A per-agent pause between when this agent starts and when the next one starts, for rate-limit smoothing on big teams.

**Standup** -- A regular meeting where each agent reports their status. Happens automatically on a configurable schedule.

**Strategic tier** -- Leaders who plan and delegate. Opposite of worker tier.

**Task queue** -- The per-agent list of pending, in-progress, and blocked tasks. Carries over on in-place restart and snapshot restore.

**Team score (A–F)** -- Per-agent performance grade computed from completion rate, speed, quality, and collaboration.

**Team template** -- A prebuilt team configuration (e.g., Full-Stack Compact, Research Lab) that you can spawn with one click.

**Timeline tab** -- Dashboard tab with a unified chronological feed of all office events, filterable by kind or agent.

**Triage** -- The act of reassigning a blocked task from one agent to another better suited to solve it.

**Turn cap** -- The per-session maximum number of AI turns. Default 100. When crossed, the session is stopped automatically and pending tasks are reassigned.

**Tool-level loop advisory** -- A red pulsing pill that appears on the agent card when the same tool runs with the same arguments producing the same output three+ times in a short span. Strong signal of a real loop.

**Webhook action** -- A scheduled job's ability to POST to an external URL with custom headers and body.

**Wedged session** -- An AI session that reports itself as "working" but has produced no tool activity or turn events for longer than the configured timeout (default 20 minutes). Automatically stopped.

**Whiteboard** -- A freeform text note per agent, visible to the team. Useful for "here is what I am working on" annotations. Persists across restarts and snapshots.

**Worker tier** -- Agents who execute tasks assigned by leaders. Opposite of strategic tier.

**Workdir bump** -- Redirecting one agent's working directory mid-run via a prompt injection. Keeps the agent but shifts their focus.

**Wrap-up** -- The graceful shutdown sequence: agents finish current work, commit, save memory, and exit cleanly.

---

## Next Steps

If you are evaluating Jarvis for your organization:

1. **Read the technical README** if you or your team want to understand the implementation
2. **Deploy a trial team** on a non-critical codebase to see it in action
3. **Run for a week** in goal-focused mode so you can review the work
4. **Measure the output** -- tasks completed, bugs fixed, tests written
5. **Try the lifecycle controls** -- pause mid-run, take a snapshot, restore it, restart an agent in place -- to see how resilient the platform is to real-world interruptions
6. **Scale up** based on what you observe

The biggest barrier to adoption is usually not the technology -- it is cultural. Teams that are open to rethinking how engineering work happens will get the most value. Teams that try to bolt Jarvis onto their existing workflows may find friction.

The good news: you can start small. Spawn a team for a weekend, see what it does, and decide whether to expand. Jarvis is designed to be low-commitment to try and high-value when deployed seriously.

---

*Questions? Open an issue on the project repository or reach out to the maintainers. This document is deliberately non-technical -- if you need engineering detail, see README.md.*
