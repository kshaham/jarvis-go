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
  | 'office:claudeSessionStopped';

export type SSEHandler = (evt: { name: string; data: unknown }) => void;

export interface SSESubscription {
  close(): void;
}

export function subscribeSSE(handler: SSEHandler): SSESubscription {
  const url = '/sse';
  const src = new EventSource(url, { withCredentials: true });
  const generic = (e: MessageEvent) => {
    let data: unknown = e.data;
    try {
      data = JSON.parse(e.data);
    } catch {
      // leave raw
    }
    handler({ name: e.type, data });
  };
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
  ];
  for (const n of names) src.addEventListener(n, generic as EventListener);
  return {
    close() {
      for (const n of names) src.removeEventListener(n, generic as EventListener);
      src.close();
    },
  };
}
