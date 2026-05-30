package office

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/kshaham/jarvis/internal/office/spawnpayload"
	"github.com/kshaham/jarvis/internal/strs"
	"github.com/kshaham/jarvis/internal/teamtemplates"
)

// PreassignHookSessionIDs generates a fresh UUID session ID per
// member and returns two parallel views:
//
//   - preassignedIDs: memberConfigID → sessionID, used by the spawn
//     loop to thread the same ID through pty.SessionConfig so the
//     spawned process gets the ID we wrote into its hook URLs.
//   - hookSpawnInfo: ordered list consumed by SetupWorktreesWithHooks
//     to write each agent's .claude/settings.local.json BEFORE
//     claude launches. Pre-writing is non-negotiable in hooks mode
//     — if the file lands AFTER claude reads it, every hook URL
//     points at the wrong session and the dispatcher fail-opens.
//
// Pure function. Caller passes the resolved member list and decides
// when to call (hooks mode only — SDK mode lets the SDK assign IDs).
//
// uuid.New() is the same generator pty.NewSDKSession uses, so the
// preassigned IDs match the format of any session ID jarvis ever
// hands out.
func PreassignHookSessionIDs(members []teamtemplates.TeamTemplateMember) (preassignedIDs map[string]string, hookSpawnInfo []HookSpawnInfo) {
	preassignedIDs = make(map[string]string, len(members))
	hookSpawnInfo = make([]HookSpawnInfo, 0, len(members))
	for _, m := range members {
		id := uuid.New().String()
		preassignedIDs[m.ID] = id
		hookSpawnInfo = append(hookSpawnInfo, HookSpawnInfo{
			ConfigID:  m.ID,
			SessionID: id,
		})
	}
	return preassignedIDs, hookSpawnInfo
}

// TemplateLookup is the minimum surface ResolveMemberList needs to
// find a template by ID. Both the built-in registry
// (teamtemplates.BuiltInTemplates) and the operator-saved store
// (teamtemplates.Store.Get) implement it via a thin wrapper.
//
// Keeping this a function type (not an interface) avoids forcing
// callers to define a method receiver; both backing stores can be
// adapted in one line at the call site.
type TemplateLookup func(id string) *teamtemplates.TeamTemplate

// defaultTemplateID is the fallback when the request specifies no
// template or names a template that no longer exists.
const defaultTemplateID = "office-7"

// ResolveMemberList produces the final ordered list of team members
// for a spawn request and reports which template ID was actually
// used (the caller persists this onto the team for later restore).
//
// Order:
//
//  1. Start with the chosen template's members (req.TemplateID, or
//     defaultTemplateID if blank / not-found).
//  2. If req.MemberOverrides is non-empty, treat that list as the
//     source of truth: drop template members the operator removed
//     in the spawn modal, keep the survivors in their template
//     order, then append any overrides whose ID has no template
//     counterpart (inline-added "synthetic" members from the modal's
//     +Add / duplicate button).
//
// Pure: no Context, no SSE, no manager mutations. Safe to call from
// tests with a stub TemplateLookup.
//
// Lives in the office package (not spawnpayload) because it depends
// on teamtemplates which is an office-side concept.
func ResolveMemberList(req spawnpayload.SpawnTeamRequest, lookup TemplateLookup) ([]teamtemplates.TeamTemplateMember, string) {
	var members []teamtemplates.TeamTemplateMember
	templateUsed := defaultTemplateID

	if req.TemplateID != "" && lookup != nil {
		if tpl := lookup(req.TemplateID); tpl != nil {
			members = tpl.Members
			templateUsed = tpl.ID
		}
	}
	if members == nil {
		if lookup != nil {
			if tpl := lookup(defaultTemplateID); tpl != nil {
				members = tpl.Members
			}
		}
		if members == nil {
			// Last-resort fallback when even the default template is
			// missing (unit-test edge-case mostly — production always
			// ships at least one builtin).
			builtins := teamtemplates.BuiltInTemplates()
			if len(builtins) > 0 {
				members = builtins[0].Members
				templateUsed = builtins[0].ID
			}
		}
	}

	// No overrides → return the template list verbatim.
	if len(req.MemberOverrides) == 0 {
		return members, templateUsed
	}

	// Index existing template member IDs so we can tell template
	// members apart from synthetic (inline-added) ones.
	existing := make(map[string]struct{}, len(members))
	for _, m := range members {
		existing[m.ID] = struct{}{}
	}

	// Filter the template list: keep only members the operator
	// retained (their ID appears in MemberOverrides). Preserve
	// template order.
	overrideIndex := make(map[string]struct{}, len(req.MemberOverrides))
	for _, ov := range req.MemberOverrides {
		overrideIndex[ov.ID] = struct{}{}
	}
	filtered := make([]teamtemplates.TeamTemplateMember, 0, len(members))
	for _, m := range members {
		if _, kept := overrideIndex[m.ID]; kept {
			filtered = append(filtered, m)
		}
	}
	members = filtered

	// Append synthetic members for override IDs that don't appear in
	// the template. Provider + model are inherited from the override
	// the same way template members handle them downstream.
	for _, ov := range req.MemberOverrides {
		if _, isTemplate := existing[ov.ID]; isTemplate {
			continue
		}
		members = append(members, teamtemplates.TeamTemplateMember{
			ID:           ov.ID,
			Name:         strs.FirstNonEmpty(ov.Name, ov.ID),
			Role:         strs.FirstNonEmpty(ov.Role, "Engineer"),
			Tier:         strs.FirstNonEmpty(ov.Tier, "worker"),
			Color:        strs.FirstNonEmpty(ov.Color, "blue"),
			Emoji:        ov.Emoji,
			Personality:  ov.Personality,
			Voice:        ov.Voice,
			SystemPrompt: ov.SystemPrompt,
			Skills:       ov.Skills,
		})
	}

	return members, templateUsed
}

