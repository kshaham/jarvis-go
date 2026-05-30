import React, { useMemo, useReducer, useRef } from 'react';
import { toast } from 'sonner';

import { Modal } from './motion/Modal';
import { Metric } from './motion/Metric';
import { Button } from './ui/Button';
import { Field, Input, Textarea } from './ui/Field';
import { usePeers } from '../hooks/usePeers';
import { useSendRelay } from '../hooks/useRelayMessages';
import { useOfficeStatus } from '../hooks/useOfficeStatus';
import type { RelayAttachment } from '../api/types';

const MAX_CONTENT = 4000;

const previewPanelStyle: React.CSSProperties = {
  flex: 1,
  padding: 14,
  borderRadius: 'var(--radius-md)',
  background: 'var(--bg-surface)',
  border: '1px solid var(--border-subtle)',
  overflowY: 'auto',
  fontSize: 13,
  color: 'var(--text-primary)',
  lineHeight: 1.55,
  whiteSpace: 'pre-wrap',
  fontFamily: 'var(--font-sans)',
};

// ---- State management -------------------------------------------------------

interface FormState {
  toPeerId: string;
  content: string;
  missionExcerpt: string;
  includeMission: boolean;
  customAttachments: RelayAttachment[];
  customDraft: { title: string; body: string };
  error: string | null;
}

type FormAction =
  | { type: 'setToPeerId'; value: string }
  | { type: 'setContent'; value: string }
  | { type: 'setMissionExcerpt'; value: string }
  | { type: 'setIncludeMission'; value: boolean }
  | { type: 'addCustomAttachment'; item: RelayAttachment }
  | { type: 'removeCustomAttachment'; index: number }
  | { type: 'setCustomDraftTitle'; value: string }
  | { type: 'setCustomDraftBody'; value: string }
  | { type: 'setError'; value: string | null }
  | { type: 'openReset'; toPeerId: string; missionExcerpt: string }
  | { type: 'closeReset' };

function formReducer(state: FormState, action: FormAction): FormState {
  switch (action.type) {
    case 'setToPeerId':
      return { ...state, toPeerId: action.value };
    case 'setContent':
      return { ...state, content: action.value };
    case 'setMissionExcerpt':
      return { ...state, missionExcerpt: action.value };
    case 'setIncludeMission':
      return { ...state, includeMission: action.value };
    case 'addCustomAttachment':
      return {
        ...state,
        customAttachments: [...state.customAttachments, action.item],
        customDraft: { title: '', body: '' },
      };
    case 'removeCustomAttachment':
      return {
        ...state,
        customAttachments: state.customAttachments.filter((_, idx) => idx !== action.index),
      };
    case 'setCustomDraftTitle':
      return { ...state, customDraft: { ...state.customDraft, title: action.value } };
    case 'setCustomDraftBody':
      return { ...state, customDraft: { ...state.customDraft, body: action.value } };
    case 'setError':
      return { ...state, error: action.value };
    case 'openReset':
      return { ...state, error: null, toPeerId: action.toPeerId, missionExcerpt: action.missionExcerpt };
    case 'closeReset':
      return {
        ...state,
        content: '',
        missionExcerpt: '',
        includeMission: false,
        customAttachments: [],
        customDraft: { title: '', body: '' },
      };
    default:
      return state;
  }
}

const INITIAL_FORM: FormState = {
  toPeerId: '',
  content: '',
  missionExcerpt: '',
  includeMission: false,
  customAttachments: [],
  customDraft: { title: '', body: '' },
  error: null,
};

// ---- Sub-components ---------------------------------------------------------

interface RecipientFieldProps {
  toPeerId: string;
  peerList: Array<{ peerId: string; displayName: string }>;
  onChange: (id: string) => void;
}

function RecipientField({ toPeerId, peerList, onChange }: RecipientFieldProps) {
  return (
    <Field label="Recipient">
      <select
        value={toPeerId}
        onChange={(e) => onChange(e.target.value)}
        className="field"
      >
        <option value="">Select a peer team…</option>
        {peerList.map((p) => (
          <option key={p.peerId} value={p.peerId}>
            {p.displayName}
          </option>
        ))}
      </select>
    </Field>
  );
}

