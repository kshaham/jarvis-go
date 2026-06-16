// P13.5 · Type mirrors for the subset of /api/office responses the
// React island consumes today. Extend as more screens land.
//
// Mirrors Go shapes at internal/web/handler/office.go and
// internal/office/manager.go GetOfficeState().

export type AgentStatus = 'working' | 'idle' | 'meeting' | 'stuck';

export interface AgentScore {
  grade?: string;
  overallScore?: number;
  completionRate?: number;
  collaborationScore?: number;
}

export interface OfficeMember {
  sessionId: string;
  name: string;
  role: string;
  status: AgentStatus;
  avatarEmoji?: string;
  avatarInitial?: string;
  avatarColor?: string;
  voice?: string;
  totalCost?: number;
  lastActivityAt?: number;
  workingDir?: string;
  score?: AgentScore;
  paused?: boolean;
}

export interface OfficeStatus {
  teamSpawned: boolean;
  // ID of the template the current team was spawned from. Used to tell a
  // one-click "Demo Jarvis" team (teamTemplateId === 'demo-jarvis') apart
  // from a real team so the rail can offer a "Stop demo" control.
  teamTemplateId?: string;
  paused?: boolean;
  pauseReason?: string;
  mission?: string;
  prNumber?: number;
  members: OfficeMember[];
  recentMessages?: Array<{
    fromSessionId: string;
    toSessionId: string;
    fromName: string;
    toName: string;
    content: string;
    timestamp: number;
  }>;
}

// ---------------------------------------------------------------------
// Inter-team relay
// ---------------------------------------------------------------------

export type PeerStatus = 'unknown' | 'ok' | 'unreachable' | 'unauthorized';

export interface Peer {
  peerId: string;
  displayName: string;
  publicUrl: string;
  pairToken: string;
  addedAt: string;
  lastSeenAt?: string;
  status: PeerStatus;
  lastError?: string;
  // Server-augmented: count of unread inbound messages from this peer.
  unreadInbound?: number;
}

export interface LocalIdentity {
  peerId: string;
  displayName: string;
  publicUrl: string;
  createdAt: string;
}

export type RelayAttachmentKind = 'experience' | 'workflow' | 'task' | 'missionExcerpt';

export interface RelayAttachment {
  kind: RelayAttachmentKind;
  ref: string;
  title: string;
  body: string;
}

export interface RelayMessage {
  id: string;
  fromPeerId: string;
  fromDisplayName: string;
  toPeerId: string;
  content: string;
  attachments?: RelayAttachment[];
  createdAt: string;
  deliveredAt?: string;
  readAt?: string;
  direction: 'inbound' | 'outbound';
}

// ---------------------------------------------------------------------
// Tasks
// ---------------------------------------------------------------------

export type TaskStatus =
  | 'pending'
  | 'acknowledged'
  | 'in_progress'
  | 'completed'
  | 'failed'
  | 'blocked'
  | 'handoff_pending';

export type TaskPriority = 'high' | 'medium' | 'low';

export interface TaskProgressEntry {
  timestamp: string;
  note: string;
}

export interface Task {
  id: string;
  title: string;
  description?: string;
  status: TaskStatus;
  priority?: TaskPriority;
  assignedAt?: string;
  startedAt?: string;
  completedAt?: string;
  source?: string;
  blockedBy?: string[];
  blockerDescription?: string;
  progressLog?: TaskProgressEntry[];
  labels?: string[];
  engPhase?: string;
  finalCostUSD?: number;
  finalTurnCount?: number;
}

export interface TaskQueue {
  sessionId: string;
  agentName: string;
  tasks: Task[];
}

export interface TaskQueuesResponse {
  queues: TaskQueue[];
}

// ---------------------------------------------------------------------
// Messages (chat)
// ---------------------------------------------------------------------

export interface OfficeMessageWire {
  id: string;
  fromSessionId: string;
  toSessionId?: string;
  fromName?: string;
  toName?: string;
  content: string;
  timestamp: string;
}

// ---------------------------------------------------------------------
// Standup
// ---------------------------------------------------------------------

export interface StandupTranscriptEntry {
  sessionId: string;
  speakerName?: string;
  content: string;
  timestamp: string;
  kind?: string;
}

export interface Standup {
  id?: string;
  active: boolean;
  startedAt?: string;
  endedAt?: string;
  participants: string[];
  currentSpeakerId?: string;
  transcript: StandupTranscriptEntry[];
}

// ---------------------------------------------------------------------
// Timeline / Activity
// ---------------------------------------------------------------------

export interface TimelineEntry {
  timestamp: string;
  kind: string;
  agentId?: string;
  agentName?: string;
  title: string;
  summary?: string;
  data?: Record<string, unknown>;
}

// ---------------------------------------------------------------------
// Knowledge
// ---------------------------------------------------------------------

