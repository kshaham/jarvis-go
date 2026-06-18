---
name: office-self-improvement
description: How an office agent proposes and lands a change to the office system itself -- editing SKILL.md files, adjusting per-role bundles in internal/office/skills.go, extending kickstart.go, respecting the <12000 char size budget, and routing the change through office-code-review and Forge. The meta-capability that lets agents evolve their own environment.
---

# Office Self-Improvement

Every other skill in this library teaches agents how to do their job. This one teaches agents how to **improve the environment itself** -- edit the SKILL.md files, change per-role bundles, extend kickstart, so the next spawn is better than this one.

This is a meta-capability. A high bar applies: if you're reading this skill because you want to polish a comma, stop. If you're reading this skill because you've noticed a *systemic* problem that will keep biting agents until it's fixed, continue.

## Cardinal rule

> Systemic problem + measurable improvement + `office-code-review` pass + test coverage = ship it. Anything short of all four: file a task instead.

## When to reach for this skill

**Yes** -- the kind of change that belongs here:

- You noticed three different agents all re-derived the same pattern because no skill documented it. (Write a new skill.)
- You noticed a role bundle doesn't include a skill the role actually needs. (Swap a slot in `suggestedSkillsByRole`.)
- You noticed `kickstart.go` is missing a section that would save every spawn several reads. (Extend `BuildKickstartPrompt`.)
- You noticed a skill has become stale (file references moved, advice is contradicted by a newer pattern). (Update the skill.)
- You shipped a new subsystem (new auth flow, new cm command, new MCP tool) and the existing skills don't mention it. (Add a skill or update the existing one.)

**No** -- file `cm task self-add` instead:

- You want to fix a typo or reword a paragraph. (Not systemic.)
- You disagree with a skill's stylistic choice but can't point to concrete failures. (Preference.)
- The skill is fine but verbose. (Trimming is discretionary, not systemic.)
- You want to add a skill for a topic only one agent touches once a month. (Low ROI.)

## The change surface

Three areas you might edit:

### 1. Skills library (`claudeman/skills/<name>/SKILL.md`)

Each skill has YAML frontmatter (`name`, `description`) + markdown body. Hard caps:

