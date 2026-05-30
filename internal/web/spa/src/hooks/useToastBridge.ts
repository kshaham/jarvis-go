import { useEffect, useRef } from 'react';
import { useNavigate } from 'react-router-dom';
import { toast } from 'sonner';

import { subscribeSSE } from '@/api/sse';
import { useOfficeStatus } from '@/hooks/useOfficeStatus';
import { useSSEConnected } from '@/hooks/useSSEConnected';
import type { AgentStatus } from '@/api/types';

type RelayReceived = { fromDisplayName?: string; fromPeerId?: string };

function asRelay(data: unknown): RelayReceived {
  return data && typeof data === 'object' ? (data as RelayReceived) : {};
}

// useToastBridge — the single place that turns live office events into
// non-blocking Sonner toasts. Mounted ONCE in AppShell (not in the
// shared data hooks, which mount many times) so events fire exactly
// once. All diffing is ref-guarded so React StrictMode's double-invoke
// in dev never duplicates a toast.
export function useToastBridge() {
  const navigate = useNavigate();
  const connected = useSSEConnected();
  const { data } = useOfficeStatus();

  // SSE connection — toast only on transitions after the first sample.
  const prevConnected = useRef<boolean | null>(null);
  useEffect(() => {
    const prev = prevConnected.current;
    if (prev !== null && prev !== connected) {
      if (connected) toast.success('Live connection restored');
      else toast.warning('Live connection lost — retrying…');
    }
    prevConnected.current = connected;
  }, [connected]);

  // Agent → stuck transitions. Seed the map on first sight (no toast),
  // then alert when a known agent crosses into 'stuck'. Stable toast id
  // dedupes repeat events for the same agent.
  // Lazy-init pattern: useRef has no factory form, so we check null on
  // first access to avoid allocating a new Map on every render.
  const lastStatusRef = useRef<Map<string, AgentStatus> | null>(null);
  if (lastStatusRef.current === null) lastStatusRef.current = new Map();
  // Non-null cast: the initializer above guarantees this is always set.
  const lastStatus = lastStatusRef as { current: Map<string, AgentStatus> };
  useEffect(() => {
    const members = data?.members ?? [];
    const map = lastStatus.current;
    for (const m of members) {
      const prev = map.get(m.sessionId);
      if (prev && prev !== 'stuck' && m.status === 'stuck') {
        toast.error(`${m.name} is stuck`, { id: `stuck:${m.sessionId}` });
      }
      map.set(m.sessionId, m.status);
    }
    for (const id of [...map.keys()]) {
      if (!members.some((m) => m.sessionId === id)) map.delete(id);
    }
  }, [data, lastStatus]);

  // New inbound relay messages — offer a jump to the conversation.
  useEffect(() => {
    const sub = subscribeSSE(({ name, data: payload }) => {
      if (name !== 'office:relayReceived') return;
      const r = asRelay(payload);
      const who = r.fromDisplayName ?? 'a peer team';
      const peerId = r.fromPeerId;
      toast(`New message from ${who}`, {
        action: peerId
          ? {
              label: 'Open',
              onClick: () => navigate(`/relay?peer=${encodeURIComponent(peerId)}`),
            }
          : undefined,
      });
    });
    return () => sub.close();
  }, [navigate]);
}