// WorkingDirResolver is the callback shape
// ResolveMemberWorkingDir uses to delegate the actual allowlist
// enforcement. The real implementation in handler.Context
// (ResolveWorkingDir) checks the operator-configured
// workingDirAllowlist; tests can pass an identity function.
type WorkingDirResolver func(raw string) (string, error)

// ResolveMemberWorkingDir picks the working directory a single
// agent will spawn into. Precedence:
//
//  1. Per-member override (ov.WorkingDir) when non-empty.
//  2. Team-wide spawnRequest.WorkingDir.
//
// When a per-member override is supplied, it MUST pass the same
// allowlist check the team-level dir went through — otherwise an
// operator could supply `{"memberOverrides":[{"workingDir":"/etc"}]}`
// and bypass policy via the per-member path. The resolver callback
// is the choke point.
//
// Returns the resolved directory plus an error suitable for the
// handler to surface as a 400 (wraps the resolver's error with the
// member ID for diagnosability).
//
// Pure given a deterministic resolver. ov may be nil (member has
// no override at all) — function returns the team-wide value
// unchanged.
func ResolveMemberWorkingDir(
	teamWideDir string,
	memberID string,
	ov *spawnpayload.MemberOverride,
	resolver WorkingDirResolver,
) (string, error) {
	if ov == nil || ov.WorkingDir == "" {
		return teamWideDir, nil
	}
	if resolver == nil {
		return ov.WorkingDir, nil
	}
	resolved, err := resolver(ov.WorkingDir)
	if err != nil {
		return "", fmt.Errorf("invalid workingDir for member %s: %w", memberID, err)
	}
	return resolved, nil
}

// MaxSpawnDelay caps each per-member spawn delay so an operator
// can't accidentally stall a spawn for an unreasonable interval
// (e.g., a typo turning 100ms into 100s would block the rest of
// the team). Exported so handler tests can assert the same bound.
var MaxSpawnDelay = 60 * time.Second

// SpawnDelaySleep applies the per-member spawn delay between two
// agent spawns. Skipped when:
//   - delayMs is non-positive (default behaviour for members with
//     no SpawnDelayMs override)
//   - isLast is true (nothing follows; no point sleeping)
//
// The delay is capped at MaxSpawnDelay. Caller passes memberName
// only so the log line can identify which member's stagger fired.
//
// Pure (modulo time.Sleep). The handler used to live with this
// inline; centralising it gives one place to tune the cap + log
// format if operators ever need different behaviour.
func SpawnDelaySleep(memberName string, delayMs int, isLast bool) {
	if delayMs <= 0 || isLast {
		return
	}
	delay := time.Duration(delayMs) * time.Millisecond
	if delay > MaxSpawnDelay {
		delay = MaxSpawnDelay
	}
	slog.Info("Per-member spawn delay", "name", memberName, "ms", delay.Milliseconds())
	time.Sleep(delay)
}

