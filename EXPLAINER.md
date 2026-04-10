# Jarvis -- The Business Explainer

> A plain-English guide to what Jarvis is, what it does, and why it matters.
> No jargon. No engineering required.

---

## At a Glance

**Jarvis is a virtual software engineering team that works around the clock on your codebase.** Instead of hiring one AI assistant that you have to babysit, you deploy a self-organizing team of AI engineers -- each with a role, personality, and specialty -- who coordinate through meetings, share knowledge, and continuously improve your code without human supervision. They learn from their work, remember what they've done, and hand off tasks to each other just like a real team would.

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
11. [Typical Use Cases](#11-typical-use-cases)
12. [Business Value](#12-business-value)
13. [What Jarvis Is Not](#13-what-jarvis-is-not)
14. [Getting Started](#14-getting-started)
15. [Security and Reliability](#15-security-and-reliability)
16. [Frequently Asked Questions](#16-frequently-asked-questions)
17. [Glossary](#17-glossary)

---

## 1. The Problem

Software engineering is expensive, slow, and bottlenecked by human attention. Even with AI coding assistants, a human still has to sit in front of a screen and guide the assistant through every task. When the human stops, the work stops.

Today's AI coding tools share three big limitations:

**They need a babysitter.** You hire an AI assistant, give it one task, and then you have to review its work, correct its mistakes, and give it the next task. The AI does not run itself.

**They forget.** After a conversation ends, the AI has no memory of what it learned. Every new task starts from scratch. Knowledge built up during hours of work is discarded.

**They work alone.** Real codebases are complex and benefit from specialists -- a backend engineer, a frontend engineer, a quality assurance engineer, a designer. A single AI assistant cannot be all of these at once. And when it tries, it loses context and makes contradictory decisions.

On top of that, **AI assistants are expensive to run continuously.** Every time they read a large file, they consume "context" -- and context costs money. An AI assistant working on a real codebase for 8 hours can easily spend $40-50 just reading files to understand what is going on.

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

Jarvis runs as a web application. You open a browser, click "Spawn Team," watch the dashboard, and let the team work. Or close the browser and check back in the morning.

---

## 3. The Virtual Office

The best way to understand Jarvis is through an office metaphor. When you deploy a team, you are staffing a virtual office.

### Hiring a Team

Jarvis ships with **six prebuilt team templates** you can deploy with one click:

| Template | Size | What it does |
|---|---|---|
| **Office (Default)** | 7 members | A classic full-stack team: an Engineering Manager, a Senior Architect, a UI/UX Designer, a Frontend Engineer, a Backend Engineer, a DevOps Engineer, and a QA Engineer |
| **Backend / AI** | 6 members | Focused on AI and data work: an AI Engineering Lead, an ML Engineer, a Prompt Engineer, a Data Engineer, an API Engineer, and an AI Quality Evaluator |
| **Frontend Studio** | 6 members | Design-heavy team: a Design Lead, a Frontend Architect, a UI Engineer, an Interaction Engineer, an Accessibility Specialist, and a Performance Engineer |
| **Full-Stack Compact** | 5 members | A lean shipping team: Tech Lead, Frontend, Backend, QA/Security, and DevOps |
| **Research Lab** | 7 members | For investigation and proof-of-concept work: a Research Director, a Deep Researcher, a Technical Analyst, a POC Developer, a Presentation Specialist, a Brand Strategist, and a Documentation Writer |
| **Innovation Sprint** | 6 members | Rapid ideation and validation: an Innovation Lead, a Market Researcher, a Prototype Engineer, a Visual Storyteller, a Technical Writer, and a Validation Analyst |

You can also build custom teams by picking roles, assigning personalities, and choosing which AI models each member uses.

### Every Team Member Is Unique

Just like in a real company, every agent has:

- **A name** (e.g., "Captain," "Nova," "Dash," "Shield")
- **A role** (e.g., Tech Lead, Frontend Engineer, QA Specialist)
- **A tier** -- either *strategic* (a leader who plans and delegates) or *worker* (who executes tasks)
- **A personality** -- a multi-paragraph character description that shapes how the agent communicates
- **A skill set** -- the specific competencies they bring
- **A color and icon** -- for visual identification in the dashboard

Team members reference each other by name. Workers know who their leaders are. Leaders know their workers' strengths. This is not just for show -- it drives how tasks get routed and how problems get escalated.

### A Day in the Life

When a Jarvis team is spawned, here is what happens:

1. **Onboarding.** Each new team member receives a detailed "kickstart" briefing that explains the team's mission, who the other team members are, the tools available, past learnings from previous sessions, and what their specific role is.

2. **Morning standup.** Every few hours, the team holds an automatic standup. Each member reports what they are working on and any blockers. The leader reviews responses and assigns new tasks.

3. **Independent work.** Team members pick up tasks from their personal queue, announce they are starting (preventing duplicate work), execute the task, and report when done.

4. **Brainstorming.** When a team member runs out of work, the system triggers a brainstorming session with a teammate. They propose new tasks together, and the proposals automatically become real tasks in the queue.

5. **File coordination.** Before editing a shared file, a team member "locks" it so no one else can modify it at the same time. When they finish, they release the lock.

6. **Messaging.** Team members can send direct messages to each other, ask questions, request help, and hand off tasks.

7. **Reflection.** After completing a task, each member reflects on what they learned and adds it to their personal knowledge base.

8. **Wrap-up.** When you call "end of day," the team finishes what they are working on, commits their code, saves their memory, and shuts down cleanly.

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

A common problem in multi-agent systems: two agents try to edit the same file at the same time, creating conflicts. Jarvis solves this with a distributed locking system. Before editing a shared file, an agent acquires a lock. Other agents see the lock and work on different files instead. Locks automatically expire after 30 minutes in case an agent forgets to release one.

### Bug Tracking

The QA-role team member scans the codebase for bugs and files them as bug tasks. Bug tasks appear in the queue for other team members to fix. When a fix is committed, the bug is marked resolved.

### Water-Cooler Chat

Occasionally, two team members have a brief informal exchange -- just like coworkers chatting by the water cooler. These interactions are rare and light, but they help the team maintain social context and catch coordination issues that formal meetings miss.

---

## 5. The Learning System

Most AI tools forget everything the moment a conversation ends. Jarvis agents remember.

### Per-Agent Memory

Every team member has a **persistent memory file** that survives restarts, team respawns, and project changes. When a team member completes work and wraps up, they save a summary of what they did, what decisions they made, and what is left to do. Next time they are spawned, that memory is loaded into their briefing so they pick up exactly where they left off.

This means you can:
- Run a team for two hours, stop them, and resume a week later with no loss of context
- Switch team members in and out without losing institutional knowledge
- Build up deep project understanding that compounds over time

### Reflection After Work

After completing a task, agents are prompted to reflect on what they learned. They record a learning entry in one of several categories:

- **Technique** -- a reusable approach that worked well
- **Pattern** -- a recurring shape of problem or solution
- **Pitfall** -- something to avoid next time
- **Tool usage** -- a new way to use an existing tool
- **Codebase insight** -- something specific about this project
- **Collaboration** -- a lesson about working with teammates

Each reflection becomes a searchable entry in the agent's personal learning store.

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

This loop runs forever. Agents always have something to do.

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

When an AI agent reads a file, it consumes "context" -- roughly equivalent to the amount of text it is holding in its head at once. Every line of code it reads costs money.

A single agent working on a real codebase for eight hours can easily spend **$40 to $50** just on context costs -- and most of that is spent reading the same files over and over to understand the code structure.

With a team of five agents, that is $200 to $250 per day on context alone.

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

**Real-world impact:** In internal testing, the knowledge graph reduced context consumption by **5 to 10 times** on exploration tasks. A team that would have cost $200/day in context now costs $20-40/day.

### Why This Matters Financially

Over a month, a 5-agent team running 24/7 could consume:

- **Without knowledge graph**: ~$6,000 in context costs
- **With knowledge graph**: ~$600 to $1,200 in context costs

Most of that savings is pure profit. The knowledge graph is free and runs locally.

---

## 8. Safety and Control

Autonomy is powerful, but only if you can trust it. Jarvis is designed with safety controls at every layer.

### You Can Always Stop the Team

At any moment, you can:

- **Pause a single agent** via the dashboard
- **Graceful wrap-up** -- tell the team to finish current work, commit, save memory, and exit cleanly (takes a few minutes)
- **Hard shutdown** -- immediately stop every agent
- **Force unlock files** -- release any lock that is blocking other work

The wrap-up process is the preferred way to stop a team because it lets agents save their state and finish what they are doing. But if you need an emergency stop, hard shutdown is one click away.

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

Sometimes an agent gets stuck -- maybe it is waiting for input that never comes, or it has entered a repetitive loop. Jarvis's monitoring system detects this and takes action automatically:

1. **First**, send a gentle "are you still working?" prompt
2. **Second**, interrupt the agent and send a recovery prompt
3. **Third**, send a diagnostic prompt asking the agent to report its state
4. **If none work**, mark the agent as stuck and notify you

The goal is to recover silently when possible and alert you only when human intervention is actually needed.

### Complete Audit Log

Every action the team takes is recorded in an audit log:

- When agents join or leave
- Every task assignment, status change, and completion
- Every file lock acquisition and release
- Every message sent between agents
- Every standup, brainstorm, and reflection
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

**The Settings Panel** -- change the team mission, spawn or shut down teams, adjust monitoring intervals.

### Agent Detail View

Click any team member card and you get the full picture of that agent:

- Current task and queue
- Personal memory (what they know about the project)
- Personality (which you can override if needed)
- Learning store (their reflections and insights)
- Skills they have created
- Full task history
- Token and cost usage

You can send a direct prompt to any agent, override their personality, or give them a one-off instruction.

### Real-Time Updates

The dashboard updates live as things happen. When an agent completes a task, you see it within seconds. When a new agent joins a brainstorm, you see the room fill up. When a file lock is acquired, the lock indicator flashes.

### Mobile Notifications via Telegram

You do not have to sit at your desk to monitor the team. Connect your Telegram account and Jarvis sends you:

- Periodic office summaries
- Alerts when an agent gets stuck
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

- **Anthropic Claude** (direct API or subscription)
- **Z.AI** (Chinese company offering "Coding Plan" access to GLM models at a fraction of Claude's cost)
- A mix of providers across the team

The **Z.AI Coding Plan** is particularly interesting: it offers Anthropic-compatible access to GLM models at roughly **1/4 to 1/10 the cost** of Claude, while maintaining comparable quality for coding tasks.

### Rate-Limit-Aware Cost Optimization

Z.AI imposes rate limits on how many agents can run on each model tier simultaneously. A naive deployment would hit the limit and agents would stall. Jarvis automatically distributes agents across model tiers to stay within the limits while keeping your most important agents on the best models.

Example: a 5-agent team might be allocated as:

- **Leader on the best model** (highest quality)
- **Two workers on the second-best model** (good quality)
- **Two workers on the third-best model** (still capable)

You get maximum throughput at minimum cost, and the allocation is completely automatic.

### Custom Teams

If none of the six prebuilt templates fit your needs, build your own. You define:

- How many members the team has
- Each member's name, role, tier, and personality
- Which skills each member brings
- Which AI model each member uses
- Custom instructions and mission

### Personality Overrides

You can override an agent's personality on the fly -- useful for temporary focus shifts. "You are now an ultra-cautious security reviewer until further notice" -- the agent takes on the new personality without being recreated.

### Configurable Autonomy Level

- **Perpetual mode**: team never stops, generates its own work
- **Goal-focused mode**: team stops when assigned work is done
- **Wrap-up mode**: team is told to finish up and shut down cleanly

You choose the level of autonomy you are comfortable with.

---

## 11. Typical Use Cases

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

---

## 12. Business Value

Let us talk about the numbers.

### 24/7 Throughput

A human engineer works about 8 hours a day, 5 days a week. That is roughly 2,000 productive hours a year.

A Jarvis agent works 24 hours a day, 7 days a week. That is 8,760 hours a year -- **4.4 times more**.

A 5-agent team running perpetually produces roughly 5 * 8,760 = 43,800 engineering hours a year. The equivalent of **22 full-time engineers** in calendar hours. (Not a direct substitution -- a human is still more capable in many areas -- but the raw throughput is real.)

### Compounding Learning

Unlike human teams where knowledge walks out the door when people leave, a Jarvis team accumulates knowledge in a structured, persistent form. After three months of running, the team knows your codebase better than most of your human engineers did at month one. After a year, it knows your codebase better than anyone.

This is the compounding factor that makes long-running deployments especially valuable.

### Parallelism Without Coordination Overhead

Adding human engineers to a project does not linearly increase throughput -- every new person adds coordination overhead. But Jarvis agents coordinate automatically through the built-in protocols (standups, brainstorms, task triage, file locks). Doubling the team size roughly doubles the throughput.

### Predictable Costs

You can set a per-hour budget and know exactly what you are paying. The major costs are:

- **AI model costs** (pay-per-token to Anthropic or Z.AI)
- **Compute costs** (the server running Jarvis -- usually $20-50/month)

The largest cost is AI tokens, and that is where the code knowledge graph shines: it can cut your token spend by 5-10x on exploration-heavy work.

### Rough Cost Comparison

Ballpark numbers for a 5-agent team running 24/7 on real codebase work:

| Setup | Monthly AI cost | Notes |
|---|---|---|
| Claude direct, no graph | $4,000 to $8,000 | The expensive option |
| Claude direct, with graph | $500 to $1,500 | Knowledge graph reduces context |
| Z.AI GLM, no graph | $400 to $800 | Cheaper models |
| Z.AI GLM, with graph | $50 to $150 | Best cost-to-value ratio |

These are rough estimates and vary with workload intensity, but the cost savings from thoughtful deployment are substantial.

### Operational Savings

Beyond raw token costs, Jarvis eliminates several categories of operational cost:

- **Task management overhead** -- no Jira, no sprint planning meetings, no standups (the AI runs them)
- **Code review bottlenecks** -- the QA agent reviews in real time
- **Knowledge loss from turnover** -- memory and learnings persist
- **Weekend and holiday downtime** -- agents do not take vacations

---

## 13. What Jarvis Is Not

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

## 14. Getting Started

### What You Need

To run Jarvis, you need:

1. **A server** to host it -- a modest cloud VM (4 CPUs, 8GB RAM) is enough
2. **An AI provider account** -- Claude (Anthropic) or Z.AI or both
3. **A codebase** to run the team on
4. **A git repository** -- the team commits their work automatically
5. **Optional: a Telegram account** -- for mobile notifications

### Deployment Options

**Docker (recommended)**. Jarvis ships as a Docker image. Run one command and it is up.

**Self-hosted binary**. Download the prebuilt binary for Linux or macOS and run it. No Docker required.

### Setup Effort

A technical operator can have Jarvis running in about **30 minutes** from a fresh server:

1. Create a cloud VM (5 minutes)
2. Install Docker (5 minutes)
3. Configure AI provider credentials (5 minutes)
4. Run `docker compose up` (5 minutes)
5. Open the browser and spawn a team (10 minutes)

After the initial setup, starting a new team is a single button click.

### Cost to Run

The server itself costs about **$20 to $50 per month** for a cloud VM. AI token costs vary with workload but typically land in the **$50 to $1,500 per month** range for a 5-agent team depending on which models you use and how heavy the work is.

For comparison, a single human software engineer costs **$10,000 to $20,000 per month** in the US (salary + benefits + overhead). Even at the high end, Jarvis is a fraction of the cost of one engineer while producing the throughput of several.

---

## 15. Security and Reliability

### Built-in Safety

Jarvis includes many layers of protection:

- **Password protection** -- optional login to the dashboard
- **Authentication tokens** -- requests are verified to prevent spoofing
- **Rate limiting** -- prevents brute-force attacks on the login
- **Path isolation** -- agents cannot access files outside their working directory
- **Injection prevention** -- user input is carefully validated before use
- **Encrypted credential storage** -- API keys are stored with restricted file permissions
- **Audit logging** -- every action is recorded for review

### Battle-Tested Code

Jarvis has undergone **10 comprehensive code reviews** with **95 bug fixes** shipped across:

- Memory safety (no crashes or hangs)
- Concurrency (no race conditions)
- Security (no injection, no path traversal)
- Resource management (no leaks)
- Error handling (graceful failures)

The codebase is tested with Go's race detector on every change. Tests pass with over 340 unit and integration tests.

### Graceful Failure Modes

When something goes wrong, Jarvis fails gracefully:

- **Agent crashes**: automatically restarted by the respawn system
- **Server crashes**: all state persists to disk, resumes on restart
- **Network outages**: agents reconnect automatically
- **AI provider outages**: agents pause, resume when the provider returns
- **Disk full**: state persistence is lossless (no partial writes)

You will not find Jarvis corrupting state, losing work, or entering an unrecoverable state.

### Compliance-Friendly Audit Log

Every action is recorded. If you need to demonstrate to auditors that your AI team's actions are traceable, you can export the audit log and show:

- When each agent joined and left
- What tasks were assigned and by whom
- What files were modified and when
- What code was committed and to which branch
- What messages were exchanged between agents

---

## 16. Frequently Asked Questions

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

Depends heavily on how intensive the work is. Ballpark ranges for a 5-agent team running 24/7:

- **Low intensity**: $50 to $200 per month (Z.AI with knowledge graph)
- **Medium intensity**: $200 to $1,000 per month
- **High intensity**: $1,000 to $5,000 per month (Claude without knowledge graph)

Compare to **one human engineer at $120,000 to $240,000 per year** and the math is usually favorable.

### Can I customize the team?

Absolutely. You can:

- Use any of six prebuilt templates
- Build custom teams with any mix of roles
- Write custom personalities
- Override personalities at any time
- Choose different AI models per team member
- Adjust monitoring intervals and thresholds
- Enable or disable specific features

### What happens when agents disagree?

Leaders have authority. If a worker is blocked or uncertain, the task gets escalated to a strategic-tier leader who makes the call. This mirrors how human teams resolve disagreements -- push decisions up the chain of command.

In rare cases where a disagreement is intractable, the task is marked as failed and you are notified.

### How do I stop them?

Three options:

1. **Wrap-up** (recommended): agents finish what they are working on, commit, save memory, and exit cleanly
2. **Shutdown**: immediate stop, all agents terminate within seconds
3. **Pause individual agents**: keep the team alive but halt specific members

All three are available as buttons in the dashboard.

### Do they work on proprietary code?

Yes. Jarvis is self-hosted -- it runs on your server, not ours. Your code never leaves your infrastructure except to query the AI provider (Claude or Z.AI) for the current task. If you are concerned about code going to an AI provider, you can use models that run entirely on your own hardware (though that requires more setup).

### Can I run multiple teams at once?

Yes. Each team runs independently. You can have a backend team working on one project and a frontend team working on another, all from the same Jarvis instance.

### Do I need to be technical to use it?

To **operate** Jarvis day-to-day (spawning teams, monitoring, reviewing work), no technical skill is required. The dashboard is designed to be usable by a product manager or business leader.

To **deploy** Jarvis initially, you need either a technical operator or about 2 hours following the setup guide.

### What if the AI provider has an outage?

Agents pause and wait. When the provider is back online, they resume from exactly where they left off. Nothing is lost.

### Can it work offline?

Not today. Jarvis requires internet access to communicate with the AI provider. If you need fully offline operation, you would need to run a local AI model, which is an advanced deployment.

### How do I migrate to a new codebase?

Spawn a new team pointing at the new codebase. Each team member starts fresh on the new codebase but inherits the shared skill library and distilled insights from previous deployments. You do not lose prior learning, but you do start with a clean memory for the new project.

### Can agents interact with external services?

Yes, within limits. Agents can call APIs, make HTTP requests, and interact with command-line tools. They cannot, however, interact with services that require interactive authentication (like OAuth flows) -- you would need to set those up ahead of time.

### What languages does it support?

Any language Claude Code or Z.AI GLM supports -- which is essentially all major languages: Python, JavaScript, TypeScript, Go, Rust, Java, C#, C++, Ruby, PHP, Swift, Kotlin, and many more. The team members do not have to know the language in advance; they learn it from the codebase.

### How long should I let a team run?

Depends on the goal:

- **Short sprints** (2-8 hours) for focused tasks
- **Daily runs** (8-24 hours) for active projects
- **Multi-day runs** (days to weeks) for long-term refactoring or research
- **Perpetual runs** (indefinite) for continuous improvement

The team's learning system actually improves with longer runs -- they get better at your codebase over time.

---

## 17. Glossary

**Agent** -- A single AI team member with a specific role, name, and personality. Each agent runs as an independent process and coordinates with others through Jarvis.

**Brainstorm** -- A meeting between two or more agents where they propose new tasks. The proposals automatically become real tasks in the queue.

**Dashboard** -- The browser interface for monitoring and controlling Jarvis. Accessible at the server's address.

**Distilled insights** -- Condensed wisdom generated from an agent's accumulated learning reflections. Prevents the learning store from growing indefinitely.

**File lock** -- A temporary reservation on a file so only one agent can edit it at a time. Prevents conflicts.

**Goal-focused mode** -- Operating mode where agents complete their assigned work and stop. The opposite of perpetual mode.

**Kanban board** -- A visual representation of tasks flowing through columns (Backlog → To Do → In Progress → Review → Done). Standard software-project-management interface.

**Kickstart** -- The initial briefing given to each agent when the team is spawned. Includes mission, memory, learnings, role, teammates, and tools.

**Knowledge graph** -- A searchable map of the codebase that lets agents query relationships instead of reading files. Dramatically cuts token costs.

**Leader** -- An agent with strategic tier, responsible for planning and delegating work to workers. Usually the Tech Lead or Engineering Manager role.

**Learning store** -- A per-agent file that accumulates reflections on past work. Feeds into future briefings.

**Memory** -- A per-agent, per-project file that records what the agent knows about the project. Persists across restarts.

**Mission** -- The high-level goal given to the team. Can be enriched over time as the team discovers context.

**Monitoring loop** -- The automatic background process that checks for stuck agents, expired file locks, idle teams, and other issues. Runs every 15 seconds.

**Perpetual mode** -- Operating mode where agents never stop. When a task is done, they find new work automatically.

**RALPH loop** -- The continuous improvement cycle agents follow in perpetual mode: Recognize, Assess, Log, Plan, Handle.

**Reflection** -- A post-task note recording what an agent learned. Categorized and persisted in the learning store.

**Respawn** -- Restarting an agent after a crash or other failure. Automatic.

**Session** -- An individual running instance of an agent. A team is made up of multiple sessions running in parallel.

**Shared skill** -- A reusable technique documented by one agent and visible to all others. Stored in a team-wide skill library.

**Standup** -- A regular meeting where each agent reports their status. Happens automatically on a configurable schedule.

**Strategic tier** -- Leaders who plan and delegate. Opposite of worker tier.

**Task queue** -- The per-agent list of pending, in-progress, and blocked tasks.

**Team template** -- A prebuilt team configuration (e.g., Full-Stack Compact, Research Lab) that you can spawn with one click.

**Triage** -- The act of reassigning a blocked task from one agent to another better suited to solve it.

**Worker tier** -- Agents who execute tasks assigned by leaders. Opposite of strategic tier.

**Wrap-up** -- The graceful shutdown sequence: agents finish current work, commit, save memory, and exit cleanly.

---

## Next Steps

If you are evaluating Jarvis for your organization:

1. **Read the technical README** if you or your team want to understand the implementation
2. **Deploy a trial team** on a non-critical codebase to see it in action
3. **Run for a week** in goal-focused mode so you can review the work
4. **Measure the output** -- tasks completed, bugs fixed, tests written
5. **Scale up** based on what you observe

The biggest barrier to adoption is usually not the technology -- it is cultural. Teams that are open to rethinking how engineering work happens will get the most value. Teams that try to bolt Jarvis onto their existing workflows may find friction.

The good news: you can start small. Spawn a team for a weekend, see what it does, and decide whether to expand. Jarvis is designed to be low-commitment to try and high-value when deployed seriously.

---

*Questions? Open an issue on the project repository or reach out to the maintainers. This document is deliberately non-technical -- if you need engineering detail, see README.md.*
