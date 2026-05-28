import * as Dialog from '@radix-ui/react-dialog';
import { AnimatePresence, motion, useReducedMotion } from 'motion/react';
import type { ReactNode } from 'react';

import { spring, timing } from '../../lib/motion';

export type ModalSize = 'sm' | 'md' | 'lg' | 'wide' | 'full';

const SIZE_MAX_WIDTH: Record<ModalSize, string> = {
  sm: '480px',
  md: '640px',
  lg: '840px',
  wide: '1100px',
  full: '95vw',
};

type Props = {
  open: boolean;
  onOpenChange: (next: boolean) => void;
  // Required for screen readers; placed in the visual heading too.
  title: string;
  // Optional second line under the title.
  description?: string;
  // 'sm' (480) | 'md' (640) | 'lg' (840) | 'wide' (1100) | 'full' (95vw).
  // 'wide' is the canonical size for multi-pane editors like
  // ComposeRelayModal. 'md' matches the CommandBar baseline.
  size?: ModalSize;
  children: ReactNode;
  // Optional fixed footer (action row). Rendered below the body with a
  // top divider so it visually anchors the long content above.
  footer?: ReactNode;
  // Hides the default header — for fully custom panels like the
  // CommandBar.  When `true` the caller is responsible for rendering
  // an accessible heading using <Dialog.Title> + <Dialog.Description>.
  hideHeader?: boolean;
};

// Modal — the canonical office-redesign dialog primitive. Built on
// Radix Dialog so the accessibility floor is handled (focus trap,
// scroll lock, Esc to close, aria attrs). Motion-wrapped for the
// signature scale-in-from-96% + 8px-rise enter; backdrop blurs to
// 24px (via the --elev-command-blur token). Five size presets cover
// every modal we expect to ship.
export function Modal({
  open,
  onOpenChange,
  title,
  description,
  size = 'md',
  children,
  footer,
  hideHeader,
}: Props) {
  const reduce = useReducedMotion();
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <AnimatePresence>
        {open ? (
          <Dialog.Portal forceMount>
            <Dialog.Overlay asChild>
              <motion.div
                key="overlay"
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                exit={{ opacity: 0 }}
                transition={timing.short}
                style={{
                  position: 'fixed',
                  inset: 0,
                  zIndex: 60,
                  background: 'rgba(6,8,12,0.55)',
                  backdropFilter: 'blur(var(--elev-command-blur))',
                  WebkitBackdropFilter: 'blur(var(--elev-command-blur))',
                }}
              />
            </Dialog.Overlay>
            <Dialog.Content asChild aria-describedby={description ? 'modal-desc' : undefined}>
              <motion.div
                key="content"
                initial={reduce ? { opacity: 0 } : { opacity: 0, scale: 0.96, y: -8 }}
                animate={reduce ? { opacity: 1 } : { opacity: 1, scale: 1, y: 0 }}
                exit={reduce ? { opacity: 0 } : { opacity: 0, scale: 0.96, y: -8 }}
                transition={spring.smooth}
                style={{
                  position: 'fixed',
                  top: '8vh',
                  left: '50%',
                  transform: 'translateX(-50%)',
                  zIndex: 61,
                  width: 'min(100% - 32px, ' + SIZE_MAX_WIDTH[size] + ')',
                  maxHeight: '84vh',
                  display: 'flex',
                  flexDirection: 'column',
                  background: 'var(--bg-elevated)',
                  border: '1px solid var(--border-strong)',
                  borderRadius: 'var(--radius-xl)',
                  boxShadow: 'var(--elev-command-shadow)',
                  overflow: 'hidden',
                }}
              >
                {hideHeader ? (
                  // Caller renders their own visually-styled heading; we
                  // still expose Dialog.Title to satisfy ARIA.
                  <Dialog.Title style={visuallyHidden}>{title}</Dialog.Title>
                ) : (
                  <header
                    style={{
                      padding: '18px 22px 14px',
                      borderBottom: '1px solid var(--border-subtle)',
                      display: 'flex',
                      flexDirection: 'column',
                      gap: 4,
                    }}
                  >
                    <Dialog.Title
                      style={{
                        margin: 0,
                        fontSize: 20,
                        fontWeight: 600,
                        color: 'var(--text-primary)',
                      }}
                    >
                      {title}
                    </Dialog.Title>
                    {description ? (
                      <Dialog.Description
                        id="modal-desc"
                        style={{
                          margin: 0,
                          fontSize: 12,
                          color: 'var(--text-muted)',
                        }}
                      >
                        {description}
                      </Dialog.Description>
                    ) : null}
                  </header>
                )}
                <div
                  style={{
                    flex: '1 1 auto',
                    overflow: 'auto',
                    padding: hideHeader ? 0 : '18px 22px',
                  }}
                >
                  {children}
                </div>
                {footer ? (
                  <footer
                    style={{
                      padding: '14px 22px',
                      borderTop: '1px solid var(--border-subtle)',
                      display: 'flex',
                      justifyContent: 'flex-end',
                      gap: 10,
                      background: 'var(--bg-surface)',
                    }}
                  >
                    {footer}
                  </footer>
                ) : null}
              </motion.div>
            </Dialog.Content>
          </Dialog.Portal>
        ) : null}
      </AnimatePresence>
    </Dialog.Root>
  );
}

// Used for the hidden Dialog.Title when the caller renders their own
// header. Standard sr-only pattern — content remains accessible but
// is removed from the visual layout.
const visuallyHidden: React.CSSProperties = {
  position: 'absolute',
  width: 1,
  height: 1,
  padding: 0,
  margin: -1,
  overflow: 'hidden',
  clip: 'rect(0,0,0,0)',
  whiteSpace: 'nowrap',
  border: 0,
};
