import type { CSSProperties } from 'react';
import { Input } from '@/components/ui';
import { ChipGroup, Field, ModelSelect, SectionCard, Toggle } from './formPrimitives';
import { inputStyle } from './formPrimitives.utils';

type Provider =
  | 'claude-subscription'
  | 'claude-api'
  | 'zai-glm'
  | 'openai-codex'
  | 'openai-custom'
  | 'openai-subscription';

const PROVIDERS: { value: Provider; label: string; hint: string; placeholder: string }[] = [
  {
    value: 'claude-subscription',
    label: 'Claude · Subscription',
    hint: 'Uses the operator account login (no API key needed).',
    placeholder: 'claude-opus-4-8',
  },
  {
    value: 'claude-api',
    label: 'Claude · API',
    hint: 'Direct Anthropic API. Requires ANTHROPIC_API_KEY.',
    placeholder: 'claude-opus-4-8',
  },
  {
    value: 'zai-glm',
    label: 'Z.AI · GLM',
    hint: 'Anthropic-compatible Z.AI API. Requires ZAI_API_KEY.',
    placeholder: 'glm-5.2',
  },
  {
    value: 'openai-codex',
    label: 'OpenAI · Codex (API key)',
    hint: 'Runs the whole team on the Codex CLI billed per-token against the shared OpenAI API key.',
    placeholder: 'gpt-5.5',
  },
  {
    value: 'openai-subscription',
    label: 'OpenAI · Codex (subscription)',
    hint: 'Runs on your ChatGPT/Codex Pro plan via `codex login` (no per-token cost). Uses the plan’s models — leave Model blank for the default.',
    placeholder: '(plan default)',
  },
  {
    value: 'openai-custom',
    label: 'OpenAI · Custom endpoint',
    hint: 'Codex CLI against an internal OpenAI-compatible endpoint (Responses API). Set the base URL + API key below.',
    placeholder: 'gpt-5.5',
  },
];

const KNOWN_VALUES = new Set<string>(PROVIDERS.map((p) => p.value));

// providerLabel returns the short display label for a provider value (e.g.
// "Claude · Subscription"), or the raw value for a custom/unknown provider.
// Used by the spawn rail's secondary-value line.
export function providerLabel(provider: string): string {
  return PROVIDERS.find((p) => p.value === provider)?.label ?? provider;
}

const INTERVALS = [30, 60, 120, 240];

// Perpetual auto-wrap-up durations in hours; 0 renders as "Unlimited".
const PERPETUAL_HOURS = [0, 1, 2, 4, 6, 8, 12, 24, 48, 72];

// Any OpenAI provider runs on the Codex harness, where Dynamic Workflows is N/A.
export function isCodexHarnessProvider(provider: string): boolean {
  return (
    provider === 'openai-codex' ||
    provider === 'openai-custom' ||
    provider === 'openai-subscription'
  );
}

const stackStyle: CSSProperties = { display: 'flex', flexDirection: 'column', gap: 10 };

const codexLoginHintStyle: CSSProperties = {
  padding: '8px 10px',
  borderRadius: 'var(--radius-md)',
  background: 'var(--lg-fill)',
  border: '1px solid var(--lg-rim)',
  fontFamily: 'var(--font-mono)',
  fontSize: 12,
  color: 'var(--text-secondary)',
};

// ProviderModelSection — the team-wide provider + model defaults tile. Owns
// the conditional OpenAI base-URL / API-key fields and the Codex-login hint;
// these defaults apply to every member unless overridden in the editor.
export function ProviderModelSection({
  defaultProvider,
  setDefaultProvider,
  defaultModel,
  setDefaultModel,
  openaiBaseUrl,
  setOpenaiBaseUrl,
  openaiApiKey,
  setOpenaiApiKey,
  openaiKeyConfigured,
}: {
  defaultProvider: string;
  setDefaultProvider: (v: string) => void;
  defaultModel: string;
  setDefaultModel: (v: string) => void;
  openaiBaseUrl: string;
  setOpenaiBaseUrl: (v: string) => void;
  openaiApiKey: string;
  setOpenaiApiKey: (v: string) => void;
  openaiKeyConfigured: boolean;
}) {
  const activeProvider = KNOWN_VALUES.has(defaultProvider)
    ? PROVIDERS.find((p) => p.value === defaultProvider) ?? null
    : null;
  const providerHint = activeProvider
    ? activeProvider.hint
    : `Custom provider: ${defaultProvider || '(none)'}`;
  const modelPlaceholder = activeProvider?.placeholder ?? '(provider default)';
  const isSubscription = defaultProvider === 'openai-subscription';
  const showOpenAIKeyFields =
    defaultProvider === 'openai-codex' || defaultProvider === 'openai-custom';

  return (
    <SectionCard
      title="Provider & model"
      hint="Applies to every member unless overridden in the editor."
    >
      <Field label="Provider" hint={providerHint}>
        <ChipGroup<Provider | ''>
          ariaLabel="Default provider"
          options={PROVIDERS.map((p) => ({ value: p.value, label: p.label }))}
          value={activeProvider ? activeProvider.value : ''}
          onChange={(v) => {
            if (v) setDefaultProvider(v);
          }}
        />
      </Field>

      <Field label="Model" hint="Leave blank for the provider default.">
        <ModelSelect
          provider={defaultProvider}
          value={defaultModel}
          onChange={setDefaultModel}
          placeholder={modelPlaceholder}
          ariaLabel="Default model"
        />
      </Field>

      {isSubscription ? (
        <Field
          label="Codex login required"
          hint="One-time, in the container: run  CODEX_HOME=~/.jarvis/codex-auth codex login  to authorize your ChatGPT/Codex Pro account. jarvis stages those OAuth credentials into each agent — no API key, no per-token cost."
        >
          <div style={codexLoginHintStyle}>CODEX_HOME=~/.jarvis/codex-auth codex login</div>
        </Field>
      ) : null}

      {showOpenAIKeyFields ? (
        <div style={stackStyle}>
          {defaultProvider === 'openai-custom' ? (
            <Field
              label="OpenAI base URL"
              hint="Internal endpoint, e.g. https://your-proxy/v1 (must speak the Responses API)."
            >
              <Input
                value={openaiBaseUrl}
                onChange={(e) => setOpenaiBaseUrl(e.target.value)}
                placeholder="https://api.openai.com/v1"
                aria-label="OpenAI base URL"
                style={{ fontFamily: 'var(--font-mono)' }}
              />
            </Field>
          ) : null}
          <Field
            label="OpenAI API key"
            hint={
              openaiKeyConfigured
                ? 'A shared key is already configured for deployment — leave blank to use it.'
                : 'Sent as a Bearer token to the endpoint. Stored in settings.'
            }
          >
            <Input
              type="password"
              value={openaiApiKey}
              onChange={(e) => setOpenaiApiKey(e.target.value)}
              placeholder={openaiKeyConfigured ? 'configured ✓ (override optional)' : 'sk-...'}
              aria-label="OpenAI API key"
              style={{ fontFamily: 'var(--font-mono)' }}
            />
          </Field>
        </div>
      ) : null}
    </SectionCard>
  );
}

