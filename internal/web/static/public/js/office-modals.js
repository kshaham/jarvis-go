/* office-modals.js — member modal, settings, browser health.
 * Extracted from office.js via the prototype-extension pattern.
 *
 * NOT extracted (require class-body syntax):
 *  - Spawn Config Modal: has class statics (PROVIDER_MODELS,
 *    TEAM_MEMBERS_INFO, SPAWN_AVATAR_COLORS).
 *  - Template Management: has class field declarations
 *    (_editorMembers = [], _editingTemplateId = null).
 *
 * Both stay in office.js until a static-extraction pattern lands
 * (likely a separate `Object.defineProperty` / direct OfficeApp.X
 * assignment block paired with constructor-augmenting code).
 */

(function () {
  'use strict';
  if (typeof OfficeApp === 'undefined') {
    console.error('office-modals.js loaded before OfficeApp; check script order');
    return;
  }

  Object.assign(OfficeApp.prototype, {

  // ========== Member Modal ==========

  async openMemberModal(sessionId) {
    if (this.modalAbortController) this.modalAbortController.abort();
    this.modalAbortController = new AbortController();
    const signal = this.modalAbortController.signal;

    this.selectedMemberId = sessionId;
    const member = this.state.members.find(m => m.sessionId === sessionId);
    if (!member) return;

    const modal = document.getElementById('memberModal');
    const avatar = document.getElementById('modalAvatar');
    const name = document.getElementById('modalName');
    const role = document.getElementById('modalRole');
    const status = document.getElementById('modalStatus');
    const workdir = document.getElementById('modalWorkDir');
    const memoryContent = document.getElementById('modalMemoryContent');

    const modalIcon = this.getAgentIcon(member);
    avatar.className = `modal-avatar ${modalIcon ? 'agent-avatar-icon' : ''} avatar-${member.avatarColor}`;
    if (modalIcon) {
      avatar.innerHTML = modalIcon;
    } else {
      avatar.textContent = member.avatarInitial;
    }
    name.textContent = member.name;
    role.textContent = member.role;
    status.innerHTML = `<span class="member-card-status ${member.status}">${member.status}</span>`;
    workdir.textContent = member.workingDir || 'N/A';
    memoryContent.textContent = 'Loading...';

    const taskSection = document.getElementById('modalCurrentTask');
    const taskTitle = document.getElementById('modalTaskTitle');
    const taskProgressFill = document.getElementById('modalTaskProgressFill');
    const taskProgressLabel = document.getElementById('modalTaskProgressLabel');

    const queue = (this.taskQueues || []).find(q => q.sessionId === sessionId);
    if (queue && queue.tasks && queue.tasks.length > 0) {
      const currentTask = queue.tasks.find(t => t.status === 'in_progress') || queue.tasks.find(t => t.status === 'pending');
      if (currentTask) {
        taskTitle.textContent = currentTask.title;
        const total = queue.tasks.length;
        const completed = queue.tasks.filter(t => t.status === 'completed' || t.status === 'failed').length;
        const pct = total > 0 ? Math.round((completed / total) * 100) : 0;
        taskProgressFill.style.width = `${pct}%`;
        taskProgressLabel.textContent = `${completed}/${total} tasks (${pct}%)`;
        taskSection.classList.remove('hidden');
      } else {
        taskSection.classList.add('hidden');
      }
    } else {
      taskSection.classList.add('hidden');
    }

    const lastActivityEl = document.getElementById('modalLastActivity');
    if (member.lastActivityAt) {
      lastActivityEl.textContent = `Last active: ${this.formatTimeAgo(member.lastActivityAt)}`;
    } else {
      lastActivityEl.textContent = '';
    }

    const agentMsgList = document.getElementById('modalAgentMessagesList');
    const agentMessages = (this.state.recentMessages || []).filter(
      m => m.fromSessionId === sessionId || m.toSessionId === sessionId
    ).slice(-5).reverse();

    if (agentMessages.length > 0) {
      agentMsgList.innerHTML = agentMessages.map(msg => {
        const direction = msg.fromSessionId === sessionId ? `To ${this.escapeHtml(msg.toName)}` : `From ${this.escapeHtml(msg.fromName)}`;
        const ago = this.formatTimeAgo(msg.timestamp);
        return `<div class="modal-agent-message-entry">
          <div class="modal-agent-message-header">${direction} (${ago})</div>
          ${this.escapeHtml((msg.content || '').slice(0, 200))}
        </div>`;
      }).join('');
      document.getElementById('modalAgentMessages').classList.remove('hidden');
    } else {
      agentMsgList.innerHTML = '<div class="modal-agent-message-entry">No recent messages.</div>';
    }

    const promptInput = document.getElementById('modalPromptInput');
    if (promptInput) promptInput.value = '';

    const personalityInput = document.getElementById('modalPersonalityInput');
    const systemPromptInput = document.getElementById('modalSystemPromptInput');
    if (personalityInput) personalityInput.value = '';
    if (systemPromptInput) systemPromptInput.value = '';

    // Feature 8: Populate autonomy dropdown
    const autonomySelect = document.getElementById('modalAutonomySelect');
    if (autonomySelect) {
      autonomySelect.value = this._autonomyLevels.get(sessionId) || 'full';
    }

    modal.classList.remove('hidden');

    try {
      const persRes = await fetch(`/api/office/member/${sessionId}/personality`, { signal });
      if (this.selectedMemberId !== sessionId) return;
      const persData = await persRes.json();
      if (persData.success) {
        if (personalityInput) personalityInput.value = persData.personality || '';
        if (systemPromptInput) systemPromptInput.value = persData.systemPrompt || '';
      }
    } catch {
      // Non-critical
    }

    try {
      const res = await fetch(`/api/office/memory/${sessionId}`, { signal });
      if (this.selectedMemberId !== sessionId) return;
      const data = await res.json();
      if (data && data.shortTerm) {
        const entries = data.shortTerm.slice(-10).reverse();
        if (entries.length > 0) {
          memoryContent.textContent = entries.map(e => {
            const ago = Math.round((Date.now() - e.timestamp) / 60000);
            return `[${e.category}] ${e.summary} (${ago}m ago)`;
          }).join('\n');
        } else {
          memoryContent.textContent = 'No recent activity recorded.';
        }
      } else {
        memoryContent.textContent = 'No memory data available.';
      }
    } catch {
      memoryContent.textContent = 'Failed to load memory.';
    }
  },

  async sendPromptToMember() {
    if (!this.selectedMemberId) return;
    const input = document.getElementById('modalPromptInput');
    if (!input) return;
    const prompt = input.value.trim();
    if (!prompt) return;

    try {
      const data = await this.postJson(`/api/sessions/${this.selectedMemberId}/input`, { input: prompt });
      if (data && !data.error) {
        input.value = '';
        this.showNotification(`Prompt sent to ${this.state.members.find(m => m.sessionId === this.selectedMemberId)?.name || 'agent'}`, 'success');
      } else {
        this.showNotification('Failed to send prompt', 'error');
      }
    } catch {
      this.showNotification('Failed to send prompt', 'error');
    }
  },

  async saveAgentPersonality() {
    if (!this.selectedMemberId) return;
    const personality = (document.getElementById('modalPersonalityInput')?.value || '').trim();
    const systemPrompt = (document.getElementById('modalSystemPromptInput')?.value || '').trim();

    try {
      const data = await this.putJson(`/api/office/member/${this.selectedMemberId}/personality`, {
        personality,
        systemPrompt,
      });
      if (data.success) {
        this.showNotification('Personality updated and applied', 'success');
        this.addActivityEntry('system', 'Agent personality updated');
      } else {
        this.showNotification(data.error || 'Failed to update personality', 'error');
      }
    } catch {
      this.showNotification('Failed to update personality', 'error');
    }
  },

  async resetAgentPersonality() {
    if (!this.selectedMemberId) return;

    try {
      const data = await this.putJson(`/api/office/member/${this.selectedMemberId}/personality`, {
        personality: '',
        systemPrompt: '',
      });
      if (data.success) {
        const persRes = await fetch(`/api/office/member/${this.selectedMemberId}/personality`);
        const persData = await persRes.json();
        if (persData.success) {
          const personalityInput = document.getElementById('modalPersonalityInput');
          const systemPromptInput = document.getElementById('modalSystemPromptInput');
          if (personalityInput) personalityInput.value = persData.personality || '';
          if (systemPromptInput) systemPromptInput.value = persData.systemPrompt || '';
        }
        this.showNotification('Personality reset to default', 'info');
        this.addActivityEntry('system', 'Agent personality reset to default');
      } else {
        this.showNotification(data.error || 'Failed to reset personality', 'error');
      }
    } catch {
      this.showNotification('Failed to reset personality', 'error');
    }
  },

  closeMemberModal() {
    if (this.modalAbortController) {
      this.modalAbortController.abort();
      this.modalAbortController = null;
    }
    document.getElementById('memberModal').classList.add('hidden');
    this.selectedMemberId = null;
  },


  // ========== Settings ==========

  async fetchOfficeSettings() {
    try {
      const res = await fetch('/api/office/settings');
      const data = await res.json();
      if (data.success) {
        this.officeSettings = data;
        this.populateSettingsForm(data);
        this.updateStatusBar();
      }
    } catch (err) {
      // Non-critical
    }
  },

  populateSettingsForm(settings) {
    const el = (id) => document.getElementById(id);
    const autoEnabled = el('settingAutoStandupEnabled');
    const autoInterval = el('settingAutoStandupInterval');
    const brainstormCooldown = el('settingBrainstormCooldown');
    const brainstormResponseTimeout = el('settingBrainstormResponseTimeout');
    const idleThreshold = el('settingIdleThreshold');
    const meetingTimeout = el('settingMeetingTimeout');
    const standupTimeout = el('settingStandupTimeout');
    const fileLockExpiry = el('settingFileLockExpiry');
    const teamMission = el('settingTeamMission');

    if (autoEnabled && settings.autoStandup) autoEnabled.checked = settings.autoStandup.enabled;
    if (autoInterval && settings.autoStandup) autoInterval.value = Math.round(settings.autoStandup.intervalMs / 3600000 * 10) / 10;
    if (brainstormCooldown) brainstormCooldown.value = Math.round(settings.brainstormCooldownMs / 60000);
    if (brainstormResponseTimeout) brainstormResponseTimeout.value = Math.round(settings.brainstormResponseTimeoutMs / 1000);
    if (idleThreshold) idleThreshold.value = Math.round(settings.idleThresholdMs / 60000);
    if (meetingTimeout) meetingTimeout.value = Math.round(settings.meetingInactivityTimeoutMs / 1000);
    if (standupTimeout) standupTimeout.value = Math.round(settings.standupResponseTimeoutMs / 1000);
    if (fileLockExpiry) fileLockExpiry.value = Math.round(settings.fileLockExpiryMs / 60000);
    if (teamMission) teamMission.value = settings.teamMission || '';
    this._updateMissionCharCount();

    this.updateMissionBanner(settings.teamMission);
  },

  _filterSettings(q) {
    const panel = document.getElementById('settingsPanel');
    if (!panel) return;
    const needle = (q || '').trim().toLowerCase();
    const sections = panel.querySelectorAll('.settings-section');
    let visible = 0;
    sections.forEach(sec => {
      if (!needle) {
        sec.classList.remove('filtered-out');
        visible++;
        return;
      }
      // Match against the section title plus any label/span text.
      const hay = sec.textContent.toLowerCase();
      const match = hay.includes(needle);
      sec.classList.toggle('filtered-out', !match);
      if (match) visible++;
    });
    const empty = document.getElementById('settingsSearchEmpty');
    if (empty) empty.classList.toggle('hidden', visible > 0);
  },

  _setActivityFilter(type) {
    this._activityFilter = type || 'all';
    document.querySelectorAll('#activityTypeBar .timeline-filter-chip').forEach(btn => {
      btn.classList.toggle('active', btn.dataset.activityType === this._activityFilter);
    });
    // Apply to currently rendered entries. The activity feed is always
    // fully rebuilt when events arrive (or stays stable), so we can
    // toggle .filtered-out without touching activityLog itself.
    const feed = document.getElementById('activityFeed');
    if (!feed) return;
    feed.querySelectorAll('.activity-entry').forEach(el => {
      const cls = Array.from(el.classList).find(c => c.startsWith('type-')) || '';
      const entryType = cls.replace('type-', '');
      const show = this._activityFilter === 'all' || entryType === this._activityFilter;
      el.classList.toggle('filtered-out', !show);
    });
  },

  _updateMissionCharCount() {
    const ta = document.getElementById('settingTeamMission');
    const out = document.getElementById('missionCharCount');
    if (!ta || !out) return;
    const len = ta.value.length;
    const max = parseInt(ta.getAttribute('maxlength') || '1000', 10);
    out.textContent = `${len} / ${max}`;
    out.classList.toggle('near-limit', len >= max * 0.9 && len < max);
    out.classList.toggle('at-limit', len >= max);
  },

  async updateSetting(key) {
    const el = (id) => document.getElementById(id);
    const body = {};

    switch (key) {
      case 'autoStandupEnabled':
        body.autoStandup = { enabled: el('settingAutoStandupEnabled').checked };
        break;
      case 'autoStandupInterval': {
        const hours = parseFloat(el('settingAutoStandupInterval').value);
        if (hours >= 0.5) body.autoStandup = { intervalMs: Math.round(hours * 3600000) };
        break;
      }
      case 'brainstormCooldown': {
        const mins = parseInt(el('settingBrainstormCooldown').value);
        if (mins >= 1) body.brainstormCooldownMs = mins * 60000;
        break;
      }
      case 'brainstormResponseTimeout': {
        const secs = parseInt(el('settingBrainstormResponseTimeout').value);
        if (secs >= 30) body.brainstormResponseTimeoutMs = secs * 1000;
        break;
      }
      case 'idleThreshold': {
        const mins = parseInt(el('settingIdleThreshold').value);
        if (mins >= 1) body.idleThresholdMs = mins * 60000;
        break;
      }
      case 'meetingTimeout': {
        const secs = parseInt(el('settingMeetingTimeout').value);
        if (secs >= 10) body.meetingInactivityTimeoutMs = secs * 1000;
        break;
      }
      case 'standupTimeout': {
        const secs = parseInt(el('settingStandupTimeout').value);
        if (secs >= 30) body.standupResponseTimeoutMs = secs * 1000;
        break;
      }
      case 'fileLockExpiry': {
        const mins = parseInt(el('settingFileLockExpiry').value);
        if (mins >= 1) body.fileLockExpiryMs = mins * 60000;
        break;
      }
      case 'teamMission':
        body.teamMission = el('settingTeamMission').value;
        break;
    }

    try {
      const data = await this.putJson('/api/office/settings', body);
      if (data.success) {
        this.officeSettings = data;
        this.showSettingsSaved();
        this.showToast('Settings saved', 'success');
      }
    } catch (err) {
      console.error('Failed to update setting:', err);
      this.showToast('Failed to save settings', 'error');
    }
  },

  showSettingsSaved() {
    const status = document.getElementById('settingsSaveStatus');
    if (status) {
      status.textContent = 'Settings saved';
      setTimeout(() => { status.textContent = ''; }, 2000);
    }
  },

  updateMissionBanner(mission) {
    const banner = document.getElementById('officeMissionBanner');
    const text = document.getElementById('missionBannerText');
    if (!banner || !text) return;

    if (mission && mission.trim()) {
      const trimmed = mission.trim();
      text.textContent = trimmed;
      // Full mission in a native tooltip so operators can see the whole
      // thing when the badge ellipsis truncates it.
      banner.setAttribute('title', trimmed);
      banner.classList.remove('hidden');
    } else {
      banner.classList.add('hidden');
      banner.removeAttribute('title');
      text.textContent = '';
    }
  },


  // ========== R34 · Browser health indicator (Settings tab) ==========

  startBrowserHealthPoll() {
    this.pollBrowserHealth();
    if (this._browserHealthTimer) return;
    this._browserHealthTimer = setInterval(() => this.pollBrowserHealth(), 60000);
  },

  stopBrowserHealthPoll() {
    if (this._browserHealthTimer) {
      clearInterval(this._browserHealthTimer);
      this._browserHealthTimer = null;
    }
  },

  async pollBrowserHealth() {
    const pill = document.getElementById('browserHealthPill');
    const dot = document.getElementById('browserHealthDot');
    const label = document.getElementById('browserHealthLabel');
    if (!pill || !dot || !label) return;
    try {
      const res = await fetch('/api/office/browser/health');
      if (!res.ok) throw new Error('HTTP ' + res.status);
      const data = await res.json();
      if (data.ok) {
        pill.classList.remove('browser-health-pill--down');
        pill.classList.add('browser-health-pill--up');
        label.textContent = 'browser: ready';
        pill.title = `${data.browserVersion || 'ok'} · ${data.latencyMs}ms · ${data.host}:${data.port}`;
      } else {
        pill.classList.add('browser-health-pill--down');
        pill.classList.remove('browser-health-pill--up');
        label.textContent = 'browser: unreachable';
        pill.title = `${data.error || 'unreachable'} · ${data.host}:${data.port}`;
      }
    } catch (err) {
      pill.classList.add('browser-health-pill--down');
      pill.classList.remove('browser-health-pill--up');
      label.textContent = 'browser: error';
      pill.title = err.message || String(err);
    }
  },

  _renderExperimentHistory(experiments) {
    if (!experiments || experiments.length === 0) {
      return `<div class="experiment-no-data"><div class="empty-icon">&#128202;</div><div class="empty-text">No experiments recorded yet</div><div class="empty-hint">Experiments are recorded when teams are shut down</div></div>`;
    }

    // Show most recent first (up to 20).
    const recent = [...experiments].reverse().slice(0, 20);

    const rows = recent.map(exp => {
      const ts = exp.timestamp ? new Date(exp.timestamp).toLocaleString() : '-';
      const completionPct = exp.taskCount > 0
        ? Math.round((exp.completedCount / exp.taskCount) * 100) + '%'
        : '-';
      const avgScorePct = typeof exp.avgScore === 'number'
        ? Math.round(exp.avgScore * 100) + '%'
        : '-';
      const mission = exp.mission ? this.escapeHtml(exp.mission.slice(0, 40)) + (exp.mission.length > 40 ? '…' : '') : '-';
      return `<tr>
        <td>${this.escapeHtml(ts)}</td>
        <td>${mission}</td>
        <td>${exp.taskCount || 0}</td>
        <td>${completionPct}</td>
        <td>${avgScorePct}</td>
        <td>${this.escapeHtml(exp.duration || '-')}</td>
        <td>${this.escapeHtml(exp.topPerformer || '-')}</td>
      </tr>`;
    }).join('');

    return `<div class="experiment-table-wrapper">
      <table class="experiment-table">
        <thead>
          <tr>
            <th>Time</th>
            <th>Mission</th>
            <th>Tasks</th>
            <th>Completed</th>
            <th>Avg Score</th>
            <th>Duration</th>
            <th>Top Performer</th>
          </tr>
        </thead>
        <tbody>${rows}</tbody>
      </table>
    </div>`;
  },

  _renderHealthScore(metrics, members) {
    let score = 100;
    if ((metrics.totalStuckAlerts || 0) > 5) score -= 20;
    if ((metrics.totalRecoveryAttempts || 0) > 10) score -= 15;
    const successRate = (metrics.totalTasks || 0) > 0
      ? (metrics.totalTasksCompleted || 0) / metrics.totalTasks
      : 1;
    if (successRate < 0.7) score -= 20;
    if (members.filter(mem => mem.status === 'stuck').length > 0) score -= 10;

    const grade = score >= 90 ? 'A' : score >= 80 ? 'B' : score >= 70 ? 'C' : score >= 60 ? 'D' : 'F';
    const gradeColor = grade === 'A' ? '#22c55e'
      : grade === 'B' ? '#4ade80'
      : grade === 'C' ? '#eab308'
      : grade === 'D' ? '#f97316'
      : '#ef4444';
    const gradeLabel = grade === 'A' ? 'Excellent'
      : grade === 'B' ? 'Good'
      : grade === 'C' ? 'Fair'
      : grade === 'D' ? 'Needs Attention'
      : 'Critical';

    return `<div class="health-score-display"><div class="health-grade" style="font-size:48px;font-weight:700;color:${gradeColor}">${grade}</div><div class="health-detail">${score}/100 - ${gradeLabel}</div></div>`;
  },


  });
})();