export interface KnowledgeStats {
  experiences?: number;
  workflows?: number;
  standups?: number;
  promoted?: number;
  revoked?: number;
  pageIndexStatus?: string;
}

export interface KnowledgePromotion {
  id: string;
  kind: string;
  title: string;
  promotedAt?: string;
  reuseCount?: number;
  avgOutcome?: number;
  revoked?: boolean;
}

// ---------------------------------------------------------------------
// Reports
// ---------------------------------------------------------------------

export interface SavedReport {
  filename: string;
  generatedAt?: string;
  size?: number;
  preview?: string;
}

// ---------------------------------------------------------------------
// Office settings
// ---------------------------------------------------------------------

export interface OfficeSettings {
  mission?: string;
  autoStandup?: {
    enabled?: boolean;
    intervalHours?: number;
    intervalMinutes?: number;
    nextAt?: string;
  };
  brainstorm?: {
    cooldownMinutes?: number;
    responseTimeoutSeconds?: number;
  };
  idleDetection?: {
    enabled?: boolean;
    thresholdMinutes?: number;
  };
  meetings?: {
    inactivityTimeoutSeconds?: number;
    standupResponseTimeoutSeconds?: number;
  };
  fileLocks?: {
    lockExpiryMinutes?: number;
  };
  [key: string]: unknown;
}

// ---------------------------------------------------------------------
// Dev instances
// ---------------------------------------------------------------------

export interface DevInstance {
  slot: string;
  url?: string;
  pid?: number;
  healthFailures?: number;
  lastCheckAt?: string;
  nextCheckAt?: string;
  credentials?: Record<string, string>;
}

// ---------------------------------------------------------------------
// File locks
// ---------------------------------------------------------------------

export interface FileLock {
  path: string;
  owner: string;
  ownerName?: string;
  acquiredAt: string;
  expiresAt?: string;
}

// ---------------------------------------------------------------------
// Snapshots
// ---------------------------------------------------------------------

export interface Snapshot {
  name: string;
  createdAt: string;
  members?: number;
  mission?: string;
  templateId?: string;
}

// ---------------------------------------------------------------------
// Team templates (used by spawn flow)
// ---------------------------------------------------------------------

export interface TeamTemplateMember {
  id?: string;
  name: string;
  role: string;
  tier?: 'strategic' | 'worker' | 'support';
  color?: string;
  emoji?: string;
  personality?: string;
  systemPrompt?: string;
  skills?: string[];
  workingDir?: string;
  maxTurns?: number;
  spawnDelaySeconds?: number;
  provider?: string;
  model?: string;
}

export interface TeamTemplate {
  id: string;
  name: string;
  description?: string;
  category?: string;
  isBuiltIn?: boolean;
  members: TeamTemplateMember[];
  defaultProvider?: string;
  defaultModel?: string;
  perpetualImprovement?: boolean;
  sevenStep?: boolean;
  telegram?: {
    enabled?: boolean;
    intervalMinutes?: number;
  };
}

// ---------------------------------------------------------------------
// Metrics
// ---------------------------------------------------------------------

export interface MetricsSnapshot {
  standupsCompleted?: number;
  tasksDone?: number;
  bugsFiled?: number;
  bugsFixed?: number;
  recoveries?: number;
  stuckAlerts?: number;
  avgResponsePct?: number;
  totalCostUSD?: number;
  inputTokens?: number;
  outputTokens?: number;
  totalMessages?: number;
  uptimeByAgent?: Record<string, number>;
}

// ---------------------------------------------------------------------
// Labels rollup (insights)
// ---------------------------------------------------------------------

export interface LabelRollup {
  label: string;
  taskCount?: number;
  totalCostUSD?: number;
  avgTurns?: number;
}

// ---------------------------------------------------------------------
// Standup history
// ---------------------------------------------------------------------

export interface StandupHistoryEntry {
  id: string;
  startedAt: string;
  endedAt?: string;
  participants?: string[];
  messageCount?: number;
}

// ---------------------------------------------------------------------
// Browser health (R34)
// ---------------------------------------------------------------------

export interface BrowserHealth {
  ok: boolean;
  status: string;
  message?: string;
  lastCheckedAt?: string;
}

// ---------------------------------------------------------------------
// Whiteboard
// ---------------------------------------------------------------------

export interface Whiteboard {
  text?: string;
  updatedAt?: string;
  updatedBy?: string;
}

// ---------------------------------------------------------------------
// Provider / model catalog (spawn page dropdowns)
// ---------------------------------------------------------------------

