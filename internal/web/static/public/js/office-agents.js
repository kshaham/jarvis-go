/* office-agents.js — agent grid rendering, detail panel, and agent
 * detail modal (whiteboard + sub-tabs). Extracted from office.js via
 * the prototype-extension pattern.
 */

(function () {
  'use strict';
  if (typeof OfficeApp === 'undefined') {
    console.error('office-agents.js loaded before OfficeApp; check script order');
    return;
  }

  Object.assign(OfficeApp.prototype, {

  // ========== Rendering ==========

  render() {
    this.renderAgentCards();
    this.updateSpawnButton();
    this.renderKanbanBoard();
    this.renderMessages();
    this.renderStandup();
    this.updateStatusBar();
  },

  updateSpawnButton() {
    const btn = document.getElementById('btnSpawnTeam');
    if (btn) {
      // Keep clickable even when a team is active -- clicking opens the modal
      // in view-mode so the operator can still inspect templates + the
      // current team's configuration without wrapping up first.
      btn.disabled = false;
      btn.textContent = this.state.teamSpawned ? 'Team Config' : 'Spawn Team';
      btn.title = this.state.teamSpawned
        ? 'View templates and current team configuration'
        : 'Spawn a new team';
    }
  },

  /**
   * Renders all agent cards in the grid.
   * Replaces the old renderDesk() / renderFloor() pattern.
   */
  renderAgentCards() {
    const grid = document.getElementById('agentGrid');
    if (!grid) return;

    const empty = document.getElementById('agentEmpty');

    // Remove skeleton cards if present
    for (const sk of grid.querySelectorAll('.skeleton-card')) {
      sk.remove();
    }

    if (this.state.members.length === 0 && !this.state.teamSpawned) {
      if (empty) empty.style.display = '';
      for (const card of grid.querySelectorAll('.agent-card')) {
        card.remove();
      }
      return;
    }

    // Show skeletons while team is spawned but members haven't arrived yet
    if (this.state.teamSpawned && this.state.members.length === 0) {
      if (empty) empty.style.display = 'none';
      for (const card of grid.querySelectorAll('.agent-card')) {
        card.remove();
      }
      for (let i = 0; i < 3; i++) {
        const sk = document.createElement('div');
        sk.className = 'skeleton-card skeleton';
        grid.appendChild(sk);
      }
      return;
    }

    if (empty) empty.style.display = 'none';

    // Apply filter
    const filter = this._agentFilter || 'all';
    let visibleMembers = filter === 'all'
      ? this.state.members.slice()
      : this.state.members.filter(m => m.status === filter);

    // F3 · pinned agents float to the top. Stable sort: pinned first,
    // preserving the existing order within each group.
    visibleMembers = visibleMembers
      .map((m, i) => ({ m, i, pinned: this.isPinned(m) }))
      .sort((a, b) => (b.pinned - a.pinned) || (a.i - b.i))
      .map(({ m }) => m);

    const existingCards = new Map();
    for (const card of grid.querySelectorAll('.agent-card')) {
      const sid = card.dataset.sessionId;
      if (sid) existingCards.set(sid, card);
    }

    const visibleIds = new Set(visibleMembers.map(m => m.sessionId));

    // Remove cards that are not visible (filtered out or stale)
    for (const [sid, card] of existingCards) {
      if (!visibleIds.has(sid)) {
        card.remove();
        existingCards.delete(sid);
      }
    }

    // Render/update. To reflect pinning order changes, re-append every
    // card in the new order — moving an existing node is cheap and keeps
    // pinned agents at the top.
    for (const member of visibleMembers) {
      let card = existingCards.get(member.sessionId);
      if (card) {
        this.updateAgentCard(card, member);
      } else {
        card = this.createAgentCard(member);
      }
      card.classList.toggle('is-pinned', this.isPinned(member));
      grid.appendChild(card);
    }

    // If filter results in no cards but members exist, show a hint
    if (visibleMembers.length === 0 && this.state.members.length > 0) {
      if (empty) {
        empty.style.display = '';
        const hint = empty.querySelector('.agent-empty-hint');
        if (hint) hint.textContent = `No agents with status "${filter}".`;
      }
    } else {
      if (empty) {
        const hint = empty.querySelector('.agent-empty-hint');
        if (hint) hint.textContent = 'Click "Spawn Team" to create an AI team.';
      }
    }

    this.updateNavBadges();
  },

  createAgentCard(member) {
    const card = document.createElement('div');
    // .border-beam is always present so the ::before pseudo is ready;
    // .border-beam--active is added only while the agent is working
    // so the rotating conic-gradient only spins on active tiles.
    card.className = `agent-card border-beam status-${member.status}${member.status === 'working' ? ' border-beam--active' : ''}${this.isPinned(member) ? ' is-pinned' : ''}`;
    card.dataset.sessionId = member.sessionId;
    if (member.configId) card.dataset.configId = member.configId;
    card.tabIndex = 0;
    card.setAttribute('role', 'button');
    card.setAttribute('aria-label', `${member.name} - ${member.role} (${member.status})`);
    card.onclick = () => this.showDetail(member.sessionId);
    card.onkeydown = (e) => {
      if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault();
        this.showDetail(member.sessionId);
      }
    };
    this.fillAgentCard(card, member);
    this._injectStarButton(card, member);
    return card;
  },

  // F3 · inject a star-toggle button into an agent card. Idempotent:
  // safely skips if the card already has one (e.g. on updateAgentCard
  // paths that re-run fillAgentCard).
  _injectStarButton(card, member) {
    if (card.querySelector(':scope > .agent-star-btn')) return;
    const btn = document.createElement('button');
    btn.className = 'agent-star-btn';
    btn.setAttribute('aria-label', this.isPinned(member) ? 'Unpin agent' : 'Pin agent');
    btn.setAttribute('title', 'Pin to top');
    btn.innerHTML = '<svg viewBox="0 0 24 24"><polygon points="12 2 15.09 8.26 22 9.27 17 14.14 18.18 21.02 12 17.77 5.82 21.02 7 14.14 2 9.27 8.91 8.26 12 2"/></svg>';
    btn.onclick = (e) => {
      e.stopPropagation();
      this.togglePin(member);
      btn.classList.remove('pop');
      // Force reflow so class re-add restarts animation.
      void btn.offsetWidth;
      btn.classList.add('pop');
    };
    card.appendChild(btn);
  },

  // F3 · pin helpers.
  _pinKey(member) {
    // Prefer configId (survives respawn); fall back to sessionId.
    return (member && (member.configId || member.sessionId)) || '';
  },

  isPinned(member) {
    const key = this._pinKey(member);
    return !!key && this._pinnedAgents.has(key);
  },

  togglePin(member) {
    const key = this._pinKey(member);
    if (!key) return;
    if (this._pinnedAgents.has(key)) {
      this._pinnedAgents.delete(key);
    } else {
      this._pinnedAgents.add(key);
    }
    this._savePinnedAgents();
    // Re-render the grid so sort order + is-pinned class refresh.
    this.renderAgentCards();
  },

  _loadPinnedAgents() {
    try {
      const raw = localStorage.getItem('office_pinned_agents');
      if (!raw) return new Set();
      const arr = JSON.parse(raw);
      return new Set(Array.isArray(arr) ? arr : []);
    } catch {
      return new Set();
    }
  },

  _savePinnedAgents() {
    try {
      localStorage.setItem('office_pinned_agents', JSON.stringify([...this._pinnedAgents]));
    } catch { /* quota */ }
  },

  // F3 · map a Claude tool name to a sprite icon reference. Unknown
  // tools fall back to a generic "activity" icon. The referenced
  // symbols live in the inline sprite at the top of office.html.
  _toolIconRef(name) {
    const lower = String(name || '').toLowerCase();
    if (lower.startsWith('bash')) return 'icon-tool-bash';
    if (lower.startsWith('read')) return 'icon-tool-read';
    if (lower.startsWith('edit') || lower.startsWith('write') || lower.startsWith('multiedit')) return 'icon-tool-edit';
    if (lower.startsWith('grep') || lower.startsWith('glob')) return 'icon-tool-search';
    if (lower.startsWith('webfetch') || lower.startsWith('websearch')) return 'icon-tool-web';
    if (lower.startsWith('task') || lower.startsWith('agent')) return 'icon-tool-agent';
    return 'icon-tool-generic';
  },

  // Human-friendly cost formatter. Cents for sub-dollar values so the
  // card doesn't advertise "$0.0341" clutter; K/M abbreviation above
  // $1000 / $1M so ambitious teams don't blow the layout.
  _formatCost(n) {
    const v = Number(n) || 0;
    if (v < 0.005)        return '0c';
    if (v < 1)            return Math.round(v * 100) + 'c';
    if (v < 1000)         return '$' + v.toFixed(2);
    if (v < 1_000_000)    return '$' + (v / 1000).toFixed(1) + 'K';
    return '$' + (v / 1_000_000).toFixed(1) + 'M';
  },

  // R37 · helper for the new claudehooks SSE handlers. Looks up an
  // agent's display name from the in-memory roster so the activity
  // feed says "Captain: starting context compact" instead of a raw
  // session UUID. Returns empty string when the session is unknown
  // (caller falls back to a truncated session id).
  _agentNameFromSession(sessionId) {
    if (!sessionId || !this.state || !Array.isArray(this.state.members)) return '';
    const m = this.state.members.find(x => x.sessionId === sessionId);
    return m && m.name ? m.name : '';
  },

  // R37 · briefly highlight an agent card to draw operator attention.
  // Used by the Notification handler when claude raises a permission
  // prompt or "needs your input" so the operator can spot the affected
  // agent without reading the activity feed. Adds a class for 2s, then
  // removes -- pure CSS animation, no JS animation loop.
  _flashAgentCard(sessionId) {
    if (!sessionId) return;
    const grid = document.getElementById('agentGrid');
    if (!grid) return;
    const card = grid.querySelector(`.agent-card[data-session-id="${sessionId}"]`);
    if (!card) return;
    card.classList.remove('agent-card-flash');
    void card.offsetWidth; // restart animation
    card.classList.add('agent-card-flash');
    setTimeout(() => card.classList.remove('agent-card-flash'), 2200);
  },

  fillAgentCard(card, member) {
    // Defensive defaults -- prevent "undefined" rendering
    if (!member.name) member.name = member.configId || 'Agent';
    if (!member.role) member.role = '';
    if (!member.status) member.status = 'idle';
    if (!member.avatarColor) member.avatarColor = member.color || 'blue';

    // F4 · compact view branch — emits a dense row with only the most
    // scannable data (status dot + name + role + tasks + cost). The
    // rich card markup below is skipped entirely so re-renders stay
    // cheap when a 12+ agent team is in compact mode.
    if (this._viewMode === 'compact') {
      const queue = (this.taskQueues || []).find(q => q.sessionId === member.sessionId);
      const tasks = queue ? queue.tasks || [] : [];
      const completed = tasks.filter(t => t.status === 'completed' || t.status === 'failed').length;
      const total = tasks.length;
      const cost = this._formatCost(Number(member.totalCost) || 0);
      card.innerHTML = `
        <span class="agent-compact-dot" data-status="${this.escapeAttr(member.status)}" title="${this.escapeAttr(member.status)}"></span>
        <span class="agent-compact-name">${this.escapeHtml(member.name)}</span>
        <span class="agent-compact-role">${this.escapeHtml(member.role)}</span>
        <span></span>
        <span class="agent-compact-tasks">${completed}/${total}</span>
        <span class="agent-compact-cost">${cost}</span>
        <span></span>
      `;
      return;
    }

    const queue = (this.taskQueues || []).find(q => q.sessionId === member.sessionId);
    const tasks = queue ? queue.tasks || [] : [];
    const total = tasks.length;
    const completed = tasks.filter(t => t.status === 'completed' || t.status === 'failed').length;
    const pct = total > 0 ? Math.round((completed / total) * 100) : 0;
    const currentTask = tasks.find(t => t.status === 'in_progress') || tasks.find(t => t.status === 'pending');
    const rawCost = Number(member.totalCost) || 0;
    const costFmt = this._formatCost(rawCost);
    // Magnitude-based tier so high-burn agents pop visually:
    //   <= $0.05 faint | <= $1 normal | <= $5 warn | >$5 alert
    let costTier = 'cost-normal';
    if (rawCost > 5)      costTier = 'cost-alert';
    else if (rawCost > 1) costTier = 'cost-warn';
    else if (rawCost <= 0.05) costTier = 'cost-faint';
    const costStr = `<span class="${costTier}" title="$${rawCost.toFixed(4)}">${costFmt}</span>`;

    const badgeClass = `badge-${member.status}`;
    const statusLabel = member.status.charAt(0).toUpperCase() + member.status.slice(1);
    // Map each status to a sprite icon reference. Unknown statuses fall
    // back to the idle moon (better than a missing graphic).
    const iconByStatus = {
      working:    { href: 'icon-working',    extra: 'status-icon--pulse' },
      idle:       { href: 'icon-idle',       extra: '' },
      stuck:      { href: 'icon-stuck',      extra: '' },
      meeting:    { href: 'icon-meeting',    extra: '' },
      brainstorm: { href: 'icon-brainstorm', extra: 'status-icon--pulse' },
    };
    const iconMeta = iconByStatus[member.status] || iconByStatus.idle;
    const statusIconHtml = `<svg class="status-icon ${iconMeta.extra}" aria-hidden="true"><use href="#${iconMeta.href}"/></svg>`;
    const taskHtml = currentTask
      ? `<div class="agent-card-task">${this.escapeHtml((currentTask.title || '').slice(0, 60))}</div>`
      : '';

    const progressHtml = total > 0 ? `
      <div class="agent-card-progress">
        <div class="agent-progress-label">
          <span>${completed}/${total} tasks</span>
          <span>${pct}%</span>
        </div>
        <div class="agent-progress-bar">
          <div class="agent-progress-fill" style="width:${pct}%"></div>
        </div>
      </div>
    ` : '';

    // Avatar: SVG icon (by configId/role) or fallback to initials
    const avatarColors = {
      blue: '#1d4ed8', purple: '#7e22ce', pink: '#9d174d',
      green: '#166534', orange: '#9a3412', yellow: '#854d0e', red: '#991b1b',
    };
    const avatarBg = avatarColors[member.avatarColor] || '#27272a';
    const iconSvg = this.getAgentIcon(member);
    const avatarContent = member.avatarInitial || (member.name ? member.name[0] : '?');
    const avatarHtml = iconSvg
      ? `<div class="agent-avatar agent-avatar-icon" style="background:${avatarBg}">${iconSvg}</div>`
      : `<div class="agent-avatar" style="background:${avatarBg}">${this.escapeHtml(avatarContent)}</div>`;

    const lastActiveStr = member.lastActivityAt ? this.timeAgo(member.lastActivityAt) : '';
    const lastActiveHtml = lastActiveStr
      ? `<span class="agent-card-last-active" title="Last active">${this.escapeHtml(lastActiveStr)}</span>`
      : '';

    // R18 · Turn-count badge. Shows after 50 turns (~mid-session), goes
    // red at 80+ so operators can spot long-runners before the 100-turn
    // cap stops them. Tooltip exposes exact count + cap flag.
    const turnCount = (member.usage && typeof member.usage.turnCount === 'number') ? member.usage.turnCount : 0;
    const turnCapped = !!(member.usage && member.usage.turnCapReached);
    let turnBadgeHtml = '';
    if (turnCount >= 50 || turnCapped) {
      const cls = turnCapped ? 'turn-badge turn-capped' : turnCount >= 80 ? 'turn-badge turn-hot' : 'turn-badge';
      turnBadgeHtml = `<span class="${cls}" title="${turnCount} turns${turnCapped ? ' — turn cap reached' : ''}">${turnCount}t</span>`;
    }

    // R19 · Event-drop pill. Non-zero means the SDK session's outbound
    // channel overflowed; dashboard data for this agent may be stale.
    // Small grey pill so it doesn't scream, but the tooltip makes the
    // implication explicit.
    const eventsDropped = (member.usage && typeof member.usage.eventsDropped === 'number') ? member.usage.eventsDropped : 0;
    const dropBadgeHtml = eventsDropped > 0
      ? `<span class="drop-badge" title="${eventsDropped} SSE event${eventsDropped === 1 ? '' : 's'} dropped — dashboard data may be stale">⚠ ${eventsDropped}ev</span>`
      : '';

    // R20 · Loop advisory badge. Advisory-only — the backend never
    // auto-stops. Orange so it reads as "check this" without the red
    // alarm of a hard failure. Clears on the next turn boundary.
    const inLoop = !!(member.usage && member.usage.inLoop);
    const loopBadgeHtml = inLoop
      ? `<span class="loop-badge" title="Output-repeat advisory — agent may be looping on the same emission">⟳ loop?</span>`
      : '';

    // R21 · Tool-level loop advisory badge. Red because (toolName,
    // resultHash) duplication is almost always a real loop —
    // agent re-ran the same tool with the same args/output 3+ times.
    // Shows the looping tool name ("⟲ Bash") so operators can jump
    // straight to inspecting that command chain.
    const toolInLoop = !!(member.usage && member.usage.toolInLoop);
    const loopingTool = (member.usage && member.usage.loopingTool) || 'tool';
    const toolLoopBadgeHtml = toolInLoop
      ? `<span class="tool-loop-badge" title="Tool-loop advisory — ${this.escapeAttr(loopingTool)} produced identical output 3+ times">⟲ ${this.escapeHtml(loopingTool)}</span>`
      : '';

    // Feature 5: Unread message badge
    const unreadCount = (this.state.recentMessages || []).filter(
      m => (m.toSessionId === member.sessionId) && m.id && !this._readMessages.has(m.id)
    ).length;
    const unreadBadgeHtml = unreadCount > 0
      ? `<span class="unread-badge" title="${unreadCount} unread message${unreadCount > 1 ? 's' : ''}">${unreadCount}</span>`
      : '';

    // Feature 6: Active tool / thinking indicator
    let activeToolHtml = '';
    if (member.status === 'working') {
      const activeTool = member.activeTool || (member.activeTools && member.activeTools[0]);
      if (activeTool) {
        // F3 · crafted tool row: uppercase tool name + optional
        // monospace argument preview, truncated with full value in
        // the title tooltip. Pairs with the border-beam on working
        // tiles so the card reads "beam + currently running Bash
        // on <cmd>" at a glance.
        const toolStr = String(activeTool);
        const colonIdx = toolStr.indexOf(':');
        const toolName = colonIdx > 0 ? toolStr.slice(0, colonIdx) : toolStr;
        const toolArg = colonIdx > 0 ? toolStr.slice(colonIdx + 1).trim() : '';
        const iconRef = this._toolIconRef(toolName);
        const argPreview = toolArg ? toolArg.slice(0, 40) + (toolArg.length > 40 ? '…' : '') : '';
        activeToolHtml = `<div class="agent-card-tool-now" title="${this.escapeAttr(toolStr)}">
          <svg class="tool-icon" aria-hidden="true"><use href="#${iconRef}"/></svg>
          <span class="tool-name">${this.escapeHtml(toolName)}</span>
          ${argPreview ? `<span class="tool-arg">${this.escapeHtml(argPreview)}</span>` : ''}
        </div>`;
      } else {
        activeToolHtml = `<div class="agent-thinking"><span class="thinking-dots"><span class="thinking-dot"></span><span class="thinking-dot"></span><span class="thinking-dot"></span></span><span>Thinking...</span></div>`;
      }
    }

    // Feature 8: Autonomy badge
    const autonomyLevel = this._autonomyLevels.get(member.sessionId) || 'full';
    const autonomyLabels = { full: 'Full Auto', cautious: 'Cautious', manual: 'Manual' };
    const autonomyBadgeHtml = `<span class="autonomy-badge autonomy-${autonomyLevel}">${this.escapeHtml(autonomyLabels[autonomyLevel] || 'Full Auto')}</span>`;

    const learningCount = member.learningCount || 0;
    const skillCount = member.skillCount || 0;
    const learningsBadgeHtml = learningCount > 0
      ? `<div class="agent-learnings">🧠 ${learningCount}${skillCount > 0 ? ` · ${skillCount} skills` : ''}</div>`
      : '';

    // R37 · session-mode badge so operators see at a glance which spawn
    // path and billing pool each agent is on. "sdk" = Claude Agent SDK
    // (hits the new SDK credit pool post-2026-06-15); "hooks" = PTY
    // claude with HTTP hooks (interactive subscription); "legacy" =
    // PTY without hooks. Empty string means pre-R37 spawn -- skip the
    // badge entirely so old agents look the same as before.
    const sessionMode = (member.sessionMode || '').toLowerCase();
    const sessionModeLabels = {
      sdk:    { text: 'via SDK',    title: 'Claude Agent SDK -- programmatic billing pool' },
      hooks:  { text: 'via hooks',  title: 'Interactive PTY + HTTP hooks -- interactive subscription pool' },
      legacy: { text: 'via PTY',    title: 'Legacy ModeClaude (no structured events)' },
    };
    const modeMeta = sessionModeLabels[sessionMode];
    const sessionModeBadgeHtml = modeMeta
      ? `<span class="session-mode-badge mode-${this.escapeAttr(sessionMode)}" title="${this.escapeAttr(modeMeta.title)}">${this.escapeHtml(modeMeta.text)}</span>`
      : '';

    // Score badge from server-computed AgentScore
    const score = member.score || {};
    const scoreGrade = score.grade || '';
    const scoreOverall = typeof score.overallScore === 'number' ? Math.round(score.overallScore * 100) : null;
    const scoreBadgeHtml = scoreGrade
      ? `<span class="score-badge grade-${scoreGrade}" title="Score: ${scoreOverall !== null ? scoreOverall + '%' : '?'}">${scoreGrade}</span>`
      : '';

    // Recent activities: last 5 tasks (most recent first) -- SVG icons
    const recentTasks = tasks.slice().sort((a, b) => {
      const aTime = a.completedAt || a.assignedAt || '';
      const bTime = b.completedAt || b.assignedAt || '';
      return bTime > aTime ? 1 : bTime < aTime ? -1 : 0;
    }).slice(0, 5);

    const actIcons = {
      completed: '<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>',
      in_progress: '<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polygon points="5 3 19 12 5 21 5 3"/></svg>',
      failed: '<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>',
      blocked: '<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="4.93" y1="4.93" x2="19.07" y2="19.07"/></svg>',
    };

    const recentActivityHtml = recentTasks.length > 0 ? `
      <div class="agent-card-activities">
        ${recentTasks.map(t => {
          const icon = actIcons[t.status] || '<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/></svg>';
          const cls = t.status === 'completed' ? 'act-done' : t.status === 'in_progress' ? 'act-wip' : t.status === 'failed' ? 'act-fail' : 'act-pending';
          return `<div class="agent-activity-item ${cls}"><span class="act-icon">${icon}</span><span class="act-text">${this.escapeHtml((t.title || '').slice(0, 45))}</span></div>`;
        }).join('')}
      </div>
    ` : '';

    // Quick action buttons (visible on hover). R18 adds Inject (nudge an
    // agent with operator text) and Kill (surgical stop) alongside the
    // existing Details / Prompt / Terminal trio.
    const quickActionsHtml = `
      <div class="agent-card-actions">
        <button class="agent-quick-btn" onclick="event.stopPropagation(); office.openMemberModal('${member.sessionId}')" title="Details">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="15 3 21 3 21 9"/><line x1="10" y1="14" x2="21" y2="3"/></svg>
        </button>
        <button class="agent-quick-btn" onclick="event.stopPropagation(); office.quickPrompt('${member.sessionId}')" title="Send prompt">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="22" y1="2" x2="11" y2="13"/><polygon points="22 2 15 22 11 13 2 9 22 2"/></svg>
        </button>
        <button class="agent-quick-btn" onclick="event.stopPropagation(); office.injectPrompt('${member.sessionId}')" title="Inject operator prompt">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 2l3 7h7l-5.5 4.5L18 21l-6-4-6 4 1.5-7.5L2 9h7z"/></svg>
        </button>
        <button class="agent-quick-btn" onclick="event.stopPropagation(); office.navigateToSessionDirect('${member.sessionId}')" title="Terminal">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="4 17 10 11 4 5"/><line x1="12" y1="19" x2="20" y2="19"/></svg>
        </button>
        <button class="agent-quick-btn" onclick="event.stopPropagation(); office.bumpAgentWorkdir('${member.sessionId}')" title="Change working directory">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 7h6l2 3h10v9H3z"/></svg>
        </button>
        <button class="agent-quick-btn" onclick="event.stopPropagation(); office.restartAgent('${member.sessionId}')" title="Restart agent in place (keep memory + tasks)">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15A9 9 0 1 1 18 5.3L23 10"/></svg>
        </button>
        <button class="agent-quick-btn agent-quick-btn--danger" onclick="event.stopPropagation(); office.killAgent('${member.sessionId}')" title="Kill agent">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="4.93" y1="4.93" x2="19.07" y2="19.07"/></svg>
        </button>
      </div>
    `;

    card.innerHTML = `
      ${unreadBadgeHtml}
      ${quickActionsHtml}
      <div class="agent-card-header">
        ${avatarHtml}
        <div class="agent-card-meta">
          <div class="agent-card-name-row">
            <span class="agent-card-name">${this.escapeHtml(member.name)}</span>
            ${scoreBadgeHtml}
          </div>
          <div class="agent-card-role">${this.escapeHtml(member.role)}</div>
        </div>
        <span class="agent-status-badge ${badgeClass}">
          ${statusIconHtml}
          <span class="status-dot"></span>
          ${statusLabel}
        </span>
      </div>
      ${taskHtml}
      ${progressHtml}
      ${activeToolHtml ? `<div class="agent-card-tool-wrap">${activeToolHtml}</div>` : ''}
      ${recentActivityHtml}
      <div class="agent-card-footer">
        <span class="agent-card-workdir" title="${this.escapeAttr(member.workingDir || '')}">${this.escapeHtml((member.workingDir || '').replace(/^.*[/\\]/, ''))}</span>
        <span class="agent-card-cost">${lastActiveHtml}${toolLoopBadgeHtml}${loopBadgeHtml}${dropBadgeHtml}${turnBadgeHtml}${costStr}</span>
      </div>
      <div class="agent-card-badges">${autonomyBadgeHtml}${sessionModeBadgeHtml}${learningsBadgeHtml}</div>
      ${this._renderCardSparkline(member)}
    `;
  },

  _renderCardSparkline(member) {
    const recent = member && member.usage && Array.isArray(member.usage.recentTurnCosts)
      ? member.usage.recentTurnCosts : [];
    if (recent.length < 2) return '';
    const svg = this._renderSparkline(recent, 60, 14);
    if (!svg) return '';
    return `<div class="agent-card-spark" title="Recent turn costs">${svg}</div>`;
  },

  updateAgentCard(card, member) {
    // Update CSS status class; include no-anim to suppress fadeSlideIn on re-render.
    // Preserve .border-beam so the pseudo-element stays ready, toggle
    // .border-beam--active based on whether the agent is working.
    // Round 10 · preserve .is-pinned across re-renders.
    const pinned = this.isPinned(member);
    card.className = `agent-card border-beam status-${member.status} no-anim${member.status === 'working' ? ' border-beam--active' : ''}${pinned ? ' is-pinned' : ''}`;
    card.setAttribute('aria-label', `${member.name} - ${member.role} (${member.status})`);
    // Re-fill content to reflect new state
    this.fillAgentCard(card, member);
    // fillAgentCard overwrites innerHTML, so re-inject the star button.
    this._injectStarButton(card, member);
  },


  // ========== Detail Panel ==========

  showDetail(sessionId) {
    this.detailSessionId = sessionId;
    const panel = document.getElementById('detailPanel');
    if (panel) panel.classList.add('open');
    this.renderDetailPanel(sessionId);
  },

  closeDetail() {
    this.detailSessionId = null;
    const panel = document.getElementById('detailPanel');
    if (panel) panel.classList.remove('open');
  },

  renderDetailPanel(sessionId) {
    const container = document.getElementById('detailPanelContent');
    if (!container) return;

    const member = this.state.members.find(m => m.sessionId === sessionId);
    if (!member) {
      container.innerHTML = '<div style="color:#52525b;font-size:12px;padding:16px">Member not found</div>';
      return;
    }

    const queue = (this.taskQueues || []).find(q => q.sessionId === sessionId);
    const tasks = queue ? queue.tasks || [] : [];

    // Tasks HTML
    const taskItems = tasks.slice(0, 10).map(t => `
      <div class="detail-task-item">
        <span class="detail-task-status ${t.status}"></span>
        <span>${this.escapeHtml((t.title || '').slice(0, 50))}</span>
      </div>
    `).join('');

    // Recent messages HTML
    const agentMessages = (this.state.recentMessages || [])
      .filter(m => m.fromSessionId === sessionId || m.toSessionId === sessionId)
      .slice(-5)
      .reverse();

    const msgItems = agentMessages.map(msg => {
      const direction = msg.fromSessionId === sessionId
        ? `To ${this.escapeHtml(msg.toName)}`
        : `From ${this.escapeHtml(msg.fromName)}`;
      const ago = this.formatTimeAgo(msg.timestamp);
      return `<div class="detail-message-item">
        <div class="detail-message-meta">${direction} (${ago})</div>
        ${this.escapeHtml((msg.content || '').slice(0, 120))}
      </div>`;
    }).join('');

    const costStr = (member.totalCost || 0) >= 1
      ? '$' + (member.totalCost || 0).toFixed(2)
      : (((member.totalCost || 0) * 100).toFixed(0)) + 'c';

    const lastActive = member.lastActivityAt
      ? `Last active: ${this.formatTimeAgo(member.lastActivityAt)}`
      : '';

    const detailScore = member.score || {};
    const detailGrade = detailScore.grade || '';
    const detailGradeColor = { A: '#22c55e', B: '#4ade80', C: '#eab308', D: '#f97316', F: '#ef4444' }[detailGrade] || '#52525b';
    const detailScoreHtml = detailGrade ? `
      <div style="margin-top:6px;display:flex;align-items:center;gap:8px;flex-wrap:wrap">
        <span class="score-badge grade-${detailGrade}" style="font-size:11px;padding:2px 8px">${detailGrade}</span>
        <span style="font-size:11px;color:${detailGradeColor}">${Math.round((detailScore.overallScore || 0) * 100)}% overall</span>
        <span style="font-size:11px;color:#52525b">Completion: ${Math.round((detailScore.completionRate || 0) * 100)}%</span>
        <span style="font-size:11px;color:#52525b">Collab: ${Math.round((detailScore.collaborationScore || 0) * 100)}%</span>
      </div>` : '';

    container.innerHTML = `
      <div class="detail-agent-header">
        <div class="agent-avatar ${this.getAgentIcon(member) ? 'agent-avatar-icon' : ''} avatar-${member.avatarColor}" style="width:44px;height:44px;font-size:16px;border-radius:10px">${this.getAgentIcon(member) || this.escapeHtml(member.avatarEmoji || member.avatarInitial)}</div>
        <div>
          <div class="detail-agent-name">${this.escapeHtml(member.name)}</div>
          <div class="detail-agent-role">${this.escapeHtml(member.role)}</div>
        </div>
      </div>
      <div class="detail-section">
        <div class="detail-section-title">Status</div>
        <span class="agent-status-badge badge-${member.status}">
          <span class="status-dot"></span>
          ${member.status}
        </span>
        ${lastActive ? `<div style="font-size:11px;color:#52525b;margin-top:4px">${lastActive}</div>` : ''}
        ${member.workingDir ? `<div style="font-size:11px;color:#52525b;font-family:monospace;word-break:break-all">${this.escapeHtml(member.workingDir)}</div>` : ''}
        <div style="font-size:11px;color:#52525b;margin-top:2px">Cost: ${costStr}</div>
        ${detailScoreHtml}
      </div>
      <div class="detail-section">
        <div class="detail-section-title">Cost per turn</div>
        <svg class="activity-widget-spark" id="detailUsageSpark" viewBox="0 0 120 32" preserveAspectRatio="none" aria-hidden="true" style="width:100%;height:32px;color:var(--ds-status-info, #3B82F6)"></svg>
        <div class="ds-tabular" id="detailUsageStats" style="font-size:11px;color:#52525b;margin-top:4px">Loading turns…</div>
      </div>
      ${tasks.length > 0 ? `
      <div class="detail-section">
        <div class="detail-section-title">Tasks (${tasks.length})</div>
        <div class="detail-task-list">${taskItems}</div>
      </div>` : ''}
      ${agentMessages.length > 0 ? `
      <div class="detail-section">
        <div class="detail-section-title">Recent Messages</div>
        <div class="detail-message-list">${msgItems}</div>
      </div>` : ''}
      <div class="detail-section">
        <div class="detail-section-title">Whiteboard</div>
        <textarea class="agent-detail-whiteboard-area" id="detailWhiteboardText" placeholder="Freeform notes about what this agent is working on...">Loading...</textarea>
        <div style="display:flex;gap:6px;margin-top:6px;align-items:center">
          <button class="panel-btn" onclick="office.saveWhiteboard('${this.escapeAttr(sessionId)}')">Save</button>
          <span id="detailWhiteboardMeta" style="font-size:11px;color:#52525b"></span>
        </div>
      </div>
      <div class="detail-section">
        <div class="agent-detail-subtabs" role="tablist">
          <button class="agent-detail-subtab active" data-subtab="activity" onclick="office.switchDetailSubtab('${this.escapeAttr(sessionId)}','activity')">Activity</button>
          <button class="agent-detail-subtab" data-subtab="learnings" onclick="office.switchDetailSubtab('${this.escapeAttr(sessionId)}','learnings')">Learnings</button>
          <button class="agent-detail-subtab" data-subtab="cost" onclick="office.switchDetailSubtab('${this.escapeAttr(sessionId)}','cost')">Cost &amp; Tokens</button>
        </div>
        <div class="agent-detail-subpanel" id="detailSubpanel">Loading...</div>
      </div>
      <div class="detail-section">
        <div class="detail-section-title">Terminal</div>
        <pre class="detail-terminal">Loading...</pre>
      </div>
      <div class="detail-section">
        <div class="detail-section-title">Session Analysis</div>
        <div class="analysis-timeline">
          <div class="analysis-event-empty">Loading...</div>
        </div>
      </div>
      <div class="detail-section">
        <button class="panel-btn" style="width:100%" onclick="office.openMemberModal('${this.escapeAttr(sessionId)}')">Open Full Details</button>
        <button class="panel-btn panel-btn-primary" style="width:100%;margin-top:6px" onclick="office.navigateToSessionById('${this.escapeAttr(sessionId)}')">Open Session</button>
        <button class="review-btn" style="width:100%;margin-top:6px" onclick="office.showReview('${this.escapeAttr(sessionId)}')">Review Code Changes</button>
      </div>
    `;

    // Load whiteboard + default sub-tab in parallel with terminal.
    this._loadWhiteboard(sessionId);
    this.switchDetailSubtab(sessionId, 'activity');

    // P13.7 · cost-per-turn sparkline (fire-and-forget).
    this._loadDetailUsageSeries(sessionId);

    // Feature 1: fetch terminal buffer with short-lived cache, then build session analysis timeline
    const detailContent = document.getElementById('detailPanelContent');
    this._fetchTerminalPreview(sessionId)
      .then(data => {
        const termEl = detailContent ? detailContent.querySelector('.detail-terminal') : null;
        if (termEl && data && data.terminalBuffer) {
          const lines = data.terminalBuffer.split('\n').slice(-30).join('\n');
          termEl.textContent = lines;

          // Feature 4: build session analysis timeline from terminal output
          const analysisEl = detailContent ? detailContent.querySelector('.analysis-timeline') : null;
          if (analysisEl) {
            const events = this.parseSessionEvents(data.terminalBuffer);
            if (events.length > 0) {
              analysisEl.innerHTML = events.map(ev =>
                `<div class="analysis-event">
                  <span class="event-icon">${this.escapeHtml(ev.icon)}</span>
                  <span class="event-text">${this.escapeHtml(ev.text)}</span>
                </div>`
              ).join('');
            } else {
              analysisEl.innerHTML = '<div class="analysis-event-empty">No events detected.</div>';
            }
          }
        } else if (termEl) {
          termEl.textContent = '(no terminal output)';
        }
      }).catch(() => {
        const termEl = detailContent ? detailContent.querySelector('.detail-terminal') : null;
        if (termEl) termEl.textContent = '(terminal unavailable)';
      });
  },

  /** Fetch terminal preview with a short-lived 5-second cache to avoid redundant requests. */
  async _fetchTerminalPreview(sessionId) {
    const cached = this._detailCache.get(sessionId);
    if (cached && Date.now() - cached.time < this._detailCacheTimeout) {
      return cached.data;
    }
    const res = await fetch(`/api/sessions/${sessionId}/terminal?tail=4096`);
    if (!res.ok) return null;
    const data = await res.json();
    this._detailCache.set(sessionId, { data, time: Date.now() });
    return data;
  },

  /**
   * Parse terminal output lines and extract key session events.
   * Returns an array of { icon, text } objects (max 15 events).
   */
  parseSessionEvents(terminalBuffer) {
    const lines = terminalBuffer.split('\n');
    const events = [];

    for (const line of lines) {
      if (events.length >= 15) break;
      const stripped = line.replace(/\x1b\[[0-9;]*m/g, '').trim();
      if (!stripped) continue;

      if (/Bash\s*\(/.test(stripped)) {
        const match = stripped.match(/Bash\s*\(\s*["']?([^"')]+)/);
        const cmd = match ? match[1].slice(0, 50) : stripped.slice(0, 50);
        events.push({ icon: '$', text: cmd });
      } else if (/\bWrite\s*\(/.test(stripped)) {
        const match = stripped.match(/Write\s*\(\s*["']?([^"',)]+)/);
        const path = match ? match[1].slice(0, 50) : 'file';
        events.push({ icon: 'W', text: path });
      } else if (/\bEdit\s*\(/.test(stripped)) {
        const match = stripped.match(/Edit\s*\(\s*["']?([^"',)]+)/);
        const path = match ? match[1].slice(0, 50) : 'file';
        events.push({ icon: 'E', text: path });
      } else if (/\bRead\s*\(/.test(stripped)) {
        const match = stripped.match(/Read\s*\(\s*["']?([^"',)]+)/);
        const path = match ? match[1].slice(0, 50) : 'file';
        events.push({ icon: 'R', text: path });
      } else if (/[✓✔]/.test(stripped) || /\bpassed\b/i.test(stripped)) {
        events.push({ icon: '+', text: stripped.slice(0, 60) });
      } else if (/[✗✘]/.test(stripped) || /\bfailed\b/i.test(stripped)) {
        events.push({ icon: '!', text: stripped.slice(0, 60) });
      }
    }

    return events;
  },


  // ========== Agent detail modal: whiteboard + sub-tabs ==========

  // _loadDetailUsageSeries was moved to js/office-widgets.js (P13.7).

  async _loadWhiteboard(sessionId) {
    const area = document.getElementById('detailWhiteboardText');
    const meta = document.getElementById('detailWhiteboardMeta');
    if (!area) return;
    try {
      const res = await fetch('/api/office/whiteboard');
      if (!res.ok) { area.value = ''; return; }
      const data = await res.json();
      const notes = (data && (data.notes || data.data || data)) || {};
      const note = notes[sessionId];
      if (note) {
        area.value = note.content || '';
        if (meta && note.updatedAt) {
          meta.textContent = 'Updated ' + this.formatTimeAgo(new Date(note.updatedAt).getTime());
        }
      } else {
        area.value = '';
        if (meta) meta.textContent = '';
      }
    } catch (err) {
      area.value = '';
    }
  },

  async saveWhiteboard(sessionId) {
    const area = document.getElementById('detailWhiteboardText');
    if (!area) return;
    try {
      await this.postJson('/api/office/whiteboard', {
        sessionId,
        text: area.value || '',
      });
      const meta = document.getElementById('detailWhiteboardMeta');
      if (meta) meta.textContent = 'Saved just now';
    } catch (err) {
      console.error('Failed to save whiteboard:', err);
    }
  },

  switchDetailSubtab(sessionId, subtab) {
    document.querySelectorAll('.agent-detail-subtab').forEach(b => {
      b.classList.toggle('active', b.getAttribute('data-subtab') === subtab);
    });
    const panel = document.getElementById('detailSubpanel');
    if (!panel) return;
    panel.innerHTML = '<div style="color:#52525b;font-size:12px">Loading...</div>';
    if (subtab === 'activity') this._renderDetailActivity(sessionId, panel);
    else if (subtab === 'learnings') this._renderDetailLearnings(sessionId, panel);
    else if (subtab === 'cost') this._renderDetailCost(sessionId, panel);
  },

  async _renderDetailActivity(sessionId, panel) {
    try {
      const res = await fetch(`/api/office/timeline?agent=${encodeURIComponent(sessionId)}&limit=10`);
      if (!res.ok) { panel.innerHTML = '<div style="color:#52525b;font-size:12px">No recent activity.</div>'; return; }
      const data = await res.json();
      const entries = data.entries || [];
      if (entries.length === 0) {
        panel.innerHTML = '<div style="color:#52525b;font-size:12px">No recent activity.</div>';
        return;
      }
      panel.innerHTML = entries.map(e => `
        <div class="timeline-entry">
          <div class="timeline-entry-dot kind-${this._escapeAttr(e.kind)}"></div>
          <div class="timeline-entry-body">
            <div class="timeline-entry-title">${this._escapeHtml(e.title || '')}</div>
            ${e.summary ? `<div class="timeline-entry-summary">${this._escapeHtml(e.summary)}</div>` : ''}
            <div class="timeline-entry-time">${this.formatTimeAgo(new Date(e.timestamp).getTime())}</div>
          </div>
        </div>
      `).join('');
    } catch (err) {
      panel.innerHTML = '<div style="color:#52525b;font-size:12px">Failed to load activity.</div>';
    }
  },

  async _renderDetailLearnings(sessionId, panel) {
    const member = this.state.members.find(m => m.sessionId === sessionId);
    const configId = member && member.configId;
    if (!configId) {
      panel.innerHTML = '<div style="color:#52525b;font-size:12px">No agent profile associated.</div>';
      return;
    }
    try {
      const res = await fetch(`/api/office/learnings/${encodeURIComponent(configId)}`);
      if (!res.ok) { panel.innerHTML = '<div style="color:#52525b;font-size:12px">No learnings available.</div>'; return; }
      const data = await res.json();
      const items = data.learnings || data.entries || [];
      if (items.length === 0) {
        panel.innerHTML = '<div style="color:#52525b;font-size:12px">No learnings yet.</div>';
        return;
      }
      panel.innerHTML = items.slice(0, 30).map(l => `
        <div class="agent-detail-learning-item">
          <div class="agent-detail-learning-head">
            <span class="agent-detail-learning-cat">${this._escapeHtml(l.category || 'general')}</span>
            <span class="agent-detail-learning-time">${l.learnedAt ? this.formatTimeAgo(new Date(l.learnedAt).getTime()) : ''}</span>
          </div>
          <div class="agent-detail-learning-body">${this._escapeHtml(l.content || l.text || '')}</div>
        </div>
      `).join('');
    } catch (err) {
      panel.innerHTML = '<div style="color:#52525b;font-size:12px">Failed to load learnings.</div>';
    }
  },

  _renderDetailCost(sessionId, panel) {
    const member = this.state.members.find(m => m.sessionId === sessionId);
    if (!member) { panel.innerHTML = '<div style="color:#52525b;font-size:12px">No member data.</div>'; return; }
    const u = member.usage || {};
    const total = u.totalCostUSD || member.totalCost || 0;
    const input = u.totalInputTokens || 0;
    const output = u.totalOutputTokens || 0;
    const turns = u.turnCount || 0;
    const recent = Array.isArray(u.recentTurnCosts) ? u.recentTurnCosts : [];
    const spark = recent.length > 1 ? this._renderSparkline(recent, 180, 36) : '';
    panel.innerHTML = `
      <div class="agent-detail-cost-grid">
        <div class="agent-detail-cost-cell">
          <div class="agent-detail-cost-label">Total spend</div>
          <div class="agent-detail-cost-value">$${total.toFixed(4)}</div>
        </div>
        <div class="agent-detail-cost-cell">
          <div class="agent-detail-cost-label">Turns</div>
          <div class="agent-detail-cost-value">${turns}</div>
        </div>
        <div class="agent-detail-cost-cell">
          <div class="agent-detail-cost-label">Input tokens</div>
          <div class="agent-detail-cost-value">${input.toLocaleString()}</div>
        </div>
        <div class="agent-detail-cost-cell">
          <div class="agent-detail-cost-label">Output tokens</div>
          <div class="agent-detail-cost-value">${output.toLocaleString()}</div>
        </div>
      </div>
      ${spark ? `<div style="margin-top:8px"><div style="font-size:11px;color:#52525b;margin-bottom:4px">Recent turn costs</div>${spark}</div>` : ''}
    `;
  },

  _renderSparkline(values, width, height) {
    if (!values || values.length < 2) return '';
    const max = Math.max(...values, 0.0001);
    const min = Math.min(...values, 0);
    const range = max - min || 1;
    const stepX = width / (values.length - 1);
    const points = values.map((v, i) => {
      const x = (i * stepX).toFixed(1);
      const y = (height - ((v - min) / range) * (height - 2) - 1).toFixed(1);
      return `${x},${y}`;
    }).join(' ');
    return `<svg class="agent-detail-spark-svg" width="${width}" height="${height}" viewBox="0 0 ${width} ${height}" preserveAspectRatio="none">
      <polyline points="${points}" fill="none" stroke="#60a5fa" stroke-width="1.5" />
    </svg>`;
  },


  });
})();