interface AttachSectionProps {
  missionExcerpt: string;
  includeMission: boolean;
  customDraft: { title: string; body: string };
  customAttachments: RelayAttachment[];
  onToggleMission: (checked: boolean) => void;
  onMissionExcerptChange: (value: string) => void;
  onCustomDraftTitleChange: (value: string) => void;
  onCustomDraftBodyChange: (value: string) => void;
  onAddCustomAttachment: () => void;
  onRemoveCustomAttachment: (index: number) => void;
}

function AttachSection({
  missionExcerpt,
  includeMission,
  customDraft,
  customAttachments,
  onToggleMission,
  onMissionExcerptChange,
  onCustomDraftTitleChange,
  onCustomDraftBodyChange,
  onAddCustomAttachment,
  onRemoveCustomAttachment,
}: AttachSectionProps) {
  return (
    <Field label="Attach" hint="Knowledge to share alongside the message.">
      <label
        style={{
          display: 'flex',
          alignItems: 'flex-start',
          gap: 10,
          padding: 10,
          borderRadius: 'var(--radius-md)',
          background: 'var(--bg-surface)',
          border: '1px solid var(--border-subtle)',
        }}
      >
        <input
          type="checkbox"
          checked={includeMission}
          onChange={(e) => onToggleMission(e.target.checked)}
          style={{ marginTop: 3 }}
        />
        <div style={{ flex: 1 }}>
          <div style={{ fontSize: 13, fontWeight: 600, color: 'var(--text-primary)' }}>
            Mission excerpt
          </div>
          <Textarea
            value={missionExcerpt}
            onChange={(e) => onMissionExcerptChange(e.target.value.slice(0, 2000))}
            placeholder="Pre-filled from the current team mission. Edit freely."
            rows={4}
            disabled={!includeMission}
            style={{
              marginTop: 6,
              fontSize: 12,
              opacity: includeMission ? 1 : 0.55,
              resize: 'vertical',
            }}
          />
        </div>
      </label>

      {/* Custom attachment composer */}
      <div
        style={{
          padding: 10,
          borderRadius: 'var(--radius-md)',
          background: 'var(--bg-surface)',
          border: '1px solid var(--border-subtle)',
          display: 'flex',
          flexDirection: 'column',
          gap: 6,
        }}
      >
        <div style={{ fontSize: 13, fontWeight: 600, color: 'var(--text-primary)' }}>
          Custom note
        </div>
        <Input
          value={customDraft.title}
          onChange={(e) => onCustomDraftTitleChange(e.target.value)}
          placeholder="Title (e.g. 'Lessons from the auth migration')"
        />
        <Textarea
          value={customDraft.body}
          onChange={(e) => onCustomDraftBodyChange(e.target.value)}
          placeholder="Free-form notes; markdown allowed."
          rows={3}
          style={{ resize: 'vertical' }}
        />
        <Button
          variant="secondary"
          size="sm"
          onClick={onAddCustomAttachment}
          className="self-start"
        >
          + Add attachment
        </Button>
      </div>

      {customAttachments.length > 0 ? (
        <ul style={{ listStyle: 'none', padding: 0, margin: 0, display: 'flex', flexDirection: 'column', gap: 6 }}>
          {customAttachments.map((a, i) => (
            <li
              key={`${a.kind}:${a.title}`}
              className="transition-colors hover:bg-[var(--bg-elevated)]"
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 8,
                padding: '6px 10px',
                background: 'var(--bg-pressed)',
                borderRadius: 'var(--radius-sm)',
                fontSize: 12,
              }}
            >
              <span style={{ flex: 1, color: 'var(--text-primary)' }}>{a.title}</span>
              <button
                type="button"
                onClick={() => onRemoveCustomAttachment(i)}
                className="focus-ring transition-colors hover:text-status-error"
                style={{
                  background: 'transparent',
                  border: 'none',
                  color: 'var(--text-muted)',
                  cursor: 'pointer',
                  fontSize: 14,
                }}
                aria-label="Remove attachment"
              >
                ×
              </button>
            </li>
          ))}
        </ul>
      ) : null}
    </Field>
  );
}

interface PreviewPanelProps {
  content: string;
  attachments: RelayAttachment[];
}

