// P13.5 · EventSource wrapper for jarvis's SSE stream. Mirrors mobile's
// subscribeSSE shape so the same event names route consistently.

export type SSEEventName =
  | 'office:memberUpdated'
  | 'office:teamSpawned'
  | 'office:teamKilled'
  | 'office:taskAssigned'
  | 'office:taskUpdated'
  | 'office:taskCompleted'
  | 'office:taskReviewed'
  | 'office:message'
  | 'office:snapshotRestored'
  | 'office:remoteSessionCreated'
  | 'office:claudeSessionStopped'
  | 'office:relaySent'
  | 'office:relayReceived'
  | 'office:peerStatusChanged';

export type SSEHandler = (evt: { name: string; data: unknown }) => void;

export interface SSESubscription {
  close(): void;
}

// Module-level connected state — exposed to UI via subscribeSSEConnected
// so the SSEPulse indicator and any polling-fallback hooks can pause
// when the stream is up.
let activeSources = 0;
let isConnected = false;
const connectedListeners = new Set<() => void>();

function setConnected(next: boolean) {
  if (isConnected === next) return;
  isConnected = next;
  for (const cb of connectedListeners) cb();
}

export function subscribeSSEConnected(fn: () => void): () => void {
  connectedListeners.add(fn);
  return () => {
    connectedListeners.delete(fn);
  };
}

export function getSSEConnected(): boolean {
  return isConnected;
}

export function subscribeSSE(handler: SSEHandler): SSESubscription {
  // The server's SSE stream lives at /api/events (the classic UI uses the
  // same route). No `?subscribe=` param → the broker treats it as "all
  // events"; we filter by event name on the client below.
  const url = '/api/events';
  const src = new EventSource(url, { withCredentials: true });
  activeSources++;
  const generic = (e: MessageEvent) => {
    let data: unknown = e.data;
    try {
      data = JSON.parse(e.data);
    } catch {
      // leave raw
    }
    handler({ name: e.type, data });
  };
  const onOpen = () => setConnected(true);
  const onError = () => {
    // Only flip to disconnected if no other sources are open. Multiple
    // subscribers share the EventSource lifecycle in practice but each
    // creates its own — keep semantics conservative.
    if (activeSources <= 1) setConnected(false);
  };
  src.addEventListener('open', onOpen);
  src.addEventListener('error', onError);
  // EventSource fires named events on listenerForName; subscribe to the
  // canonical office:* event set.
  const names: SSEEventName[] = [
    'office:memberUpdated',
    'office:teamSpawned',
    'office:teamKilled',
    'office:taskAssigned',
    'office:taskUpdated',
    'office:taskCompleted',
    'office:taskReviewed',
    'office:message',
    'office:snapshotRestored',
    'office:remoteSessionCreated',
    'office:claudeSessionStopped',
    'office:relaySent',
    'office:relayReceived',
    'office:peerStatusChanged',
  ];
  for (const n of names) src.addEventListener(n, generic as EventListener);
  return {
    close() {
      for (const n of names) src.removeEventListener(n, generic as EventListener);
      src.removeEventListener('open', onOpen);
      src.removeEventListener('error', onError);
      src.close();
      activeSources = Math.max(0, activeSources - 1);
      if (activeSources === 0) setConnected(false);
    },
  };
}
