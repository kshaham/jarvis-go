import type { MemberEdit } from './memberEditor.utils';

// Rough, ILLUSTRATIVE daily cost heuristic for the spawn screen. These are
// hand-tuned per-agent/day USD figures for a "busy" agent, NOT billed
// actuals and NOT sourced from any pricing API — they exist only to give
// operators a relative sense of cost before spawning. Tune freely.
const RATES = {
  opus: 12, // large frontier model
  sonnet: 4, // mid model
  haiku: 1, // small model
  glm: 2, // Z.AI GLM
  gpt: 8, // OpenAI gpt-5.x via Codex/custom (per-token)
  generic: 5, // unknown metered provider
} as const;

// Perpetual improvement keeps agents working after their queue drains, so
// metered spend runs higher — bump the estimate.
const PERPETUAL_MULTIPLIER = 1.5;

export interface CostEstimate {
  perDay: number; // estimated metered USD/day across the team
  memberCount: number;
  meteredCount: number; // members on a per-token (non-subscription) provider
  allSubscription: boolean; // every member runs on a flat-rate plan → no per-token cost
}

// Subscription providers are flat-rate (operator login / ChatGPT-Codex plan):
// no per-token cost, so they don't add to the metered estimate.
function isSubscription(provider: string): boolean {
  return provider === 'claude-subscription' || provider === 'openai-subscription';
}

// Best-effort per-agent/day rate for a metered provider+model. Matches the
// model name first (so per-member overrides are honored), then falls back to
// a sensible provider default when the model is left blank.
function meteredRate(provider: string, model: string): number {
  const m = model.toLowerCase();
  if (m.includes('opus')) return RATES.opus;
  if (m.includes('sonnet')) return RATES.sonnet;
  if (m.includes('haiku')) return RATES.haiku;
  if (m.includes('glm')) return RATES.glm;
  if (m.includes('gpt')) return RATES.gpt;
  // Model blank → infer from the provider default.
  if (provider === 'zai-glm') return RATES.glm;
  if (provider === 'claude-api') return RATES.opus; // default model is opus
  if (provider === 'openai-codex' || provider === 'openai-custom') return RATES.gpt;
  return RATES.generic;
}

export function estimateDailyCost(
  members: MemberEdit[],
  defaultProvider: string,
  defaultModel: string,
  perpetual: boolean,
): CostEstimate {
  let perDay = 0;
  let meteredCount = 0;
  for (const mem of members) {
    const provider = mem.provider || defaultProvider;
    const model = mem.model || defaultModel;
    if (isSubscription(provider)) continue;
    meteredCount += 1;
    perDay += meteredRate(provider, model);
  }
  if (perpetual) perDay *= PERPETUAL_MULTIPLIER;
  return {
    perDay,
    memberCount: members.length,
    meteredCount,
    allSubscription: meteredCount === 0,
  };
}

// Format a USD/day figure compactly (no cents — it's an estimate).
export function formatPerDay(perDay: number): string {
  return `~$${Math.round(perDay)}/day`;
}