// ResolveMemberProviderModel layers provider + model sources by
// precedence (low → high) and returns the values BuildMemberPayload
// will surface on the dashboard:
//
//  1. Team-wide default provider (passed in by the caller from
//     operator settings). Model stays empty at this tier.
//  2. Per-template member.Provider / member.Model — custom
//     templates can pin a specific LLM per team member.
//  3. Per-spawn override from the modal — the highest-precedence
//     source. Empty override fields don't clobber.
//  4. Z.AI sonnet fallback — if the resolved model is still empty
//     AND zaiSonnet is non-empty (Z.AI provider + allocator gave
//     this member a tier), use that. Captures the rate-limit-aware
//     allocation when no template / override pinned a model.
//
// Pure function. Pass override=nil when the member has no spawn-
// modal override; pass zaiSonnet="" when Z.AI isn't the provider
// (or the allocator didn't include this member).
func ResolveMemberProviderModel(
	member teamtemplates.TeamTemplateMember,
	override *spawnpayload.MemberOverride,
	teamWideProvider string,
	zaiSonnet string,
) (provider, model string) {
	provider = teamWideProvider
	if member.Provider != "" {
		provider = member.Provider
	}
	if member.Model != "" {
		model = member.Model
	}
	if override != nil {
		if override.Provider != "" {
			provider = override.Provider
		}
		if override.Model != "" {
			model = override.Model
		}
	}
	// Lead/strategic agents on a first-party Claude provider default to
	// Opus 4.8 — the model that authors Dynamic Workflows and benefits
	// most from the higher effort tiers. Workers and non-Claude providers
	// (e.g. zai-glm) keep their cheaper default; an explicit template or
	// spawn-modal model always wins (handled above).
	if model == "" && member.Tier == "strategic" {
		switch provider {
		case "claude-subscription", "", "claude-api":
			model = "claude-opus-4-8"
		case "openai-codex", "openai-custom":
			// Strategic Codex agents (leads) default to the flagship gpt-5.5;
			// workers fall through to gpt-5.4 below.
			model = "gpt-5.5"
		}
	}
	// Codex workers (no template/override/strategic default) default to the
	// full gpt-5.4 (one step down from the gpt-5.5 leads). An explicit
	// per-member model from the template or spawn modal always wins above.
	if model == "" {
		switch provider {
		case "openai-codex", "openai-custom":
			model = "gpt-5.4"
		}
	}
	// Z.AI rate-limit allocator fallback — applies ONLY to zai-glm members.
	// When the TEAM default is zai-glm the allocator runs for every member, so
	// a member that overrode to a different provider (e.g. openai-subscription,
	// which intentionally has no forced default) must NOT inherit a GLM model
	// here. Without the provider guard a subscription agent would be pinned to
	// "glm-5.1" and fail against OpenAI.
	if model == "" && zaiSonnet != "" && provider == "zai-glm" {
		model = zaiSonnet
	}
	return provider, model
}

// HarnessFor maps a provider id to the agent backend ("harness") that runs
// it. Claude providers (and zai-glm, which speaks the Anthropic wire
// protocol) run on the Claude Code harness; the OpenAI providers run on the
// Codex CLI harness. Callers branch the spawn path on this.
func HarnessFor(provider string) string {
	switch provider {
	case "openai-codex", "openai-custom", "openai-subscription":
		return "codex"
	default:
		return "claude"
	}
}

// ProviderEnvBuilder is the callback shape BuildPerMemberEnvOverrides
// uses to delegate the actual env-var construction. The real
// implementation in handler.Context translates a (provider, model,
// sonnet) triple into the full Anthropic/Z.AI environment block;
// tests can pass a stub that records the calls.
//
// Lifting the env construction behind a callback is what makes
// BuildPerMemberEnvOverrides portable into the office package — the
// inner translation depends on operator-configured account
// settings (handler-side) but the loop logic does not.
type ProviderEnvBuilder func(provider, model, sonnet string) map[string]string

// BuildPerMemberEnvOverrides walks the per-member overrides and
// builds the memberID → env-vars map that the spawn loop uses to
// pass per-agent provider tuning to pty.SessionConfig.
//
// sonnetAlloc (when non-nil) carries the Z.AI rate-limit-aware
// model assignment from AllocateZAIModels — passed through to the
// env builder as the per-member "sonnet override" so an agent's
// SDK session picks up the right ANTHROPIC_DEFAULT_SONNET_MODEL.
//
// Pure given a deterministic envBuilder. Members WITHOUT an
// override are absent from the returned map; the spawn loop falls
// back to the team-wide default in that case.
func BuildPerMemberEnvOverrides(
	overrides []spawnpayload.MemberOverride,
	sonnetAlloc map[string]string,
	envBuilder ProviderEnvBuilder,
) map[string]map[string]string {
	out := make(map[string]map[string]string, len(overrides))
	for _, ov := range overrides {
		sonnet := ""
		if sonnetAlloc != nil {
			sonnet = sonnetAlloc[ov.ID]
		}
		out[ov.ID] = envBuilder(ov.Provider, ov.Model, sonnet)
	}
	return out
}

// IndexMemberOverrides builds a memberID → override lookup map from
// the flat overrides slice on a spawn request. Many downstream
// helpers (envvar building, working-dir override, session tuning)
// want O(1) lookups by member ID.
//
// Returns an empty map (not nil) when overrides is empty, so the
// caller can index unconditionally.
func IndexMemberOverrides(overrides []spawnpayload.MemberOverride) map[string]spawnpayload.MemberOverride {
	out := make(map[string]spawnpayload.MemberOverride, len(overrides))
	for _, ov := range overrides {
		out[ov.ID] = ov
	}
	return out
}

