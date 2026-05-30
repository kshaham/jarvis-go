import { Handle, Position, type NodeProps } from '@xyflow/react';

import { agentAvatarClass, agentAvatarGlyph } from '../agentCard.utils';
import { StatusDot } from '../motion/StatusDot';
import { cn } from '@/lib/cn';
import type { OfficeMember } from '@/api/types';

export interface AgentNodeData {
  member: OfficeMember;
}

// AgentFlowNode — a React Flow custom node that reuses the AgentCard /
// StatusPill visual language (avatar tint + animated status dot) on the
// elevated card surface. Handles are hidden so edges read as a
// node-to-node constellation. Typed loosely (NodeProps) and cast to
// avoid generic-variance friction in the nodeTypes map.
export function AgentFlowNode({ data, selected }: NodeProps) {
  const member = (data as unknown as AgentNodeData).member;
  if (!member) return null;
  return (
    <div
      className={cn(
        // Solid surface: graph nodes sit on an animating canvas, so skip the
        // per-node backdrop blur (perf) while keeping the card chrome.
        'surface-card surface-card--solid flex items-center gap-2 px-3 py-2',
        member.status === 'working' && 'shadow-glow-sm',
      )}
      style={{ borderColor: selected ? 'var(--accent)' : undefined, minWidth: 132 }}
    >
      <Handle type="target" position={Position.Left} style={{ opacity: 0 }} isConnectable={false} />
      <span
        className={cn(
          'flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-sm font-semibold',
          agentAvatarClass(member.avatarColor),
        )}
      >
        {agentAvatarGlyph(member)}
      </span>
      <div className="min-w-0">
        <div
          className="truncate text-xs font-semibold"
          style={{ color: 'var(--text-primary)', maxWidth: 120 }}
        >
          {member.name}
        </div>
        <span
          className="inline-flex items-center gap-1 text-[10px] capitalize"
          style={{ color: 'var(--text-muted)' }}
        >
          <StatusDot status={member.status} size={6} />
          {member.status}
        </span>
      </div>
      <Handle type="source" position={Position.Right} style={{ opacity: 0 }} isConnectable={false} />
    </div>
  );
}
