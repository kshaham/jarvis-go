import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';

import { Modal } from './motion/Modal';
import { Press } from './motion';
import { fetchJSON } from '../api/client';
import { useOfficeStatus } from '../hooks/useOfficeStatus';
import type { TaskPriority } from '../api/types';

type Props = {
  open: boolean;
  onClose: () => void;
};

// QuickAddTaskModal — bound to the AppShell "+ Task" button and the
// `t` keyboard shortcut. Drops a task into any agent's queue via
// /api/office/agent/task/self-add. Focus-trapped + Esc-closeable by
// the Modal primitive.
export function QuickAddTaskModal({ open, onClose }: Props) {
  const status = useOfficeStatus();
  const qc = useQueryClient();
  const members = status.data?.members ?? [];

  const [sessionId, setSessionId] = useState('');
  const [title, setTitle] = useState('');
  const [description, setDescription] = useState('');
  const [priority, setPriority] = useState<TaskPriority>('medium');

  // Reset the form when the modal opens. Adjust-state-during-render
  // pattern (mirror of the relay compose modal).
  const [lastOpen, setLastOpen] = useState(open);
  if (open !== lastOpen) {
    if (open) {
      setTitle('');
      setDescription('');
      setPriority('medium');
      if (!sessionId && members.length > 0) setSessionId(members[0].sessionId);
    }
    setLastOpen(open);
  }

  const submit = useMutation({
    mutationFn: () =>
      fetchJSON('/api/office/agent/task/self-add', {
        method: 'POST',
        body: { sessionId, title, description, priority },
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['office', 'task-queues'] });
      onClose();
    },
  });

  const canSubmit = !!sessionId && title.trim().length > 0;

  return (
    <Modal
      open={open}
      onOpenChange={(next) => (next ? null : onClose())}
      size="md"
      title="Quick-add task"
      description="Drop a task into any agent's queue. They'll pick it up at the next turn."
      footer={
        <>
          <Press pressScale={0.96} onClick={onClose}>
            <span
              style={{
                padding: '8px 14px',
                background: 'transparent',
                color: 'var(--text-muted)',
                borderRadius: 'var(--radius-md)',
                fontSize: 13,
                fontWeight: 600,
              }}
            >
              Cancel
            </span>
          </Press>
          <Press
            pressScale={0.96}
            onClick={() => submit.mutate()}
            disabled={!canSubmit || submit.isPending}
          >
            <span
              style={{
                padding: '8px 16px',
                background: 'var(--accent)',
                color: '#fff',
                borderRadius: 'var(--radius-md)',
                fontSize: 13,
                fontWeight: 600,
                opacity: !canSubmit || submit.isPending ? 0.5 : 1,
              }}
            >
              {submit.isPending ? 'Adding…' : 'Add task'}
            </span>
          </Press>
        </>
      }
    >
      <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
        <Field label="Assign to">
          <select value={sessionId} onChange={(e) => setSessionId(e.target.value)} style={inputStyle}>
            <option value="">Select an agent…</option>
            {members.map((m) => (
              <option key={m.sessionId} value={m.sessionId}>
                {m.name} · {m.role}
              </option>
            ))}
          </select>
        </Field>
        <Field label="Title">
          <input
            autoFocus
            value={title}
            onChange={(e) => setTitle(e.target.value.slice(0, 200))}
            placeholder="Short summary (≤ 200 chars)"
            style={inputStyle}
          />
        </Field>
        <Field label="Description" hint="Optional details (≤ 800 chars).">
          <textarea
            value={description}
            onChange={(e) => setDescription(e.target.value.slice(0, 800))}
            rows={4}
            placeholder="Details, links, expected output, etc."
            style={{ ...inputStyle, resize: 'vertical', fontFamily: 'inherit' }}
          />
        </Field>
        <Field label="Priority">
          <div style={{ display: 'flex', gap: 6 }}>
            {(['high', 'medium', 'low'] as TaskPriority[]).map((p) => {
              const active = p === priority;
              const color =
                p === 'high'
                  ? 'var(--status-error)'
                  : p === 'low'
                    ? 'var(--text-muted)'
                    : 'var(--status-warn)';
              return (
                <Press key={p} pressScale={0.95} onClick={() => setPriority(p)}>
                  <span
                    style={{
                      padding: '6px 14px',
                      background: active ? 'var(--accent-muted)' : 'var(--bg-pressed)',
                      border: '1px solid ' + (active ? 'var(--accent)' : 'var(--border-default)'),
                      borderRadius: 999,
                      color: active ? 'var(--accent)' : color,
                      fontSize: 12,
                      fontWeight: 600,
                      textTransform: 'capitalize',
                      display: 'inline-block',
                    }}
                  >
                    {p}
                  </span>
                </Press>
              );
            })}
          </div>
        </Field>
      </div>
    </Modal>
  );
}

function Field({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: string;
  children: React.ReactNode;
}) {
  return (
    <label style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
      <span style={{ fontSize: 11, color: 'var(--text-muted)', letterSpacing: 0.4, textTransform: 'uppercase' }}>
        {label}
      </span>
      {children}
      {hint ? <span style={{ fontSize: 11, color: 'var(--text-faint)', lineHeight: 1.4 }}>{hint}</span> : null}
    </label>
  );
}

const inputStyle: React.CSSProperties = {
  padding: 10,
  background: 'var(--bg-app)',
  border: '1px solid var(--border-default)',
  borderRadius: 'var(--radius-md)',
  color: 'var(--text-primary)',
  fontSize: 13,
  outline: 'none',
  width: '100%',
};