// PROVIDER_MODEL_OPTIONS maps each AI provider to the models the spawn
// page offers in its dropdown. Mirrors the harness/pricing support on the
// Go side (internal/state/pricing.go, internal/office/spawn_helpers.go).
// openai-codex / openai-custom run on the Codex CLI; the rest on Claude Code.
const PROVIDER_MODEL_OPTIONS: Record<string, string[]> = {
  'claude-subscription': ['claude-opus-4-8', 'claude-sonnet-4-6', 'claude-haiku-4-5'],
  'claude-api': ['claude-opus-4-8', 'claude-sonnet-4-6', 'claude-haiku-4-5'],
  'zai-glm': ['glm-5.1', 'glm-5', 'glm-5-turbo', 'glm-4.7', 'glm-4.5-air'],
  'openai-codex': ['gpt-5.5', 'gpt-5.4', 'gpt-5.4-mini', 'gpt-5.3-codex', 'gpt-5.3-codex-spark', 'gpt-5.2'],
  'openai-custom': ['gpt-5.5', 'gpt-5.4', 'gpt-5.4-mini', 'gpt-5.3-codex', 'gpt-5.3-codex-spark', 'gpt-5.2'],
  // Subscription uses the plan's own model lineup, so no fixed dropdown —
  // ModelSelect falls back to free text (blank = plan default).
  'openai-subscription': [],
};

// modelOptionsForProvider returns the dropdown choices for a provider, or an
// empty array for unknown/custom providers (callers fall back to free text).
export function modelOptionsForProvider(provider: string): string[] {
  return PROVIDER_MODEL_OPTIONS[provider] ?? [];
}

// Settings/accounts response subset the spawn page consumes for the OpenAI
// "configured" prefill (the key value itself is returned masked, never raw).
export interface SettingsAccounts {
  provider?: string;
  openaiBaseUrl?: string;
  openaiKeyConfigured?: boolean;
}

// SavedProject — one entry in the operator's persisted spawn-page project
// list (GET /api/projects). For local projects path is the workdir; for
// github projects repoUrl is the canonical clone URL and path is filled in
// after the first successful clone.
export interface SavedProject {
  id: string;
  name: string;
  kind: 'local' | 'github';
  path: string;
  repoUrl?: string;
  createdAt: string;
  lastUsedAt: string;
}

// ── Tests page (per-project coverage + vulnerability scanning) ──────────
export type QualityStatus = 'idle' | 'running' | 'ok' | 'failed' | 'unsupported';

export interface CoverageResult {
  language: string;
  pct: number | null; // present only when status === 'ok'
  status: QualityStatus;
  error?: string;
  lastRunAt?: string;
}

export interface VulnResult {
  critical: number;
  high: number;
  medium: number;
  low: number;
  unknown: number;
  total: number;
  status: QualityStatus;
  error?: string;
  lastScanAt?: string;
}

// ReactDoctorResult — react-doctor's 0..100 health score (React projects only).
export interface ReactDoctorResult {
  score: number | null; // present only when status === 'ok'
  label?: string; // react-doctor's "Great" / "Needs work" / "Critical"
  status: QualityStatus; // 'unsupported' for non-React projects
  error?: string;
  lastRunAt?: string;
}

// TestProject is a SavedProject joined with its latest coverage + vuln + react
// results.
export interface TestProject extends SavedProject {
  coverage: CoverageResult;
  vuln: VulnResult;
  react: ReactDoctorResult;
}

// ── Mockups page ────────────────────────────────────────────────────────
export interface MockupStatus {
  status: 'idle' | 'running' | 'done' | 'error';
  expected: number;
  received: number;
  startedAt?: string;
  updatedAt?: string;
  error?: string;
}

export interface MockupImage {
  index: number;
  url: string;
}

export type MockupKind = 'ui' | 'presentation';

export interface MockupKindState {
  status: MockupStatus;
  mockups: MockupImage[];
}

// MockupProject — a SavedProject joined with its per-kind mockup state.
export interface MockupProject extends SavedProject {
  hasUi: boolean;
  hasDocs: boolean;
  teamActive: boolean; // a team is currently spawned on this project's workdir
  ui: MockupKindState;
  presentation: MockupKindState;
}

// ── Deliverables page ───────────────────────────────────────────────────
export type DeliverableType =
  | 'research-brief'
  | 'competitive-analysis'
  | 'findings-memo'
  | 'literature-review'
  | 'project-plan'
  | 'sop'
  | 'decision-memo'
  | 'status-report'
  | 'custom';

export interface Deliverable {
  slug: string;
  title: string;
  type: string;
  versions: number[]; // unix timestamps, newest first
  updatedAt?: string;
}

export interface DeliverableDrafting {
  type: string;
  title?: string;
  startedAt: string;
}

// DeliverableProject — a SavedProject joined with its deliverables + draft state.
export interface DeliverableProject extends SavedProject {
  teamActive: boolean;
  drafting?: DeliverableDrafting;
  deliverables: Deliverable[];
}
