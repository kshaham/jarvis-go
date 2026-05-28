import { useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';

import { Press } from '../components/motion';
import { useAddPeer, useGenerateToken, useIdentity } from '../hooks/usePeers';

// AddPeerScreen is a two-step inline wizard. Step 1 reveals this
// team's pair token so the operator can share it with the peer.
// Step 2 collects the peer's URL + their token + an optional alias.
// Submit POSTs to /api/peers; on success we redirect to /teams.
export function AddPeerScreen() {
  const identity = useIdentity();
  const genToken = useGenerateToken();
  const addPeer = useAddPeer();
  const navigate = useNavigate();

  const [publicUrl, setPublicUrl] = useState('');
  const [pairToken, setPairToken] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [error, setError] = useState<string | null>(null);

  const submit = async () => {
    setError(null);
    try {
      await addPeer.mutateAsync({ publicUrl, pairToken, displayName });
      navigate('/teams');
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  };

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 24, maxWidth: 720 }}>
      <header>
        <h1 style={{ margin: 0, fontSize: 24, fontWeight: 600, color: 'var(--text-primary)' }}>
          Add a peer team
        </h1>
        <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 13 }}>
          Two-way registration. You generate a token for them; they generate one for you.
          Each side stores the other's token and uses it to authenticate every message.
        </p>
      </header>

      {/* Step 1: your token */}
      <section
        style={{
          padding: 18,
          borderRadius: 'var(--radius-card)',
          background: 'var(--bg-elevated)',
          border: '1px solid var(--border-subtle)',
          display: 'flex',
          flexDirection: 'column',
          gap: 12,
        }}
      >
        <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between' }}>
          <h2 style={{ margin: 0, fontSize: 15, fontWeight: 600, color: 'var(--text-primary)' }}>
            1. Share your token
          </h2>
          <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>
            Generate, copy, send to the other operator.
          </span>
        </div>
        <p style={{ margin: 0, fontSize: 12, color: 'var(--text-muted)', lineHeight: 1.5 }}>
          This team's public URL is <code style={{ fontFamily: 'var(--font-mono)' }}>{identity.data?.publicUrl || 'unset — edit in /teams'}</code>
          {' '}and the peer ID they'll see is{' '}
          <code style={{ fontFamily: 'var(--font-mono)' }}>{identity.data?.peerId || '—'}</code>.
        </p>
        <Press pressScale={0.96} onClick={() => genToken.mutate()} disabled={genToken.isPending}>
          <span
            style={{
              padding: '8px 14px',
              background: 'var(--bg-pressed)',
              border: '1px solid var(--border-default)',
              borderRadius: 'var(--radius-md)',
              color: 'var(--text-primary)',
              fontSize: 13,
              fontWeight: 600,
              alignSelf: 'flex-start',
              display: 'inline-block',
            }}
          >
            {genToken.isPending ? 'Generating…' : 'Generate token'}
          </span>
        </Press>
        {genToken.data?.token ? (
          <code
            style={{
              padding: 12,
              fontFamily: 'var(--font-mono)',
              fontSize: 12,
              background: 'var(--bg-app)',
              color: 'var(--accent)',
              borderRadius: 'var(--radius-sm)',
              wordBreak: 'break-all',
              border: '1px solid var(--border-default)',
            }}
          >
            {genToken.data.token}
          </code>
        ) : null}
      </section>

      {/* Step 2: paste theirs */}
      <section
        style={{
          padding: 18,
          borderRadius: 'var(--radius-card)',
          background: 'var(--bg-elevated)',
          border: '1px solid var(--border-subtle)',
          display: 'flex',
          flexDirection: 'column',
          gap: 14,
        }}
      >
        <h2 style={{ margin: 0, fontSize: 15, fontWeight: 600, color: 'var(--text-primary)' }}>
          2. Register their team
        </h2>
        <Field label="Their public URL" hint="The HTTPS endpoint that reaches their jarvis instance.">
          <input
            value={publicUrl}
            onChange={(e) => setPublicUrl(e.target.value)}
            placeholder="https://backend-team.example.com"
            style={inputStyle}
          />
        </Field>
        <Field
          label="Their pair token"
          hint="The token they generated and shared with you."
        >
          <input
            value={pairToken}
            onChange={(e) => setPairToken(e.target.value)}
            placeholder="a1b2c3..."
            style={{ ...inputStyle, fontFamily: 'var(--font-mono)' }}
          />
        </Field>
        <Field
          label="Display name (optional)"
          hint="If empty, we use the displayName from their /api/identity endpoint."
        >
          <input
            value={displayName}
            onChange={(e) => setDisplayName(e.target.value)}
            placeholder="Backend team"
            style={inputStyle}
          />
        </Field>

        {error ? (
          <div
            style={{
              padding: 10,
              background: 'rgba(255,90,95,0.10)',
              border: '1px solid var(--status-error)',
              borderRadius: 'var(--radius-md)',
              color: 'var(--status-error)',
              fontSize: 12,
            }}
          >
            {error}
          </div>
        ) : null}

        <div style={{ display: 'flex', gap: 10, justifyContent: 'flex-end' }}>
          <Link to="/teams" style={{ textDecoration: 'none' }}>
            <Press pressScale={0.96}>
              <span
                style={{
                  padding: '10px 16px',
                  background: 'transparent',
                  color: 'var(--text-muted)',
                  fontSize: 13,
                  fontWeight: 600,
                  borderRadius: 'var(--radius-md)',
                  display: 'inline-block',
                }}
              >
                Cancel
              </span>
            </Press>
          </Link>
          <Press
            pressScale={0.96}
            onClick={submit}
            disabled={addPeer.isPending || !publicUrl || !pairToken}
          >
            <span
              style={{
                padding: '10px 16px',
                background: 'var(--accent)',
                color: '#fff',
                fontSize: 13,
                fontWeight: 600,
                borderRadius: 'var(--radius-md)',
                display: 'inline-block',
                opacity: !publicUrl || !pairToken ? 0.5 : 1,
              }}
            >
              {addPeer.isPending ? 'Verifying…' : 'Register peer'}
            </span>
          </Press>
        </div>
      </section>
    </section>
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
        <span style={{ fontSize: 11, color: 'var(--text-faint)', lineHeight: 1.5 }}>{hint}</span>
      ) : null}
    </label>
  );
}

const inputStyle: React.CSSProperties = {
  padding: '10px 12px',
  background: 'var(--bg-app)',
  border: '1px solid var(--border-default)',
  borderRadius: 'var(--radius-md)',
  color: 'var(--text-primary)',
  fontSize: 14,
  outline: 'none',
  width: '100%',
};
