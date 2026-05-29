import { forwardRef } from 'react';
import type {
  InputHTMLAttributes,
  ReactNode,
  TextareaHTMLAttributes,
} from 'react';

import { cn } from '@/lib/cn';

// Input / Textarea — token-styled form controls with the .field focus
// affordances. API-compatible with components/spawn/formPrimitives so
// those screens can migrate by import swap later.
export const Input = forwardRef<HTMLInputElement, InputHTMLAttributes<HTMLInputElement>>(
  function Input({ className, ...rest }, ref) {
    return <input ref={ref} className={cn('field', className)} {...rest} />;
  },
);

export const Textarea = forwardRef<HTMLTextAreaElement, TextareaHTMLAttributes<HTMLTextAreaElement>>(
  function Textarea({ className, ...rest }, ref) {
    return <textarea ref={ref} className={cn('field', className)} {...rest} />;
  },
);

// Field — label + control + hint wrapper with consistent vertical rhythm.
export function Field({
  label,
  hint,
  htmlFor,
  children,
}: {
  label?: ReactNode;
  hint?: ReactNode;
  htmlFor?: string;
  children: ReactNode;
}) {
  return (
    <div className="flex flex-col gap-1.5">
      {label ? (
        <label htmlFor={htmlFor} className="text-xs font-semibold" style={{ color: 'var(--text-secondary)' }}>
          {label}
        </label>
      ) : null}
      {children}
      {hint ? (
        <span className="text-[11px]" style={{ color: 'var(--text-muted)' }}>
          {hint}
        </span>
      ) : null}
    </div>
  );
}

// NumField — a labelled number input on the shared .field styling.
export function NumField({
  label,
  value,
  onChange,
  min,
  max,
  step,
  hint,
}: {
  label?: ReactNode;
  value: number;
  onChange: (n: number) => void;
  min?: number;
  max?: number;
  step?: number;
  hint?: ReactNode;
}) {
  return (
    <Field label={label} hint={hint}>
      <input
        type="number"
        className="field"
        value={value}
        min={min}
        max={max}
        step={step ?? 1}
        onChange={(e) => onChange(Number(e.target.value))}
      />
    </Field>
  );
}
