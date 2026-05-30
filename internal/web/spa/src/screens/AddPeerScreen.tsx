import React, { useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { AnimatePresence, m } from 'motion/react';

import { RevealItem } from '../components/motion/RevealItem';
import { Button } from '../components/ui/Button';
import { Field, Input } from '../components/ui/Field';
import { Section } from '../components/ui/Section';
import { spring } from '../lib/motion';
import { useAddPeer, useGenerateToken, useIdentity } from '../hooks/usePeers';
import { toast } from 'sonner';

const tokenCodeStyle: React.CSSProperties = {
  padding: 12,
  fontFamily: 'var(--font-mono)',
  fontSize: 12,
  background: 'var(--bg-app)',
  color: 'var(--accent)',
  borderRadius: 'var(--radius-sm)',
  wordBreak: 'break-all',
  border: '1px solid var(--border-default)',
};

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

  const onGenerate = () => {
    genToken.mutate(undefined, {
      onSuccess: () => toast.success('Pair token generated'),
      onError: (e) =>
        toast.error(`Token failed: ${e instanceof Error ? e.message : String(e)}`),
    });
  };

  const submit = async () => {
    setError(null);
    try {
      await addPeer.mutateAsync({ publicUrl, pairToken, displayName });
      toast.success(`Registered ${displayName || 'peer team'}`);
      navigate('/teams');
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e);
      setError(message);
      toast.error(`Registration failed: ${message}`);
    }
  };

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 24, maxWidth: 720 }}>
      <RevealItem index={0} withExit={false}>
        <header>
          <h1 style={{ margin: 0, fontSize: 24, fontWeight: 600, color: 'var(--text-primary)' }}>
            Add a peer team
          </h1>
          <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 13 }}>
            Two-way registration. You generate a token for them; they generate one for you.
            Each side stores the other's token and uses it to authenticate every message.
          </p>
        </header>
      </RevealItem>

      {/* Step 1: your token */}
      <RevealItem index={1} withExit={false}>
        <Section
          title="1. Share your token"
          hint="Generate, copy, send to the other operator."
        >
          <p style={{ margin: 0, fontSize: 12, color: 'var(--text-muted)', lineHeight: 1.5 }}>
            This team's public URL is{' '}
            <code style={{ fontFamily: 'var(--font-mono)' }}>
              {identity.data?.publicUrl || 'unset — edit in /teams'}
            </code>{' '}
            and the peer ID they'll see is{' '}
            <code style={{ fontFamily: 'var(--font-mono)' }}>{identity.data?.peerId || '—'}</code>.
          </p>
          <div>
            <Button variant="secondary" onClick={onGenerate} disabled={genToken.isPending}>
              {genToken.isPending ? 'Generating…' : 'Generate token'}
            </Button>
          </div>
          {genToken.data?.token ? (
            <code style={tokenCodeStyle}>
              {genToken.data.token}
            </code>
          ) : null}
        </Section>
      </RevealItem>

      {/* Step 2: paste theirs */}
      <RevealItem index={2} withExit={false}>
        <Section title="2. Register their team">
          <Field
            label="Their public URL"
            hint="The HTTPS endpoint that reaches their jarvis instance."
          >
            <Input
              value={publicUrl}
              onChange={(e) => setPublicUrl(e.target.value)}
              placeholder="https://backend-team.example.com"
            />
          </Field>
          <Field label="Their pair token" hint="The token they generated and shared with you.">
            <Input
              value={pairToken}
              onChange={(e) => setPairToken(e.target.value)}
              placeholder="a1b2c3..."
              style={{ fontFamily: 'var(--font-mono)' }}
            />
          </Field>
          <Field
            label="Display name (optional)"
            hint="If empty, we use the displayName from their /api/identity endpoint."
          >
            <Input
              value={displayName}
              onChange={(e) => setDisplayName(e.target.value)}
              placeholder="Backend team"
            />
          </Field>

          <AnimatePresence initial={false}>
            {error ? (
              <m.div
                initial={{ opacity: 0, y: -6 }}
                animate={{ opacity: 1, y: 0 }}
                exit={{ opacity: 0, y: -6 }}
                transition={spring.smooth}
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
              </m.div>
            ) : null}
          </AnimatePresence>

          <div style={{ display: 'flex', gap: 10, justifyContent: 'flex-end' }}>
            <Link to="/teams" style={{ textDecoration: 'none' }}>
              <Button variant="ghost">Cancel</Button>
            </Link>
            <Button
              variant="primary"
              onClick={submit}
              disabled={addPeer.isPending || !publicUrl || !pairToken}
            >
              {addPeer.isPending ? 'Verifying…' : 'Register peer'}
            </Button>
          </div>
        </Section>
      </RevealItem>
    </section>
  );
}
