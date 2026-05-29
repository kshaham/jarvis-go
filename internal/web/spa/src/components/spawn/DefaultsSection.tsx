import { Input } from '@/components/ui';
import { ChipGroup, Field, inputStyle, SectionCard, Toggle } from './formPrimitives';

type Provider = 'claude-subscription' | 'claude-api' | 'zai-glm';

const PROVIDERS: { value: Provider; label: string; hint: string; placeholder: string }[] = [
  {
    value: 'claude-subscription',
    label: 'Claude · Subscription',
    hint: 'Uses the operator account login (no API key needed).',
    placeholder: 'claude-opus-4-7',
  },
  {
    value: 'claude-api',
    label: 'Claude · API',
    hint: 'Direct Anthropic API. Requires ANTHROPIC_API_KEY.',
    placeholder: 'claude-opus-4-7',
  },
  {
    value: 'zai-glm',
    label: 'Z.AI · GLM',
    hint: 'Anthropic-compatible Z.AI API. Requires ZAI_API_KEY.',
    placeholder: 'glm-5',
  },
];

const KNOWN_VALUES = new Set<string>(PROVIDERS.map((p) => p.value));

const INTERVALS = [30, 60, 120, 240];

export function DefaultsSection({
  defaultProvider,
  setDefaultProvider,
  defaultModel,
  setDefaultModel,
  perpetual,
  setPerpetual,
  telegramEnabled,
  setTelegramEnabled,
  telegramInterval,
  setTelegramInterval,
}: {
  defaultProvider: string;
  setDefaultProvider: (v: string) => void;
  defaultModel: string;
  setDefaultModel: (v: string) => void;
  perpetual: boolean;
  setPerpetual: (b: boolean) => void;
  telegramEnabled: boolean;
  setTelegramEnabled: (b: boolean) => void;
  telegramInterval: number;
  setTelegramInterval: (n: number) => void;
}) {
  const activeProvider = KNOWN_VALUES.has(defaultProvider)
    ? PROVIDERS.find((p) => p.value === defaultProvider) ?? null
    : null;
  const providerHint = activeProvider
    ? activeProvider.hint
    : `Custom provider: ${defaultProvider || '(none)'}`;
  const modelPlaceholder = activeProvider?.placeholder ?? '(provider default)';

  return (
    <SectionCard
      title="Defaults"
      hint="Applies to every member unless overridden in the editor below."
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
        <Input
          value={defaultModel}
          onChange={(e) => setDefaultModel(e.target.value)}
          placeholder={modelPlaceholder}
          aria-label="Default model"
          style={{ fontFamily: 'var(--font-mono)' }}
        />
      </Field>

      <div
        style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))',
          gap: 12,
        }}
      >
        <Toggle
          label="Perpetual improvement"
          value={perpetual}
          onChange={setPerpetual}
          hint="Auto-restart agents that finish their queue."
        />
        <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
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
      </div>
    </SectionCard>
  );
}
