// Centralised hooks for the office system's read-side. Each hook
// queries one /api/office/* endpoint via TanStack Query and listens
// on the relevant SSE events for cache invalidation. Mirrors the
// vanilla office.js _scheduleRender debounce pattern at a cleaner
// abstraction level — the cache is the debounce.

import { useEffect, useRef } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';

import { fetchJSON } from '@/api/client';
import { subscribeSSE } from '@/api/sse';
import type {
  BrowserHealth,
  DevInstance,
  FileLock,
  KnowledgePromotion,
  KnowledgeStats,
  LabelRollup,
  MetricsSnapshot,
  OfficeMessageWire,
  SavedReport,
  Snapshot,
  Standup,
  StandupHistoryEntry,
  TaskQueuesResponse,
  TeamTemplate,
  TimelineEntry,
  Whiteboard,
} from '@/api/types';

const TASK_QUEUES_KEY = ['office', 'task-queues'] as const;
const MESSAGES_KEY = ['office', 'messages'] as const;
const STANDUP_KEY = ['office', 'standup'] as const;
const TIMELINE_KEY = ['office', 'timeline'] as const;
const KNOWLEDGE_STATS_KEY = ['knowledge', 'stats'] as const;
const KNOWLEDGE_PROMOTIONS_KEY = ['knowledge', 'promotions'] as const;
const REPORTS_KEY = ['office', 'reports'] as const;

// useInvalidateOn — register an SSE listener that invalidates the
// given queryKey when any of the listed event names arrive. Returns
// nothing; the cleanup is internal to the effect.
function useInvalidateOn(eventNames: string[], queryKey: readonly unknown[]) {
  const qc = useQueryClient();
  // Serialize to stable strings so the effect re-subscribes only when
  // the actual names/keys change, not on every render due to new refs.
  const eventKey = eventNames.join('|');
  const queryKeyStr = queryKey.join('|');
  // Keep latest arrays in refs so the effect closure always reads fresh
  // values without listing the unstable array references in deps.
  const eventNamesRef = useRef(eventNames);
  const queryKeyRef = useRef(queryKey);
  eventNamesRef.current = eventNames;
  queryKeyRef.current = queryKey;
  useEffect(() => {
    const set = new Set(eventNamesRef.current);
    const sub = subscribeSSE(({ name }) => {
      if (set.has(name)) qc.invalidateQueries({ queryKey: queryKeyRef.current });
    });
    return () => sub.close();
  }, [qc, eventKey, queryKeyStr]);
}

// ---------------------------------------------------------------------
// Tasks
// ---------------------------------------------------------------------

export function useTaskQueues() {
  useInvalidateOn(
    [
      'office:taskQueueGenerated',
      'office:taskQueueUpdated',
      'office:taskAssigned',
      'office:taskUpdated',
      'office:taskCompleted',
      'office:bugTaskCreated',
      'office:bugTaskUpdated',
      'office:bugTaskResolved',
    ],
    TASK_QUEUES_KEY,
  );
  return useQuery<TaskQueuesResponse>({
    queryKey: TASK_QUEUES_KEY,
    queryFn: ({ signal }) =>
      fetchJSON<TaskQueuesResponse>('/api/office/task-queues', { signal }),
    refetchInterval: 60_000,
  });
}

// ---------------------------------------------------------------------
// Messages (chat between agents)
// ---------------------------------------------------------------------

export function useOfficeMessages() {
  useInvalidateOn(['office:message'], MESSAGES_KEY);
  return useQuery<{ messages: OfficeMessageWire[] }>({
    queryKey: MESSAGES_KEY,
    queryFn: ({ signal }) =>
      fetchJSON<{ messages: OfficeMessageWire[] }>('/api/office/messages', { signal }),
    refetchInterval: 60_000,
  });
}

// ---------------------------------------------------------------------
// Standup
// ---------------------------------------------------------------------

export function useStandup() {
  useInvalidateOn(
    [
      'office:standupStarted',
      'office:standupMessage',
      'office:standupSpeaker',
      'office:standupEnded',
    ],
    STANDUP_KEY,
  );
  return useQuery<{ standup: Standup | null }>({
    queryKey: STANDUP_KEY,
    queryFn: ({ signal }) =>
      fetchJSON<{ standup: Standup | null }>('/api/office/standup', { signal }),
    refetchInterval: 30_000,
  });
}