function PreviewPanel({ content, attachments }: PreviewPanelProps) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
      <div
        style={{
          fontSize: 12,
          color: 'var(--text-muted)',
          letterSpacing: 0.4,
          textTransform: 'uppercase',
        }}
      >
        Preview
      </div>
      <div style={previewPanelStyle}>
        {content.trim() ? content : (
          <span style={{ color: 'var(--text-faint)', fontStyle: 'italic' }}>
            The message body shows here as the operator types.
          </span>
        )}
        {attachments.length > 0 ? (
          <>
            <hr
              style={{
                border: 'none',
                borderTop: '1px solid var(--border-subtle)',
                margin: '14px 0',
              }}
            />
            <div
              style={{
                fontSize: 12,
                color: 'var(--text-muted)',
                letterSpacing: 0.4,
                textTransform: 'uppercase',
                marginBottom: 8,
              }}
            >
              Knowledge attached
            </div>
            {attachments.map((a, i) => (
              <details
                key={`${a.kind}:${a.title}`}
                open={i === 0}
                style={{
                  marginBottom: 8,
                  padding: '8px 10px',
                  borderRadius: 'var(--radius-sm)',
                  background: 'var(--bg-elevated)',
                  border: '1px solid var(--border-subtle)',
                }}
              >
                <summary
                  style={{
                    cursor: 'pointer',
                    fontWeight: 600,
                    color: 'var(--text-primary)',
                    fontSize: 13,
                  }}
                >
                  {kindBadge(a.kind)} {a.title}
                </summary>
                <div
                  style={{
                    marginTop: 6,
                    whiteSpace: 'pre-wrap',
                    fontSize: 12,
                    color: 'var(--text-secondary)',
                  }}
                >
                  {a.body}
                </div>
              </details>
            ))}
          </>
        ) : null}
      </div>
    </div>
  );
}

// ---- Main component ---------------------------------------------------------

type Props = {
  open: boolean;
  onClose: () => void;
  // If provided, pre-selects this peer in the recipient picker.
  initialPeerId?: string;
};

