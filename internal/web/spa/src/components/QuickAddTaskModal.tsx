import { useRef, useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';

import { Modal } from './motion/Modal';
import { Button } from './ui/Button';
import { Chip } from './ui/Chip';
import { Field, Input, Textarea } from './ui/Field';
import { fetchJSON } from '../api/client';
import { useOfficeStatus } from '../hooks/useOfficeStatus';
import type { TaskPriority } from '../api/types';

type Props = {
  open: boolean;
  onClose: () => void;
};

const PRIORITIES: TaskPriority[] = ['high', 'medium', 'low'];

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

  // Reset the form when the modal opens. Track the previous open value
  // in a ref (never rendered) so the setters below fire during the
  // parent-triggered render without causing a second re-render cycle.
  const prevOpenRef = useRef(open);
  if (open !== prevOpenRef.current) {
    prevOpenRef.current = open;
    if (open) {
      setTitle('');
      setDescription('');
      setPriority('medium');
      if (!sessionId && members.length > 0) setSessionId(members[0].sessionId);
    }
  }

  const submit = useMutation({
    mutationFn: () =>
      fetchJSON('/api/office/agent/task/self-add', {
        method: 'POST',
        body: { sessionId, title, description, priority },
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['office', 'task-queues'] });
      toast.success('Task added to queue');
      onClose();
    },
    onError: () => toast.error('Could not add task'),
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
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button onClick={() => submit.mutate()} disabled={!canSubmit || submit.isPending}>
            {submit.isPending ? 'Adding…' : 'Add task'}
          </Button>
        </>
      }
    >
      <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
        <Field label="Assign to">
          <select
            value={sessionId}
            onChange={(e) => setSessionId(e.target.value)}
            className="field"
          >
            <option value="">Select an agent…</option>
            {members.map((m) => (
              <option key={m.sessionId} value={m.sessionId}>
                {m.name} · {m.role}
              </option>
            ))}
          </select>
        </Field>
        <Field label="Title">
          <Input
            autoFocus
            value={title}
            onChange={(e) => setTitle(e.target.value.slice(0, 200))}
            placeholder="Short summary (≤ 200 chars)"
          />
        </Field>
        <Field label="Description" hint="Optional details (≤ 800 chars).">
          <Textarea
            value={description}
            onChange={(e) => setDescription(e.target.value.slice(0, 800))}
            rows={4}
            placeholder="Details, links, expected output, etc."
            style={{ resize: 'vertical' }}
          />
        </Field>
        <Field label="Priority">
          <div style={{ display: 'flex', gap: 6 }}>
            {PRIORITIES.map((p) => (
              <Chip
                key={p}
                active={p === priority}
                onClick={() => setPriority(p)}
                className="capitalize"
              >
                {p}
              </Chip>
            ))}
          </div>
        </Field>
      </div>
    </Modal>
  );
}
