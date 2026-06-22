---
name: office-feature-improvement
description: How an office agent in perpetual mode keeps finding real work -- the INVENTORY -> PICK -> SCOPE -> BUILD -> RECORD cycle that turns "improve the project" into concrete enhancements to existing features and valuable new features, so a team never idles or declares the project "done" / "mined dry". Read this whenever your task queue is empty.
---

# Office Feature Improvement

In perpetual (RALPH) mode your standing job is to keep making the project materially
better for its users. The failure this skill prevents: a team burns through the obvious
small chores (a few tests, a lint pass, a doc tweak), finds no more chores, and concludes
the project is "solid" or "mined dry" -- then goes idle. That conclusion is always wrong.
A real product always has a next most-valuable improvement. Your job is to find it.

## What "improve" means

> Improving the project = scan its EXISTING features, then either DEEPEN an existing
> feature or ADD a valuable new feature. Tests, lint, and docs SUPPORT that work -- they
> are not themselves the improvement. Never stop at chores; never declare the project done.

## Cardinal rule

> Empty queue -> run the cycle below -> self-add ONE substantive, user-facing,
> verifiable task -> start it. Inventory before you pick. Record after you ship.

## The improvement cycle

### 1. INVENTORY -- know what the project already does
Spend a few minutes building a mental map before deciding anything:
- Read the project's entry points, pages/screens, and the key modules for your role.
- Skim the `README` and any product/spec docs.
- Open the live preview (the running dev instance) and actually use the app.
- Read `./IMPROVEMENTS.md` if it exists -- it lists what already shipped and what's
  queued, so you neither repeat work nor re-propose a rejected idea.
- `cm roster` -- see what teammates are mid-flight so you pick something different.

### 2. PICK the single highest-value piece of work
Choose ONE, biased toward user-visible value:
- **Deepen an existing feature** -- the most reliable source of work. Real features are
  rarely "finished": add the missing states (empty/loading/error), edge cases, filters,
  sorting, search, validation, keyboard support, responsive/mobile layout, polish, copy.
- **Add a new feature the project is missing** -- something a real user of this kind of
  product would expect but the project lacks.
- **Classify by lens** when stuck for ideas: completeness, UX/usability, accessibility
  (a11y), performance, robustness/error-handling, visual design/consistency.
Prefer high value + clearly in your role + not overlapping a teammate.

### 3. SCOPE it into one verifiable task
- One concrete deliverable, sized to finish AND pass the verify gate within one loop.
- Write a real title + description and acceptance criteria a gate can check.
- `cm task self-add "<title>" --priority <high|medium>` (add `--criterion "<check>"`).
- Off-limits without a human: auth, payments, deploy, schema migrations, dependency
  upgrades, architecture rewrites. Those escalate to your lead, never self-add.

### 4. BUILD it past the verify gate
- Implement, then run the project's own test/build command yourself.
- A task is DONE only when that gate passes -- not when you say "done". For web-app
  missions, confirm it renders in the operator live preview (see `team-dev-instance`).
- Wrap multi-step work in `/goal` (see `office-goal-loop`).

### 5. RECORD what you shipped
- Append a line to `./IMPROVEMENTS.md`: what you changed, and (optionally) the next
  obvious follow-up. This is the team's memory that stops the "mined dry" loop -- the
  next agent reads it, sees the trail, and picks the next item instead of re-deriving.
- `cm reflect "<lesson>"` for anything reusable.

Then go back to step 1. The loop only ends when the team wraps up or a hard stop fires.

## Worked example

You're a Frontend Engineer; your queue just went empty.

1. **INVENTORY:** open the live preview. The app is a savings-finder with a results list
   and a detail page. `./IMPROVEMENTS.md` shows the hero + results grid already shipped.
   `cm roster` -- a teammate is on the detail page. You take the results list.
2. **PICK:** the results list has no empty state and no sort control -- both are concrete
   gaps a real user hits. Sorting is higher value. (Deepen an existing feature.)
3. **SCOPE:** `cm task self-add "Add sort control (price, name) to results list" --priority high --criterion "results reorder on sort change; build+typecheck pass"`.
4. **BUILD:** implement the control + sort logic, run the project's `build`/`test`, verify
   the live preview reorders.
5. **RECORD:** append to `./IMPROVEMENTS.md`: "results list: added price/name sort.
   Next: add an empty state + a result-count badge." Start the next loop.

## Anti-patterns

- **Declaring victory.** "Site's solid / codebase mined dry / nothing left to add." Always
  false in perpetual mode -- it means you stopped at INVENTORY. Pick a feature and go.
- **Chore loops.** Endlessly adding tests / lint-fixes / doc tweaks because they feel safe.
  Those support real improvements; they are not the improvement. Ship a feature.
- **Skipping the inventory.** Self-adding "refactor things" without knowing what exists
  produces low-value churn and teammate overlap.
- **Boiling the ocean.** One giant "redesign everything" task that never passes a gate.
  Scope to one deliverable per loop.
- **Forgetting to record.** No `IMPROVEMENTS.md` trail -> the team re-proposes the same
  ideas and circles back to "mined dry".
- **Self-adding off-limits work.** Auth/payments/deploy/migration/deps/architecture go to
  the lead, not your own queue.

## Related skills

- `office-goal-loop` -- wrap the BUILD step in `/goal` so an evaluator gates "done".
- `team-dev-instance` -- run + verify the live preview for web-app missions.
- `office-protocols` -- the `cm task ack/start/progress/complete` lifecycle.
- `office-knowledge-search` -- check what teammates already learned before you build.

## Verification

You're applying this skill correctly when, with an empty queue, you can in a few minutes
name (a) what the project already does, (b) the single highest-value next improvement and
why, and (c) the one verifiable task you just self-added and started -- and `IMPROVEMENTS.md`
grows by a line every time you finish one.
