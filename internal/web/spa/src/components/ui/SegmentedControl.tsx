import { motion, useReducedMotion } from 'motion/react';
import { useId } from 'react';

import { spring } from '@/lib/motion';

// SegmentedControl — a compact tab/segmented switch on a recessed liquid-
// glass track. The selected segment is a raised glass thumb that slides
// between options via a shared `layoutId` spring (matching the nav-rail
// active pill). Reduced motion drops the slide for an instant swap.
export function SegmentedControl<T extends string>({
  options,
  value,
  onChange,
  ariaLabel,
}: {
  options: { value: T; label: string }[];
  value: T;
  onChange: (v: T) => void;
  ariaLabel?: string;
}) {
  const groupId = useId();
  const reduce = useReducedMotion();
  return (
    <div role="tablist" aria-label={ariaLabel} className="lg-segment">
      {options.map((opt) => {
        const selected = opt.value === value;
        return (
          <button
            key={opt.value}
            type="button"
            role="tab"
            aria-selected={selected}
            onClick={() => onChange(opt.value)}
            className="lg-segment__btn focus-ring"
          >
            {selected ? (
              reduce ? (
                <span className="lg-segment__thumb" aria-hidden />
              ) : (
                <motion.span
                  layoutId={`seg-thumb-${groupId}`}
                  className="lg-segment__thumb"
                  transition={spring.smooth}
                  aria-hidden
                />
              )
            ) : null}
            <span style={{ position: 'relative', zIndex: 1 }}>{opt.label}</span>
          </button>
        );
      })}
    </div>
  );
}
