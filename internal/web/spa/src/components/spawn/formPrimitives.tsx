import { modelOptionsForProvider } from '../../api/types';
import { inputStyle } from './formPrimitives.utils';

// Thin re-exports of the shared Aurora UI primitives. The spawn screen
// historically defined its own SectionCard / Field / Toggle / ChipGroup
// / SegmentedControl; those now live in '@/components/ui' as the single
// source of truth. We keep the old import surface intact so every
// spawn/* file resolves without churn — SectionCard is the legacy alias
// for the shared Section.
export { Section as SectionCard, Field, Toggle, ChipGroup, SegmentedControl } from '@/components/ui';

// ModelSelect — a per-provider model dropdown. When the provider has a known
// model catalog (PROVIDER_MODEL_OPTIONS), it renders a <select> seeded with
// those models plus a blank "(provider default)" option; an out-of-catalog
// current value (e.g. a template-pinned model) is preserved as an extra
// option so selecting it round-trips. For unknown/custom providers it falls
// back to a free-text input so operators can still type any model id.
export function ModelSelect({
  provider,
  value,
  onChange,
  placeholder,
  ariaLabel,
}: {
  provider: string;
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
  ariaLabel?: string;
}) {
  const options = modelOptionsForProvider(provider);
  if (options.length === 0) {
    return (
      <input
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder ?? '(provider default)'}
        aria-label={ariaLabel ?? 'Model'}
        style={{ ...inputStyle, fontFamily: 'var(--font-mono)' }}
      />
    );
  }
  // Preserve a current value that isn't in the catalog (custom/pinned model).
  const withCurrent = value && !options.includes(value) ? [value, ...options] : options;
  return (
    <select
      value={value}
      onChange={(e) => onChange(e.target.value)}
      aria-label={ariaLabel ?? 'Model'}
      style={{ ...inputStyle, fontFamily: 'var(--font-mono)' }}
    >
      <option value="">{placeholder ?? '(provider default)'}</option>
      {withCurrent.map((m) => (
        <option key={m} value={m}>
          {m}
        </option>
      ))}
    </select>
  );
}
