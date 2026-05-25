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
  totalCost?: number;
  lastActivityAt?: number;
  workingDir?: string;
  score?: AgentScore;
  paused?: boolean;
}

export interface OfficeStatus {
  teamSpawned: boolean;
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