// ---------------------------------------------------------------------
// Timeline
// ---------------------------------------------------------------------

export function useTimeline(limit = 200) {
  useInvalidateOn(
    [
      'office:taskQueueUpdated',
      'office:taskAssigned',
      'office:taskCompleted',
      'office:standupMessage',
      'office:standupEnded',
      'office:message',
      'office:taskReviewed',
      'office:fileLockChanged',
    ],
    TIMELINE_KEY,
  );
  return useQuery<{ entries: TimelineEntry[] }>({
    queryKey: [...TIMELINE_KEY, limit],
    queryFn: ({ signal }) =>
      fetchJSON<{ entries: TimelineEntry[] }>(
        `/api/office/timeline?limit=${encodeURIComponent(String(limit))}`,
        { signal },
      ),
    refetchInterval: 60_000,
  });
}

// ---------------------------------------------------------------------
// Knowledge
// ---------------------------------------------------------------------

export function useKnowledgeStats() {
  useInvalidateOn(['knowledge:autoPromoted', 'knowledge:revoked'], KNOWLEDGE_STATS_KEY);
  return useQuery<KnowledgeStats>({
    queryKey: KNOWLEDGE_STATS_KEY,
    queryFn: ({ signal }) =>
      fetchJSON<KnowledgeStats>('/api/knowledge/stats', { signal }),
    refetchInterval: 5 * 60_000,
  });
}

export function useKnowledgePromotions() {
  useInvalidateOn(['knowledge:autoPromoted', 'knowledge:revoked'], KNOWLEDGE_PROMOTIONS_KEY);
  return useQuery<{ promotions: KnowledgePromotion[] }>({
    queryKey: KNOWLEDGE_PROMOTIONS_KEY,
    queryFn: ({ signal }) =>
      fetchJSON<{ promotions: KnowledgePromotion[] }>('/api/knowledge/promotions', {
        signal,
      }),
    refetchInterval: 5 * 60_000,
  });
}

// ---------------------------------------------------------------------
// Reports
// ---------------------------------------------------------------------

export function useReports() {
  useInvalidateOn(['office:reportGenerated'], REPORTS_KEY);
  return useQuery<{ reports: SavedReport[] }>({
    queryKey: REPORTS_KEY,
    queryFn: ({ signal }) =>
      fetchJSON<{ reports: SavedReport[] }>('/api/office/reports', { signal }),
    refetchInterval: 2 * 60_000,
  });
}

// ---------------------------------------------------------------------
// Dev instances
// ---------------------------------------------------------------------

const DEV_INSTANCES_KEY = ['office', 'dev-instances'] as const;

export function useDevInstances() {
  useInvalidateOn(
    [
      'office:devInstanceChanged',
      'office:devInstanceCleared',
      'office:devInstanceForced',
    ],
    DEV_INSTANCES_KEY,
  );
  return useQuery<{ instances: DevInstance[] }>({
    queryKey: DEV_INSTANCES_KEY,
    queryFn: ({ signal }) =>
      fetchJSON<{ instances: DevInstance[] }>('/api/office/dev-instances', { signal }),
    refetchInterval: 60_000,
  });
}

// ---------------------------------------------------------------------
// File locks
// ---------------------------------------------------------------------

const FILE_LOCKS_KEY = ['office', 'file-locks'] as const;

export function useFileLocks() {
  useInvalidateOn(['office:fileLockChanged'], FILE_LOCKS_KEY);
  return useQuery<{ locks: FileLock[] }>({
    queryKey: FILE_LOCKS_KEY,
    queryFn: ({ signal }) =>
      fetchJSON<{ locks: FileLock[] }>('/api/office/file-locks', { signal }),
    refetchInterval: 30_000,
  });
}

// ---------------------------------------------------------------------
// Snapshots
// ---------------------------------------------------------------------

const SNAPSHOTS_KEY = ['office', 'snapshots'] as const;

export function useSnapshots() {
  useInvalidateOn(
    ['office:snapshotCreated', 'office:snapshotRestored', 'office:snapshotDeleted'],
    SNAPSHOTS_KEY,
  );
  return useQuery<{ snapshots: Snapshot[] }>({
    queryKey: SNAPSHOTS_KEY,
    queryFn: ({ signal }) =>
      fetchJSON<{ snapshots: Snapshot[] }>('/api/office/snapshots', { signal }),
    refetchInterval: 5 * 60_000,
  });
}

