import type { CSSProperties } from 'react';

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