// ResolvePerpetualMode resolves the perpetual-mode flag on a spawn
// request. Default is true (agents stay alive and self-replenish
// after task queues drain). Operators explicitly opting out via
// `{"perpetualMode": false}` get a one-shot team.
//
// A pointer-bool on the request (not a plain bool) is the only way
// to distinguish "operator didn't set it" from "operator explicitly
// set false" — important so the default doesn't silently change
// when the spawn modal omits the field.
func ResolvePerpetualMode(req spawnpayload.SpawnTeamRequest) bool {
	if req.PerpetualMode == nil {
		return true
	}
	return *req.PerpetualMode
}

// CollectModelRequests gathers the inputs the Z.AI rate-limit
// allocator needs: an ordered list of every member's ID, plus a
// map of memberID → operator-requested model (drawn from per-member
// overrides; members without an override are absent from the map,
// which AllocateZAIModels treats as "use the default tier").
//
// Pure function. Caller passes the resolved member list + the same
// overrides slice that flowed through ResolveMemberList and
// ApplyMemberOverrides.
func CollectModelRequests(
	members []teamtemplates.TeamTemplateMember,
	overrides []spawnpayload.MemberOverride,
) (memberIDs []string, requestedModels map[string]string) {
	memberIDs = make([]string, 0, len(members))
	for _, m := range members {
		memberIDs = append(memberIDs, m.ID)
	}
	requestedModels = make(map[string]string, len(overrides))
	for _, ov := range overrides {
		if ov.Model != "" {
			requestedModels[ov.ID] = ov.Model
		}
	}
	return memberIDs, requestedModels
}

// BuildTeamMemberConfigs converts a resolved member list into the
// []*TeamMemberConfig shape the OfficeManager + kickstart prompt
// builder consume. One-to-one mapping of the fields that survive
// into the runtime team state (Provider / Model live elsewhere —
// they flow through env vars on the spawn).
//
// Returned configs are fresh pointers, safe to mutate per-member
// downstream (e.g., ResumeClaudeSessionID propagation from
// snapshot-restore).
func BuildTeamMemberConfigs(members []teamtemplates.TeamTemplateMember) []*TeamMemberConfig {
	out := make([]*TeamMemberConfig, 0, len(members))
	for _, m := range members {
		out = append(out, &TeamMemberConfig{
			ID:           m.ID,
			Name:         m.Name,
			Role:         m.Role,
			Tier:         m.Tier,
			Color:        m.Color,
			Personality:  m.Personality,
			Voice:        m.Voice,
			SystemPrompt: m.SystemPrompt,
			Skills:       m.Skills,
		})
	}
	return out
}

// ApplyMemberOverrides overlays per-member field overrides (name,
// role, tier, color, emoji, personality, system prompt, skills) onto
// an existing member list IN PLACE. Empty / zero-value override
// fields are skipped — only non-default values win. Members with no
// matching override are left untouched.
//
// The mutation is in-place because the caller already owns the
// slice (returned by ResolveMemberList) and doesn't need a copy.
// Synthetic members appended by ResolveMemberList carry the same
// override fields, so this pass also smooths out their final shape
// (e.g., when an override has both ov.Name and is a synthetic
// member, ResolveMemberList already set Name; this re-applies as a
// no-op).
func ApplyMemberOverrides(members []teamtemplates.TeamTemplateMember, overrides []spawnpayload.MemberOverride) {
	if len(overrides) == 0 || len(members) == 0 {
		return
	}
	index := make(map[string]spawnpayload.MemberOverride, len(overrides))
	for _, ov := range overrides {
		index[ov.ID] = ov
	}
	for i := range members {
		ov, has := index[members[i].ID]
		if !has {
			continue
		}
		if ov.Name != "" {
			members[i].Name = ov.Name
		}
		if ov.Role != "" {
			members[i].Role = ov.Role
		}
		if ov.Tier != "" {
			members[i].Tier = ov.Tier
		}
		if ov.Color != "" {
			members[i].Color = ov.Color
		}
		if ov.Emoji != "" {
			members[i].Emoji = ov.Emoji
		}
		if ov.Personality != "" {
			members[i].Personality = ov.Personality
		}
		if ov.Voice != "" {
			members[i].Voice = ov.Voice
		}
		if ov.SystemPrompt != "" {
			members[i].SystemPrompt = ov.SystemPrompt
		}
		if len(ov.Skills) > 0 {
			members[i].Skills = ov.Skills
		}
	}
}