// ---------------------------------------------------------------------
// Team templates
// ---------------------------------------------------------------------

const TEMPLATES_KEY = ['office', 'templates'] as const;

export function useTeamTemplates() {
  return useQuery<{ templates: TeamTemplate[] }>({
    queryKey: TEMPLATES_KEY,
    queryFn: ({ signal }) =>
      fetchJSON<{ templates: TeamTemplate[] }>('/api/office/templates', { signal }),
    staleTime: 5 * 60_000,
  });
}

// ---------------------------------------------------------------------
// Metrics
// ---------------------------------------------------------------------

const METRICS_KEY = ['office', 'metrics'] as const;

export function useMetrics() {
  useInvalidateOn(
    ['session:updated', 'office:taskCompleted', 'office:standupEnded'],
    METRICS_KEY,
  );
  return useQuery<{ metrics: MetricsSnapshot }>({
    queryKey: METRICS_KEY,
    queryFn: ({ signal }) =>
      fetchJSON<{ metrics: MetricsSnapshot }>('/api/office/metrics', { signal }),
    refetchInterval: 60_000,
  });
}

// ---------------------------------------------------------------------
// Labels rollup (insights cost-by-label)
// ---------------------------------------------------------------------

const LABELS_ROLLUP_KEY = ['office', 'labels', 'rollups'] as const;

export function useLabelsRollup() {
  useInvalidateOn(['session:updated', 'office:taskCompleted'], LABELS_ROLLUP_KEY);
  return useQuery<{ rollups: LabelRollup[] }>({
    queryKey: LABELS_ROLLUP_KEY,
    queryFn: ({ signal }) =>
      fetchJSON<{ rollups: LabelRollup[] }>('/api/office/labels/rollups', { signal }),
    refetchInterval: 5 * 60_000,
  });
}

// ---------------------------------------------------------------------
// Standup history (meetings)
// ---------------------------------------------------------------------

const STANDUP_HISTORY_KEY = ['office', 'standup', 'history'] as const;

export function useStandupHistory() {
  useInvalidateOn(['office:standupEnded'], STANDUP_HISTORY_KEY);
  return useQuery<{ history: StandupHistoryEntry[] }>({
    queryKey: STANDUP_HISTORY_KEY,
    queryFn: ({ signal }) =>
      fetchJSON<{ history: StandupHistoryEntry[] }>('/api/office/standup/history', {
        signal,
      }),
    refetchInterval: 5 * 60_000,
  });
}

export function useStandupDetail(id: string | null) {
  return useQuery<{ standup: Standup }>({
    queryKey: ['office', 'standup', 'detail', id],
    queryFn: ({ signal }) =>
      fetchJSON<{ standup: Standup }>(`/api/office/standup/${encodeURIComponent(id ?? '')}`, {
        signal,
      }),
    enabled: !!id,
    staleTime: 5 * 60_000,
  });
}

// ---------------------------------------------------------------------
// Browser health (R34)
// ---------------------------------------------------------------------

const BROWSER_HEALTH_KEY = ['office', 'browser-health'] as const;

export function useBrowserHealth() {
  return useQuery<BrowserHealth>({
    queryKey: BROWSER_HEALTH_KEY,
    queryFn: ({ signal }) =>
      fetchJSON<BrowserHealth>('/api/office/browser/health', { signal }),
    refetchInterval: 60_000,
    // Failures are expected when the sibling container isn't up; don't
    // bother retrying loudly.
    retry: false,
  });
}

// ---------------------------------------------------------------------
// Whiteboard
// ---------------------------------------------------------------------

const WHITEBOARD_KEY = ['office', 'whiteboard'] as const;

export function useWhiteboard() {
  useInvalidateOn(['office:whiteboardUpdate'], WHITEBOARD_KEY);
  return useQuery<{ whiteboard: Whiteboard }>({
    queryKey: WHITEBOARD_KEY,
    queryFn: ({ signal }) =>
      fetchJSON<{ whiteboard: Whiteboard }>('/api/office/whiteboard', { signal }),
    staleTime: 30_000,
  });
}
