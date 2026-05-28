import { useMemo, useState } from 'react';

import { Modal } from './motion/Modal';
import { Metric, Press } from './motion';
import { usePeers } from '../hooks/usePeers';
import { useSendRelay } from '../hooks/useRelayMessages';
import { useOfficeStatus } from '../hooks/useOfficeStatus';
import type { RelayAttachment } from '../api/types';

const MAX_CONTENT = 4000;

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
  const [toPeerId, setToPeerId] = useState<string>(initialPeerId ?? '');
  const [content, setContent] = useState('');
  const [missionExcerpt, setMissionExcerpt] = useState('');
  const [includeMission, setIncludeMission] = useState(false);
  const [customAttachments, setCustomAttachments] = useState<RelayAttachment[]>([]);
  const [customDraft, setCustomDraft] = useState({ title: '', body: '' });
  const [error, setError] = useState<string | null>(null);

  // When the modal opens, hydrate the recipient + the mission excerpt
  // default from the current team mission. Adjust-state-during-render
  // pattern to keep the React 19 set-state-in-effect lint happy.
  const [lastOpenState, setLastOpenState] = useState(open);
  if (open !== lastOpenState) {
    if (open) {
      setError(null);
      if (initialPeerId) setToPeerId(initialPeerId);
      if (!toPeerId && peerList.length === 1) setToPeerId(peerList[0].peerId);
      const mission = status.data?.mission ?? '';
      setMissionExcerpt(mission.slice(0, 1200));
    } else {
      // Clear the form fully on close so reopening is fresh.
      setContent('');
      setMissionExcerpt('');
      setIncludeMission(false);
      setCustomAttachments([]);
      setCustomDraft({ title: '', body: '' });
    }
    setLastOpenState(open);
  }

  // Build the live attachment list: mission excerpt toggle + the
  // operator-typed custom attachments.
  const attachments = useMemo<RelayAttachment[]>(() => {
    const list: RelayAttachment[] = [];
    if (includeMission && missionExcerpt.trim()) {
      list.push({
        kind: 'missionExcerpt',
        ref: '',
        title: 'Mission excerpt',
        body: missionExcerpt.trim(),
      });
    }
    list.push(...customAttachments);
    return list;
  }, [includeMission, missionExcerpt, customAttachments]);

  const wordCount = useMemo(
    () => attachments.reduce((sum, a) => sum + countWords(a.body), 0),
    [attachments],
  );

  const charsLeft = MAX_CONTENT - content.length;
  const canSubmit = !!toPeerId && (content.trim().length > 0 || attachments.length > 0);

  const submit = async () => {
    setError(null);
    if (!canSubmit) return;
    try {
      const res = await send.mutateAsync({
        toPeerId,
        content,
        attachments,
      });
      if (res.error) {
        setError(res.error);
        return;
      }
      onClose();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
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
              fontSize: 11,
              color: 'var(--text-muted)',
              display: 'inline-flex',
              alignItems: 'center',
              gap: 8,
            }}
          >
            <Metric value={attachments.length} size={11} tone="muted" /> attachments ·{' '}
            <Metric value={wordCount} size={11} tone="muted" /> words ·{' '}
            <Metric
              value={charsLeft}
              size={11}
              tone={charsLeft < 200 ? 'warning' : 'muted'}
            />{' '}
            chars left
          </span>
          {error ? (
            <span style={{ color: 'var(--status-error)', fontSize: 12 }}>{error}</span>
          ) : null}
          <Press pressScale={0.96} onClick={onClose}>
            <span
              style={{
                padding: '8px 14px',
                background: 'transparent',
                color: 'var(--text-muted)',
                fontSize: 13,
                fontWeight: 600,
                borderRadius: 'var(--radius-md)',
              }}
            >
              Cancel
            </span>
          </Press>
          <Press
            pressScale={0.96}
            onClick={submit}
            disabled={!canSubmit || send.isPending}
          >
            <span
              style={{
                padding: '8px 18px',
                background: 'var(--accent)',
                color: '#fff',
                fontSize: 13,
                fontWeight: 600,
                borderRadius: 'var(--radius-md)',
                opacity: !canSubmit || send.isPending ? 0.5 : 1,
              }}
            >
              {send.isPending ? 'Sending…' : 'Send to team'}
            </span>
          </Press>
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
          <Field label="Recipient">
            <select
              value={toPeerId}
              onChange={(e) => setToPeerId(e.target.value)}
              style={inputStyle}
            >
              <option value="">Select a peer team…</option>
              {peerList.map((p) => (
                <option key={p.peerId} value={p.peerId}>
                  {p.displayName}
                </option>
              ))}
            </select>
          </Field>

          <Field label="Message" hint="Markdown allowed. The recipient sees this verbatim.">
            <textarea
              value={content}
              onChange={(e) => setContent(e.target.value.slice(0, MAX_CONTENT))}
              placeholder="What did your team learn that another team should know?"
              rows={8}
              style={{ ...inputStyle, fontFamily: 'var(--font-sans)', resize: 'vertical' }}
            />
          </Field>

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
                onChange={(e) => setIncludeMission(e.target.checked)}
                style={{ marginTop: 3 }}
              />
              <div style={{ flex: 1 }}>
                <div style={{ fontSize: 13, fontWeight: 600, color: 'var(--text-primary)' }}>
                  Mission excerpt
                </div>
                <textarea
                  value={missionExcerpt}
                  onChange={(e) => setMissionExcerpt(e.target.value.slice(0, 2000))}
                  placeholder="Pre-filled from the current team mission. Edit freely."
                  rows={4}
                  disabled={!includeMission}
                  style={{
                    ...inputStyle,
                    marginTop: 6,
                    fontSize: 12,
                    background: includeMission ? 'var(--bg-app)' : 'transparent',
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
              <input
                value={customDraft.title}
                onChange={(e) => setCustomDraft({ ...customDraft, title: e.target.value })}
                placeholder="Title (e.g. 'Lessons from the auth migration')"
                style={inputStyle}
              />
              <textarea
                value={customDraft.body}
                onChange={(e) => setCustomDraft({ ...customDraft, body: e.target.value })}
                placeholder="Free-form notes; markdown allowed."
                rows={3}
                style={{ ...inputStyle, resize: 'vertical' }}
              />
              <Press
                pressScale={0.96}
                onClick={() => {
                  const { title, body } = customDraft;
                  if (!title.trim() || !body.trim()) return;
                  setCustomAttachments((prev) => [
                    ...prev,
                    { kind: 'task', ref: '', title: title.trim(), body: body.trim() },
                  ]);
                  setCustomDraft({ title: '', body: '' });
                }}
              >
                <span
                  style={{
                    alignSelf: 'flex-start',
                    padding: '6px 12px',
                    background: 'var(--bg-pressed)',
                    border: '1px solid var(--border-default)',
                    borderRadius: 'var(--radius-sm)',
                    color: 'var(--text-primary)',
                    fontSize: 12,
                    fontWeight: 600,
                    display: 'inline-block',
                  }}
                >
                  + Add attachment
                </span>
              </Press>
            </div>

            {customAttachments.length > 0 ? (
              <ul style={{ listStyle: 'none', padding: 0, margin: 0, display: 'flex', flexDirection: 'column', gap: 6 }}>
                {customAttachments.map((a, i) => (
                  <li
                    key={i}
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
                      onClick={() =>
                        setCustomAttachments((prev) => prev.filter((_, idx) => idx !== i))
                      }
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
        </div>

        {/* Right: preview */}
        <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
          <div
            style={{
              fontSize: 11,
              color: 'var(--text-muted)',
              letterSpacing: 0.4,
              textTransform: 'uppercase',
            }}
          >
            Preview
          </div>
          <div
            style={{
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
            }}
          >
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
                    fontSize: 11,
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
                    key={i}
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
      <span
        style={{
          fontSize: 11,
          color: 'var(--text-muted)',
          letterSpacing: 0.4,
          textTransform: 'uppercase',
        }}
      >
        {label}
      </span>
      {children}
      {hint ? (
        <span style={{ fontSize: 11, color: 'var(--text-faint)', lineHeight: 1.4 }}>{hint}</span>
      ) : null}
    </label>
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

const inputStyle: React.CSSProperties = {
  padding: '8px 10px',
  background: 'var(--bg-app)',
  border: '1px solid var(--border-default)',
  borderRadius: 'var(--radius-md)',
  color: 'var(--text-primary)',
  fontSize: 13,
  outline: 'none',
  width: '100%',
  fontFamily: 'inherit',
};

// Bind ComposeRelayModal as default export for convenience in lazy
// imports if needed later.
export default ComposeRelayModal;
