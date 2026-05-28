import { useSyncExternalStore } from 'react';
import { getSSEConnected, subscribeSSEConnected } from '@/api/sse';

// useSSEConnected returns true when the SSE stream is currently open.
// Backed by useSyncExternalStore so it correctly handles concurrent
// rendering and avoids tearing. SSEPulse + any polling-fallback hooks
// use this to flip behaviour based on real-time availability.
export function useSSEConnected(): boolean {
  return useSyncExternalStore(subscribeSSEConnected, getSSEConnected, () => false);
}