// ComposeRelayModal is the wide (1100px) two-pane editor for sending
// a message to another team. Left pane is the form: recipient picker,
// markdown body, attachment toggles. Right pane shows a live preview
// of exactly what the recipient will see.
//
// Attachments today: mission excerpt (operator pastes / edits text),
// task summary (operator pastes). Experience/workflow attachments
// require the SPA to fetch from the library; we ship the type plumbing
// here so a future PR can drop in a real attachment picker without
// touching the modal.
export function ComposeRelayModal({ open, onClose, initialPeerId }: Props) {
  const peers = usePeers();
  const status = useOfficeStatus();
  const send = useSendRelay();

  const peerList = useMemo(() => peers.data?.peers ?? [], [peers.data?.peers]);
  const [form, dispatch] = useReducer(formReducer, {
    ...INITIAL_FORM,
    toPeerId: initialPeerId ?? '',
  });

  // When the modal opens, hydrate the recipient + the mission excerpt
  // default from the current team mission.
  const prevOpenRef = useRef(open);
  if (open !== prevOpenRef.current) {
    prevOpenRef.current = open;
    if (open) {
      const resolvedPeerId = initialPeerId ?? (!form.toPeerId && peerList.length === 1 ? peerList[0].peerId : form.toPeerId);
      const mission = status.data?.mission ?? '';
      dispatch({ type: 'openReset', toPeerId: resolvedPeerId, missionExcerpt: mission.slice(0, 1200) });
    } else {
      dispatch({ type: 'closeReset' });
    }
  }

  // Build the live attachment list: mission excerpt toggle + custom attachments.
  const attachments = useMemo<RelayAttachment[]>(() => {
    const list: RelayAttachment[] = [];
    if (form.includeMission && form.missionExcerpt.trim()) {
      list.push({
        kind: 'missionExcerpt',
        ref: '',
        title: 'Mission excerpt',
        body: form.missionExcerpt.trim(),
      });
    }
    list.push(...form.customAttachments);
    return list;
  }, [form.includeMission, form.missionExcerpt, form.customAttachments]);

  const wordCount = useMemo(
    () => attachments.reduce((sum, a) => sum + countWords(a.body), 0),
    [attachments],
  );

  const charsLeft = MAX_CONTENT - form.content.length;
  const canSubmit = !!form.toPeerId && (form.content.trim().length > 0 || attachments.length > 0);

  const submit = async () => {
    dispatch({ type: 'setError', value: null });
    if (!canSubmit) return;
    try {
      const res = await send.mutateAsync({
        toPeerId: form.toPeerId,
        content: form.content,
        attachments,
      });
      if (res.error) {
        dispatch({ type: 'setError', value: res.error });
        toast.error('Message queued but delivery failed — will retry');
        return;
      }
      toast.success('Message sent to team');
      onClose();
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e);
      dispatch({ type: 'setError', value: message });
      toast.error('Failed to send message');
    }
  };

  const addCustomAttachment = () => {
    const { title, body } = form.customDraft;
    if (!title.trim() || !body.trim()) return;
    dispatch({
      type: 'addCustomAttachment',
      item: { kind: 'task', ref: '', title: title.trim(), body: body.trim() },
    });
  };

  return (
    <Modal
      open={open}
      onOpenChange={(next) => (next ? null : onClose())}
      size="wide"
      title="Compose message to peer team"
      description="Share context, attach what your team learned, and send it across to another team's inbox."
      footer={
        <>
          <span
            style={{
              flex: 1,
              fontSize: 12,
              color: 'var(--text-muted)',
              display: 'inline-flex',
              alignItems: 'center',
              gap: 8,
            }}
          >
            <Metric value={attachments.length} size={11} tone="muted" /> attachments ·{' '}
            <Metric value={wordCount} size={11} tone="muted" /> words ·{' '}
            <Metric value={charsLeft} size={11} tone={charsLeft < 200 ? 'warning' : 'muted'} /> chars left
          </span>
          {form.error ? (
            <span style={{ color: 'var(--status-error)', fontSize: 12 }}>{form.error}</span>
          ) : null}
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button onClick={submit} disabled={!canSubmit || send.isPending}>
            {send.isPending ? 'Sending…' : 'Send to team'}
          </Button>
        </>
      }
    >
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 18, minHeight: 460 }}>
        {/* Left: form */}
        <div
          style={{
            display: 'flex',
            flexDirection: 'column',
            gap: 14,
            paddingRight: 18,
            borderRight: '1px solid var(--border-subtle)',
          }}
        >
          <RecipientField
            toPeerId={form.toPeerId}
            peerList={peerList}
            onChange={(id) => dispatch({ type: 'setToPeerId', value: id })}
          />

          <Field label="Message" hint="Markdown allowed. The recipient sees this verbatim.">
            <Textarea
              value={form.content}
              onChange={(e) => dispatch({ type: 'setContent', value: e.target.value.slice(0, MAX_CONTENT) })}
              placeholder="What did your team learn that another team should know?"
              rows={8}
              style={{ resize: 'vertical' }}
            />
          </Field>

          <AttachSection
            missionExcerpt={form.missionExcerpt}
            includeMission={form.includeMission}
            customDraft={form.customDraft}
            customAttachments={form.customAttachments}
            onToggleMission={(checked) => dispatch({ type: 'setIncludeMission', value: checked })}
            onMissionExcerptChange={(value) => dispatch({ type: 'setMissionExcerpt', value })}
            onCustomDraftTitleChange={(value) => dispatch({ type: 'setCustomDraftTitle', value })}
            onCustomDraftBodyChange={(value) => dispatch({ type: 'setCustomDraftBody', value })}
            onAddCustomAttachment={addCustomAttachment}
            onRemoveCustomAttachment={(index) => dispatch({ type: 'removeCustomAttachment', index })}
          />
        </div>

        {/* Right: preview */}
        <PreviewPanel content={form.content} attachments={attachments} />
      </div>
    </Modal>
  );
}

function kindBadge(kind: RelayAttachment['kind']): string {
  switch (kind) {
    case 'experience':
      return '📚';
    case 'workflow':
      return '🛠';
    case 'task':
      return '📝';
    case 'missionExcerpt':
      return '🎯';
    default:
      return '•';
  }
}

function countWords(text: string): number {
  return text.trim().split(/\s+/).filter(Boolean).length;
}
