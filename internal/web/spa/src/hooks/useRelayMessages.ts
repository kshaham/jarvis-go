import { useEffect } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { fetchJSON } from '@/api/client';
import { subscribeSSE } from '@/api/sse';
import type { RelayAttachment, RelayMessage } from '@/api/types';

type RelayListResponse = { messages: RelayMessage[] };

// useRelayMessages reads the inter-team relay log. Pass a peerId to
// filter to one conversation; omit to get all messages. SSE-driven
// invalidation on office:relayReceived + office:relaySent.
export function useRelayMessages(peerId?: string) {
  const qc = useQueryClient();
  const key = ['relay', 'messages', peerId ?? '__all__'] as const;
  const q = useQuery<RelayListResponse>({
    queryKey: key,
    queryFn: ({ signal }) => {
      const url = peerId
        ? `/api/relay/messages?peerId=${encodeURIComponent(peerId)}`
        : '/api/relay/messages';
      return fetchJSON<RelayListResponse>(url, { signal });
    },
    refetchInterval: 60_000,
  });
  useEffect(() => {
    const sub = subscribeSSE(({ name }) => {
      if (name === 'office:relayReceived' || name === 'office:relaySent') {
        qc.invalidateQueries({ queryKey: ['relay', 'messages'] });
      }
    });
    return () => sub.close();
  }, [qc]);
  return q;
}

export interface SendRelayInput {
  toPeerId: string;
  content: string;
  attachments?: RelayAttachment[];
  attachmentRefs?: { kind: string; ref: string }[];
}

// useSendRelay POSTs to /api/relay/send. Returns the stored message
// even on delivery failure (server returns 202 in that case so the UI
// can show "queued / retry" without throwing).
export function useSendRelay() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: SendRelayInput) =>
      fetchJSON<{ message: RelayMessage; error?: string }>('/api/relay/send', {
        method: 'POST',
        body: input,
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['relay', 'messages'] });
      qc.invalidateQueries({ queryKey: ['peers'] });
    },
  });
}

export function useMarkRelayRead() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) =>
      fetchJSON<{ read: boolean }>(`/api/relay/messages/${encodeURIComponent(id)}/read`, {
        method: 'POST',
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['relay', 'messages'] });
      qc.invalidateQueries({ queryKey: ['peers'] });
    },
  });
}
