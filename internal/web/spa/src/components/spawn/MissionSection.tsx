import { Textarea } from '@/components/ui';
import { SectionCard } from './formPrimitives';

const MAX = 1000;

export function MissionSection({
  value,
  onChange,
  templateName,
}: {
  value: string;
  onChange: (v: string) => void;
  templateName?: string;
}) {
  const len = value.length;
  const near = len > MAX * 0.8;
  return (
    <SectionCard
      title="Mission"
      hint="What's the team working on?"
      trailing={
        <span
          style={{
            fontSize: 12,
            color: near ? 'var(--status-warn)' : 'var(--text-muted)',
            fontVariantNumeric: 'tabular-nums',
          }}
        >
          {len} / {MAX}
        </span>
      }
    >
      <Textarea
        value={value}
        onChange={(e) => onChange(e.target.value.slice(0, MAX))}
        rows={5}
        placeholder={
          templateName
            ? `e.g. Ship the ${templateName} stack: scope, build, ship.`
            : 'e.g. Migrate the auth service to the new identity provider.'
        }
        aria-label="Team mission"
        style={{
          resize: 'vertical',
          minHeight: 96,
          lineHeight: 1.5,
        }}
      />
    </SectionCard>
  );
}
