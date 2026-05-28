import { useEffect } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { fetchJSON } from '@/api/client';
import { subscribeSSE } from '@/api/sse';
import type { LocalIdentity, Peer } from '@/api/types';

const PEERS_KEY = ['peers'] as const;
const IDENTITY_KEY = ['identity', 'me'] as const;

// usePeers loads the registered peer list. SSE-driven invalidation on
// peer status / relay events so the badge counts stay live.
export function usePeers() {
  const qc = useQueryClient();
  const q = useQuery<{ peers: Peer[] }>({
    queryKey: PEERS_KEY,
    queryFn: ({ signal }) => fetchJSON<{ peers: Peer[] }>('/api/peers', { signal }),
    refetchInterval: 60_000,
  });
  useEffect(() => {
    const sub = subscribeSSE(({ name }) => {
      if (
        name === 'office:peerStatusChanged' ||
        name === 'office:relayReceived' ||
        name === 'office:relaySent'
      ) {
        qc.invalidateQueries({ queryKey: PEERS_KEY });
      }
    });
    return () => sub.close();
  }, [qc]);
  return q;
}

// useIdentity returns this jarvis instance's LocalIdentity (peerId,
// displayName, publicUrl). The peerId is the bearer claim a peer
// jarvis pastes during registration; displayName/publicUrl are
// operator-editable.
export function useIdentity() {
  return useQuery<LocalIdentity>({
    queryKey: IDENTITY_KEY,
    queryFn: ({ signal }) => fetchJSON<LocalIdentity>('/api/identity/me', { signal }),
    staleTime: 5 * 60_000,
  });
}

// useUpdateIdentity mutates displayName / publicUrl. Empty fields are
// left unchanged server-side.
export function useUpdateIdentity() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (patch: { displayName?: string; publicUrl?: string }) =>
      fetchJSON<LocalIdentity>('/api/identity/me', { method: 'PUT', body: patch }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: IDENTITY_KEY });
    },
  });
}

// useGenerateToken returns a fresh pair token the operator can share
// out of band with another team. Each call returns a new token; the
// server holds no per-token state until a peer registers us using it.
export function useGenerateToken() {
  return useMutation({
    mutationFn: () =>
      fetchJSON<{ token: string }>('/api/identity/token', { method: 'POST' }),
  });
}

// useAddPeer registers a new peer team. The server verifies the URL
// resolves to a real jarvis (via the peer's /api/identity) before
// persisting.
export function useAddPeer() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { publicUrl: string; pairToken: string; displayName?: string }) =>
      fetchJSON<Peer>('/api/peers', { method: 'POST', body: input }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: PEERS_KEY });
    },
  });
}

export function useRemovePeer() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (peerId: string) =>
      fetchJSON<{ deleted: boolean }>(`/api/peers/${encodeURIComponent(peerId)}`, {
        method: 'DELETE',
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: PEERS_KEY });
    },
  });
}

export function usePingPeer() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (peerId: string) =>
      fetchJSON<{ peer: Peer; error?: string }>(
        `/api/peers/${encodeURIComponent(peerId)}/ping`,
        { method: 'POST' },
      ),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: PEERS_KEY });
    },
  });
}
