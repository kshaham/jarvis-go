import { Modal } from './motion/Modal';

const SHORTCUTS: { keys: string[]; label: string }[] = [
  { keys: ['⌘', 'K'], label: 'Open the command palette' },
  { keys: ['t'], label: 'Quick-add a task' },
  { keys: ['?'], label: 'Show this shortcuts dialog' },
  { keys: ['Esc'], label: 'Close any dialog' },
];

// ShortcutsModal — the keyboard cheat-sheet opened with `?`.
export function ShortcutsModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  return (
    <Modal
      open={open}
      onOpenChange={(next) => (next ? null : onClose())}
      size="sm"
      title="Keyboard shortcuts"
      description="Move around the office without the mouse."
    >
      <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
        {SHORTCUTS.map((s) => (
          <div
            key={s.label}
            style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 16 }}
          >
            <span style={{ fontSize: 13, color: 'var(--text-secondary)' }}>{s.label}</span>
            <span style={{ display: 'inline-flex', gap: 4 }}>
              {s.keys.map((k) => (
                <kbd
                  key={k}
                  style={{
                    fontFamily: 'var(--font-mono)',
                    fontSize: 12,
                    minWidth: 22,
                    textAlign: 'center',
                    padding: '2px 6px',
                    borderRadius: 6,
                    background: 'var(--bg-pressed)',
                    border: '1px solid var(--border-default)',
                    color: 'var(--text-primary)',
                  }}
                >
                  {k}
                </kbd>
              ))}
            </span>
          </div>
        ))}
      </div>
    </Modal>
  );
}
