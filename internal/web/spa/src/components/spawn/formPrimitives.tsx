import type { CSSProperties } from 'react';

// Thin re-exports of the shared Aurora UI primitives. The spawn screen
// historically defined its own SectionCard / Field / Toggle / ChipGroup
// / SegmentedControl; those now live in '@/components/ui' as the single
// source of truth. We keep the old import surface intact so every
// spawn/* file resolves without churn — SectionCard is the legacy alias
// for the shared Section.
export { Section as SectionCard, Field, Toggle, ChipGroup, SegmentedControl } from '@/components/ui';

// inputStyle — the shared inline style for bare <input>/<textarea>/<select>
// elements that aren't yet swapped to the <Input>/<Textarea> components
// (e.g. number/select inputs and mono-font fields). Mirrors the .field
// CSS class so focus/hover affordances stay consistent.
export const inputStyle: CSSProperties = {
  padding: 8,
  background: 'var(--lg-fill)',
  border: '1px solid var(--lg-rim)',
  borderRadius: 'var(--radius-md)',
  color: 'var(--text-primary)',
  fontSize: 13,
  outline: 'none',
  width: '100%',
  fontFamily: 'inherit',
  WebkitBackdropFilter: 'blur(var(--lg-blur-tile)) saturate(var(--lg-saturate))',
  backdropFilter: 'blur(var(--lg-blur-tile)) saturate(var(--lg-saturate))',
  boxShadow: 'var(--lg-specular-concave)',
};