// TogglesSection — the team-wide behavior switches (Options). `defaultProvider`
// only drives the Dynamic-workflows hint (ignored on the Codex harness).
// Telegram moved to its own Advanced section (TelegramSection, below).
export function TogglesSection({
  defaultProvider,
  perpetual,
  setPerpetual,
  perpetualHours,
  setPerpetualHours,
  sevenStep,
  setSevenStep,
  workflowsEnabled,
  setWorkflowsEnabled,
}: {
  defaultProvider: string;
  perpetual: boolean;
  setPerpetual: (b: boolean) => void;
  perpetualHours: number;
  setPerpetualHours: (n: number) => void;
  sevenStep: boolean;
  setSevenStep: (b: boolean) => void;
  workflowsEnabled: boolean;
  setWorkflowsEnabled: (b: boolean) => void;
}) {
  const isCodexHarness = isCodexHarnessProvider(defaultProvider);
  return (
    <SectionCard title="Options" hint="Team-wide behavior switches.">
      <div style={stackStyle}>
        <Toggle
          label="Perpetual improvement"
          value={perpetual}
          onChange={setPerpetual}
          hint="Auto-restart agents that finish their queue."
        />
        {perpetual ? (
          <Field label="Auto wrap-up after (hours)">
            <select
              value={perpetualHours}
              onChange={(e) => setPerpetualHours(Number(e.target.value))}
              style={inputStyle}
              aria-label="Perpetual auto wrap-up hours"
            >
              {PERPETUAL_HOURS.map((h) => (
                <option key={h} value={h}>
                  {h === 0 ? 'Unlimited' : `${h} hours`}
                </option>
              ))}
            </select>
          </Field>
        ) : null}
        <Toggle
          label="7-step engineering"
          value={sevenStep}
          onChange={setSevenStep}
          hint="Drive each task through DEFINE→PLAN→BUILD→VERIFY→REVIEW→SIMPLIFY→SHIP (agent-skills). Combines with the mode above."
        />
        <Toggle
          label="Dynamic workflows"
          value={workflowsEnabled}
          onChange={setWorkflowsEnabled}
          hint={
            isCodexHarness
              ? 'Claude Code only — ignored on the Codex harness.'
              : 'Let leads fan out subagents for large tasks. Off disables it team-wide.'
          }
        />
      </div>
    </SectionCard>
  );
}

// TelegramSection — the (Advanced) Telegram notifications fields, split out of
// Options so the primary flow stays focused on behavior switches.
export function TelegramSection({
  telegramEnabled,
  setTelegramEnabled,
  telegramInterval,
  setTelegramInterval,
}: {
  telegramEnabled: boolean;
  setTelegramEnabled: (b: boolean) => void;
  telegramInterval: number;
  setTelegramInterval: (n: number) => void;
}) {
  return (
    <SectionCard title="Telegram" hint="Periodic office summaries to your phone.">
      <div style={stackStyle}>
        <Toggle
          label="Telegram updates"
          value={telegramEnabled}
          onChange={setTelegramEnabled}
          hint="Sends an office summary on a cadence."
        />
        {telegramEnabled ? (
          <Field label="Interval (minutes)">
            <select
              value={telegramInterval}
              onChange={(e) => setTelegramInterval(Number(e.target.value))}
              style={inputStyle}
              aria-label="Telegram interval minutes"
            >
              {INTERVALS.map((v) => (
                <option key={v} value={v}>
                  every {v} min
                </option>
              ))}
            </select>
          </Field>
        ) : null}
      </div>
    </SectionCard>
  );
}
