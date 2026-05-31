// EventSource wrapper for jarvis's SSE stream at /api/events.
//
// IMPORTANT: a SINGLE shared connection is multiplexed to every
// subscriber. Opening one EventSource per hook (the old behaviour)
// stacked ~6 long-lived connections, saturating the browser's
// per-origin HTTP/1.1 connection limit and stalling unrelated XHRs
// (e.g. /api/office/templates would hang forever → stuck skeletons).
// Now all subscribers share one connection; the server sends every
// event (no ?subscribe= filter) and we fan out by event name client-side.

export type SSEEventName =
  | 'office:memberUpdated'
  | 'office:teamSpawned'
  | 'office:teamKilled'
  | 'office:taskQueueGenerated'
  | 'office:taskQueueUpdated'
  | 'office:taskAssigned'
  | 'office:taskUpdated'
  | 'office:taskCompleted'
  | 'office:taskReviewed'
  | 'office:bugTaskCreated'
  | 'office:bugTaskUpdated'
  | 'office:bugTaskResolved'
  | 'office:message'
  | 'office:standupStarted'
  | 'office:standupMessage'
  | 'office:standupSpeaker'
  | 'office:standupEnded'
  | 'office:standupAudioProgress'
  | 'office:standupAudioReady'
  | 'office:standupAudioError'
  | 'office:reportGenerated'
  | 'office:devInstanceChanged'
  | 'office:devInstanceCleared'
  | 'office:devInstanceForced'
  | 'office:fileLockChanged'
  | 'office:snapshotCreated'
  | 'office:snapshotRestored'
  | 'office:snapshotDeleted'
  | 'office:whiteboardUpdate'
  | 'office:remoteSessionCreated'
  | 'office:claudeSessionStopped'
  | 'office:relaySent'
  | 'office:relayReceived'
  | 'office:peerStatusChanged'
  | 'session:updated'
  | 'knowledge:autoPromoted'
  | 'knowledge:revoked'
  | 'tests:coverageProgress'
  | 'tests:coverageComplete'
  | 'tests:scanProgress'
  | 'tests:scanComplete'
  | 'tests:reactProgress'
  | 'tests:reactComplete';

export type SSEHandler = (evt: { name: string; data: unknown }) => void;

export interface SSESubscription {
  close(): void;
}

// Every event name any hook listens for. The shared EventSource must
// register a listener per name (EventSource can't wildcard); add new
// server event names here when a hook starts depending on them.
const EVENT_NAMES: SSEEventName[] = [
  'office:memberUpdated',
  'office:teamSpawned',
  'office:teamKilled',
  'office:taskQueueGenerated',
  'office:taskQueueUpdated',
  'office:taskAssigned',
  'office:taskUpdated',
  'office:taskCompleted',
  'office:taskReviewed',
  'office:bugTaskCreated',
  'office:bugTaskUpdated',
  'office:bugTaskResolved',
  'office:message',
  'office:standupStarted',
  'office:standupMessage',
  'office:standupSpeaker',
  'office:standupEnded',
  'office:standupAudioProgress',
  'office:standupAudioReady',
  'office:standupAudioError',
  'office:reportGenerated',
  'office:devInstanceChanged',
  'office:devInstanceCleared',
  'office:devInstanceForced',
  'office:fileLockChanged',
  'office:snapshotCreated',
  'office:snapshotRestored',
  'office:snapshotDeleted',
  'office:whiteboardUpdate',
  'office:remoteSessionCreated',
  'office:claudeSessionStopped',
  'office:relaySent',
  'office:relayReceived',
  'office:peerStatusChanged',
  'session:updated',
  'knowledge:autoPromoted',
  'knowledge:revoked',
  'tests:coverageProgress',
  'tests:coverageComplete',
  'tests:scanProgress',
  'tests:scanComplete',
  'tests:reactProgress',
  'tests:reactComplete',
];

// ── Connected state ──────────────────────────────────────────────────
// Exposed via subscribeSSEConnected so the SSEPulse indicator and the
// ConnectionBanner can reflect whether the stream is live.
let isConnected = false;
const connectedListeners = new Set<() => void>();

function setConnected(next: boolean): void {
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

// ── Single shared EventSource ────────────────────────────────────────
const handlers = new Set<SSEHandler>();
let source: EventSource | null = null;

function dispatch(e: MessageEvent): void {
  let data: unknown = e.data;
  try {
    data = JSON.parse(e.data);
  } catch {
    // leave raw
  }
  for (const h of handlers) h({ name: e.type, data });
}

function onOpen(): void {
  setConnected(true);
}

function onError(): void {
  // EventSource auto-reconnects; reflect the gap — onOpen flips it back
  // once the connection re-establishes.
  setConnected(false);
}

function openSource(): void {
  if (source) return;
  source = new EventSource('/api/events', { withCredentials: true });
  source.addEventListener('open', onOpen);
  source.addEventListener('error', onError);
  for (const name of EVENT_NAMES) {
    source.addEventListener(name, dispatch as EventListener);
  }
}

function closeSource(): void {
  if (!source) return;
  for (const name of EVENT_NAMES) {
    source.removeEventListener(name, dispatch as EventListener);
  }
  source.removeEventListener('open', onOpen);
  source.removeEventListener('error', onError);
  source.close();
  source = null;
  setConnected(false);
}

// subscribeSSE registers a handler on the shared stream. The single
// connection opens on the first subscriber and closes when the last one
// unsubscribes (the persistent shell keeps ≥1 subscriber for the session).
export function subscribeSSE(handler: SSEHandler): SSESubscription {
  handlers.add(handler);
  openSource();
  return {
    close() {
      handlers.delete(handler);
      if (handlers.size === 0) closeSource();
    },
  };
}