- **200 lines per skill** (so `cm skill read` doesn't itself blow context).
- **Single-line description** (the frontmatter cache in `internal/office/skills.go` reads only the first line).
- **No multi-line YAML folded scalars** (same reason).

When editing an existing skill, preserve the structure: Cardinal rule -> When to use -> Patterns -> Worked examples -> Anti-patterns -> Verification. Other agents rely on this shape to skim efficiently.

When adding a new skill, follow the naming:
- `office-*` for team workflow / discipline skills.
- `go-*` for Go language + ecosystem skills.
- `jarvis-*` for jarvis-go repo-specific subsystems.
- Others for stack-agnostic tools (e.g. `git-advanced`, `shell-scripting-safe`, `docker-compose-debug`).

Read `office-skill-discovery` before adding -- it explains the search-then-read discipline your new skill should support.

### 2. Per-role bundles (`internal/office/skills.go`)

`suggestedSkillsByRole` is a `map[string][]string` with 7 role keys and 8-slot bundles each, plus `defaultSuggestedSkills`. Every slot is precious -- the kickstart only shows the top 8 for each role.

To change a bundle:

1. Decide which slot to drop. Dropped skills stay discoverable via `cm skill search`.
2. Add the new skill name.
3. **Update `TestPickSkillsForRoleMatchesExpectedBuckets`** in `kickstart_test.go` to reflect the new assertion. If you don't, tests break.
4. Re-run `go test ./internal/office/...` -- the size-regression test (`TestKickstartPromptUnderSizeLimit`) confirms you haven't blown the 12000-char cap.

Rule of thumb: swap 1:1 unless you're actively shrinking a bundle. Growing past 8 slots requires a wider discussion.

### 3. Kickstart prompt (`internal/office/kickstart.go`)

`BuildKickstartPrompt` is the function that assembles the initial system prompt every agent sees on spawn. It's deliberately short (round 1 slimmed it from ~280 lines to ~140). The 12000-char hard cap is enforced by `TestKickstartPromptUnderSizeLimit`.

Changes here should be rare. Good reasons:

- Adding a *pointer* to a new skill so agents know it exists (e.g. "read `cm skill read X` when Y").
- Reordering sections for better discoverability.
- Adding a new role-specific block (like the existing "strategic tier" leader section).

Bad reasons:

- Pasting the content of a skill directly into the prompt (defeats the round-1 slim-down).
- Adding per-role customization that could be handled via `suggestedSkillsByRole` instead.
- Adding a "temporary" section that never gets removed.

Any kickstart change MUST keep the size test green. Run it before committing.

## The workflow

1. **Identify the systemic problem.** Write it down in one sentence: "Agents repeatedly X because no skill / bundle / kickstart line covers Y."
2. **File a task for yourself** via `cm task self-add "improve: <one-line>" "<description>" medium`. This creates a paper trail and makes the work reviewable.
3. **Sketch the fix on a scratch branch.** You already live on `agent/<your-id>` -- just make the edit there. Use `git-advanced` worktree pattern if you need to compare against main.
4. **Test.** Run `go test ./internal/office/...` minimum. If you touched `skills.go` or `kickstart.go`, also check the size regression test.
5. **Ask for review.** Send a `cm message` to a peer (preferably one whose role bundle is affected) referencing the task ID and the diff. Use `office-code-review` as the review gate.
6. **Address feedback.** Revise until the reviewer approves or escalates.
7. **Ship via Forge.** Forge is the only agent who merges `agent/<id>` -> `main`. Send the review-approved branch + task ID to Forge via `cm message`.
8. **Record the change.** Add a line to your `cm memory section task_history` describing what changed and why. Future agents (including your future self) need the context.

## Worked example: "I noticed agents keep re-deriving the Secure cookie rule"

Suppose as Bolt you're reviewing a teammate's auth patch and realize they set the cookie Secure flag from `isHTTPS` (the global flag) instead of from `isRequestSecure(r)`. You check -- there's no skill covering this. Every new backend task is going to trip on the same rule.

**Systemic?** Yes -- it's not a one-off bug, it's a gap in documentation.

Walk through:

1. **Problem:** "Backend agents hardcode `isHTTPS` for cookie Secure flag because no skill documents the `isRequestSecure(r)` per-request rule."
2. **`cm task self-add "improve: document per-request Secure cookie rule in jarvis-auth skill"`**
3. **Sketch:** Notice `jarvis-auth` skill exists -- good, no new skill needed. Add a section to the existing one covering `isRequestSecure`, the regression it prevented, and the rule "always pass `*http.Request`."
4. **Test:** `wc -l skills/jarvis-auth/SKILL.md` (confirm still under 200), no Go test needed for docs-only edit.
5. **Review:** Send to Sage (architect) via `cm message` with task ID + diff path.
6. **Revise:** Sage notes the example code should match real `internal/web/auth/manager.go` line numbers. Fix.
7. **Ship:** Forward approved patch to Forge with task ID. Forge merges.
8. **Record:** `cm memory section task_history --append "[2026-04-11] task improve-auth-skill: added isRequestSecure section to jarvis-auth skill. Prevents repeat of login regression bug."`

Total elapsed time: maybe 30 minutes. Every future backend agent now gets the rule on their first `cm skill read jarvis-auth`.

## Anti-patterns

- **Skill churn.** Polishing skills instead of doing real work. Tasks > skills.
- **Bypassing `office-code-review`.** The review gate exists so one agent's bright idea doesn't ship unreviewed to everyone.
- **Growing a bundle past 8 slots.** The cap is a feature -- it forces prioritization.
- **Adding a skill for one specific incident.** That's a `cm reflect` entry, not a full skill. Use the `office-reflection` pattern instead.
- **Editing `kickstart.go` to add content** that should be in a skill. Kickstart is a pointer library, not a documentation library.
- **Forgetting to update tests** when you change `suggestedSkillsByRole`. Breaks CI, wastes time.
- **Editing someone else's role bundle without consulting them.** If you're Bolt and you want to change Nova's designer bundle, message Nova first.

## Verification

1. `go build ./... && go test ./internal/office/...` -- all green.
2. `TestKickstartPromptUnderSizeLimit` specifically -- still under 12000 chars per role.
3. `TestPickSkillsForRoleMatchesExpectedBuckets` specifically -- your assertions match reality.
4. `wc -l <edited-skills>` -- every skill still under 200 lines.
5. Review approval recorded from at least one peer.
6. The problem you started with no longer applies -- spin up a fresh agent (or re-read your own kickstart) and confirm the new skill / bundle / kickstart line is discoverable and load-bearing.
