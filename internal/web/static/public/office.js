/**
 * @fileoverview Office Dashboard - Linear/Notion-style team view
 *
 * Connects to Jarvis's SSE endpoint for real-time updates,
 * renders agent cards in a CSS Grid, handles inter-session
 * messaging, standup meetings, and team spawning.
 *
 * Vanilla JS, no framework. Uses fetch + EventSource.
 */

/* global EventSource */

// Agent personality icons -- Feather-style inline SVGs (24x24 viewBox)
const AGENT_ICONS = {
  // office-7
  max: '<svg viewBox="0 0 24 24"><path d="M2 20h20l-2-8-4 4-4-8-4 8-4-4z"/><path d="M5 20v2h14v-2"/></svg>',
  sage: '<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="10"/><path d="M16.24 7.76l-2.12 6.36-6.36 2.12 2.12-6.36z"/></svg>',
  nova: '<svg viewBox="0 0 24 24"><circle cx="13.5" cy="6.5" r="2.5"/><path d="M3 20c0 0 5-2 5-10"/><path d="M9.5 14.6L16 20"/><path d="M5.5 12.8L2 22l5-3.4"/></svg>',
  vex: '<svg viewBox="0 0 24 24"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/><line x1="14.5" y1="4" x2="9.5" y2="20"/></svg>',
  bolt: '<svg viewBox="0 0 24 24"><rect x="2" y="2" width="20" height="8" rx="2"/><rect x="2" y="14" width="20" height="8" rx="2"/><line x1="6" y1="6" x2="6.01" y2="6"/><line x1="6" y1="18" x2="6.01" y2="18"/></svg>',
  forge: '<svg viewBox="0 0 24 24"><path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z"/></svg>',
  lint: '<svg viewBox="0 0 24 24"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/><path d="M8 11l2 2 4-4"/></svg>',
  // backend-ai-6
  aria: '<svg viewBox="0 0 24 24"><path d="M12 2a7 7 0 0 0-7 7c0 3 2 5.5 4 7.5L12 22l3-5.5c2-2 4-4.5 4-7.5a7 7 0 0 0-7-7z"/><circle cx="12" cy="9" r="2.5"/></svg>',
  tensor: '<svg viewBox="0 0 24 24"><circle cx="5" cy="6" r="2"/><circle cx="5" cy="18" r="2"/><circle cx="12" cy="9" r="2"/><circle cx="12" cy="15" r="2"/><circle cx="19" cy="12" r="2"/><line x1="7" y1="6" x2="10" y2="9"/><line x1="7" y1="18" x2="10" y2="15"/><line x1="14" y1="9" x2="17" y2="12"/><line x1="14" y1="15" x2="17" y2="12"/></svg>',
  prompt: '<svg viewBox="0 0 24 24"><path d="M17 3a2.83 2.83 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5Z"/><path d="M15 5l4 4"/></svg>',
  dataflow: '<svg viewBox="0 0 24 24"><ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M21 12c0 1.66-4 3-9 3s-9-1.34-9-3"/><path d="M3 5v14c0 1.66 4 3 9 3s9-1.34 9-3V5"/><path d="M12 8v4l3 2"/></svg>',
  nexus: '<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="10"/><line x1="2" y1="12" x2="22" y2="12"/><path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"/></svg>',
  guardian: '<svg viewBox="0 0 24 24"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/><path d="M9 12l2 2 4-4"/></svg>',
  // frontend-studio-6
  pixel: '<svg viewBox="0 0 24 24"><path d="M12 19l7-7 3 3-7 7-3-3z"/><path d="M18 13l-1.5-7.5L2 2l3.5 14.5L13 18l5-5z"/><path d="M2 2l7.586 7.586"/><circle cx="11" cy="11" r="2"/></svg>',
  react: '<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="2.5"/><ellipse cx="12" cy="12" rx="10" ry="4" transform="rotate(0 12 12)"/><ellipse cx="12" cy="12" rx="10" ry="4" transform="rotate(60 12 12)"/><ellipse cx="12" cy="12" rx="10" ry="4" transform="rotate(120 12 12)"/></svg>',
  craft: '<svg viewBox="0 0 24 24"><polygon points="12 2 2 7 12 12 22 7 12 2"/><polyline points="2 17 12 22 22 17"/><polyline points="2 12 12 17 22 12"/></svg>',
  flow: '<svg viewBox="0 0 24 24"><path d="M3 3l7.07 16.97 2.51-7.39 7.39-2.51L3 3z"/><path d="M13 13l6 6"/></svg>',
  access: '<svg viewBox="0 0 24 24"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/></svg>',
  perf: '<svg viewBox="0 0 24 24"><path d="M12 2a10 10 0 0 1 0 20 10 10 0 0 1 0-20"/><path d="M12 12L16.5 7.5"/><path d="M12 6v2"/><path d="M6 12h2"/><path d="M18 12h-2"/></svg>',
  // fullstack-compact-5
  captain: '<svg viewBox="0 0 24 24"><polygon points="12 2 15.09 8.26 22 9.27 17 14.14 18.18 21.02 12 17.77 5.82 21.02 7 14.14 2 9.27 8.91 8.26 12 2"/></svg>',
  dash: '<svg viewBox="0 0 24 24"><rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/></svg>',
  stack: '<svg viewBox="0 0 24 24"><rect x="4" y="4" width="16" height="4" rx="1"/><rect x="4" y="10" width="16" height="4" rx="1"/><rect x="4" y="16" width="16" height="4" rx="1"/></svg>',
  shield: '<svg viewBox="0 0 24 24"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/><rect x="9" y="9" width="6" height="8" rx="1"/><circle cx="12" cy="7" r="1.5"/></svg>',
  ops: '<svg viewBox="0 0 24 24"><path d="M4.5 16.5c-1.5 1.26-2 5-2 5s3.74-.5 5-2c.71-.84.7-2.13-.09-2.91a2.18 2.18 0 0 0-2.91-.09z"/><path d="M12 15l-3-3a22 22 0 0 1 2-3.95A12.88 12.88 0 0 1 22 2c0 2.72-.78 7.5-6 11a22.35 22.35 0 0 1-4 2z"/><path d="M9 12H4s.55-3.03 2-4c1.62-1.08 5 0 5 0"/><path d="M12 15v5s3.03-.55 4-2c1.08-1.62 0-5 0-5"/></svg>',
  // research-lab-7
  atlas: '<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="10"/><path d="M16.24 7.76l-2.12 6.36-6.36 2.12 2.12-6.36z"/><circle cx="12" cy="12" r="2"/></svg>',
  cipher: '<svg viewBox="0 0 24 24"><path d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20"/><path d="M6.5 2H20v20H6.5A2.5 2.5 0 0 1 4 19.5v-15A2.5 2.5 0 0 1 6.5 2z"/><circle cx="14" cy="10" r="3"/><line x1="17" y1="13" x2="19" y2="15"/></svg>',
  prism: '<svg viewBox="0 0 24 24"><polygon points="12 2 22 20 2 20"/><line x1="18" y1="14" x2="22" y2="8"/><line x1="18" y1="14" x2="22" y2="14"/><line x1="18" y1="14" x2="22" y2="20"/></svg>',
  proto: '<svg viewBox="0 0 24 24"><path d="M9 3h6v4a3 3 0 0 1-3 3 3 3 0 0 1-3-3V3z"/><path d="M6 10a6 6 0 0 0 12 0"/><path d="M6 10v8a2 2 0 0 0 2 2h8a2 2 0 0 0 2-2v-8"/><circle cx="12" cy="15" r="2"/></svg>',
  slide: '<svg viewBox="0 0 24 24"><rect x="2" y="3" width="20" height="14" rx="2"/><line x1="8" y1="21" x2="16" y2="21"/><line x1="12" y1="17" x2="12" y2="21"/><rect x="6" y="7" width="4" height="6"/><rect x="12" y="9" width="4" height="4"/></svg>',
  muse: '<svg viewBox="0 0 24 24"><polygon points="12 2 15 8.5 22 9.3 17 14 18.2 21 12 17.8 5.8 21 7 14 2 9.3 9 8.5"/><circle cx="12" cy="12" r="3"/></svg>',
  quill: '<svg viewBox="0 0 24 24"><path d="M17 3a2.83 2.83 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5Z"/><path d="M15 5l4 4"/><line x1="2" y1="22" x2="10" y2="22"/></svg>',
  // innovation-sprint-6
  spark: '<svg viewBox="0 0 24 24"><path d="M9 18h6"/><path d="M10 22h4"/><path d="M12 2a7 7 0 0 0-4 12.7V17h8v-2.3A7 7 0 0 0 12 2z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="10" y1="11" x2="14" y2="11"/></svg>',
  scout: '<svg viewBox="0 0 24 24"><circle cx="8" cy="10" r="5"/><circle cx="16" cy="10" r="5"/><line x1="3" y1="10" x2="3" y2="10"/><line x1="21" y1="10" x2="21" y2="10"/><path d="M11 10h2"/></svg>',
  flux: '<svg viewBox="0 0 24 24"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/><path d="M13 4l-2 16"/><line x1="12" y1="8" x2="12" y2="16"/></svg>',
  pitch: '<svg viewBox="0 0 24 24"><rect x="2" y="3" width="20" height="14" rx="2"/><polygon points="10 7 10 13 15 10"/><line x1="8" y1="21" x2="16" y2="21"/><line x1="12" y1="17" x2="12" y2="21"/></svg>',
  brief: '<svg viewBox="0 0 24 24"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><path d="M9 15l2 2 4-4"/></svg>',
  verify: '<svg viewBox="0 0 24 24"><path d="M9 5H7a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V7a2 2 0 0 0-2-2h-2"/><rect x="9" y="3" width="6" height="4" rx="1"/><circle cx="12" cy="14" r="3"/><line x1="15" y1="17" x2="17" y2="19"/></svg>',
  // Role-based fallbacks
  _role_leader: '<svg viewBox="0 0 24 24"><path d="M2 20h20l-2-8-4 4-4-8-4 8-4-4z"/><path d="M5 20v2h14v-2"/></svg>',
  _role_qa: '<svg viewBox="0 0 24 24"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/><path d="M9 12l2 2 4-4"/></svg>',
  _role_frontend: '<svg viewBox="0 0 24 24"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/><line x1="14.5" y1="4" x2="9.5" y2="20"/></svg>',
  _role_backend: '<svg viewBox="0 0 24 24"><rect x="2" y="2" width="20" height="8" rx="2"/><rect x="2" y="14" width="20" height="8" rx="2"/><line x1="6" y1="6" x2="6.01" y2="6"/><line x1="6" y1="18" x2="6.01" y2="18"/></svg>',
  _role_design: '<svg viewBox="0 0 24 24"><circle cx="13.5" cy="6.5" r="2.5"/><path d="M3 20c0 0 5-2 5-10"/><path d="M9.5 14.6L16 20"/><path d="M5.5 12.8L2 22l5-3.4"/></svg>',
  _role_devops: '<svg viewBox="0 0 24 24"><path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z"/></svg>',
  _role_data: '<svg viewBox="0 0 24 24"><ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M21 12c0 1.66-4 3-9 3s-9-1.34-9-3"/><path d="M3 5v14c0 1.66 4 3 9 3s9-1.34 9-3V5"/></svg>',
  _role_research: '<svg viewBox="0 0 24 24"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/><line x1="11" y1="8" x2="11" y2="14"/><line x1="8" y1="11" x2="14" y2="11"/></svg>',
  _role_presenter: '<svg viewBox="0 0 24 24"><rect x="2" y="3" width="20" height="14" rx="2"/><polygon points="10 7 10 13 15 10"/><line x1="12" y1="17" x2="12" y2="21"/></svg>',
  _role_writer: '<svg viewBox="0 0 24 24"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="8" y1="13" x2="16" y2="13"/><line x1="8" y1="17" x2="12" y2="17"/></svg>',
  _role_brand: '<svg viewBox="0 0 24 24"><polygon points="12 2 15 8.5 22 9.3 17 14 18.2 21 12 17.8 5.8 21 7 14 2 9.3 9 8.5"/></svg>',
  _role_validation: '<svg viewBox="0 0 24 24"><path d="M9 5H7a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V7a2 2 0 0 0-2-2h-2"/><rect x="9" y="3" width="6" height="4" rx="1"/><path d="M9 14l2 2 4-4"/></svg>',
};

class OfficeApp {
  constructor() {
    this.state = {
      members: [],
      standupActive: false,
      standupTranscript: [],
      recentMessages: [],
      meetings: [],
      fileLocks: [],
      teamSpawned: false,
      whiteboardNotes: [],
    };
    this.bugTasks = [];
    this.taskQueues = [];
    this.csrfToken = null;
    this.eventSource = null;
    this.activeTab = 'agents';
    this.selectedMemberId = null;
    this._readMessages = new Set();
    this._autonomyLevels = new Map();
    this.sseReconnectDelay = 1000;
    this.sseMaxReconnectDelay = 16000;
    this.sseReconnecting = false;
    this.autoStandupConfig = { enabled: false, intervalMs: 0, nextAt: null, teamSpawnedAt: 0 };
    this.statusBarInterval = null;
    this.activityLog = [];
    this.maxActivityEntries = 200;
    this.officeSettings = null;
    this.sseConnected = false;
    this.healthCheckInterval = null;
    this.lastHealthCheck = 0;
    this.tickerMessages = [];
    this.lastCompletedStandup = null;
    this.lastRenderedStandupCount = 0;
    this.modalAbortController = null;
    this.detailSessionId = null;
    this._agentFilter = 'all';
    this._cmdPaletteActive = -1;
    this._notifications = [];
    this._notifUnread = 0;
    this._searchDebounceTimer = null;
    this._notifDropdownOpen = false;
    this._renderTimers = {};
    this._detailCache = new Map();
    this._detailCacheTimeout = 5000;
    this._lastActivityRenderCount = 0;
    this._viewMode = localStorage.getItem('office-view-mode') || 'grid';
    this._netNodes = null;
    this._networkSelectedNode = null;
    this._networkEdgePair = null;
    // AbortController used to cancel stale Insights-tab fetches when the
    // operator toggles tabs rapidly. Initialized null; renderInsights
    // aborts-then-recreates before each render cycle.
    this._insightsAbortController = null;
    // Same pattern for Tasks tab (3 parallel fetches), Meetings list,
    // and individual meeting open (last-click-wins when user clicks
    // meetings quickly). Each surface gets its own controller so
    // aborting one doesn't stomp the others.
    this._tasksAbortController = null;
    this._meetingsAbortController = null;
    this._openMeetingAbortController = null;
    // Round 7 filter state -- in-memory, survives re-renders within a
    // session. Tasks filter supports a text query plus multi-value
    // priority / status chips (OR within category, AND across).
    this._taskFilter = { q: '', priority: new Set(), status: new Set() };
    this._activityFilter = 'all';
    // Full meetings list, kept so _filterMeetings can re-filter without refetching.
    this._meetingsAll = [];
    // Round 9 state: session-local throughput counter, persisted blocker snapshot,
    // last-seen standup timestamp (mirrored from SSE + settings refresh so the
    // overview widget can show "Last standup" without an extra fetch).
    this._sessionTasksCompleted = 0;
    this._completedTaskIds = new Set();
    this._lastStandupAt = 0;
    // Round 10 state
    this._messageFilter = { q: '', unreadOnly: false };
    this._messageFilterDebounce = null;
    // Pinned agents — stored as a Set of configId for role-stable pinning
    // (survives respawn of the same role). Loaded synchronously from
    // localStorage so first render already honors pins.
    this._pinnedAgents = this._loadPinnedAgents();
    // Which participants have spoken in the current standup — keyed by
    // speakerName (matches what the backend puts in transcript entries)
    // so we don't need to resolve names per render.
    this._standupSpoken = new Set();
    // Install keyboard shortcut bindings (? for help, g+<key> chord for tab nav).
    this._installShortcutBindings();
  }

  getAgentIcon(member) {
    if (member.configId && AGENT_ICONS[member.configId]) {
      return AGENT_ICONS[member.configId];
    }
    const role = (member.role || '').toLowerCase();
    if (role.includes('manager') || role.includes('lead')) return AGENT_ICONS._role_leader;
    if (role.includes('qa') || role.includes('test') || role.includes('evaluator')) return AGENT_ICONS._role_qa;
    if (role.includes('frontend') || role.includes('ui')) return AGENT_ICONS._role_frontend;
    if (role.includes('backend') || role.includes('api')) return AGENT_ICONS._role_backend;
    if (role.includes('design') || role.includes('ux')) return AGENT_ICONS._role_design;
    if (role.includes('devops') || role.includes('ops') || role.includes('infra')) return AGENT_ICONS._role_devops;
    if (role.includes('data') || role.includes('ml') || role.includes('ai')) return AGENT_ICONS._role_data;
    if (role.includes('research') || role.includes('analyst')) return AGENT_ICONS._role_research;
    if (role.includes('present') || role.includes('storytell') || role.includes('pitch')) return AGENT_ICONS._role_presenter;
    if (role.includes('writer') || role.includes('document')) return AGENT_ICONS._role_writer;
    if (role.includes('brand') || role.includes('strateg')) return AGENT_ICONS._role_brand;
    if (role.includes('valid') || role.includes('verif')) return AGENT_ICONS._role_validation;
    return null;
  }

  // ========== Render Debouncer ==========

  /**
   * Schedule a debounced render call.
   * Multiple calls with the same key within delayMs are coalesced into one.
   */
  _scheduleRender(renderFn, key, delayMs) {
    if (this._renderTimers[key]) clearTimeout(this._renderTimers[key]);
    this._renderTimers[key] = setTimeout(() => {
      delete this._renderTimers[key];
      renderFn();
    }, delayMs || 50);
  }

  // ========== Initialization ==========

  async init() {
    this.restoreActivityLog();
    this.restoreLastStandup();
    await this.fetchCsrfToken();
    await this.fetchInitialState();
    this.connectSSE();
    this.render();
    this.updateMessageSelects();
    this.startStatusBarUpdates();
    this.fetchAutoStandupConfig();
    this.fetchOfficeSettings();
    this.startHealthCheck();
    this.fetchWhiteboardNotes();
    this.fetchAndRenderTasks();
    this.setupKeyboardShortcuts();
    this._installContextMenus();
    // Keep the header budget pill and Agents-tab overview widget in sync with
    // whatever state the initial fetch populated.
    this._updateBudgetPill();
    this._scheduleOverviewRefresh();
    // R15 · Team dev instances card: initial load; subsequent updates come
    // over the office:devInstance* SSE events.
    this.refreshDevInstances();

    // Restore view mode preference (list or compact; grid is default)
    if (this._viewMode === 'list' || this._viewMode === 'compact') {
      this.setViewMode(this._viewMode);
    }

    // Refresh CSRF token periodically
    this.csrfInterval = setInterval(() => this.fetchCsrfToken(), 45 * 60 * 1000);

    // Periodic state sync -- safety net to self-correct any missed SSE updates
    this._stateRefreshInterval = setInterval(() => {
      if (this.state.teamSpawned && this.state.members && this.state.members.length > 0) {
        this.fetchInitialState().then(() => { this.renderAgentCards(); this.renderMessages(); });
      }
    }, 30000);

    // Clean up all resources on page unload
    window.addEventListener('beforeunload', () => {
      if (this.eventSource) this.eventSource.close();
      if (this.statusBarInterval) clearInterval(this.statusBarInterval);
      if (this.uptimeInterval) clearInterval(this.uptimeInterval);
      if (this.healthCheckInterval) clearInterval(this.healthCheckInterval);
      if (this.csrfInterval) clearInterval(this.csrfInterval);
      if (this._stateRefreshInterval) clearInterval(this._stateRefreshInterval);
      if (this._sseReconnectTimer) clearTimeout(this._sseReconnectTimer);
      if (this._searchDebounceTimer) clearTimeout(this._searchDebounceTimer);
    });

    // Handle Enter key in message input
    const messageInput = document.getElementById('messageInput');
    if (messageInput) {
      messageInput.addEventListener('keydown', (e) => {
        if (e.key === 'Enter') this.sendMessage();
      });
    }

    // Handle Enter key in modal prompt input (Send on Enter, newline on Shift+Enter)
    const promptInput = document.getElementById('modalPromptInput');
    if (promptInput) {
      promptInput.addEventListener('keydown', (e) => {
        if (e.key === 'Enter' && !e.shiftKey) {
          e.preventDefault();
          this.sendPromptToMember();
        }
      });
    }

    // Handle Enter key in standup input
    const standupInput = document.getElementById('standupInputField');
    if (standupInput) {
      standupInput.addEventListener('keydown', (e) => {
        if (e.key === 'Enter') this.sendStandupMessage();
      });
    }

    // Toggle Telegram options visibility when checkbox changes
    const tgCheckbox = document.getElementById('spawnTelegramEnabled');
    if (tgCheckbox) {
      tgCheckbox.addEventListener('change', (e) => {
        const opts = document.getElementById('spawnTelegramOptions');
        if (opts) opts.classList.toggle('hidden', !e.target.checked);
      });
    }

    // Wire up agent filter pills
    const filterContainer = document.getElementById('agentFilters');
    if (filterContainer) {
      filterContainer.addEventListener('click', (e) => {
        const pill = e.target.closest('.filter-pill');
        if (!pill) return;
        filterContainer.querySelectorAll('.filter-pill').forEach(p => p.classList.remove('active'));
        pill.classList.add('active');
        this._agentFilter = pill.dataset.filter || 'all';
        this.renderAgentCards();
      });
    }

    // Wire up command palette input
    const cmdInput = document.getElementById('cmdInput');
    if (cmdInput) {
      cmdInput.addEventListener('input', () => {
        this._cmdPaletteActive = -1;
        this.renderCommandResults(cmdInput.value);
      });
      cmdInput.addEventListener('keydown', (e) => {
        this.handleCommandPaletteKey(e);
      });
    }

    // Wire up global search
    const globalSearch = document.getElementById('globalSearch');
    if (globalSearch) {
      globalSearch.addEventListener('input', (e) => {
        clearTimeout(this._searchDebounceTimer);
        this._searchDebounceTimer = setTimeout(() => {
          this.handleSearch(e.target.value);
        }, 300);
      });
      globalSearch.addEventListener('blur', () => {
        setTimeout(() => this.hideSearchResults(), 200);
      });
      globalSearch.addEventListener('focus', (e) => {
        if (e.target.value.length >= 2) this.handleSearch(e.target.value);
      });
    }

    // Close notification dropdown when clicking outside
    document.addEventListener('click', (e) => {
      if (!e.target.closest('.notification-center')) {
        this.closeNotifications();
      }
    });
  }

  // ========== CSRF ==========

  async fetchCsrfToken() {
    try {
      const res = await fetch('/api/csrf-token');
      const data = await res.json();
      this.csrfToken = data.token;
    } catch (err) {
      console.error('Failed to fetch CSRF token:', err);
    }
  }

  async postJson(url, body) {
    const res = await fetch(url, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'X-CSRF-Token': this.csrfToken || '',
      },
      body: JSON.stringify(body),
    });

    if (res.status === 403) {
      await this.fetchCsrfToken();
      const retry = await fetch(url, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': this.csrfToken || '',
        },
        body: JSON.stringify(body),
      });
      return this.parseJsonResponse(retry);
    }

    return this.parseJsonResponse(res);
  }

  async putJson(url, body) {
    const res = await fetch(url, {
      method: 'PUT',
      headers: {
        'Content-Type': 'application/json',
        'X-CSRF-Token': this.csrfToken || '',
      },
      body: JSON.stringify(body),
    });

    if (res.status === 403) {
      await this.fetchCsrfToken();
      const retry = await fetch(url, {
        method: 'PUT',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': this.csrfToken || '',
        },
        body: JSON.stringify(body),
      });
      return this.parseJsonResponse(retry);
    }

    return this.parseJsonResponse(res);
  }

  async deleteJson(url) {
    const res = await fetch(url, {
      method: 'DELETE',
      headers: { 'X-CSRF-Token': this.csrfToken || '' },
    });
    if (res.status === 403) {
      await this.fetchCsrfToken();
      const retry = await fetch(url, {
        method: 'DELETE',
        headers: { 'X-CSRF-Token': this.csrfToken || '' },
      });
      return this.parseJsonResponse(retry);
    }
    return this.parseJsonResponse(res);
  }

  safeParseSSE(e) {
    try {
      return JSON.parse(e.data);
    } catch (err) {
      console.error('Failed to parse SSE event:', err);
      return null;
    }
  }

  async parseJsonResponse(res) {
    if (!res.ok) {
      const text = await res.text().catch(() => '');
      throw new Error(`HTTP ${res.status}: ${text.slice(0, 200)}`);
    }
    return res.json();
  }

  // ========== Data Fetching ==========

  async fetchInitialState() {
    try {
      const res = await fetch('/api/office/status');
      const data = await res.json();
      if (data.success !== false) {
        // Preserve standup transcript and messages across state refreshes
        const prevTranscript = this.state.standupTranscript;
        const prevMessages = this.state.recentMessages;
        this.state = data;
        // Restore transcript if server sent empty but we had a local copy
        if ((!data.standupTranscript || data.standupTranscript.length === 0) && prevTranscript && prevTranscript.length > 0) {
          this.state.standupTranscript = prevTranscript;
        }
        // Merge messages: keep local messages not in server response
        if (prevMessages && prevMessages.length > 0) {
          const serverMsgIds = new Set((data.recentMessages || []).map(m => m.id));
          const merged = [...(data.recentMessages || [])];
          for (const m of prevMessages) {
            if (m.id && !serverMsgIds.has(m.id)) {
              merged.push(m);
            }
          }
          this.state.recentMessages = merged.slice(-50);
        }
        if (data.bugTasks) this.bugTasks = data.bugTasks;
        if (data.taskQueues) this.taskQueues = data.taskQueues;
        this.updateWrapUpButton(data.wrapUp && data.wrapUp.active);
      }
    } catch (err) {
      console.error('Failed to fetch office state:', err);
    }
  }

  // ========== SSE ==========

  connectSSE() {
    if (this._sseReconnectTimer) {
      clearTimeout(this._sseReconnectTimer);
      this._sseReconnectTimer = null;
    }
    if (this.eventSource) {
      this.eventSource.close();
      this.eventSource = null;
    }

    const subscriptions = [
      'office:*',
      'office:bugTaskCreated',
      'office:bugTaskUpdated',
      'office:bugTaskResolved',
      'office:taskQueueGenerated',
      'office:taskQueueUpdated',
      'office:meetingStarted',
      'office:meetingEnded',
      'office:fileLockChanged',
      'office:autoStandupScheduled',
      'session:created',
      'session:deleted',
      'session:updated',
      'session:idle',
      'session:working',
      'init',
    ].join(',');

    this.eventSource = new EventSource(`/api/events?subscribe=${subscriptions}`);

    this.eventSource.addEventListener('office:memberUpdated', (e) => {
      const data = this.safeParseSSE(e);
      if (data) this.handleMemberUpdated(data);
    });

    this.eventSource.addEventListener('office:memberJoined', (e) => {
      const data = this.safeParseSSE(e);
      if (data) this.handleMemberJoined(data);
    });

    this.eventSource.addEventListener('office:memberLeft', (e) => {
      const data = this.safeParseSSE(e);
      if (data) {
        this.handleMemberLeft(data);
        this.handleShutdownMemberLeft(data.sessionId);
      }
    });

    this.eventSource.addEventListener('office:message', (e) => {
      const data = this.safeParseSSE(e);
      if (data) this.handleMessage(data);
      this._refreshTimelineIfActive();
    });

    this.eventSource.addEventListener('office:wrapUpInitiated', () => {
      this.updateWrapUpButton(true);
    });

    this.eventSource.addEventListener('office:wrapUpCancelled', () => {
      this.updateWrapUpButton(false);
    });

    this.eventSource.addEventListener('office:standupStarted', (e) => {
      const data = this.safeParseSSE(e);
      if (data) this.handleStandupStarted(data);
    });

    this.eventSource.addEventListener('office:standupMessage', (e) => {
      const data = this.safeParseSSE(e);
      if (data) this.handleStandupMessage(data);
    });

    this.eventSource.addEventListener('office:standupSpeaker', (e) => {
      const data = this.safeParseSSE(e);
      if (data) this.handleStandupSpeaker(data);
    });

    this.eventSource.addEventListener('office:standupEnded', (e) => {
      const data = this.safeParseSSE(e);
      if (data) this.handleStandupEnded(data);
    });

    this.eventSource.addEventListener('session:updated', (e) => {
      const data = this.safeParseSSE(e);
      if (data) this.handleSessionUpdated(data);
    });

    this.eventSource.addEventListener('session:idle', (e) => {
      const data = this.safeParseSSE(e);
      if (data) {
        this.updateMemberStatusBySessionId(data.id, 'idle');
        this.handleShutdownIdle(data.id);
      }
    });

    this.eventSource.addEventListener('session:working', (e) => {
      const data = this.safeParseSSE(e);
      if (data) this.updateMemberStatusBySessionId(data.id, 'working');
    });

    this.eventSource.addEventListener('office:bugTaskCreated', (e) => {
      const data = this.safeParseSSE(e);
      if (data) this.handleBugTaskCreated(data);
    });

    this.eventSource.addEventListener('office:bugTaskUpdated', (e) => {
      const data = this.safeParseSSE(e);
      if (data) this.handleBugTaskUpdated(data);
    });

    this.eventSource.addEventListener('office:bugTaskResolved', (e) => {
      const data = this.safeParseSSE(e);
      if (data) this.handleBugTaskResolved(data);
    });

    this.eventSource.addEventListener('office:taskQueueGenerated', (e) => {
      const data = this.safeParseSSE(e);
      if (data) this.handleTaskQueueGenerated(data);
    });

    this.eventSource.addEventListener('office:taskQueueUpdated', (e) => {
      const data = this.safeParseSSE(e);
      if (data) this.handleTaskQueueUpdated(data);
    });

    this.eventSource.addEventListener('office:meetingStarted', (e) => {
      const data = this.safeParseSSE(e);
      if (data) this.handleMeetingStarted(data);
    });

    this.eventSource.addEventListener('office:meetingEnded', (e) => {
      const data = this.safeParseSSE(e);
      if (data) this.handleMeetingEnded(data);
    });

    this.eventSource.addEventListener('office:fileLockChanged', (e) => {
      const data = this.safeParseSSE(e);
      if (data) this.handleFileLockChanged(data);
    });

    this.eventSource.addEventListener('office:agentStuck', (e) => {
      const alert = this.safeParseSSE(e);
      if (alert) {
        this.showNotification(`Agent ${alert.agentName} is stuck and unresponsive after ${alert.attempts} recovery attempts`, 'error');
        this.addActivityEntry('system', `${alert.agentName} stuck - recovery exhausted`);
        this.addNotification('error', `${alert.agentName} is stuck`, `After ${alert.attempts} recovery attempts`);
      }
    });

    this.eventSource.addEventListener('office:autoStandupScheduled', (e) => {
      const data = this.safeParseSSE(e);
      if (data) {
        this.autoStandupConfig = data;
        this.updateStatusBar();
      }
    });

    this.eventSource.addEventListener('office:missionUpdated', (e) => {
      const data = this.safeParseSSE(e);
      if (!data) return;
      if (this.officeSettings) this.officeSettings.teamMission = data.mission;
      const missionInput = document.getElementById('settingTeamMission');
      if (missionInput) missionInput.value = data.mission || '';
      this.updateMissionBanner(data.mission);
      if (data.mission) {
        this.addActivityEntry('system', 'Team mission updated: ' + (data.mission || '').slice(0, 60));
      }
      this.showSettingsSaved();
    });

    this.eventSource.addEventListener('office:personalityUpdated', (e) => {
      const data = this.safeParseSSE(e);
      if (!data) return;
      if (this.selectedMemberId === data.sessionId) {
        const personalityInput = document.getElementById('modalPersonalityInput');
        const systemPromptInput = document.getElementById('modalSystemPromptInput');
        if (personalityInput) personalityInput.value = data.personality || '';
        if (systemPromptInput) systemPromptInput.value = data.systemPrompt || '';
      }
      this.addActivityEntry('system', `Personality updated for ${data.configId}`);
    });

    this.eventSource.addEventListener('office:whiteboardUpdate', (e) => {
      const data = this.safeParseSSE(e);
      if (data) this.handleWhiteboardUpdate(data);
    });

    this.eventSource.addEventListener('office:waterCoolerChat', (e) => {
      const data = this.safeParseSSE(e);
      if (data) {
        const agents = [data.agentA, data.agentB].filter(Boolean).join(' & ') || 'agents';
        this.addActivityEntry('message', `Water cooler: ${agents} chatting`);
      }
    });

    this.eventSource.addEventListener('office:tickerMessage', (e) => {
      const data = this.safeParseSSE(e);
      if (data && data.message) this.addActivityEntry('system', data.message);
    });

    this.eventSource.addEventListener('office:agentRestarted', (e) => {
      const data = this.safeParseSSE(e);
      if (data) {
        this.addActivityEntry('system', `${data.agentName} auto-restarted`);
        this.showNotification(`Agent ${data.agentName} has been auto-restarted`, 'info');
      }
    });

    // Provider-side turn errors (rate-limit, 4xx/5xx, gateway "Network
    // error"). Surface in both the activity feed and as a dismissible
    // toast so the operator can see the failure and retry.
    this.eventSource.addEventListener('office:agentTurnError', (e) => {
      const data = this.safeParseSSE(e);
      if (!data) return;
      const who = data.agentName || data.sessionId || 'Agent';
      const reason = (data.subtype || 'error').toString();
      this.addActivityEntry('system', `${who} turn failed (${reason})`);
      this.showNotification(`${who} hit a provider error -- may need a retry`, 'error');
    });

    // R18 · Operator stopped one agent. Flash the card then let the
    // normal memberLeft broadcast remove it; we just surface the reason
    // in the activity feed + a toast.
    this.eventSource.addEventListener('office:agentKilled', (e) => {
      const data = this.safeParseSSE(e);
      if (!data) return;
      const who = data.agentName || data.sessionId || 'Agent';
      const reason = (data.reason || 'operator').toString();
      this.addActivityEntry('system', `${who} stopped (${reason})`);
      this.showNotification(`Stopped ${who}`, 'info');
      const card = document.querySelector(`.agent-card[data-session-id="${data.sessionId}"]`);
      if (card) {
        card.classList.add('agent-card--flash-kill');
        setTimeout(() => card.classList.remove('agent-card--flash-kill'), 2000);
      }
    });

    // R18 · An agent hit its per-session turn cap and the SDK stopped
    // it. Mark the cap on the member so the badge flips red on the next
    // render, and surface a loud toast.
    this.eventSource.addEventListener('office:agentTurnCapReached', (e) => {
      const data = this.safeParseSSE(e);
      if (!data) return;
      const who = data.agentName || data.sessionId || 'Agent';
      const turns = data.turns || 0;
      const cost = typeof data.cost === 'number' ? data.cost.toFixed(2) : '?';
      this.addActivityEntry('system', `${who} hit turn cap at ${turns} turns ($${cost})`);
      this.showNotification(`${who} hit turn cap (${turns} turns, $${cost})`, 'error');
      const m = this.state.members.find(x => x.sessionId === data.sessionId);
      if (m) {
        m.usage = m.usage || {};
        m.usage.turnCapReached = true;
        m.usage.turnCount = turns;
        this._scheduleRender(() => this.renderAgentCards(), 'agents', 80);
      }
    });

    // R24 · R18's office:dailyCapHit listener was removed along with
    // the rest of the dollar-cap machinery. Runaway defense still
    // lives in the turn-cap + wedged-stop events below.

    // R19 · An SDK session was wedged in "working" state past the
    // configured timeout (default 20 min) with no tool activity. The
    // manager stopped it. Toast so operators know a silent hang was
    // cleaned up — memberLeft will remove the card on its own.
    this.eventSource.addEventListener('office:agentIdleStopped', (e) => {
      const data = this.safeParseSSE(e);
      if (!data) return;
      const who = data.agentName || data.sessionId || 'Agent';
      const idleMin = data.idleMs ? Math.round(data.idleMs / 60000) : '?';
      this.addActivityEntry('system', `${who} stopped: wedged ${idleMin}m without activity`);
      this.showNotification(`${who} stopped — wedged ${idleMin} min without activity`, 'error');
    });

    // R21 · Tool-level loop advisory — higher-fidelity signal than
    // the text-level one. Fires when the same (tool, resultHash)
    // tuple repeats 3+ times inside one turn.
    this.eventSource.addEventListener('office:agentToolLoop', (e) => {
      const data = this.safeParseSSE(e);
      if (!data) return;
      const who = data.agentName || data.sessionId || 'Agent';
      const tool = data.tool || 'tool';
      this.addActivityEntry('system', `${who} looping on ${tool}`);
      this.showNotification(`${who} is looping on ${tool} — check the last few turns`, 'error');
    });

    // R20 · Output-repeat advisory — agent emitted the same chunk
    // 3+ times inside the last 5 output events. Advisory only;
    // operator can kill/inject via R18 buttons if it doesn't clear.
    this.eventSource.addEventListener('office:agentPossibleLoop', (e) => {
      const data = this.safeParseSSE(e);
      if (!data) return;
      const who = data.agentName || data.sessionId || 'Agent';
      this.addActivityEntry('system', `${who} may be looping on repeated output`);
      this.showNotification(`${who} output-loop advisory — check agent`, 'warning');
    });

    // R20 · A corrupt state file was quarantined on boot (or a
    // write error surfaced mid-run). Toast so operator investigates
    // immediately instead of finding out days later.
    this.eventSource.addEventListener('office:stateCorruption', (e) => {
      const data = this.safeParseSSE(e);
      if (!data) return;
      const file = (data.file || 'unknown').toString().split('/').pop();
      const kind = data.kind === 'parse' ? 'parse' : 'read';
      this.addActivityEntry('system', `State corruption in ${file} (${kind})`);
      this.showNotification(`Corrupt state file ${file} quarantined — check logs`, 'error');
    });

    // R20 · A meeting still marked Active past the 2h TTL was
    // force-ended. Activity-feed only; no toast because it's
    // cleanup, not an incident.
    this.eventSource.addEventListener('office:meetingForceEnded', (e) => {
      const data = this.safeParseSSE(e);
      if (!data) return;
      const mins = data.ageMs ? Math.round(data.ageMs / 60000) : '?';
      this.addActivityEntry('system', `Force-ended stale ${data.type || 'meeting'} (${mins}m old)`);
    });

    // R20 · A bug task's assignee session is gone; assignment was
    // cleared so the dashboard stops showing a dead reference.
    this.eventSource.addEventListener('office:bugTaskOrphaned', (e) => {
      const data = this.safeParseSSE(e);
      if (!data) return;
      this.addActivityEntry('system', `Bug ${data.bugId || '?'} assignee offline — cleared`);
    });

    // R19 · Meeting facilitator died mid-flow — participants were
    // unblocked with a sentinel. Operator can restart the meeting
    // manually if desired (no auto-retry by design).
    this.eventSource.addEventListener('office:meetingAborted', (e) => {
      const data = this.safeParseSSE(e);
      if (!data) return;
      const kind = (data.type || 'meeting').toString();
      const who = data.facilitator || 'facilitator';
      this.addActivityEntry('system', `${kind} aborted — ${who} died mid-flow`);
      this.showNotification(`${kind.charAt(0).toUpperCase() + kind.slice(1)} aborted — ${who} died`, 'error');
    });

    // R19 · A long-running office goroutine panicked and safeRun
    // caught it. Keeps the server running; operator should investigate
    // via logs since the subsystem usually won't restart itself.
    this.eventSource.addEventListener('office:panicRecovered', (e) => {
      const data = this.safeParseSSE(e);
      if (!data) return;
      const where = data.where || 'unknown';
      this.addActivityEntry('system', `Goroutine panic recovered: ${where}`);
      this.showNotification(`Background task crashed: ${where} — check logs`, 'error');
    });

    // R15 · Team dev instances — re-pull the full slot map on any change so
    // the dashboard card stays in sync with adds/clears/health flips.
    this.eventSource.addEventListener('office:devInstanceChanged', () => { this.refreshDevInstances(); });
    this.eventSource.addEventListener('office:devInstanceCleared', () => { this.refreshDevInstances(); });
    this.eventSource.addEventListener('office:devInstanceForced', () => { this.refreshDevInstances(); });
    this.eventSource.addEventListener('office:devCredentialsChanged', () => { this.refreshDevInstances(); });
    // R15 · UI task completed without visual verification — surface a
    // low-urgency toast so the operator notices discipline slippage.
    this.eventSource.addEventListener('office:uiTaskMissingScreenshot', (e) => {
      const data = this.safeParseSSE(e);
      if (!data) return;
      const title = (data.title || 'UI task').toString().slice(0, 60);
      this.addActivityEntry('system', `UI task completed without visual verification: ${title}`);
    });

    // R16 · New project folder was created + seeded by the spawn-team flow.
    // Activity-feed only — no toast, since the team-spawn toast already
    // fires a couple seconds later.
    this.eventSource.addEventListener('office:projectCreated', (e) => {
      const data = this.safeParseSSE(e);
      if (!data) return;
      const name = (data.name || 'project').toString();
      const path = (data.path || '').toString();
      this.addActivityEntry('system', `Created new project ${name}${path ? ` at ${path}` : ''} (seeded with CLAUDE.md)`);
    });

    this.eventSource.onopen = () => {
      const wasReconnecting = this.sseReconnecting;
      this.sseReconnectDelay = 1000;
      this.sseReconnecting = false;
      this.sseConnected = true;
      this.updateConnectionStatus('connected');
      this.addActivityEntry('system', 'SSE connection established');

      if (wasReconnecting) {
        this.fetchInitialState().then(() => {
          this.render();
          this.fetchAutoStandupConfig();
          this.fetchOfficeSettings();
          this.addActivityEntry('system', 'State refreshed after reconnect');
        });
      }
    };

    this.eventSource.onerror = () => {
      this.sseConnected = false;
      if (this.sseReconnecting) return;
      this.sseReconnecting = true;
      this.updateConnectionStatus('reconnecting');
      this.addActivityEntry('system', 'SSE connection lost, reconnecting...');
      console.warn(`SSE connection lost, reconnecting in ${this.sseReconnectDelay}ms...`);
      this.eventSource.close();
      if (this._sseReconnectTimer) clearTimeout(this._sseReconnectTimer);
      this.sseReconnectDelay = Math.min(this.sseReconnectDelay * 2, this.sseMaxReconnectDelay);
      this._sseReconnectTimer = setTimeout(() => this.connectSSE(), this.sseReconnectDelay);
    };
  }

  // ========== SSE Handlers ==========

  handleMemberUpdated(member) {
    const idx = this.state.members.findIndex(m => m.sessionId === member.sessionId);
    if (idx >= 0) {
      this.state.members[idx] = { ...this.state.members[idx], ...member };
    }
    // Use the MERGED member (with all fields) for rendering, not the partial SSE payload
    const fullMember = idx >= 0 ? this.state.members[idx] : member;
    // Targeted card update -- only patch the one card that changed
    const card = document.querySelector(`.agent-card[data-session-id="${member.sessionId}"]`);
    if (card) {
      this.updateAgentCard(card, fullMember);
    } else {
      // New member or card missing -- schedule a debounced full render
      this._scheduleRender(() => this.renderAgentCards(), 'agents', 80);
    }
    // Refresh detail panel if open for this member
    if (this.detailSessionId === member.sessionId) {
      this.renderDetailPanel(member.sessionId);
    }
    // Round 9 · member-updated often carries a new totalCost — refresh
    // the header budget pill + Agents-tab overview widget so the at-a-
    // glance numbers stay in sync with the card.
    this._updateBudgetPill();
    this._scheduleOverviewRefresh();
  }

  handleMemberJoined(member) {
    if (!this.state.members.find(m => m.sessionId === member.sessionId)) {
      this.state.members.push(member);
    }
    this.state.teamSpawned = true;
    this.render();
    this.addActivityEntry('system', `${member.name} joined the office`);
    this._scheduleOverviewRefresh();
  }

  handleMemberLeft(data) {
    this.state.members = this.state.members.filter(m => m.sessionId !== data.sessionId);
    if (this.detailSessionId === data.sessionId) {
      this.closeDetail();
    }
    this.renderAgentCards();
    if (this.state.members.length === 0) {
      this.state.teamSpawned = false;
      this.render();
    }
    this._scheduleOverviewRefresh();
  }

  handleMessage(message) {
    // Assign a synthetic ID if not present
    if (!message.id) {
      message.id = `${message.timestamp || Date.now()}-${message.fromSessionId}-${message.toSessionId}`;
    }
    this.state.recentMessages.push(message);
    if (this.state.recentMessages.length > 50) {
      this.state.recentMessages = this.state.recentMessages.slice(-50);
    }
    // Debounce full message list rebuild since threads need re-grouping
    this._scheduleRender(() => this.renderMessages(), 'messages', 100);
    // Update only the unread badge on the specific recipient card, not a full re-render
    if (this.activeTab !== 'messages' && message.toSessionId) {
      this._updateUnreadBadgeOnCard(message.toSessionId);
    }
    this.addActivityEntry('message', `${message.fromName} -> ${message.toName}: ${(message.content || '').slice(0, 60)}`);
    // Live-pulse the Agent Network edge if that tab is open.
    if (this.activeTab === 'network') {
      this._pulseEdgeForMessage(message);
      this._scheduleRender(() => this.renderAgentNetwork(), 'network', 500);
      // If the conversation drawer is open and this message belongs to it,
      // append the new message row in place -- DON'T re-render the whole
      // drawer. Re-rendering blows away the composer input and steals focus
      // so the operator can't fire off two messages in a row. Fall back to
      // the full re-render only if the targeted row insertion fails.
      if (this._networkEdgePair &&
          this._isPairMatch(this._networkEdgePair, message.fromSessionId, message.toSessionId)) {
        if (!this._appendConversationRow(message)) {
          this._openNetworkEdgeDrawer(this._networkEdgePair[0], this._networkEdgePair[1]);
        }
      }
    }
  }

  /**
   * Insert a single new message row into the currently-open conversation
   * thread without re-rendering the drawer. Returns true on success, false
   * if the DOM isn't in the expected shape (caller should fall back to a
   * full render).
   */
  _appendConversationRow(message) {
    const thread = document.querySelector('#networkDrawerContent .conversation-thread');
    if (!thread || !this._networkEdgePair) return false;
    const fromId = this._networkEdgePair[0];
    const cls = message.fromSessionId === fromId ? '' : 'reverse';
    const ts = typeof message.timestamp === 'number'
      ? message.timestamp
      : new Date(message.timestamp).getTime();
    const row = document.createElement('div');
    row.className = `conversation-row ${cls}`.trim();
    row.setAttribute('onclick', `office.showDetail('${this._escapeAttr(message.fromSessionId)}')`);
    row.innerHTML = `
      <div class="conversation-row-meta"><span>${this._escapeHtml(message.fromName || message.fromSessionId)} → ${this._escapeHtml(message.toName || message.toSessionId)}</span><span>${this._escapeHtml(this.formatTimeAgo(ts))}</span></div>
      <div class="conversation-row-body">${this._escapeHtml((message.content || '').slice(0, 600))}</div>
    `;
    // Newest-first ordering matches _openNetworkEdgeDrawer's render.
    thread.insertBefore(row, thread.firstChild);
    return true;
  }

  /** Patch only the unread badge element on a single agent card without re-rendering it. */
  _updateUnreadBadgeOnCard(sessionId) {
    const card = document.querySelector(`.agent-card[data-session-id="${sessionId}"]`);
    if (!card) return;
    const unreadCount = (this.state.recentMessages || []).filter(
      m => m.toSessionId === sessionId && m.id && !this._readMessages.has(m.id)
    ).length;
    let badge = card.querySelector('.unread-badge');
    if (unreadCount > 0) {
      if (!badge) {
        badge = document.createElement('span');
        badge.className = 'unread-badge';
        card.insertBefore(badge, card.firstChild);
      }
      badge.title = `${unreadCount} unread message${unreadCount > 1 ? 's' : ''}`;
      badge.textContent = String(unreadCount);
    } else if (badge) {
      badge.remove();
    }
  }

  handleStandupStarted(standup) {
    this.state.standupActive = true;
    this.state.standupTranscript = standup.transcript || [];
    this.lastRenderedStandupCount = 0;
    this.updateStandupButtons(true);
    this.renderStandup();
    this.addActivityEntry('standup', 'Standup meeting started');
    // Round 9 · stamp for overview "Last standup" cell.
    this._lastStandupAt = Date.now();
    this._scheduleOverviewRefresh();
    this.addNotification('info', 'Standup started', 'Team standup meeting is now in progress');

    const userInput = document.getElementById('standupUserInput');
    if (userInput) userInput.classList.remove('hidden');
  }

  handleStandupMessage(message) {
    this.state.standupTranscript.push(message);
    this.renderStandup();
    const transcript = document.getElementById('standupTranscript');
    if (transcript) transcript.scrollTop = transcript.scrollHeight;
  }

  handleStandupSpeaker(data) {
    const waitEntry = {
      sessionId: data.sessionId,
      speakerName: data.speakerName,
      content: 'Waiting for response...',
      type: 'waiting',
      timestamp: Date.now(),
    };
    this.state.standupTranscript.push(waitEntry);
    this.renderStandup();
    const transcript = document.getElementById('standupTranscript');
    if (transcript) transcript.scrollTop = transcript.scrollHeight;
  }

  handleStandupEnded(standup) {
    this.state.standupActive = false;
    this.state.standupTranscript = standup.transcript || [];

    this.lastCompletedStandup = {
      transcript: standup.transcript || [],
      completedAt: standup.completedAt || Date.now(),
    };
    this.persistLastStandup();
    this.lastRenderedStandupCount = 0;

    this.updateStandupButtons(false);
    this.renderStandup();
    this.addActivityEntry('standup', 'Standup meeting ended');
    this.addNotification('success', 'Standup completed', 'Team standup finished');

    const userInput = document.getElementById('standupUserInput');
    if (userInput) userInput.classList.add('hidden');
    const inputField = document.getElementById('standupInputField');
    if (inputField) inputField.value = '';
  }

  handleSessionUpdated(data) {
    const member = this.state.members.find(m => m.sessionId === data.id);
    if (member) {
      if (data.status === 'busy') member.status = 'working';
      else if (data.status === 'idle') member.status = 'idle';
      else if (data.status === 'error') member.status = 'stuck';
      else if (data.status === 'stopped') member.status = 'offline';
      const card = document.querySelector(`.agent-card[data-session-id="${data.id}"]`);
      if (card) {
        this.updateAgentCard(card, member);
      } else {
        this._scheduleRender(() => this.renderAgentCards(), 'agents', 80);
      }
    }
  }

  updateMemberStatusBySessionId(sessionId, status) {
    const member = this.state.members.find(m => m.sessionId === sessionId);
    if (member) {
      member.status = status;
      const card = document.querySelector(`.agent-card[data-session-id="${sessionId}"]`);
      if (card) {
        this.updateAgentCard(card, member);
      } else {
        this._scheduleRender(() => this.renderAgentCards(), 'agents', 80);
      }
    }
  }

  // ========== Rendering ==========

  render() {
    this.renderAgentCards();
    this.updateSpawnButton();
    this.renderKanbanBoard();
    this.renderMessages();
    this.renderStandup();
    this.updateStatusBar();
  }

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
  }

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
  }

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
  }

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
  }

  // F3 · pin helpers.
  _pinKey(member) {
    // Prefer configId (survives respawn); fall back to sessionId.
    return (member && (member.configId || member.sessionId)) || '';
  }

  isPinned(member) {
    const key = this._pinKey(member);
    return !!key && this._pinnedAgents.has(key);
  }

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
  }

  _loadPinnedAgents() {
    try {
      const raw = localStorage.getItem('office_pinned_agents');
      if (!raw) return new Set();
      const arr = JSON.parse(raw);
      return new Set(Array.isArray(arr) ? arr : []);
    } catch {
      return new Set();
    }
  }

  _savePinnedAgents() {
    try {
      localStorage.setItem('office_pinned_agents', JSON.stringify([...this._pinnedAgents]));
    } catch { /* quota */ }
  }

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
  }

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
  }

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
      <div class="agent-card-badges">${autonomyBadgeHtml}${learningsBadgeHtml}</div>
      ${this._renderCardSparkline(member)}
    `;
  }

  _renderCardSparkline(member) {
    const recent = member && member.usage && Array.isArray(member.usage.recentTurnCosts)
      ? member.usage.recentTurnCosts : [];
    if (recent.length < 2) return '';
    const svg = this._renderSparkline(recent, 60, 14);
    if (!svg) return '';
    return `<div class="agent-card-spark" title="Recent turn costs">${svg}</div>`;
  }

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
  }

  // ========== Detail Panel ==========

  showDetail(sessionId) {
    this.detailSessionId = sessionId;
    const panel = document.getElementById('detailPanel');
    if (panel) panel.classList.add('open');
    this.renderDetailPanel(sessionId);
  }

  closeDetail() {
    this.detailSessionId = null;
    const panel = document.getElementById('detailPanel');
    if (panel) panel.classList.remove('open');
  }

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
  }

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
  }

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
  }

  // ========== Agent detail modal: whiteboard + sub-tabs ==========

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
  }

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
  }

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
  }

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
  }

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
  }

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
  }

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
  }

  // ========== Tab Navigation ==========

  switchTab(tab) {
    this.activeTab = tab;

    // Update nav icons
    document.querySelectorAll('.nav-icon').forEach(btn => {
      btn.classList.toggle('active', btn.dataset.tab === tab);
    });

    // Show/hide panels
    const panels = ['agents', 'tasks', 'standup', 'messages', 'metrics', 'activity', 'reports', 'settings', 'timeline', 'insights', 'meetings', 'network'];
    for (const panel of panels) {
      const el = document.getElementById(`panel-${panel}`);
      if (el) {
        el.classList.toggle('hidden', tab !== panel);
        el.classList.toggle('active', tab === panel);
      }
    }

    if (tab === 'tasks') this.fetchAndRenderTasks();
    if (tab === 'activity') this.renderActivity();
    if (tab === 'metrics') this.fetchAndRenderMetrics();
    if (tab === 'settings') this.fetchOfficeSettings();
    if (tab === 'reports') this.renderReportDashboard();
    if (tab === 'timeline') this.renderTimeline();
    if (tab === 'insights') this.renderInsights();
    if (tab === 'meetings') this.renderMeetings();
    if (tab === 'network') this.renderAgentNetwork();
  }

  // Refresh the timeline when it's the active tab. Throttled so a bursty
  // series of SSE events doesn't produce a stampede of fetches.
  _refreshTimelineIfActive() {
    const panel = document.getElementById('panel-timeline');
    if (!panel || panel.classList.contains('hidden')) return;
    if (this._timelineRefreshTimer) return;
    this._timelineRefreshTimer = setTimeout(() => {
      this._timelineRefreshTimer = null;
      this.renderTimeline();
    }, 500);
  }

  // ========== Timeline tab (unified event stream) ==========

  setTimelineKind(kind) {
    this._timelineKind = kind;
    // Reflect active chip state -- only the kind chips, not the new
    // range chips which live in a sibling bar.
    const chips = document.querySelectorAll('.timeline-filters .timeline-filter-chip[data-timeline-kind]');
    chips.forEach(c => c.classList.toggle('active', (c.getAttribute('data-timeline-kind') || '') === kind));
    this.renderTimeline();
  }

  setTimelineAgent(sessionId) {
    this._timelineAgent = sessionId || '';
    this.renderTimeline();
  }

  // F5 · quick time-range for the timeline -- backend's /api/office/timeline
  // already accepts `since=<ISO8601>`; we just translate the chip label
  // to a window and pass it through.
  setTimelineRange(range) {
    this._timelineRange = range || 'all';
    const chips = document.querySelectorAll('.timeline-range-bar .timeline-filter-chip[data-timeline-range]');
    chips.forEach(c => c.classList.toggle('active', c.getAttribute('data-timeline-range') === this._timelineRange));
    this.renderTimeline();
  }

  // Populate the agent filter dropdown from the current roster. Idempotent;
  // re-called on render so new members appear without reload.
  _populateTimelineAgentFilter() {
    const sel = document.getElementById('timelineAgentFilter');
    if (!sel) return;
    const currentValue = sel.value || '';
    const roster = (this.state && Array.isArray(this.state.members)) ? this.state.members : [];
    const opts = ['<option value="">All agents</option>'];
    for (const m of roster) {
      const id = this._escapeHtml(m.sessionId || '');
      const name = this._escapeHtml(m.name || m.sessionId || '');
      opts.push(`<option value="${id}">${name}</option>`);
    }
    sel.innerHTML = opts.join('');
    sel.value = currentValue;
  }

  async renderTimeline() {
    this._populateTimelineAgentFilter();
    const list = document.getElementById('timelineList');
    if (!list) return;
    const params = new URLSearchParams({ limit: '150' });
    if (this._timelineKind) params.set('type', this._timelineKind);
    if (this._timelineAgent) params.set('agent', this._timelineAgent);
    // F5 · time-range chip -> `since` query param. UTC ISO8601 so
    // there's no timezone mismatch between the JS client and the Go
    // server.
    const rangeMs = {
      '15m': 15 * 60 * 1000,
      '1h': 60 * 60 * 1000,
      '4h': 4 * 60 * 60 * 1000,
      '24h': 24 * 60 * 60 * 1000,
    }[this._timelineRange || 'all'];
    if (rangeMs) {
      params.set('since', new Date(Date.now() - rangeMs).toISOString());
    }
    try {
      const res = await fetch('/api/office/timeline?' + params.toString());
      const data = await res.json();
      const entries = Array.isArray(data.entries) ? data.entries : [];
      if (entries.length === 0) {
        list.innerHTML = '<div class="timeline-empty">No events yet for this filter.</div>';
        return;
      }
      // Group entries into chronological day labels (Today / Yesterday /
      // Mon Apr 14) so long scrolls have clear breakpoints.
      const today = this._dateKey(new Date());
      const yDate = new Date(); yDate.setDate(yDate.getDate() - 1);
      const yesterday = this._dateKey(yDate);
      let lastKey = null;
      const out = [];
      for (const e of entries) {
        const d = new Date(e.timestamp);
        const k = this._dateKey(d);
        if (k !== lastKey) {
          let label;
          if (k === today) label = 'Today';
          else if (k === yesterday) label = 'Yesterday';
          else label = d.toLocaleDateString(undefined, { weekday: 'short', month: 'short', day: 'numeric' });
          out.push(`<div class="timeline-day-label">${this._escapeHtml(label)}</div>`);
          lastKey = k;
        }
        out.push(this._renderTimelineEntry(e));
      }
      list.innerHTML = out.join('');
    } catch (err) {
      list.innerHTML = '<div class="timeline-empty">Failed to load timeline.</div>';
    }
  }

  _dateKey(d) {
    // YYYY-MM-DD in local time, suitable for day-boundary grouping.
    const y = d.getFullYear();
    const m = String(d.getMonth() + 1).padStart(2, '0');
    const day = String(d.getDate()).padStart(2, '0');
    return `${y}-${m}-${day}`;
  }

  _renderTimelineEntry(e) {
    const time = this._formatRelativeTime(new Date(e.timestamp));
    const title = this._escapeHtml(e.title || e.kind);
    const agent = e.agentName ? `<span class="timeline-agent">${this._escapeHtml(e.agentName)}</span> · ` : '';
    const summary = e.summary
      ? `<div class="timeline-summary">${this._escapeHtml(e.summary)}</div>`
      : '';
    return `<div class="timeline-entry" data-kind="${this._escapeHtml(e.kind)}">
      <div class="timeline-dot"></div>
      <div class="timeline-body">
        <div class="timeline-title">${title}</div>
        <div class="timeline-meta">${agent}${this._escapeHtml(e.kind)}</div>
        ${summary}
      </div>
      <div class="timeline-time">${this._escapeHtml(time)}</div>
    </div>`;
  }

  // ========== Insights tab ==========

  async renderInsights() {
    const grid = document.getElementById('insightsGrid');
    if (!grid) return;
    // Abort any previous in-flight Insights fetches so rapid tab-toggling
    // doesn't pile up overlapping network requests whose late responses
    // would stomp each other's DOM.
    if (this._insightsAbortController) {
      try { this._insightsAbortController.abort(); } catch (_) { /* already aborted */ }
    }
    this._insightsAbortController = new AbortController();
    const roster = (this.state && Array.isArray(this.state.members)) ? this.state.members : [];
    if (roster.length === 0) {
      grid.innerHTML = '<div class="insights-empty">Spawn a team to see insights.</div>';
      return;
    }
    // Sort by cost desc for the leaderboard bars.
    const byCost = [...roster].sort((a, b) =>
      ((b.usage && b.usage.totalCostUSD) || 0) - ((a.usage && a.usage.totalCostUSD) || 0));
    const maxCost = Math.max(0.0001, ...roster.map(m => (m.usage && m.usage.totalCostUSD) || 0));
    const byLearning = [...roster]
      .sort((a, b) => (b.learningCount || 0) - (a.learningCount || 0))
      .slice(0, 5);
    const byTokens = [...roster].sort((a, b) =>
      ((b.usage && (b.usage.totalInputTokens + b.usage.totalOutputTokens)) || 0) -
      ((a.usage && (a.usage.totalInputTokens + a.usage.totalOutputTokens)) || 0));
    const totalCost = roster.reduce((s, m) => s + ((m.usage && m.usage.totalCostUSD) || 0), 0);
    const totalTurns = roster.reduce((s, m) => s + ((m.usage && m.usage.turnCount) || 0), 0);

    // Max of input + output tokens across agents so bar fill reflects
    // relative share rather than being a flat 100% per row.
    const maxTokens = Math.max(1, ...roster.map(m =>
      ((m.usage && (m.usage.totalInputTokens + m.usage.totalOutputTokens)) || 0)));

    const bars = byCost.map((m, i) => this._renderCostBar(m, maxCost, i + 1)).join('');
    const tokens = byTokens.map((m, i) => this._renderTokenRow(m, maxTokens, i + 1)).join('');
    const leaderboard = byLearning.map((m, i) => `
      <div class="insight-leaderboard-row">
        <span><span class="t-rank">#${i + 1}</span>${this._escapeHtml(m.name || '')}</span>
        <span class="count">${m.learningCount || 0}</span>
      </div>`).join('');

    // Fetch task-queues once so both the narrative and the blocker
    // leaderboard can render from the same data instead of racing two
    // identical requests. AbortError is swallowed -- if the tab was
    // switched mid-fetch, the narrative simply falls back to the
    // no-blocker path.
    let queuesData = null;
    try {
      const signal = this._insightsAbortController ? this._insightsAbortController.signal : undefined;
      const res = await fetch('/api/office/task-queues', { signal });
      queuesData = res.ok ? await res.json() : { queues: [] };
    } catch (err) {
      if (err && err.name !== 'AbortError') queuesData = { queues: [] };
    }

    // Narrative summary: 2-3 sentences derived from roster + blocker
    // data, so the top of Insights tells a story instead of just
    // showing tables.
    const narrative = this._buildInsightsNarrative(roster, byCost, byLearning, totalCost, queuesData);

    grid.innerHTML = `
      ${narrative}
      <div class="insight-card insight-card--kpi">
        <div class="insight-card-title">Team spend today</div>
        <div class="insight-big-number">$${totalCost.toFixed(4)}</div>
        <div style="font-size:11px;color:#71717a">${totalTurns} turns across ${roster.length} agents</div>
        <div class="insight-budget-bar" id="insightBudgetBar" data-tier="ok">
          <div class="insight-budget-bar-track"><div class="insight-budget-bar-fill"></div></div>
          <div class="insight-budget-bar-label t-meta"></div>
        </div>
      </div>
      <div class="insight-card">
        <div class="insight-card-title">Cost per agent</div>
        ${bars || '<div class="insights-empty" style="padding:4px">No cost data yet.</div>'}
      </div>
      <div class="insight-card">
        <div class="insight-card-title">Token usage</div>
        ${tokens || '<div class="insights-empty" style="padding:4px">No token data yet.</div>'}
      </div>
      <div class="insight-card">
        <div class="insight-card-title">Learning leaderboard</div>
        ${leaderboard || '<div class="insights-empty" style="padding:4px">No learnings yet.</div>'}
      </div>
      <div class="insight-card" style="grid-column:1/-1">
        <div class="insight-card-title">Active now</div>
        <div class="active-now-grid" id="activeNowGrid"></div>
      </div>
      <div class="insight-card">
        <div class="insight-card-title">Team velocity (24h)</div>
        <div id="teamVelocity"></div>
      </div>
      <div class="insight-card">
        <div class="insight-card-title">Stage timings</div>
        <div id="stageTimings"></div>
      </div>
      <div class="insight-card">
        <div class="insight-card-title">Blocked tasks</div>
        <div id="blockerLeaderboard"></div>
      </div>
    `;
    // Populate the dynamic panels. All safe to run in parallel.
    this._updateBudgetPill();
    const an = document.getElementById('activeNowGrid');
    if (an) this._renderActiveNowFeed(an);
    const tv = document.getElementById('teamVelocity');
    if (tv) this._renderTeamVelocity(tv);
    const st = document.getElementById('stageTimings');
    if (st) this._renderStageTimings(st);
    const bl = document.getElementById('blockerLeaderboard');
    if (bl) this._renderBlockerLeaderboard(bl, queuesData);
  }

  // Build a 2-3 sentence narrative summary for the top of Insights.
  // Pure function: receives pre-fetched task-queue data so we don't
  // race a duplicate /api/office/task-queues request against the
  // blocker leaderboard renderer.
  _buildInsightsNarrative(roster, byCost, byLearning, totalCost, queuesData) {
    const sentences = [];
    const top = byCost[0];
    const avg = roster.length > 0 ? totalCost / roster.length : 0;
    const topCost = (top && top.usage && top.usage.totalCostUSD) || 0;

    if (totalCost < 0.01) {
      sentences.push(`<span class="hl">Team just spawned</span> — spend and usage metrics will light up as agents start working.`);
    } else {
      if (top && avg > 0 && topCost > avg * 1.5) {
        const ratio = (topCost / avg).toFixed(1);
        sentences.push(`<span class="hl">${this._escapeHtml(top.name || 'Top agent')}</span> leads today's spend at <span class="hl">$${topCost.toFixed(3)}</span> (${ratio}× team average).`);
      } else {
        sentences.push(`Spend is evenly distributed — team total is <span class="hl">$${totalCost.toFixed(3)}</span> across ${roster.length} agent${roster.length === 1 ? '' : 's'}.`);
      }
    }

    const topLearner = byLearning[0];
    if (topLearner && (topLearner.learningCount || 0) > 0) {
      sentences.push(`<span class="hl">${this._escapeHtml(topLearner.name)}</span> is the most prolific learner with <span class="hl">${topLearner.learningCount}</span> recorded insight${topLearner.learningCount === 1 ? '' : 's'}.`);
    }

    // Pull the oldest blocker for a "stuck on X for Y min" line. The
    // data is whatever renderInsights prefetched; if it's null the
    // narrative silently drops the blocker sentence.
    if (queuesData && Array.isArray(queuesData.queues)) {
      const blocked = [];
      for (const q of queuesData.queues) {
        for (const t of (q.tasks || [])) {
          if (t.status === 'blocked') {
            blocked.push({ title: t.title, assignedAt: t.assignedAt, agentName: q.agentName });
          }
        }
      }
      blocked.sort((a, b) => new Date(a.assignedAt).getTime() - new Date(b.assignedAt).getTime());
      if (blocked[0]) {
        const b = blocked[0];
        const mins = Math.max(0, Math.round((Date.now() - new Date(b.assignedAt).getTime()) / 60000));
        const stuckCls = mins > 60 ? 'hl-alert' : mins > 15 ? 'hl-warn' : 'hl';
        sentences.push(`<span class="${stuckCls}">${this._escapeHtml(b.agentName || 'An agent')}</span> has been blocked on "${this._escapeHtml((b.title || '').slice(0, 60))}" for <span class="${stuckCls}">${mins} min</span>.`);
      } else {
        sentences.push(`<span class="hl">No tasks blocked</span> right now — team is flowing.`);
      }
    }

    return `<div class="insight-card insight-narrative">
      <div class="insight-card-title">Team snapshot</div>
      <div class="insight-narrative-body">${sentences.join(' ')}</div>
    </div>`;
  }

  _renderCostBar(m, maxCost, rank) {
    const cost = (m.usage && m.usage.totalCostUSD) || 0;
    const pct = maxCost > 0 ? Math.round((cost / maxCost) * 100) : 0;
    return `<div class="insight-bar-row">
      <span class="insight-bar-name"><span class="t-rank">#${rank}</span>${this._escapeHtml(m.name || '')}</span>
      <div class="insight-bar-track"><div class="insight-bar-fill animate-bar-fill" style="width:${pct}%"></div></div>
      <span class="insight-bar-value t-mono">$${cost.toFixed(4)}</span>
    </div>`;
  }

  _renderTokenRow(m, maxTokens, rank) {
    const inTok = (m.usage && m.usage.totalInputTokens) || 0;
    const outTok = (m.usage && m.usage.totalOutputTokens) || 0;
    const total = inTok + outTok;
    if (total === 0) return '';
    // Proportional scaling: bar width reflects this row's share of the
    // team max. Previously every row was 100%, hiding relative usage.
    const pct = maxTokens > 0 ? Math.round((total / maxTokens) * 100) : 0;
    return `<div class="insight-bar-row">
      <span class="insight-bar-name"><span class="t-rank">#${rank}</span>${this._escapeHtml(m.name || '')}</span>
      <div class="insight-bar-track"><div class="insight-bar-fill animate-bar-fill" style="width:${pct}%"></div></div>
      <span class="insight-bar-value t-mono">${total.toLocaleString()}</span>
    </div>`;
  }

  // ========== Meetings tab ==========

  async renderMeetings() {
    const listEl = document.getElementById('meetingsList');
    if (!listEl) return;
    // Cancel any in-flight meetings-list fetch so rapid tab toggles
    // don't stomp each other's rendered list.
    if (this._meetingsAbortController) {
      try { this._meetingsAbortController.abort(); } catch (_) { /* already aborted */ }
    }
    this._meetingsAbortController = new AbortController();
    const signal = this._meetingsAbortController.signal;
    try {
      const res = await fetch('/api/office/standup/history', { signal });
      const data = await res.json();
      const standups = Array.isArray(data.standups) ? data.standups : [];
      standups.sort((a, b) => String(b.modified || '').localeCompare(String(a.modified || '')));
      if (standups.length === 0) {
        listEl.innerHTML = `<div class="meetings-empty">
          <div class="empty-state-title">No past meetings</div>
          <div class="empty-state-sub">Transcripts land here after each standup completes.</div>
          <button class="panel-btn panel-btn-primary empty-state-cta" onclick="office.startStandup()">Start Standup</button>
        </div>`;
        return;
      }
      // Cache the full list so _filterMeetings can re-filter client-side
      // without refetching.
      this._meetingsAll = standups;
      listEl.innerHTML = standups.map(s => `
        <div class="meeting-item" data-label="${this._escapeAttr((this._humanizeStandupID(s.id) + ' ' + (s.modified || '')).toLowerCase())}" onclick="office.openMeeting('${this._escapeAttr(s.id)}')">
          <div class="meeting-title">${this._escapeHtml(this._humanizeStandupID(s.id))}</div>
          <div class="meeting-meta">${this._escapeHtml(s.modified || '')}</div>
        </div>`).join('');
      // Re-apply any current search filter so switching tabs doesn't
      // reveal items the operator previously filtered out.
      const input = document.getElementById('meetingsSearch');
      if (input && input.value) this._filterMeetings(input.value);
    } catch (err) {
      if (err && err.name === 'AbortError') return;
      listEl.innerHTML = '<div class="meetings-empty">Failed to load meetings.</div>';
    }
  }

  _filterMeetings(q) {
    const needle = (q || '').trim().toLowerCase();
    const items = document.querySelectorAll('#meetingsList .meeting-item');
    items.forEach(el => {
      const label = el.dataset.label || '';
      const match = !needle || label.includes(needle);
      el.classList.toggle('filtered-out', !match);
    });
  }

  _humanizeStandupID(id) {
    // Standup IDs typically look like "standup-2026-04-16T10-30-00Z"; try to
    // pull out the date portion for readability, fall back to raw ID.
    const m = String(id || '').match(/(\d{4}-\d{2}-\d{2})[T_-](\d{2})[_-]?(\d{2})/);
    if (m) return `Standup · ${m[1]} ${m[2]}:${m[3]}`;
    return id || 'Standup';
  }

  async openMeeting(id) {
    const detail = document.getElementById('meetingsDetail');
    if (!detail) return;
    document.querySelectorAll('.meeting-item').forEach(el => el.classList.remove('active'));
    const clicked = Array.from(document.querySelectorAll('.meeting-item')).find(el =>
      (el.getAttribute('onclick') || '').includes("'" + id + "'"));
    if (clicked) clicked.classList.add('active');
    detail.innerHTML = '<div class="meetings-placeholder">Loading transcript…</div>';
    // If the operator clicks meeting A then meeting B quickly, A's
    // fetch might resolve after B's and overwrite B's transcript.
    // Abort any previous open so the last click wins deterministically.
    if (this._openMeetingAbortController) {
      try { this._openMeetingAbortController.abort(); } catch (_) { /* already aborted */ }
    }
    this._openMeetingAbortController = new AbortController();
    const signal = this._openMeetingAbortController.signal;
    try {
      const res = await fetch('/api/office/standup/' + encodeURIComponent(id), { signal });
      const data = await res.json();
      const tr = (data && (data.transcript || (data.standup && data.standup.transcript))) || [];
      if (!Array.isArray(tr) || tr.length === 0) {
        detail.innerHTML = '<div class="meetings-placeholder">No transcript for this meeting.</div>';
        return;
      }
      // Enrich each line with speaker tier (for left-border color) and
      // insert a day/time header whenever the gap between consecutive
      // lines exceeds 5 minutes, so a long transcript has clear breaks.
      const rosterByName = new Map();
      for (const mem of (this.state.members || [])) {
        if (mem.name) rosterByName.set(mem.name, mem);
      }
      const FIVE_MIN = 5 * 60 * 1000;
      let lastTs = 0;
      const parts = [];
      for (const line of tr) {
        const speaker = line.speaker || line.from || '';
        const member = rosterByName.get(speaker);
        const tier = member && member.tier
          ? member.tier
          : (speaker.toLowerCase() === 'system' ? 'system' : 'worker');
        const ts = new Date(line.timestamp || 0).getTime();
        if (ts && lastTs && ts - lastTs > FIVE_MIN) {
          const d = new Date(ts);
          const label = d.toLocaleString(undefined, { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' });
          parts.push(`<div class="meeting-day-header">${this._escapeHtml(label)}</div>`);
        }
        lastTs = ts || lastTs;
        parts.push(`<div class="meeting-transcript-line" data-tier="${this._escapeAttr(tier)}">
          <span class="meeting-speaker">${this._escapeHtml(speaker)}</span>
          <div class="meeting-content">${this._escapeHtml(line.content || line.text || '')}</div>
          <div class="meeting-timestamp t-mono">${this._escapeHtml(this._formatRelativeTime(new Date(line.timestamp || 0)))}</div>
        </div>`);
      }
      // F6 · prepend transcript search box + match count above the
      // rendered transcript lines. Pure client-side filter over the
      // already-rendered lines; no refetch.
      const searchHtml = `
        <input type="text" class="meetings-transcript-search" id="transcriptSearch" placeholder="Search this transcript…" oninput="office._filterTranscript(this.value)">
        <span class="transcript-match-count" id="transcriptMatchCount"></span>`;
      detail.innerHTML = searchHtml + parts.join('');
    } catch (err) {
      if (err && err.name === 'AbortError') return;
      detail.innerHTML = '<div class="meetings-placeholder">Failed to load meeting.</div>';
    }
  }

  // F6 · filter transcript lines by substring match on speaker or
  // content; highlight matches inline via <mark>. Input is escaped
  // before being used in the regex to prevent regex-injection.
  _filterTranscript(query) {
    const q = (query || '').trim();
    const lines = document.querySelectorAll('#meetingsDetail .meeting-transcript-line');
    const countEl = document.getElementById('transcriptMatchCount');
    if (!q) {
      lines.forEach(line => {
        line.classList.remove('filtered-out');
        // Remove any previously-injected <mark> tags.
        line.querySelectorAll('mark').forEach(m => {
          const txt = document.createTextNode(m.textContent);
          m.replaceWith(txt);
        });
      });
      if (countEl) countEl.textContent = '';
      return;
    }
    const lower = q.toLowerCase();
    const reSafe = q.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    const re = new RegExp('(' + reSafe + ')', 'gi');
    let shown = 0;
    lines.forEach(line => {
      const contentEl = line.querySelector('.meeting-content');
      const speakerEl = line.querySelector('.meeting-speaker');
      const contentTxt = contentEl ? contentEl.textContent : '';
      const speakerTxt = speakerEl ? speakerEl.textContent : '';
      const hay = (contentTxt + ' ' + speakerTxt).toLowerCase();
      const match = hay.includes(lower);
      line.classList.toggle('filtered-out', !match);
      if (match) {
        shown++;
        // Restore original text then re-highlight so repeated filters
        // don't compound <mark> wrappers. Use textContent for both
        // read + reset so we don't parse arbitrary HTML.
        if (contentEl) {
          const raw = contentEl.textContent;
          contentEl.innerHTML = this._escapeHtml(raw).replace(re, '<mark>$1</mark>');
        }
      }
    });
    if (countEl) countEl.textContent = shown === 0 ? `No matches for "${q}"` : `${shown} match${shown === 1 ? '' : 'es'}`;
  }

  // ========== Task detail modal ==========

  async openTaskDetail(sessionId, taskId) {
    const modal = document.getElementById('taskDetailModal');
    const body = document.getElementById('taskModalBody');
    const title = document.getElementById('taskModalTitle');
    if (!modal || !body) return;
    modal.classList.remove('hidden');
    body.innerHTML = '<div class="task-modal-loading">Loading…</div>';
    if (title) title.textContent = 'Task';
    try {
      const res = await fetch('/api/office/task-queues/' + encodeURIComponent(sessionId));
      const data = await res.json();
      const tasks = (data && Array.isArray(data.tasks)) ? data.tasks : [];
      const t = tasks.find(x => x.id === taskId);
      if (!t) {
        body.innerHTML = '<div class="task-modal-loading">Task not found.</div>';
        return;
      }
      if (title) title.textContent = t.title || 'Task';
      const member = this._findMemberBySession(sessionId);
      const agentLine = member
        ? `<div class="task-modal-field"><strong>Agent:</strong> <a href="#" onclick="office.showDetail('${this._escapeAttr(sessionId)}');office.closeTaskDetail();return false;">${this._escapeHtml(member.name || sessionId)}</a></div>`
        : '';
      const ts = this._taskTimestamps(t);
      const blocker = (t.status === 'blocked' && t.blockerDescription)
        ? `<div class="task-modal-field"><strong>Blocker:</strong> ${this._escapeHtml(t.blockerDescription)}</div>`
        : '';
      const progress = Array.isArray(t.progressLog) && t.progressLog.length > 0
        ? `<div class="task-modal-progress">
             <strong style="color:#e4e4e7">Progress log</strong>
             ${t.progressLog.slice().reverse().map(p => `
               <div class="task-modal-progress-item">
                 <div class="task-modal-progress-ts">${this._escapeHtml(this._formatRelativeTime(new Date(p.timestamp)))}</div>
                 <div class="task-modal-progress-note">${this._escapeHtml(p.note || '')}</div>
               </div>`).join('')}
           </div>`
        : '';
      body.innerHTML = `
        <div class="task-modal-field"><strong>Status:</strong> ${this._escapeHtml(t.status)}</div>
        <div class="task-modal-field"><strong>Priority:</strong> ${this._escapeHtml(t.priority || 'normal')}</div>
        ${agentLine}
        ${ts}
        ${blocker}
        <div class="task-modal-field" style="margin-top:10px;color:#a1a1aa">${this._escapeHtml(t.description || '')}</div>
        ${progress}
      `;
    } catch (err) {
      body.innerHTML = '<div class="task-modal-loading">Failed to load task.</div>';
    }
  }

  closeTaskDetail() {
    const modal = document.getElementById('taskDetailModal');
    if (modal) modal.classList.add('hidden');
  }

  _taskTimestamps(t) {
    const rows = [];
    if (t.assignedAt) rows.push(['Assigned', t.assignedAt]);
    if (t.startedAt && !t.startedAt.startsWith('0001')) rows.push(['Started', t.startedAt]);
    if (t.completedAt && !t.completedAt.startsWith('0001')) rows.push(['Completed', t.completedAt]);
    return rows.map(([label, ts]) => `<div class="task-modal-field"><strong>${label}:</strong> ${this._escapeHtml(this._formatRelativeTime(new Date(ts)))}</div>`).join('');
  }

  _findMemberBySession(sessionId) {
    const roster = (this.state && Array.isArray(this.state.members)) ? this.state.members : [];
    return roster.find(m => m.sessionId === sessionId);
  }

  _escapeHtml(s) {
    return String(s == null ? '' : s)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }
  _escapeAttr(s) { return this._escapeHtml(s).replace(/'/g, '&#39;'); }

  _formatRelativeTime(date) {
    if (!(date instanceof Date) || isNaN(date.getTime())) return '';
    const diff = Date.now() - date.getTime();
    const s = Math.floor(diff / 1000);
    if (s < 60) return s + 's ago';
    const m = Math.floor(s / 60);
    if (m < 60) return m + 'm ago';
    const h = Math.floor(m / 60);
    if (h < 24) return h + 'h ago';
    const d = Math.floor(h / 24);
    return d + 'd ago';
  }

  // ========== Standup ==========

  renderStandup() {
    const transcript = document.getElementById('standupTranscript');
    if (!transcript) return;
    // F2 · participant strip mirrors standupActive.
    this._renderStandupParticipantStrip();

    if (this.state.standupActive && this.state.standupTranscript && this.state.standupTranscript.length > 0) {
      const currentCount = this.state.standupTranscript.length;

      if (this.lastRenderedStandupCount === 0) {
        transcript.innerHTML = '';
        for (const msg of this.state.standupTranscript) {
          transcript.appendChild(this.createStandupEntry(msg));
        }
      } else if (currentCount > this.lastRenderedStandupCount) {
        for (let i = this.lastRenderedStandupCount; i < currentCount; i++) {
          transcript.appendChild(this.createStandupEntry(this.state.standupTranscript[i]));
        }
      }

      this.lastRenderedStandupCount = currentCount;
      return;
    }

    if (this.state.standupTranscript && this.state.standupTranscript.length > 0) {
      transcript.innerHTML = '';
      for (const msg of this.state.standupTranscript) {
        transcript.appendChild(this.createStandupEntry(msg));
      }
      this.lastRenderedStandupCount = this.state.standupTranscript.length;
      return;
    }

    if (this.lastCompletedStandup && this.lastCompletedStandup.transcript.length > 0) {
      const ago = this.formatTimeAgo(this.lastCompletedStandup.completedAt);
      transcript.innerHTML = `
        <div class="standup-history-header">
          <span class="standup-history-label">Previous Standup</span>
          <span class="standup-history-time">${ago}</span>
        </div>
      `;
      for (const msg of this.lastCompletedStandup.transcript) {
        transcript.appendChild(this.createStandupEntry(msg, true));
      }
      this.lastRenderedStandupCount = 0;
      return;
    }

    transcript.innerHTML = '<div class="standup-empty">No standup in progress. Click "Start Standup" to begin.</div>';
    this.lastRenderedStandupCount = 0;
  }

  // F2 · render the live participant strip above the transcript.
  // Derives state from the transcript (who has spoken) + members (who
  // should speak) + each member's current status. Idempotent; called on
  // every renderStandup().
  _renderStandupParticipantStrip() {
    const strip = document.getElementById('standupParticipantStrip');
    if (!strip) return;

    const active = this.state.standupActive;
    if (!active) {
      strip.classList.add('hidden');
      strip.innerHTML = '';
      this._standupSpoken = new Set();
      return;
    }

    const members = (this.state && Array.isArray(this.state.members)) ? this.state.members : [];
    if (members.length === 0) {
      strip.classList.add('hidden');
      return;
    }
    strip.classList.remove('hidden');

    // Rebuild spoken set from transcript every render — cheap and always
    // in sync with SSE updates that mutated state.standupTranscript.
    const spoken = new Set();
    const transcript = Array.isArray(this.state.standupTranscript) ? this.state.standupTranscript : [];
    for (const msg of transcript) {
      if (msg && msg.type === 'agent_response' && msg.speakerName) {
        spoken.add(msg.speakerName);
      }
    }
    this._standupSpoken = spoken;

    // Current-turn agent: the last "prompt" or "standup_prompt" entry in the
    // transcript addressed a specific agent. We infer current turn from the
    // most recent entry whose type is a prompt. Fall back to "first who
    // hasn't spoken".
    let currentTurn = null;
    for (let i = transcript.length - 1; i >= 0; i--) {
      const msg = transcript[i];
      if (msg && (msg.type === 'standup_prompt' || msg.type === 'prompt') && msg.targetName) {
        currentTurn = msg.targetName;
        break;
      }
    }
    if (!currentTurn) {
      const nextUp = members.find(m => !spoken.has(m.name));
      currentTurn = nextUp ? nextUp.name : null;
    }

    const chips = members.map(m => {
      let state = 'pending';
      if (spoken.has(m.name)) state = 'responded';
      else if (m.name === currentTurn) state = 'awaiting';
      if (m.status === 'stuck' && state !== 'responded') state = 'timeout';
      return `<span class="standup-participant-chip" data-state="${state}" title="${this.escapeAttr(m.name)} — ${state}">
        <span class="standup-participant-chip-dot"></span>
        ${this.escapeHtml(m.name || '')}
      </span>`;
    }).join('');

    const respondedCount = spoken.size;
    strip.innerHTML = `
      <span class="standup-participant-strip-label">Turn ${respondedCount} of ${members.length}</span>
      ${chips}
    `;
  }

  createStandupEntry(msg, isHistory = false) {
    const entry = document.createElement('div');
    entry.className = `standup-entry ${msg.type}${isHistory ? ' standup-history' : ''}`;
    const speakerLabel = (msg.type === 'user_input' || msg.type === 'user_task')
      ? 'You' : this.escapeHtml(msg.speakerName);
    entry.innerHTML = `
      <div class="standup-speaker">${speakerLabel}</div>
      <div class="standup-content">${this.escapeHtml(msg.content)}</div>
    `;
    return entry;
  }

  formatTimeAgo(timestamp) {
    const seconds = Math.floor((Date.now() - timestamp) / 1000);
    if (seconds < 60) return 'just now';
    const minutes = Math.floor(seconds / 60);
    if (minutes < 60) return `${minutes}m ago`;
    const hours = Math.floor(minutes / 60);
    if (hours < 24) return `${hours}h ago`;
    const days = Math.floor(hours / 24);
    return `${days}d ago`;
  }

  persistLastStandup() {
    try {
      if (this.lastCompletedStandup) {
        localStorage.setItem('jarvis_lastStandup', JSON.stringify(this.lastCompletedStandup));
      }
    } catch { /* quota exceeded or unavailable */ }
  }

  restoreLastStandup() {
    try {
      const stored = localStorage.getItem('jarvis_lastStandup');
      if (stored) {
        this.lastCompletedStandup = JSON.parse(stored);
      }
    } catch { /* corrupt data */ }
  }

  // ========== Messages ==========

  renderMessages() {
    const list = document.getElementById('messageList');
    if (!list) return;

    if (!this.state.recentMessages || this.state.recentMessages.length === 0) {
      list.innerHTML = '<div class="messages-empty">No messages yet.</div>';
      this._updateMessageFilterCount(0, 0);
      return;
    }

    // Feature 5: Group messages into threads by conversation pair
    const threadMap = new Map();
    for (const msg of this.state.recentMessages) {
      const ids = [msg.fromSessionId || msg.fromName, msg.toSessionId || msg.toName].sort();
      const threadKey = ids.join('::');
      if (!threadMap.has(threadKey)) {
        threadMap.set(threadKey, {
          key: threadKey,
          participants: `${msg.fromName} <-> ${msg.toName}`,
          messages: [],
          latestTimestamp: 0,
        });
      }
      const thread = threadMap.get(threadKey);
      thread.messages.push(msg);
      if ((msg.timestamp || 0) > thread.latestTimestamp) {
        thread.latestTimestamp = msg.timestamp || 0;
      }
    }

    const threads = Array.from(threadMap.values())
      .sort((a, b) => b.latestTimestamp - a.latestTimestamp);

    // F1 · apply filter. Returns the threads to render + the per-thread
    // highlight regex so the body HTML can mark matches inline.
    const { q, unreadOnly } = this._messageFilter;
    const needle = (q || '').trim().toLowerCase();
    const highlightRegex = needle ? new RegExp(`(${this._escapeRegex(needle)})`, 'gi') : null;

    const matchesThread = (thread) => {
      if (unreadOnly) {
        const hasUnread = thread.messages.some(m => m.id && !this._readMessages.has(m.id));
        if (!hasUnread) return false;
      }
      if (!needle) return true;
      if (thread.participants.toLowerCase().includes(needle)) return true;
      return thread.messages.some(m =>
        (m.content || '').toLowerCase().includes(needle) ||
        (m.fromName || '').toLowerCase().includes(needle) ||
        (m.toName || '').toLowerCase().includes(needle));
    };

    const visibleThreads = threads.filter(matchesThread);
    this._updateMessageFilterCount(visibleThreads.length, threads.length);

    if (visibleThreads.length === 0) {
      list.innerHTML = `<div class="messages-empty">No threads match ${needle ? `"${this.escapeHtml(q)}"` : 'your filter'}.</div>`;
      return;
    }

    const highlight = (text) => {
      const esc = this.escapeHtml(text);
      if (!highlightRegex) return esc;
      return esc.replace(highlightRegex, '<mark>$1</mark>');
    };

    list.innerHTML = visibleThreads.map(thread => {
      const unreadInThread = thread.messages.filter(
        m => m.id && !this._readMessages.has(m.id)
      ).length;
      const unreadBadge = unreadInThread > 0
        ? `<span class="thread-unread-badge">${unreadInThread}</span>`
        : '';
      const lastMsg = thread.messages[thread.messages.length - 1];
      const timeStr = lastMsg && lastMsg.timestamp
        ? new Date(lastMsg.timestamp).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
        : '';

      const messagesHtml = thread.messages.map(msg => {
        const isUnread = msg.id && !this._readMessages.has(msg.id);
        const time = msg.timestamp
          ? new Date(msg.timestamp).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
          : '';
        return `<div class="thread-message-item${isUnread ? ' unread' : ''}">
          <div class="thread-message-meta">
            <span>${highlight(msg.fromName || '')} to ${highlight(msg.toName || '')}</span>
            <span>${time}</span>
          </div>
          ${highlight(msg.content || '')}
        </div>`;
      }).join('');

      return `<div class="message-thread" data-thread-key="${this.escapeAttr(thread.key)}">
        <div class="message-thread-header" onclick="office._toggleMessageThread(this)">
          <span class="message-thread-participants">${highlight(thread.participants)}</span>
          <div class="message-thread-meta">
            ${unreadBadge}
            <span>${timeStr}</span>
            <span>${thread.messages.length} msg${thread.messages.length !== 1 ? 's' : ''}</span>
          </div>
        </div>
        <div class="message-thread-body">${messagesHtml}</div>
      </div>`;
    }).join('');
  }

  // F1 · debounced filter input handler.
  _onMessageFilterInput(q) {
    if (this._messageFilterDebounce) clearTimeout(this._messageFilterDebounce);
    this._messageFilterDebounce = setTimeout(() => {
      this._messageFilter.q = q || '';
      this.renderMessages();
    }, 120);
  }

  _toggleUnreadOnly() {
    this._messageFilter.unreadOnly = !this._messageFilter.unreadOnly;
    const btn = document.getElementById('messagesFilterUnread');
    if (btn) btn.classList.toggle('active', this._messageFilter.unreadOnly);
    this.renderMessages();
  }

  _updateMessageFilterCount(visible, total) {
    const el = document.getElementById('messagesFilterCount');
    if (!el) return;
    if (total === 0) {
      el.textContent = '0 threads';
    } else if (visible === total) {
      el.textContent = `${total} thread${total === 1 ? '' : 's'}`;
    } else {
      el.textContent = `${visible} of ${total} threads`;
    }
  }

  // Regex metacharacter escape for user-supplied substrings.
  _escapeRegex(s) {
    return String(s).replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  }

  _toggleMessageThread(headerEl) {
    const thread = headerEl.closest('.message-thread');
    if (!thread) return;
    const isOpen = thread.classList.toggle('open');
    if (isOpen) {
      // Mark all messages in this thread as read
      const msgItems = thread.querySelectorAll('.thread-message-item');
      const threadMessages = (this.state.recentMessages || []).filter(m => {
        const ids = [m.fromSessionId || m.fromName, m.toSessionId || m.toName].sort();
        return ids.join('::') === thread.dataset.threadKey;
      });
      for (const msg of threadMessages) {
        if (msg.id) this._readMessages.add(msg.id);
      }
      msgItems.forEach(item => item.classList.remove('unread'));
      const badge = thread.querySelector('.thread-unread-badge');
      if (badge) badge.remove();
      // Update only affected recipient cards' unread badges
      const uniqueRecipients = new Set(threadMessages.map(m => m.toSessionId).filter(Boolean));
      for (const sid of uniqueRecipients) {
        this._updateUnreadBadgeOnCard(sid);
      }
    }
  }

  updateMessageSelects() {
    const fromSelect = document.getElementById('messageFrom');
    const toSelect = document.getElementById('messageTo');
    if (!fromSelect || !toSelect) return;

    const options = this.state.members.map(m =>
      `<option value="${m.sessionId}">${this.escapeHtml(m.name)}</option>`
    ).join('');

    fromSelect.innerHTML = '<option value="">From...</option>' + options;
    toSelect.innerHTML = '<option value="">To...</option><option value="all">-- All Team --</option>' + options;
  }

  updateStandupButtons(active) {
    const startBtn = document.getElementById('btnStartStandupSidebar');
    const stopBtn = document.getElementById('btnStopStandup');
    const headerBtn = document.getElementById('btnStandup');

    if (startBtn) startBtn.classList.toggle('hidden', active);
    if (stopBtn) stopBtn.classList.toggle('hidden', !active);
    if (headerBtn) {
      headerBtn.textContent = active ? 'End Standup' : 'Standup';
      headerBtn.onclick = active ? () => this.stopStandup() : () => this.startStandup();
    }
  }

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
  }

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
  }

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
  }

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
  }

  closeMemberModal() {
    if (this.modalAbortController) {
      this.modalAbortController.abort();
      this.modalAbortController = null;
    }
    document.getElementById('memberModal').classList.add('hidden');
    this.selectedMemberId = null;
  }

  // ========== Feature 7: Code Review ==========

  async showReview(sessionId) {
    const modal = document.getElementById('reviewModal');
    const diffEl = document.getElementById('reviewDiff');
    if (!modal || !diffEl) return;

    modal.classList.remove('hidden');
    diffEl.innerHTML = '<div class="diff-loading">Loading git status...</div>';

    try {
      const res = await fetch(`/api/sessions/${sessionId}/git-status`);
      if (!res.ok) {
        diffEl.innerHTML = `<div class="diff-empty">Git status unavailable (HTTP ${res.status})</div>`;
        return;
      }
      const data = await res.json();

      // Support two possible response shapes: { diff } or { output } or raw text
      const rawDiff = data.diff || data.output || (typeof data === 'string' ? data : '');
      if (!rawDiff || !rawDiff.trim()) {
        diffEl.innerHTML = '<div class="diff-empty">No changes detected in the working tree.</div>';
        return;
      }

      diffEl.innerHTML = this._renderDiff(rawDiff);
    } catch (err) {
      console.error('Failed to fetch git status:', err);
      diffEl.innerHTML = '<div class="diff-empty">Failed to load code changes.</div>';
    }
  }

  _renderDiff(rawDiff) {
    const lines = rawDiff.split('\n');
    const files = [];
    let currentFile = null;

    for (const line of lines) {
      if (line.startsWith('diff --git')) {
        if (currentFile) files.push(currentFile);
        const match = line.match(/diff --git a\/(.+) b\/(.+)/);
        currentFile = {
          name: match ? match[2] : line,
          lines: [],
          adds: 0,
          dels: 0,
        };
      } else if (line.startsWith('+++ ') && currentFile) {
        const fname = line.slice(4).replace(/^b\//, '');
        if (fname !== '/dev/null') currentFile.name = fname;
      } else if (line.startsWith('@@') && currentFile) {
        currentFile.lines.push({ type: 'hunk', content: line });
      } else if (line.startsWith('+') && currentFile && !line.startsWith('+++')) {
        currentFile.lines.push({ type: 'add', content: line.slice(1) });
        currentFile.adds++;
      } else if (line.startsWith('-') && currentFile && !line.startsWith('---')) {
        currentFile.lines.push({ type: 'del', content: line.slice(1) });
        currentFile.dels++;
      } else if (currentFile) {
        currentFile.lines.push({ type: 'context', content: line.startsWith(' ') ? line.slice(1) : line });
      }
    }
    if (currentFile) files.push(currentFile);

    if (files.length === 0) {
      // Fallback: render as plain text if parsing fails
      return `<div class="diff-file"><div class="diff-file-header"><span class="diff-file-name">Changes</span></div>${
        lines.map(l => {
          let cls = 'diff-context';
          if (l.startsWith('+') && !l.startsWith('+++')) cls = 'diff-add';
          else if (l.startsWith('-') && !l.startsWith('---')) cls = 'diff-del';
          return `<div class="diff-line ${cls}"><span class="diff-line-content">${this.escapeHtml(l)}</span></div>`;
        }).join('')
      }</div>`;
    }

    return files.map(file => {
      const linesHtml = file.lines.map((l, idx) => {
        if (l.type === 'hunk') {
          return `<div class="diff-hunk-header">${this.escapeHtml(l.content)}</div>`;
        }
        return `<div class="diff-line diff-${l.type}">
          <span class="diff-line-num">${idx + 1}</span>
          <span class="diff-line-content">${this.escapeHtml(l.content)}</span>
        </div>`;
      }).join('');

      return `<div class="diff-file">
        <div class="diff-file-header">
          <span class="diff-file-name">${this.escapeHtml(file.name)}</span>
          <span class="diff-file-stats">
            <span class="diff-stat-add">+${file.adds}</span>
            <span class="diff-stat-del">-${file.dels}</span>
          </span>
        </div>
        ${linesHtml}
      </div>`;
    }).join('');
  }

  closeReview() {
    const modal = document.getElementById('reviewModal');
    if (modal) modal.classList.add('hidden');
  }

  // ========== Feature 8: Autonomy Settings ==========

  async setAutonomy(sessionId, level) {
    if (!sessionId) return;
    this._autonomyLevels.set(sessionId, level);
    const member = this.state.members.find(m => m.sessionId === sessionId);
    const card = document.querySelector(`.agent-card[data-session-id="${sessionId}"]`);
    if (card && member) {
      this.updateAgentCard(card, member);
    } else {
      this.renderAgentCards();
    }

    try {
      await this.putJson(`/api/office/member/${sessionId}/personality`, {
        autonomyLevel: level,
      });
    } catch (err) {
      console.warn('Failed to persist autonomy level:', err);
    }
  }

  navigateToSession() {
    if (this.selectedMemberId) {
      window.location.href = `/?session=${this.selectedMemberId}`;
    }
  }

  navigateToSessionById(sessionId) {
    if (sessionId) {
      window.location.href = `/?session=${sessionId}`;
    }
  }

  navigateToSessionDirect(sessionId) {
    this.navigateToSessionById(sessionId);
  }

  async quickPrompt(sessionId) {
    const member = this.state.members.find(m => m.sessionId === sessionId);
    const name = member ? member.name : 'Agent';
    const prompt = window.prompt(`Send prompt to ${name}:`);
    if (!prompt || !prompt.trim()) return;
    try {
      const data = await this.postJson(`/api/sessions/${sessionId}/input`, { input: prompt.trim() + '\n' });
      if (data && !data.error) {
        this.showNotification(`Prompt sent to ${name}`, 'success');
      } else {
        this.showNotification(`Failed to send prompt to ${name}`, 'error');
      }
    } catch {
      this.showNotification(`Failed to send prompt to ${name}`, 'error');
    }
  }

  // R18 · Inject a corrective prompt into a running agent. Hits the
  // browser-auth /api/office/agent/inject endpoint which prefixes the
  // message with [OPERATOR PROMPT] so the agent knows it came from the
  // human. Prompt is capped at 4 KB server-side.
  async injectPrompt(sessionId) {
    const member = this.state.members.find(m => m.sessionId === sessionId);
    const name = member ? member.name : 'Agent';
    const prompt = window.prompt(`Inject operator prompt for ${name} (max 4 KB):`);
    if (!prompt || !prompt.trim()) return;
    const trimmed = prompt.trim();
    if (trimmed.length > 4096) {
      this.showNotification('Prompt too long (4 KB limit)', 'error');
      return;
    }
    try {
      const data = await this.postJson('/api/office/agent/inject', {
        sessionId,
        prompt: trimmed,
      });
      if (data && data.injected) {
        this.showNotification(`Injected prompt to ${name}`, 'success');
      } else {
        this.showNotification(`Failed to inject prompt to ${name}`, 'error');
      }
    } catch {
      this.showNotification(`Failed to inject prompt to ${name}`, 'error');
    }
  }

  // R18 · Kill a single agent. Confirmation + optional reason via
  // window.prompt is enough for this operator-only action; the server
  // broadcasts office:agentKilled so the card flashes + removes.
  async killAgent(sessionId) {
    const member = this.state.members.find(m => m.sessionId === sessionId);
    const name = member ? member.name : 'Agent';
    if (!window.confirm(`Stop agent ${name}?\n\nThis ends the session and reassigns its tasks.`)) return;
    const reason = window.prompt(`Reason (optional):`, '') || '';
    try {
      const data = await this.postJson('/api/office/agent/kill', {
        sessionId,
        reason: reason.trim(),
      });
      if (data && data.stopped) {
        this.showNotification(`Stopped ${name}`, 'success');
      } else {
        this.showNotification(`Failed to stop ${name}`, 'error');
      }
    } catch {
      this.showNotification(`Failed to stop ${name}`, 'error');
    }
  }

  setViewMode(mode) {
    // Round 10 · adds 'compact' as a third option. Toggle classes rather
    // than replace so the grid's own layout classes survive.
    const valid = ['grid', 'list', 'compact'];
    if (!valid.includes(mode)) mode = 'grid';
    this._viewMode = mode;
    localStorage.setItem('office-view-mode', mode);
    const grid = document.getElementById('agentGrid');
    if (grid) {
      grid.classList.toggle('view-list', mode === 'list');
      grid.classList.toggle('view-compact', mode === 'compact');
    }
    document.querySelectorAll('.view-mode-btn').forEach(btn => {
      btn.classList.toggle('active', btn.dataset.mode === mode);
    });
    // Full re-render so compact markup branch runs for existing cards.
    if (this.state.teamSpawned) this.renderAgentCards();
  }

  updateNavBadges() {
    const taskCount = (this.taskQueues || []).reduce((sum, q) => {
      return sum + (q.tasks || []).filter(t => t.status === 'in_progress').length;
    }, 0);
    const unreadMsgCount = (this.state.recentMessages || []).filter(
      m => m.id && !this._readMessages.has(m.id)
    ).length;

    this._setNavBadge('tasks', taskCount);
    this._setNavBadge('messages', unreadMsgCount);
    this._setNavBadge('standup', this.state.standupActive ? 1 : 0);
  }

  _setNavBadge(tab, count) {
    const badge = document.getElementById(`navBadge-${tab}`);
    if (!badge) return;
    if (count > 0) {
      badge.textContent = tab === 'standup' ? '' : count;
      badge.classList.remove('hidden');
      if (tab === 'standup') badge.classList.add('nav-badge-dot');
    } else {
      badge.classList.add('hidden');
      badge.classList.remove('nav-badge-dot');
    }
  }

  // ========== Spawn Config Modal ==========

  static PROVIDER_MODELS = {
    'claude-subscription': [
      { value: 'claude-opus-4-6', label: 'Opus 4.6' },
      { value: 'claude-sonnet-4-6', label: 'Sonnet 4.6' },
      { value: 'claude-opus-4-5-20251101', label: 'Opus 4.5' },
      { value: 'claude-sonnet-4-5-20250929', label: 'Sonnet 4.5' },
    ],
    'claude-api': [
      { value: 'claude-opus-4-6', label: 'Opus 4.6' },
      { value: 'claude-sonnet-4-6', label: 'Sonnet 4.6' },
      { value: 'claude-opus-4-5-20251101', label: 'Opus 4.5' },
      { value: 'claude-sonnet-4-5-20250929', label: 'Sonnet 4.5' },
    ],
    'zai-glm': [
      { value: 'glm-5.1', label: 'GLM-5.1' },
      { value: 'glm-5-turbo', label: 'GLM-5 Turbo' },
      { value: 'glm-5', label: 'GLM-5' },
      { value: 'glm-4.7', label: 'GLM-4.7' },
    ],
  };

  static TEAM_MEMBERS_INFO = [
    { id: 'max', name: 'Max', role: 'Engineering Manager', color: 'blue' },
    { id: 'sage', name: 'Sage', role: 'Senior Architect', color: 'purple' },
    { id: 'nova', name: 'Nova', role: 'UI/UX Designer', color: 'pink' },
    { id: 'vex', name: 'Vex', role: 'Frontend Engineer', color: 'green' },
    { id: 'bolt', name: 'Bolt', role: 'Backend Engineer', color: 'orange' },
    { id: 'forge', name: 'Forge', role: 'DevOps Engineer', color: 'yellow' },
    { id: 'lint', name: 'Lint', role: 'QA Engineer', color: 'red' },
  ];

  async spawnTeam() {
    try {
      const [templatesRes, settingsRes, officeRes] = await Promise.all([
        fetch('/api/office/templates'),
        fetch('/api/settings/accounts'),
        fetch('/api/office/settings'),
      ]);
      const templatesData = await templatesRes.json();
      const settings = await settingsRes.json();
      const officeData = officeRes.ok ? await officeRes.json() : null;
      if (officeData && officeData.success) this.officeSettings = officeData;

      this._spawnTemplates = templatesData.templates || [];
      this._spawnSettings = settings;
      this._selectedTemplateId = null;

      document.getElementById('spawnStep1').classList.remove('hidden');
      document.getElementById('spawnStep2').classList.add('hidden');

      // If a team is already active, show the view-only banner + disable the
      // spawn confirm button. Operator can still browse templates + see the
      // current team's configuration.
      this._applyTeamActiveViewMode();

      this.renderTemplateGrid();
      document.getElementById('spawnConfigModal').classList.remove('hidden');
    } catch (err) {
      console.error('Failed to open spawn config:', err);
      this.showNotification('Failed to load team templates', 'error');
    }
  }

  /**
   * When a team is already running, pin a "Current team" banner at the top
   * of the spawn modal and disable the confirm button so the operator can
   * browse templates + inspect the live configuration without accidentally
   * kicking off a second spawn on top.
   */
  _applyTeamActiveViewMode() {
    const step1 = document.getElementById('spawnStep1');
    const step2 = document.getElementById('spawnStep2');
    const confirm = document.getElementById('btnSpawnConfirm');
    const existing = document.getElementById('spawnActiveBanner');
    if (existing) existing.remove();

    if (!this.state.teamSpawned) {
      if (confirm) { confirm.disabled = false; confirm.textContent = 'Spawn Team'; }
      return;
    }

    const officeSettings = this.officeSettings || {};
    const roster = (this.state.members || []);
    const templateId = officeSettings.teamTemplateId || '';
    const workingDir = officeSettings.teamWorkingDir || '';
    const mission = officeSettings.teamMission || this.state.teamMission || '';
    const spawnedAt = officeSettings.teamSpawnedAt;
    const uptime = spawnedAt ? this.formatTimeAgo(spawnedAt) : 'just now';
    const template = (this._spawnTemplates || []).find(t => t.id === templateId);
    const templateName = template ? template.name : (templateId || 'custom team');
    const statusCounts = roster.reduce((acc, m) => {
      acc[m.status || 'idle'] = (acc[m.status || 'idle'] || 0) + 1;
      return acc;
    }, {});
    const statusSummary = Object.entries(statusCounts)
      .map(([s, n]) => `${n} ${this._escapeHtml(s)}`).join(' · ');

    const rosterHtml = roster.map(m => {
      const provider = m.provider || 'claude-subscription';
      const model = m.model || '';
      return `<div class="active-team-member">
        <span class="active-team-name">${this._escapeHtml(m.name || '')}</span>
        <span class="active-team-role">${this._escapeHtml(m.role || '')}</span>
        <span class="active-team-model">${this._escapeHtml(provider)}${model ? ' · ' + this._escapeHtml(model) : ''}</span>
        <span class="active-team-status status-${this._escapeAttr(m.status || 'idle')}">${this._escapeHtml(m.status || 'idle')}</span>
      </div>`;
    }).join('');

    const banner = document.createElement('div');
    banner.id = 'spawnActiveBanner';
    banner.className = 'spawn-active-banner';
    banner.innerHTML = `
      <div class="spawn-active-banner-head">
        <div>
          <div class="spawn-active-banner-title">Current team: ${this._escapeHtml(templateName)}</div>
          <div class="spawn-active-banner-sub">
            ${roster.length} member${roster.length === 1 ? '' : 's'} · running ${uptime}${workingDir ? ' · <code>' + this._escapeHtml(workingDir) + '</code>' : ''}
            ${statusSummary ? ' · ' + statusSummary : ''}
          </div>
        </div>
        <div class="spawn-active-banner-actions">
          <button class="panel-btn" onclick="office.wrapUpAll(); office.closeSpawnConfigModal();">Wrap up team</button>
        </div>
      </div>
      ${mission ? `<div class="spawn-active-banner-mission"><span class="label">Mission:</span> ${this._escapeHtml(mission)}</div>` : ''}
      <div class="spawn-active-banner-members">${rosterHtml || '<div class="spawn-active-banner-sub">No agents registered.</div>'}</div>
      <div class="spawn-active-banner-hint">Templates below are view-only while a team is active. Wrap up the current team to spawn a new one.</div>
    `;

    if (step1 && step1.firstChild) step1.insertBefore(banner, step1.firstChild);
    // Duplicate the banner into step 2 too so it shows after a user picks a
    // template in view-mode and "configures" (which will be disabled).
    if (step2 && step2.firstChild) {
      const clone = banner.cloneNode(true);
      clone.id = 'spawnActiveBanner2';
      step2.insertBefore(clone, step2.firstChild);
    }
    if (confirm) {
      confirm.disabled = true;
      confirm.textContent = 'Team already active';
      confirm.title = 'Wrap up the current team before spawning a new one.';
    }
  }

  renderTemplateGrid() {
    const grid = document.getElementById('templateGrid');
    if (!grid || !this._spawnTemplates) return;

    // Apply search/filter
    const searchTerm = (document.getElementById('templateSearch')?.value || '').toLowerCase();
    const categoryFilter = document.getElementById('templateCategory')?.value || '';
    const filtered = this._spawnTemplates.filter(tpl => {
      if (searchTerm && !tpl.name.toLowerCase().includes(searchTerm) && !tpl.description.toLowerCase().includes(searchTerm)) return false;
      if (categoryFilter && tpl.category !== categoryFilter) return false;
      return true;
    });

    const avatarColors = {
      blue: 'var(--session-blue, #3b82f6)',
      purple: 'var(--session-purple, #a855f7)',
      pink: 'var(--session-pink, #ec4899)',
      green: 'var(--session-green, #22c55e)',
      orange: 'var(--session-orange, #f97316)',
      yellow: 'var(--session-yellow, #eab308)',
      red: 'var(--session-red, #ef4444)',
    };

    const categoryIcons = {
      fullstack: '\u{1F3D7}\u{FE0F}',
      frontend: '\u{1F3A8}',
      backend: '\u{2699}\u{FE0F}',
      research: '\u{1F52C}',
      custom: '\u{1F527}',
    };

    const cards = filtered.map(tpl => {
      const memberAvatars = tpl.members.map(m => {
        const bg = avatarColors[m.color] || 'var(--text-dim)';
        const tplIcon = AGENT_ICONS[m.id];
        const avatarInner = tplIcon
          ? `<div class="tpl-member-avatar agent-avatar-icon" style="background:${bg}" title="${this.escapeAttr(m.name)} - ${this.escapeAttr(m.role)}">${tplIcon}</div>`
          : `<div class="tpl-member-avatar" style="background:${bg}" title="${this.escapeAttr(m.name)} - ${this.escapeAttr(m.role)}">${m.emoji || m.name[0]}</div>`;
        return avatarInner;
      }).join('');

      const memberList = tpl.members.map(m => {
        const tierBadge = m.tier === 'strategic' ? '<span class="tpl-tier-badge strategic">Lead</span>' : '';
        return `<div class="tpl-member-row">
          <span class="tpl-member-name">${this.escapeHtml(m.name)}</span>
          <span class="tpl-member-role">${this.escapeHtml(m.role)}</span>
          ${tierBadge}
        </div>`;
      }).join('');

      const icon = categoryIcons[tpl.category] || '';
      const isBuiltIn = tpl.isBuiltIn !== false;

      // Action buttons: built-in gets Duplicate only, custom gets Edit/Duplicate/Delete
      let actions = `<button class="tpl-action-btn" onclick="event.stopPropagation(); office.duplicateTemplate('${this.escapeAttr(tpl.id)}')">Duplicate</button>`;
      if (!isBuiltIn) {
        actions = `<button class="tpl-action-btn" onclick="event.stopPropagation(); office.openTemplateEditor('${this.escapeAttr(tpl.id)}')">Edit</button>` + actions +
          `<button class="tpl-action-btn danger" onclick="event.stopPropagation(); office.deleteTemplate('${this.escapeAttr(tpl.id)}')">Delete</button>`;
      }

      return `<div class="template-card" data-template-id="${this.escapeAttr(tpl.id)}" onclick="office.selectTemplate('${this.escapeAttr(tpl.id)}')">
        <div class="tpl-card-header">
          <span class="tpl-card-icon">${icon}</span>
          <div class="tpl-card-title-group">
            <h3 class="tpl-card-title">${this.escapeHtml(tpl.name)}</h3>
            <span class="tpl-card-count">${tpl.memberCount || tpl.members.length} agents</span>
          </div>
        </div>
        <p class="tpl-card-desc">${this.escapeHtml(tpl.description)}</p>
        <div class="tpl-card-avatars">${memberAvatars}</div>
        <div class="tpl-card-roster">${memberList}</div>
        <div class="tpl-card-actions">${actions}</div>
        <button class="btn-select-tpl" onclick="event.stopPropagation(); office.selectTemplate('${this.escapeAttr(tpl.id)}')">Select &amp; Configure</button>
      </div>`;
    });

    // Add "Create Custom Template" card at the end
    cards.push(`<div class="template-card create-card" onclick="office.openTemplateEditor()">
      <div class="create-card-content">
        <div class="create-icon">+</div>
        <div class="create-label">Create Custom Template</div>
      </div>
    </div>`);

    grid.innerHTML = cards.join('');
  }

  filterTemplates() {
    this.renderTemplateGrid();
  }

  selectTemplate(templateId) {
    const tpl = this._spawnTemplates.find(t => t.id === templateId);
    if (!tpl) return;

    this._selectedTemplateId = templateId;
    this._currentMembers = tpl.members;

    const settings = this._spawnSettings || {};
    const defaultProvider = settings.provider || 'claude-subscription';
    const defaultModel = this.getDefaultModelForProvider(defaultProvider, settings);
    const defaultWorkingDir = settings.defaultWorkingDir || '~/Code';

    document.getElementById('spawnWorkingDir').value = defaultWorkingDir;
    document.getElementById('spawnStep2Title').textContent = tpl.name;
    document.getElementById('spawnStep2Subtitle').textContent = tpl.description;

    this.populateProviderSelect(document.getElementById('spawnDefaultProvider'), defaultProvider);
    this.populateModelSelect(document.getElementById('spawnDefaultModel'), defaultProvider, defaultModel);

    this.renderSpawnMemberRows(defaultProvider, defaultModel);
    // R22 · New tabbed layout entrypoint. renderSpawnMemberRows still
    // owns the per-member editor markup (keyed by spawn-*-${id}) so
    // confirmSpawnTeam can read them unchanged; we additionally
    // render the rail + auto-select the first member and reset the
    // tab state to "Team".
    this._renderSpawnMemberRail();
    const firstId = (this._currentMembers && this._currentMembers[0] && this._currentMembers[0].id) || null;
    if (firstId) this.selectSpawnMember(firstId);
    this.setSpawnTab('team');
    const modalShell = document.querySelector('#spawnConfigModal .spawn-config-modal');
    if (modalShell) modalShell.classList.add('spawn-modal-wide');

    document.getElementById('spawnStep1').classList.add('hidden');
    document.getElementById('spawnStep2').classList.remove('hidden');
    // Advance the breadcrumb visual state.
    const progStep1 = document.getElementById('spawnProgressStep1');
    const progStep2 = document.getElementById('spawnProgressStep2');
    if (progStep1) progStep1.classList.remove('active');
    if (progStep2) progStep2.classList.add('active');

    const missionInput = document.getElementById('spawnMission');
    if (missionInput) {
      if (this.officeSettings && this.officeSettings.teamMission) {
        missionInput.value = this.officeSettings.teamMission;
      } else {
        fetch('/api/office/settings').then(r => r.json()).then(data => {
          if (data.teamMission) {
            missionInput.value = data.teamMission;
          }
        }).catch(() => {});
      }
    }

    const telegramSection = document.getElementById('spawnTelegramSection');
    if (telegramSection) {
      fetch('/api/office/telegram-config').then(r => r.json()).then(data => {
        if (data.telegramConnected) {
          telegramSection.style.display = '';
        }
      }).catch(() => {});
    }
  }

  showTemplateStep() {
    document.getElementById('spawnStep1').classList.remove('hidden');
    document.getElementById('spawnStep2').classList.add('hidden');
    const progStep1 = document.getElementById('spawnProgressStep1');
    const progStep2 = document.getElementById('spawnProgressStep2');
    if (progStep1) progStep1.classList.add('active');
    if (progStep2) progStep2.classList.remove('active');
    // R22 · Narrow the modal shell when returning to Step 1 so the
    // template picker doesn't sit inside a 1100px container.
    const modalShell = document.querySelector('#spawnConfigModal .spawn-config-modal');
    if (modalShell) modalShell.classList.remove('spawn-modal-wide');
  }

  getDefaultModelForProvider(provider, settings) {
    const models = OfficeApp.PROVIDER_MODELS[provider];
    if (!models || models.length === 0) return '';
    if (provider === 'zai-glm' && settings.zaiModel) return settings.zaiModel;
    return models[0].value;
  }

  closeSpawnConfigModal() {
    document.getElementById('spawnConfigModal').classList.add('hidden');
    // Reset breadcrumb so reopening the modal shows "1. Template"
    // active even if the user closed it mid-configure. Without this,
    // showTemplateStep's reset only runs on the Back button path.
    const s1 = document.getElementById('spawnProgressStep1');
    const s2 = document.getElementById('spawnProgressStep2');
    if (s1) s1.classList.add('active');
    if (s2) s2.classList.remove('active');
    // R22 · Drop the wide-shell class + reset tab state so a future
    // Step 1 render (template picker) uses the default 620px width.
    const modalShell = document.querySelector('#spawnConfigModal .spawn-config-modal');
    if (modalShell) modalShell.classList.remove('spawn-modal-wide');
    this._currentSpawnTab = 'team';
  }

  // R16 · Toggle visibility of the two working-dir input groups depending
  // on which spawnMode radio is selected. Called from the radio onchange.
  _updateSpawnMode() {
    const mode = document.querySelector('input[name="spawnMode"]:checked')?.value || 'existing';
    const existing = document.getElementById('spawnExistingMode');
    const newProj = document.getElementById('spawnNewProjectMode');
    if (existing) existing.classList.toggle('hidden', mode !== 'existing');
    if (newProj) newProj.classList.toggle('hidden', mode !== 'new');
    // Autofocus the input that just became visible so the operator can
    // type immediately after toggling.
    setTimeout(() => {
      const target = mode === 'new'
        ? document.getElementById('spawnProjectName')
        : document.getElementById('spawnWorkingDir');
      if (target) target.focus();
    }, 0);
  }

  populateProviderSelect(selectEl, currentProvider) {
    const providers = [
      { value: 'claude-subscription', label: 'Claude Subscription' },
      { value: 'claude-api', label: 'Claude API' },
      { value: 'zai-glm', label: 'Z.AI GLM' },
    ];
    selectEl.innerHTML = providers.map(p =>
      `<option value="${p.value}"${p.value === currentProvider ? ' selected' : ''}>${p.label}</option>`
    ).join('');
  }

  populateModelSelect(selectEl, provider, currentModel) {
    const models = OfficeApp.PROVIDER_MODELS[provider] || [];
    selectEl.innerHTML = models.map(m =>
      `<option value="${m.value}"${m.value === currentModel ? ' selected' : ''}>${m.label}</option>`
    ).join('');
  }

  updateSpawnDefaults() {
    const provider = document.getElementById('spawnDefaultProvider').value;
    const modelSelect = document.getElementById('spawnDefaultModel');
    const models = OfficeApp.PROVIDER_MODELS[provider] || [];
    this.populateModelSelect(modelSelect, provider, models.length > 0 ? models[0].value : '');
  }

  updateSpawnDefaultModel() {
    // No-op, available for future use
  }

  applySpawnDefaultsToAll() {
    const provider = document.getElementById('spawnDefaultProvider').value;
    const model = document.getElementById('spawnDefaultModel').value;
    const membersInfo = this._currentMembers || OfficeApp.TEAM_MEMBERS_INFO;

    for (const member of membersInfo) {
      const provSelect = document.getElementById(`spawn-provider-${member.id}`);
      const modelSelect = document.getElementById(`spawn-model-${member.id}`);
      if (provSelect) {
        provSelect.value = provider;
        this.populateModelSelect(modelSelect, provider, model);
      }
    }
  }

  updateMemberProvider(memberId) {
    const provSelect = document.getElementById(`spawn-provider-${memberId}`);
    const modelSelect = document.getElementById(`spawn-model-${memberId}`);
    const provider = provSelect.value;
    const models = OfficeApp.PROVIDER_MODELS[provider] || [];
    this.populateModelSelect(modelSelect, provider, models.length > 0 ? models[0].value : '');
  }

  // Palette for member avatars — shared by the editor pane, the rail
  // list, and the review grid so color assignments stay consistent
  // across all three views.
  static get SPAWN_AVATAR_COLORS() {
    return {
      blue:   '#3b82f6',
      purple: '#a855f7',
      pink:   '#ec4899',
      green:  '#22c55e',
      orange: '#f97316',
      yellow: '#eab308',
      red:    '#ef4444',
    };
  }

  renderSpawnMemberRows(defaultProvider, defaultModel) {
    // R22 · Editor markup is rendered into #spawnMemberEditor inside
    // the Members tab. All member editor forms are mounted at once
    // (keyed by spawn-*-${id}) so confirmSpawnTeam can read any of
    // them regardless of which is currently visible; selectSpawnMember
    // toggles display. This keeps the submit flow 100% compatible
    // with the pre-R22 code path.
    const container = document.getElementById('spawnMemberEditor');
    if (!container) return;
    const membersInfo = this._currentMembers || OfficeApp.TEAM_MEMBERS_INFO;
    const avatarColors = OfficeApp.SPAWN_AVATAR_COLORS;
    const colorOptions = Object.keys(avatarColors).map(c =>
      `<option value="${c}">${c.charAt(0).toUpperCase() + c.slice(1)}</option>`
    ).join('');
    const providers = [
      { value: 'claude-subscription', label: 'Claude Subscription' },
      { value: 'claude-api',          label: 'Claude API' },
      { value: 'zai-glm',             label: 'Z.AI GLM' },
    ];

    container.innerHTML = membersInfo.map(member => {
      const bgColor = avatarColors[member.color] || '#7F8C9E';
      const initial = (member.name || '?')[0];
      const safeId = this.escapeAttr(member.id);
      const providerOptions = providers.map(p =>
        `<option value="${p.value}"${p.value === defaultProvider ? ' selected' : ''}>${p.label}</option>`
      ).join('');
      const models = OfficeApp.PROVIDER_MODELS[defaultProvider] || [];
      const modelOptions = models.map(m =>
        `<option value="${m.value}"${m.value === defaultModel ? ' selected' : ''}>${m.label}</option>`
      ).join('');
      const tierOpts = `
        <option value="strategic"${member.tier === 'strategic' ? ' selected' : ''}>Lead</option>
        <option value="worker"${member.tier === 'worker' ? ' selected' : ''}>Worker</option>`;
      const colorOpts = colorOptions.replace(
        `value="${member.color}"`,
        `value="${member.color}" selected`
      );

      return `<div class="spawn-member-editor-form" data-spawn-member-id="${safeId}" hidden>
        <div class="spawn-editor-header">
          <div class="spawn-member-avatar" style="background: ${bgColor}">${this.escapeHtml(initial)}</div>
          <div>
            <h3 class="spawn-editor-title" data-spawn-member-name-display>${this.escapeHtml(member.name || '')}</h3>
            <p class="spawn-editor-subtitle" data-spawn-member-role-display>${this.escapeHtml(member.role || '')}</p>
          </div>
        </div>

        <div class="spawn-editor-grid">
          <div class="spawn-editor-field">
            <label for="spawn-name-${safeId}">Name</label>
            <input class="spawn-config-input" type="text" id="spawn-name-${safeId}"
              value="${this.escapeAttr(member.name || '')}" placeholder="Name"
              oninput="office._syncSpawnRail('${safeId}', 'name', this.value)">
          </div>
          <div class="spawn-editor-field">
            <label for="spawn-role-${safeId}">Role</label>
            <input class="spawn-config-input" type="text" id="spawn-role-${safeId}"
              value="${this.escapeAttr(member.role || '')}" placeholder="Role"
              oninput="office._syncSpawnRail('${safeId}', 'role', this.value)">
          </div>
          <div class="spawn-editor-field">
            <label for="spawn-tier-${safeId}">Tier</label>
            <select class="spawn-config-select" id="spawn-tier-${safeId}"
              onchange="office._syncSpawnRail('${safeId}', 'tier', this.value)">${tierOpts}</select>
          </div>
          <div class="spawn-editor-field">
            <label for="spawn-color-${safeId}">Color</label>
            <select class="spawn-config-select" id="spawn-color-${safeId}"
              onchange="office._syncSpawnRail('${safeId}', 'color', this.value)">${colorOpts}</select>
          </div>
          <div class="spawn-editor-field">
            <label for="spawn-emoji-${safeId}">Emoji</label>
            <input class="spawn-config-input" type="text" id="spawn-emoji-${safeId}"
              value="${this.escapeAttr(member.emoji || '')}" placeholder="Emoji" maxlength="4">
          </div>
          <div class="spawn-editor-field">
            <label for="spawn-provider-${safeId}">Provider</label>
            <select class="spawn-config-select" id="spawn-provider-${safeId}"
              onchange="office.updateMemberProvider('${safeId}')">${providerOptions}</select>
          </div>
          <div class="spawn-editor-field span-2">
            <label for="spawn-model-${safeId}">Model</label>
            <select class="spawn-config-select" id="spawn-model-${safeId}">${modelOptions}</select>
          </div>
        </div>

        <section class="spawn-editor-section">
          <h4 class="spawn-editor-section-title">Soul / Personality</h4>
          <div class="spawn-editor-field" style="margin-bottom:8px">
            <label for="spawn-personality-${safeId}">Personality</label>
            <textarea class="spawn-config-input" id="spawn-personality-${safeId}" rows="4"
              placeholder="Agent personality/character...">${this.escapeHtml(member.personality || '')}</textarea>
          </div>
          <div class="spawn-editor-field">
            <label for="spawn-systemprompt-${safeId}">System prompt</label>
            <textarea class="spawn-config-input" id="spawn-systemprompt-${safeId}" rows="6"
              placeholder="Role-specific system prompt...">${this.escapeHtml(member.systemPrompt || '')}</textarea>
          </div>
        </section>

        <section class="spawn-editor-section">
          <h4 class="spawn-editor-section-title">Advanced</h4>
          <div class="spawn-editor-grid">
            <div class="spawn-editor-field span-2">
              <label for="spawn-workdir-${safeId}">Working directory (optional)</label>
              <input class="spawn-config-input" type="text" id="spawn-workdir-${safeId}"
                placeholder="Overrides the team working directory for this agent">
            </div>
            <div class="spawn-editor-field span-2">
              <label for="spawn-skills-${safeId}">Skills (comma-separated)</label>
              <input class="spawn-config-input" type="text" id="spawn-skills-${safeId}"
                value="${this.escapeAttr((member.skills || []).join(', '))}"
                placeholder="office-protocols, headless-browser, ...">
            </div>
            <!-- R23 · Per-member turn cap. R24 · The maxCostUSD
                 field was removed — subscription plans make a
                 dollar cap meaningless. -->
            <div class="spawn-editor-field">
              <label for="spawn-maxturns-${safeId}">Max turns</label>
              <input class="spawn-config-input" type="number" id="spawn-maxturns-${safeId}"
                min="0" step="1" placeholder="0 = team default (100)"
                value="${member.maxTurns ? member.maxTurns : ''}">
            </div>
            <div class="spawn-editor-field">
              <label for="spawn-spawndelay-${safeId}">Spawn delay (ms after this agent)</label>
              <input class="spawn-config-input" type="number" id="spawn-spawndelay-${safeId}"
                min="0" step="250" max="60000" placeholder="0 = no delay"
                value="${member.spawnDelayMs ? member.spawnDelayMs : ''}">
            </div>
          </div>
        </section>
      </div>`;
    }).join('');

    if (membersInfo.length === 0) {
      container.innerHTML = `<div class="spawn-editor-empty">No members on this template.</div>`;
    }
  }

  // R22 · Renders the left-hand rail listing all members. Clicking
  // an item calls selectSpawnMember(id). Rail entries reflect live
  // edits via _syncSpawnRail so renaming a member in the editor
  // updates the rail label immediately.
  //
  // R23 · Each row now has Duplicate + Remove action buttons; the
  // rail ends with a "+ Add member" button. All three mutate
  // _currentMembers and call _refreshSpawnMembers to re-render.
  _renderSpawnMemberRail() {
    const rail = document.getElementById('spawnMembersList');
    if (!rail) return;
    const members = this._currentMembers || [];
    const colors = OfficeApp.SPAWN_AVATAR_COLORS;
    const rowsHtml = members.map(m => {
      const bg = colors[m.color] || '#7F8C9E';
      const safeId = this.escapeAttr(m.id);
      const initial = (m.name || '?')[0];
      const tierLabel = m.tier === 'strategic' ? 'LEAD' : 'WORKER';
      const tierClass = m.tier === 'strategic' ? 'tier-strategic' : '';
      return `<li class="spawn-member-row-wrap">
        <button class="spawn-member-list-item" data-spawn-rail-id="${safeId}"
          onclick="office.selectSpawnMember('${safeId}')">
          <span class="spawn-member-avatar" style="background:${bg}">${this.escapeHtml(initial)}</span>
          <span class="spawn-member-label">
            <span class="name" data-rail-name>${this.escapeHtml(m.name || m.id)}</span>
            <span class="role" data-rail-role>${this.escapeHtml(m.role || '')}</span>
          </span>
          <span class="spawn-member-tier-chip ${tierClass}" data-rail-tier>${tierLabel}</span>
        </button>
        <div class="spawn-member-actions">
          <button class="spawn-member-action" title="Duplicate member"
            onclick="event.stopPropagation(); office.duplicateSpawnMember('${safeId}')" aria-label="Duplicate">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
          </button>
          <button class="spawn-member-action spawn-member-action--danger" title="Remove member"
            onclick="event.stopPropagation(); office.removeSpawnMember('${safeId}')" aria-label="Remove">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="3 6 5 6 21 6"/><path d="M19 6l-2 14a2 2 0 0 1-2 2H9a2 2 0 0 1-2-2L5 6"/><path d="M10 11v6M14 11v6"/></svg>
          </button>
        </div>
      </li>`;
    }).join('');
    const addBtn = `<li class="spawn-member-add-row">
      <button class="spawn-member-add" onclick="office.addSpawnMember()">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
        Add member
      </button>
    </li>`;
    rail.innerHTML = rowsHtml + addBtn;
    const count = document.getElementById('spawnTabMemberCount');
    if (count) count.textContent = String(members.length);
  }

  // R23 · Regenerate BOTH the rail and the editor panel after a
  // mutation to _currentMembers, then (optionally) select a target
  // member. Keeps the two views coherent without forcing callers to
  // remember both render calls.
  _refreshSpawnMembers(selectId) {
    const settings = this._spawnSettings || {};
    const provider = document.getElementById('spawnDefaultProvider')?.value || settings.provider || 'claude-subscription';
    const model = document.getElementById('spawnDefaultModel')?.value || this.getDefaultModelForProvider(provider, settings);
    this.renderSpawnMemberRows(provider, model);
    this._renderSpawnMemberRail();
    const target = selectId || (this._currentMembers[0] && this._currentMembers[0].id);
    if (target) this.selectSpawnMember(target);
  }

  // R23 · Generate a unique member ID that doesn't collide with any
  // currently-in-memory member. Base on the template's naming style
  // when cloning (e.g. "captain" -> "captain-2") or "member-<n>" for
  // blank adds.
  _uniqueSpawnMemberId(base) {
    const members = this._currentMembers || [];
    const ids = new Set(members.map(m => m.id));
    if (base && !ids.has(base)) return base;
    let n = 2;
    while (ids.has(`${base || 'member'}-${n}`)) n++;
    return `${base || 'member'}-${n}`;
  }

  // R23 · Append a blank worker to the team. Uses sensible defaults
  // (Worker tier, first available color) so the editor opens in a
  // usable state; operator renames and configures from there.
  addSpawnMember() {
    if (!this._currentMembers) this._currentMembers = [];
    const colors = Object.keys(OfficeApp.SPAWN_AVATAR_COLORS);
    const usedColors = new Set(this._currentMembers.map(m => m.color));
    const nextColor = colors.find(c => !usedColors.has(c)) || 'blue';
    const id = this._uniqueSpawnMemberId('member');
    const n = this._currentMembers.length + 1;
    const blank = {
      id,
      name: `Member ${n}`,
      role: 'Engineer',
      tier: 'worker',
      color: nextColor,
      emoji: '',
      personality: '',
      systemPrompt: '',
      skills: [],
    };
    this._currentMembers.push(blank);
    this._refreshSpawnMembers(id);
  }

  // R23 · Duplicate an existing member. Clones all config fields
  // except the ID, assigns a new unique ID, and auto-selects the
  // clone so the operator can edit it immediately.
  duplicateSpawnMember(memberId) {
    const members = this._currentMembers || [];
    const idx = members.findIndex(m => m.id === memberId);
    if (idx < 0) return;
    const src = members[idx];
    const clone = {
      ...src,
      skills: Array.isArray(src.skills) ? [...src.skills] : [],
      id: this._uniqueSpawnMemberId(`${src.id}-copy`),
      name: `${src.name} (copy)`,
    };
    members.splice(idx + 1, 0, clone);
    this._refreshSpawnMembers(clone.id);
  }

  // R23 · Remove a member after a confirmation. Refuses to remove
  // the last remaining member — a team needs at least one agent or
  // the spawn handler will reject it.
  removeSpawnMember(memberId) {
    const members = this._currentMembers || [];
    if (members.length <= 1) {
      this.showNotification('A team needs at least one member', 'warning');
      return;
    }
    const target = members.find(m => m.id === memberId);
    const label = target ? (target.name || target.id) : 'this member';
    if (!window.confirm(`Remove ${label} from the team?`)) return;
    const idx = members.findIndex(m => m.id === memberId);
    if (idx < 0) return;
    members.splice(idx, 1);
    // Prefer the neighbor above the removed row when reselecting.
    const fallback = members[Math.max(0, idx - 1)];
    this._refreshSpawnMembers(fallback ? fallback.id : null);
  }

  // R22 · Live sync of rail labels when the editor mutates
  // name/role/tier/color. Keeps the master-detail view coherent
  // without a full re-render on every keystroke.
  _syncSpawnRail(memberId, field, value) {
    const rail = document.querySelector(`[data-spawn-rail-id="${CSS.escape(memberId)}"]`);
    if (!rail) return;
    if (field === 'name') {
      const el = rail.querySelector('[data-rail-name]');
      if (el) el.textContent = value || memberId;
      const editor = document.querySelector(`[data-spawn-member-id="${CSS.escape(memberId)}"] [data-spawn-member-name-display]`);
      if (editor) editor.textContent = value || memberId;
    } else if (field === 'role') {
      const el = rail.querySelector('[data-rail-role]');
      if (el) el.textContent = value || '';
      const editor = document.querySelector(`[data-spawn-member-id="${CSS.escape(memberId)}"] [data-spawn-member-role-display]`);
      if (editor) editor.textContent = value || '';
    } else if (field === 'tier') {
      const chip = rail.querySelector('[data-rail-tier]');
      if (chip) {
        chip.textContent = value === 'strategic' ? 'LEAD' : 'WORKER';
        chip.classList.toggle('tier-strategic', value === 'strategic');
      }
    } else if (field === 'color') {
      const bg = OfficeApp.SPAWN_AVATAR_COLORS[value] || '#7F8C9E';
      const railAvatar = rail.querySelector('.spawn-member-avatar');
      if (railAvatar) railAvatar.style.background = bg;
      const editorAvatar = document.querySelector(`[data-spawn-member-id="${CSS.escape(memberId)}"] .spawn-editor-header .spawn-member-avatar`);
      if (editorAvatar) editorAvatar.style.background = bg;
    }
  }

  // R22 · Show one editor form at a time and highlight the
  // matching rail item.
  selectSpawnMember(memberId) {
    const editorForms = document.querySelectorAll('[data-spawn-member-id]');
    editorForms.forEach(f => {
      f.hidden = f.dataset.spawnMemberId !== memberId;
    });
    document.querySelectorAll('[data-spawn-rail-id]').forEach(btn => {
      btn.classList.toggle('selected', btn.dataset.spawnRailId === memberId);
    });
    this._selectedSpawnMemberId = memberId;
  }

  // R22 · Tab switching. Shows the matching panel, updates aria +
  // footer controls. The footer's Next/Back buttons only navigate
  // between tabs; the Spawn button replaces Next on the Review tab.
  setSpawnTab(tabName) {
    const order = ['team', 'members', 'review'];
    const idx = order.indexOf(tabName);
    if (idx < 0) return;
    this._currentSpawnTab = tabName;

    document.querySelectorAll('.spawn-tab').forEach(btn => {
      const match = btn.dataset.tab === tabName;
      btn.classList.toggle('active', match);
      btn.setAttribute('aria-selected', match ? 'true' : 'false');
    });
    ['team', 'members', 'review'].forEach(t => {
      const panel = document.getElementById('spawnTab' + t[0].toUpperCase() + t.slice(1));
      if (panel) panel.classList.toggle('active', t === tabName);
    });

    const prevBtn = document.getElementById('spawnPrevTabBtn');
    const nextBtn = document.getElementById('spawnNextTabBtn');
    const spawnBtn = document.getElementById('btnSpawnConfirm');
    if (prevBtn) prevBtn.classList.toggle('hidden', idx === 0);
    if (nextBtn) nextBtn.classList.toggle('hidden', idx === order.length - 1);
    if (spawnBtn) spawnBtn.classList.toggle('hidden', idx !== order.length - 1);

    if (tabName === 'review') this.renderSpawnReview();
  }

  advanceSpawnTab(delta) {
    const order = ['team', 'members', 'review'];
    const idx = order.indexOf(this._currentSpawnTab || 'team');
    const nextIdx = Math.max(0, Math.min(order.length - 1, idx + delta));
    this.setSpawnTab(order[nextIdx]);
  }

  // R22 · Summary view rendered into the Review tab on entry. Pulls
  // live values from the DOM so any in-progress edits are reflected
  // without having to refresh after every keystroke.
  renderSpawnReview() {
    const summary = document.getElementById('spawnReviewSummary');
    const grid = document.getElementById('spawnReviewGrid');
    if (!summary || !grid) return;

    const mission = (document.getElementById('spawnMission')?.value || '').trim();
    const spawnMode = document.querySelector('input[name="spawnMode"]:checked')?.value || 'existing';
    const workdir = spawnMode === 'new'
      ? '/app/workspace/' + (document.getElementById('spawnProjectName')?.value || '…')
      : (document.getElementById('spawnWorkingDir')?.value || '');
    const perpetual = document.getElementById('spawnPerpetualMode')?.checked ? 'On' : 'Off';
    const defProvider = document.getElementById('spawnDefaultProvider')?.value || '';
    const defModel = document.getElementById('spawnDefaultModel')?.value || '';
    const members = this._currentMembers || [];

    summary.innerHTML = `
      <div class="spawn-review-cell" style="grid-column: span 2">
        <span class="k">Mission</span>
        <span class="v mission">${this.escapeHtml(mission || '(none set)')}</span>
      </div>
      <div class="spawn-review-cell">
        <span class="k">Working dir</span>
        <span class="v" title="${this.escapeAttr(workdir)}">${this.escapeHtml(workdir || '(not set)')}</span>
      </div>
      <div class="spawn-review-cell">
        <span class="k">Perpetual</span>
        <span class="v">${perpetual}</span>
      </div>
      <div class="spawn-review-cell">
        <span class="k">Default LLM</span>
        <span class="v">${this.escapeHtml(defProvider)} / ${this.escapeHtml(defModel)}</span>
      </div>
      <div class="spawn-review-cell">
        <span class="k">Members</span>
        <span class="v">${members.length}</span>
      </div>
    `;

    const colors = OfficeApp.SPAWN_AVATAR_COLORS;
    grid.innerHTML = members.map(m => {
      // Read LIVE values from the editor inputs so renames/tier
      // changes show up in the review summary.
      const name = document.getElementById(`spawn-name-${m.id}`)?.value || m.name || m.id;
      const role = document.getElementById(`spawn-role-${m.id}`)?.value || m.role || '';
      const color = document.getElementById(`spawn-color-${m.id}`)?.value || m.color;
      const provider = document.getElementById(`spawn-provider-${m.id}`)?.value || '';
      const model = document.getElementById(`spawn-model-${m.id}`)?.value || '';
      const bg = colors[color] || '#7F8C9E';
      return `<div class="spawn-review-card">
        <span class="spawn-member-avatar" style="background:${bg}">${this.escapeHtml((name || '?')[0])}</span>
        <span style="min-width:0;display:flex;flex-direction:column;gap:1px">
          <span class="name">${this.escapeHtml(name)}</span>
          <span class="meta">${this.escapeHtml(role)} · ${this.escapeHtml(provider)}/${this.escapeHtml(model)}</span>
        </span>
      </div>`;
    }).join('');
  }

  async confirmSpawnTeam() {
    const btn = document.getElementById('btnSpawnConfirm');
    const spawnHeaderBtn = document.getElementById('btnSpawnTeam');
    if (btn) {
      btn.disabled = true;
      btn.textContent = 'Spawning...';
    }
    if (spawnHeaderBtn) {
      spawnHeaderBtn.disabled = true;
      spawnHeaderBtn.textContent = 'Spawning...';
    }

    // R16 · Branch on "Existing folder" vs "New project" mode. In new-
    // project mode we ship {newProject, projectName} instead of
    // workingDir — the backend creates /app/workspace/<name> and seeds
    // a CLAUDE.md before validating the resolved path against the
    // allowlist.
    const spawnModeEl = document.querySelector('input[name="spawnMode"]:checked');
    const spawnMode = spawnModeEl ? spawnModeEl.value : 'existing';
    let workingDir = '';
    let newProject = false;
    let projectName = '';
    if (spawnMode === 'new') {
      newProject = true;
      projectName = (document.getElementById('spawnProjectName')?.value || '').trim();
      if (!/^[a-zA-Z0-9_-]{1,64}$/.test(projectName)) {
        this.showNotification('Project name must be 1-64 chars: letters, numbers, underscores, hyphens only', 'warning');
        if (btn) { btn.disabled = false; btn.textContent = 'Spawn Team'; }
        if (spawnHeaderBtn) { spawnHeaderBtn.disabled = false; spawnHeaderBtn.textContent = 'Spawn Team'; }
        return;
      }
    } else {
      workingDir = document.getElementById('spawnWorkingDir').value.trim();
      if (!workingDir) {
        this.showNotification('Working directory is required', 'warning');
        if (btn) { btn.disabled = false; btn.textContent = 'Spawn Team'; }
        if (spawnHeaderBtn) { spawnHeaderBtn.disabled = false; spawnHeaderBtn.textContent = 'Spawn Team'; }
        return;
      }
    }

    const membersInfo = this._currentMembers || OfficeApp.TEAM_MEMBERS_INFO;
    const memberOverrides = membersInfo.map(member => {
      const provider = document.getElementById(`spawn-provider-${member.id}`)?.value || 'claude-subscription';
      const model = document.getElementById(`spawn-model-${member.id}`)?.value || 'sonnet';
      const override = { id: member.id, provider, model };
      // Include editable fields if they differ from template defaults
      const nameVal = document.getElementById(`spawn-name-${member.id}`)?.value || '';
      const roleVal = document.getElementById(`spawn-role-${member.id}`)?.value || '';
      const tierVal = document.getElementById(`spawn-tier-${member.id}`)?.value || '';
      const colorVal = document.getElementById(`spawn-color-${member.id}`)?.value || '';
      const emojiVal = document.getElementById(`spawn-emoji-${member.id}`)?.value || '';
      const personalityVal = document.getElementById(`spawn-personality-${member.id}`)?.value || '';
      const systemPromptVal = document.getElementById(`spawn-systemprompt-${member.id}`)?.value || '';
      const skillsVal = document.getElementById(`spawn-skills-${member.id}`)?.value || '';
      const workdirVal = document.getElementById(`spawn-workdir-${member.id}`)?.value || '';
      if (nameVal) override.name = nameVal;
      if (roleVal) override.role = roleVal;
      if (tierVal) override.tier = tierVal;
      if (colorVal) override.color = colorVal;
      if (emojiVal) override.emoji = emojiVal;
      if (personalityVal) override.personality = personalityVal;
      if (systemPromptVal) override.systemPrompt = systemPromptVal;
      if (skillsVal) override.skills = skillsVal.split(',').map(s => s.trim()).filter(Boolean);
      if (workdirVal) override.workingDir = workdirVal;
      // R23 · Per-member turn cap + spawn delay. Drop invalid /
      // zero / negative values so the backend treats them as
      // "use defaults". R24 removed the maxCostUSD counterpart.
      const maxTurnsRaw = parseInt(document.getElementById(`spawn-maxturns-${member.id}`)?.value || '', 10);
      if (Number.isFinite(maxTurnsRaw) && maxTurnsRaw > 0) override.maxTurns = maxTurnsRaw;
      const delayRaw = parseInt(document.getElementById(`spawn-spawndelay-${member.id}`)?.value || '', 10);
      if (Number.isFinite(delayRaw) && delayRaw > 0) {
        override.spawnDelayMs = Math.min(delayRaw, 60000);
      }
      return override;
    });

    const telegramEnabled = document.getElementById('spawnTelegramEnabled')?.checked || false;
    const telegramIntervalMin = parseInt(document.getElementById('spawnTelegramInterval')?.value || '60', 10);

    try {
      const mission = document.getElementById('spawnMission')?.value?.trim() || '';
      const perpetualMode = document.getElementById('spawnPerpetualMode')?.checked ?? true;
      // R23's dailyCapUSD payload was removed in R24 — operator uses
      // subscription plans where dollar caps don't apply.
      const data = await this.postJson('/api/office/spawn-team', {
        workingDir,
        mission,
        memberOverrides,
        perpetualMode,
        templateId: this._selectedTemplateId || undefined,
        telegramEnabled,
        telegramIntervalMin,
        newProject,
        projectName,
      });

      if (data.success) {
        this.state.teamSpawned = true;
        if (spawnHeaderBtn) spawnHeaderBtn.textContent = 'Team Active';
        this.closeSpawnConfigModal();
        this.showToast('Team spawned!', 'success');
        if (data.errors && data.errors.length > 0) {
          this.showNotification(`Spawned with warnings: ${data.errors.join('; ')}`, 'warning');
        }
      } else {
        const errMsg = 'Failed to spawn team: ' + (data.error || 'Unknown error');
        this.showNotification(errMsg, 'error');
        this.showToast(errMsg, 'error');
        if (btn) { btn.disabled = false; btn.textContent = 'Spawn Team'; }
        if (spawnHeaderBtn) { spawnHeaderBtn.disabled = false; spawnHeaderBtn.textContent = 'Spawn Team'; }
      }
    } catch (err) {
      console.error('Failed to spawn team:', err);
      // parseJsonResponse throws "HTTP <code>: <body>" on non-2xx. Extract the
      // server's error message so the user sees the actual reason (e.g.
      // "invalid working directory: ...outside the configured allowlist")
      // instead of a generic failure notice.
      let detail = err && err.message ? err.message : '';
      const bodyIdx = detail.indexOf(': ');
      if (bodyIdx >= 0) {
        const body = detail.slice(bodyIdx + 2);
        try {
          const parsed = JSON.parse(body);
          if (parsed && parsed.error) detail = parsed.error;
        } catch (_) {
          // leave detail as the raw "HTTP N: ..." string
        }
      }
      const msg = 'Failed to spawn team: ' + (detail || 'unknown error');
      this.showNotification(msg, 'error');
      this.showToast(msg, 'error');
      if (btn) { btn.disabled = false; btn.textContent = 'Spawn Team'; }
      if (spawnHeaderBtn) { spawnHeaderBtn.disabled = false; spawnHeaderBtn.textContent = 'Spawn Team'; }
    }
  }

  // ========== Template Management ==========

  _editorMembers = [];
  _editingTemplateId = null;

  openTemplateEditor(templateId) {
    this._editingTemplateId = templateId || null;
    if (templateId) {
      const tpl = this._spawnTemplates?.find(t => t.id === templateId);
      if (!tpl) return;
      document.getElementById('templateEditorTitle').textContent = 'Edit Template';
      document.getElementById('tplEditorName').value = tpl.name;
      document.getElementById('tplEditorDesc').value = tpl.description || '';
      document.getElementById('tplEditorCategory').value = tpl.category || 'custom';
      this._editorMembers = tpl.members.map(m => ({...m}));
    } else {
      document.getElementById('templateEditorTitle').textContent = 'Create Custom Template';
      document.getElementById('tplEditorName').value = '';
      document.getElementById('tplEditorDesc').value = '';
      document.getElementById('tplEditorCategory').value = 'custom';
      this._editorMembers = [
        { id: 'agent-1', name: 'Leader', role: 'Tech Lead', tier: 'strategic', color: 'blue', emoji: '', provider: '', model: '' },
        { id: 'agent-2', name: 'Worker', role: 'Developer', tier: 'worker', color: 'green', emoji: '', provider: '', model: '' },
      ];
    }
    this.renderEditorMembers();
    document.getElementById('templateEditorModal').classList.remove('hidden');
  }

  closeTemplateEditor() {
    document.getElementById('templateEditorModal').classList.add('hidden');
  }

  renderEditorMembers() {
    const container = document.getElementById('tplEditorMembers');
    const tierOpts = `<option value="strategic">Lead</option><option value="worker">Worker</option>`;
    const colorOpts = ['blue','purple','pink','green','orange','yellow','red'].map(c =>
      `<option value="${c}">${c.charAt(0).toUpperCase() + c.slice(1)}</option>`
    ).join('');
    // Provider + model selects let operators pin a specific LLM per
    // template member. An empty value means "inherit from team default at
    // spawn time" -- matches how saved templates used to behave silently.
    const providerOpts = [
      ['', 'Inherit'],
      ['claude-subscription', 'Claude Sub'],
      ['claude-api', 'Claude API'],
      ['zai-glm', 'Z.AI GLM'],
    ].map(([v, l]) => `<option value="${v}">${l}</option>`).join('');

    container.innerHTML = this._editorMembers.map((m, i) => {
      // The model options depend on the provider currently selected for
      // THIS member; include every provider's options, then the onchange
      // swap repopulates on demand.
      const modelOpts = this._templateEditorModelOptions(m.provider || '');
      return `<div class="tpl-editor-member">
        <input type="text" value="${this.escapeAttr(m.id)}" onchange="office._editorMembers[${i}].id=this.value" placeholder="ID" style="grid-column:1/2">
        <input type="text" value="${this.escapeAttr(m.name)}" onchange="office._editorMembers[${i}].name=this.value" placeholder="Name">
        <button class="remove-btn" onclick="office.removeEditorMember(${i})" title="Remove">&times;</button>
        <input type="text" value="${this.escapeAttr(m.role)}" onchange="office._editorMembers[${i}].role=this.value" placeholder="Role">
        <select onchange="office._editorMembers[${i}].tier=this.value">${tierOpts.replace(`value="${m.tier}"`, `value="${m.tier}" selected`)}</select>
        <select onchange="office._editorMembers[${i}].color=this.value">${colorOpts.replace(`value="${m.color}"`, `value="${m.color}" selected`)}</select>
        <select onchange="office._onEditorProviderChange(${i}, this.value)">${providerOpts.replace(`value="${m.provider || ''}"`, `value="${m.provider || ''}" selected`)}</select>
        <select onchange="office._editorMembers[${i}].model=this.value">${modelOpts.replace(`value="${this.escapeAttr(m.model || '')}"`, `value="${this.escapeAttr(m.model || '')}" selected`)}</select>
      </div>`;
    }).join('');
  }

  /**
   * Builds the model <option> list for the given provider. Empty provider
   * ("Inherit") gets only a placeholder. Shared by the template editor and
   * the spawn-member grid so the two stay consistent.
   */
  _templateEditorModelOptions(provider) {
    const opts = [['', 'Inherit']];
    const models = OfficeApp.PROVIDER_MODELS[provider] || [];
    for (const m of models) opts.push([m.value, m.label]);
    return opts.map(([v, l]) => `<option value="${this.escapeAttr(v)}">${this.escapeHtml(l)}</option>`).join('');
  }

  _onEditorProviderChange(index, provider) {
    if (!this._editorMembers[index]) return;
    this._editorMembers[index].provider = provider;
    // Changing provider invalidates the previously-selected model.
    this._editorMembers[index].model = '';
    this.renderEditorMembers();
  }

  addEditorMember() {
    const idx = this._editorMembers.length + 1;
    this._editorMembers.push({ id: `agent-${idx}`, name: `Agent ${idx}`, role: 'Developer', tier: 'worker', color: 'green', emoji: '', provider: '', model: '' });
    this.renderEditorMembers();
  }

  removeEditorMember(index) {
    if (this._editorMembers.length <= 1) {
      this.showNotification('Template must have at least one member', 'warning');
      return;
    }
    this._editorMembers.splice(index, 1);
    this.renderEditorMembers();
  }

  async saveTemplate() {
    const name = document.getElementById('tplEditorName').value.trim();
    if (!name) { this.showNotification('Template name is required', 'warning'); return; }
    if (this._editorMembers.length === 0) { this.showNotification('Add at least one member', 'warning'); return; }

    const payload = {
      name,
      description: document.getElementById('tplEditorDesc').value.trim(),
      category: document.getElementById('tplEditorCategory').value,
      members: this._editorMembers,
    };

    try {
      if (this._editingTemplateId) {
        const res = await fetch(`/api/office/templates/${this._editingTemplateId}`, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': this.csrfToken || '' },
          body: JSON.stringify(payload),
        });
        if (!res.ok) throw new Error(await res.text());
      } else {
        await this.postJson('/api/office/templates', payload);
      }
      this.closeTemplateEditor();
      this.showToast('Template saved', 'success');
      // Refresh templates
      const res = await fetch('/api/office/templates');
      const data = await res.json();
      this._spawnTemplates = data.templates || [];
      this.renderTemplateGrid();
    } catch (err) {
      this.showNotification('Failed to save template: ' + err.message, 'error');
    }
  }

  async duplicateTemplate(id) {
    try {
      await this.postJson(`/api/office/templates/${id}/duplicate`, {});
      this.showToast('Template duplicated', 'success');
      const res = await fetch('/api/office/templates');
      const data = await res.json();
      this._spawnTemplates = data.templates || [];
      this.renderTemplateGrid();
    } catch (err) {
      this.showNotification('Failed to duplicate template', 'error');
    }
  }

  async deleteTemplate(id) {
    if (!confirm('Delete this custom template?')) return;
    try {
      await fetch(`/api/office/templates/${id}`, {
        method: 'DELETE',
        headers: { 'X-CSRF-Token': this.csrfToken },
      });
      this.showToast('Template deleted', 'success');
      const res = await fetch('/api/office/templates');
      const data = await res.json();
      this._spawnTemplates = data.templates || [];
      this.renderTemplateGrid();
    } catch (err) {
      this.showNotification('Failed to delete template', 'error');
    }
  }

  openSaveTeamAsTemplate() {
    document.getElementById('saveTeamTplName').value = '';
    document.getElementById('saveTeamTplDesc').value = '';
    document.getElementById('saveTeamTemplateModal').classList.remove('hidden');
  }

  async saveTeamAsTemplate() {
    const name = document.getElementById('saveTeamTplName').value.trim();
    if (!name) { this.showNotification('Template name is required', 'warning'); return; }
    try {
      await this.postJson('/api/office/templates/from-team', {
        name,
        description: document.getElementById('saveTeamTplDesc').value.trim(),
        category: 'custom',
      });
      document.getElementById('saveTeamTemplateModal').classList.add('hidden');
      this.showToast('Team saved as template', 'success');
    } catch (err) {
      this.showNotification('Failed to save team as template', 'error');
    }
  }

  // ========== Actions ==========

  async sendMessage() {
    const fromSelect = document.getElementById('messageFrom');
    const toSelect = document.getElementById('messageTo');
    const input = document.getElementById('messageInput');

    const fromId = fromSelect.value;
    const toId = toSelect.value;
    const content = input.value.trim();

    if (!fromId || !toId || !content) return;
    if (fromId === toId) return;

    try {
      await this.postJson('/api/office/message', {
        fromSessionId: fromId,
        toSessionId: toId,
        content,
      });
      input.value = '';
      this.showToast('Message sent', 'success');
      if (toId === 'all') {
        const count = (this.state.members || []).filter(m => m.sessionId && m.sessionId !== fromId).length;
        this.showNotification(`Message broadcast to ${count} team members`, 'success');
      }
    } catch (err) {
      console.error('Failed to send message:', err);
      this.showToast('Failed to send message', 'error');
    }
  }

  async startStandup() {
    try {
      await this.postJson('/api/office/standup', { action: 'start' });
      this.switchTab('standup');
      this.showToast('Standup started', 'info');
    } catch (err) {
      console.error('Failed to start standup:', err);
      this.showToast('Failed to start standup', 'error');
    }
  }

  async stopStandup() {
    try {
      await this.postJson('/api/office/standup', { action: 'stop' });
    } catch (err) {
      console.error('Failed to stop standup:', err);
    }
  }

  async wrapUpAll() {
    const activeMembers = (this.state.members || []).filter(m => m.sessionId);
    if (activeMembers.length === 0) {
      this.showNotification('No active team members to wrap up', 'info');
      return;
    }

    if (this.state.wrapUp && this.state.wrapUp.active) {
      if (confirm('Wrap-up is already active. Cancel it and resume normal operations?')) {
        try {
          await this.postJson('/api/office/wrap-up/cancel', {});
          this.showNotification('Wrap-up cancelled -- normal operations resumed', 'success');
          this.updateWrapUpButton(false);
        } catch {
          this.showNotification('Failed to cancel wrap-up', 'error');
        }
      }
      return;
    }

    try {
      const result = await this.postJson('/api/office/wrap-up', {});
      if (result.success) {
        const notified = result.sessionIds?.length || 0;
        const cancelled = result.cancelledTasks || 0;
        this.showNotification(
          `Wrap-up initiated: ${notified} agents notified, ${cancelled} pending tasks cancelled`,
          'success'
        );
        this.updateWrapUpButton(true);
      } else {
        this.showNotification(result.error || 'Failed to initiate wrap-up', 'error');
      }
    } catch {
      this.showNotification('Failed to initiate wrap-up', 'error');
    }
  }

  updateWrapUpButton(active) {
    const btn = document.getElementById('btnWrapUp');
    if (!btn) return;

    if (active) {
      btn.className = 'hdr-btn wrapping-up';
      btn.textContent = 'Cancel Wrap-Up';
      btn.title = 'Cancel wrap-up and resume normal operations';
    } else {
      btn.className = 'hdr-btn';
      btn.textContent = 'Wrap Up';
      btn.title = 'Tell all agents to wrap up current work';
    }
  }

  async shutdownTeam() {
    const activeMembers = (this.state.members || []).filter(m => m.sessionId);
    if (activeMembers.length === 0) {
      this.showNotification('No active team members to shut down', 'info');
      return;
    }

    if (!confirm(`Shut down all ${activeMembers.length} team members?`)) return;

    this.shutdownPending = new Set(activeMembers.map(m => m.sessionId));
    this.shutdownTimeouts = new Map();
    this.updateShutdownButton();

    try {
      await this.postJson('/api/office/shutdown-team', {});
      this.showNotification(`Shutdown initiated for ${activeMembers.length} members`, 'info');

      for (const sessionId of this.shutdownPending) {
        const timer = setTimeout(async () => {
          if (this.shutdownPending && this.shutdownPending.has(sessionId)) {
            try {
              await this.deleteJson(`/api/sessions/${sessionId}`);
            } catch {
              // ignore
            }
          }
        }, 90000);
        this.shutdownTimeouts.set(sessionId, timer);
      }
    } catch (err) {
      this.shutdownPending = null;
      this.updateShutdownButton();
      this.showNotification('Failed to initiate shutdown', 'error');
    }
  }

  handleShutdownIdle(sessionId) {
    if (!this.shutdownPending || !this.shutdownPending.has(sessionId)) return;
    this.deleteJson(`/api/sessions/${sessionId}`).catch(() => {});
  }

  handleShutdownMemberLeft(sessionId) {
    if (!this.shutdownPending) return;
    this.shutdownPending.delete(sessionId);
    const timer = this.shutdownTimeouts && this.shutdownTimeouts.get(sessionId);
    if (timer) {
      clearTimeout(timer);
      this.shutdownTimeouts.delete(sessionId);
    }
    this.updateShutdownButton();
  }

  updateShutdownButton() {
    const btn = document.getElementById('btnShutdownTeam');
    if (!btn) return;

    if (this.shutdownPending && this.shutdownPending.size > 0) {
      btn.className = 'hdr-btn shutting-down';
      btn.textContent = `Shutting Down... (${this.shutdownPending.size} left)`;
    } else if (this.shutdownPending && this.shutdownPending.size === 0) {
      btn.className = 'hdr-btn shutdown-complete';
      btn.textContent = 'Team Shut Down';
      this.shutdownTimeouts = null;
      setTimeout(() => {
        this.shutdownPending = null;
        btn.className = 'hdr-btn hdr-btn-danger';
        btn.textContent = 'Shut Down';
      }, 5000);
    } else {
      btn.className = 'hdr-btn hdr-btn-danger';
      btn.textContent = 'Shut Down';
    }
  }

  async sendStandupMessage() {
    const field = document.getElementById('standupInputField');
    if (!field) return;
    const text = field.value.trim();
    if (!text) return;

    let body;
    if (text.startsWith('/task ')) {
      body = { content: text.slice(6), type: 'task' };
    } else {
      body = { content: text, type: 'message' };
    }

    try {
      await this.postJson('/api/office/standup/user-message', body);
      field.value = '';
    } catch (err) {
      console.error('Failed to send standup message:', err);
    }
  }

  // ========== Tasks ==========

  async fetchAndRenderTasks() {
    // Cancel any in-flight Tasks-tab fetches; rapid tab toggling would
    // otherwise stack three-way races whose out-of-order responses
    // stomp the task/queue/lock state.
    if (this._tasksAbortController) {
      try { this._tasksAbortController.abort(); } catch (_) { /* already aborted */ }
    }
    this._tasksAbortController = new AbortController();
    const signal = this._tasksAbortController.signal;
    try {
      const [bugRes, queueRes, lockRes] = await Promise.all([
        fetch('/api/office/bug-tasks', { signal }),
        fetch('/api/office/task-queues', { signal }),
        fetch('/api/office/file-locks', { signal }),
      ]);
      const bugData = bugRes.ok ? await bugRes.json() : { success: false };
      const queueData = queueRes.ok ? await queueRes.json() : { success: false };
      const lockData = lockRes.ok ? await lockRes.json() : { success: false };

      this.bugTasks = bugData.success ? bugData.tasks : [];
      this.taskQueues = queueData.success ? queueData.queues : [];
      this.state.fileLocks = lockData.success ? lockData.locks : [];
      this.renderTasks();
      this.renderKanbanBoard();
    } catch (err) {
      if (err && err.name === 'AbortError') return;
      console.error('Failed to fetch tasks:', err);
    }
  }

  // ---- Tasks filter bar (Round 7 E1) ----
  // AND across categories (query AND priority AND status), OR within
  // a category (priority=high OR priority=med). Matches Linear /
  // GitHub filter conventions.
  _applyTaskFilter(tasks) {
    const f = this._taskFilter;
    if (!f) return tasks;
    const q = (f.q || '').trim().toLowerCase();
    const pri = f.priority || new Set();
    const st = f.status || new Set();
    if (!q && pri.size === 0 && st.size === 0) return tasks;
    return tasks.filter(t => {
      if (q) {
        const hay = `${t.title || ''} ${t.description || ''}`.toLowerCase();
        if (!hay.includes(q)) return false;
      }
      if (pri.size > 0 && !pri.has(t.priority || 'medium')) return false;
      if (st.size > 0) {
        // Map the kanban status chip onto raw task.status values.
        const s = t.status || 'pending';
        const chipMatchesStatus =
          (st.has('pending') && (s === 'pending' || s === 'acknowledged')) ||
          (st.has('in_progress') && s === 'in_progress') ||
          (st.has('blocked') && s === 'blocked') ||
          (st.has('completed') && (s === 'completed' || s === 'failed'));
        if (!chipMatchesStatus) return false;
      }
      return true;
    });
  }

  _onTaskFilterInput(q) {
    this._taskFilter.q = q || '';
    this.renderTasks();
    this.renderKanbanBoard();
  }

  _toggleTaskChip(kind, value) {
    if (!this._taskFilter[kind]) return;
    const set = this._taskFilter[kind];
    if (set.has(value)) set.delete(value); else set.add(value);
    // Reflect chip state in the DOM.
    const sel = `[data-chip="${kind}"][data-value="${value}"]`;
    const btn = document.querySelector(sel);
    if (btn) btn.classList.toggle('active', set.has(value));
    this.renderTasks();
    this.renderKanbanBoard();
  }

  _clearTaskFilters() {
    this._taskFilter = { q: '', priority: new Set(), status: new Set() };
    const input = document.getElementById('taskFilterSearch');
    if (input) input.value = '';
    document.querySelectorAll('.task-filter-chip.active').forEach(b => b.classList.remove('active'));
    this.renderTasks();
    this.renderKanbanBoard();
  }

  // ---- F2: kanban hover quick-edit actions ----
  // These call the same endpoints agents use via the `cm` CLI, so no
  // new backend surface. All handlers are fire-and-forget -- the
  // server broadcasts office:taskUpdated via SSE and our existing
  // listeners re-render on receipt.
  async _kanbanCyclePriority(sessionId, taskId, nextPri) {
    try {
      await this.postJson('/api/office/agent/task-progress', {
        sessionId, taskId, priority: nextPri,
      });
    } catch (err) {
      this.showNotification('Failed to update priority', 'error');
    }
  }
  async _kanbanTaskStart(sessionId, taskId) {
    try {
      await this.postJson('/api/office/agent/task-ack', { sessionId, taskId });
      await this.postJson('/api/office/agent/task-start', { sessionId, taskId });
    } catch (err) {
      this.showNotification('Failed to start task', 'error');
    }
  }
  async _kanbanTaskBlock(sessionId, taskId) {
    const reason = prompt('Why is this blocked?') || '';
    if (!reason) return;
    try {
      await this.postJson('/api/office/agent/task-progress', {
        sessionId, taskId, isBlocked: true, progress: reason,
      });
    } catch (err) {
      this.showNotification('Failed to mark blocked', 'error');
    }
  }
  async _kanbanTaskComplete(sessionId, taskId) {
    try {
      await this.postJson('/api/office/agent/task-complete', { sessionId, taskId });
    } catch (err) {
      this.showNotification('Failed to complete task', 'error');
    }
  }

  renderTasks() {
    const list = document.getElementById('tasksList');
    if (!list) return;

    const bugTasks = this.bugTasks || [];
    const taskQueues = this.taskQueues || [];
    const fileLocks = this.state.fileLocks || [];

    if (bugTasks.length === 0 && taskQueues.length === 0 && fileLocks.length === 0) {
      list.innerHTML = `<div class="tasks-empty">
        <div class="empty-state-title">No active tasks</div>
        <div class="empty-state-sub">Run a standup to distribute work across the team, or trigger <span class="t-mono">/lint</span> to auto-populate bug tasks.</div>
      </div>`;
      return;
    }

    let html = '';

    html += this.renderFileLocks();

    const openBugs = this._applyTaskFilter(bugTasks.filter(t => t.status !== 'resolved'));
    if (openBugs.length > 0) {
      html += '<div class="tasks-section-title">Bug Tasks</div>';
      html += openBugs.map(t => {
        const priorityClass = `priority-${t.priority}`;
        const assignee = this.state.members.find(m => m.sessionId === t.assignedTo);
        const assigneeName = assignee ? assignee.name : 'Unknown';
        return `
          <div class="task-card ${priorityClass}">
            <div class="task-card-header">
              <span class="task-priority">${t.priority.toUpperCase()}</span>
              <span class="task-category">${t.category}</span>
              <span class="task-status task-status-${t.status}">${t.status}</span>
            </div>
            <div class="task-card-file">${this.escapeHtml(t.file)}${t.line ? ':' + t.line : ''}</div>
            <div class="task-card-desc">${this.escapeHtml(t.description)}</div>
            <div class="task-card-footer">
              <span class="task-assignee">Assigned to: ${this.escapeHtml(assigneeName)}</span>
              ${t.status !== 'resolved' ? `<button class="btn-resolve" onclick="office.resolveBugTask('${t.id}')">Resolve</button>` : ''}
            </div>
          </div>
        `;
      }).join('');
    }

    if (taskQueues.length > 0) {
      html += '<div class="tasks-section-title">Sprint Tasks</div>';
      for (const queue of taskQueues) {
        if (queue.tasks.length === 0) continue;
        html += `<div class="task-queue-agent">${this.escapeHtml(queue.agentName)}</div>`;
        html += queue.tasks.map(t => `
          <div class="task-card priority-${t.priority} task-card-clickable" onclick="office.openTaskDetail('${this.escapeAttr(queue.sessionId)}','${this.escapeAttr(t.id)}')">
            <div class="task-card-header">
              <span class="task-priority">${t.priority.toUpperCase()}</span>
              <span class="task-status task-status-${t.status}">${t.status}</span>
            </div>
            <div class="task-card-desc">${this.escapeHtml(t.title)}</div>
          </div>
        `).join('');
      }
    }

    list.innerHTML = html;
  }

  async resolveBugTask(taskId) {
    try {
      await this.putJson(`/api/office/bug-tasks/${taskId}`, { status: 'resolved' });
      this.fetchAndRenderTasks();
    } catch (err) {
      console.error('Failed to resolve bug task:', err);
    }
  }

  handleBugTaskCreated(task) {
    if (!this.bugTasks) this.bugTasks = [];
    this.bugTasks.push(task);
    if (this.activeTab === 'tasks') this.renderTasks();
    this.updateStatusBar();
  }

  handleBugTaskUpdated(task) {
    if (!this.bugTasks) return;
    const idx = this.bugTasks.findIndex(t => t.id === task.id);
    if (idx >= 0) {
      this.bugTasks[idx] = task;
    }
    if (this.activeTab === 'tasks') this.renderTasks();
    this.updateStatusBar();
  }

  handleBugTaskResolved(task) {
    if (!this.bugTasks) return;
    const idx = this.bugTasks.findIndex(t => t.id === task.id);
    if (idx >= 0) this.bugTasks[idx] = task;
    if (this.activeTab === 'tasks') this.renderTasks();
    this.updateStatusBar();
  }

  handleTaskQueueGenerated(data) {
    // Re-fetch full task queues from API (debounced to avoid rapid re-fetches)
    this._scheduleRender(() => this.fetchAndRenderTasks(), 'taskFetch', 300);
    const totalTasks = data.taskCount || 0;
    this.addActivityEntry('task', `Task queues generated: ${totalTasks} tasks for ${data.agentName || 'agent'}`);
    this.addNotification('info', 'Tasks generated', `${totalTasks} tasks for ${data.agentName || 'agent'}`);
    this._scheduleRender(() => this.renderAgentCards(), 'agents', 100);
    this.updateStatusBar();
  }

  handleTaskQueueUpdated(data) {
    // Re-fetch full task queues from API (debounced to avoid rapid re-fetches)
    this._scheduleRender(() => this.fetchAndRenderTasks(), 'taskFetch', 300);
    this._scheduleRender(() => this.renderAgentCards(), 'agents', 100);
    this.updateStatusBar();
    // Round 9 · count task completions for the session-local throughput
    // counter. De-duped via _completedTaskIds so backfill fetches or
    // repeated SSE payloads don't double-count.
    if (data && data.task && (data.task.status === 'completed' || data.task.status === 'failed') && data.task.id && !this._completedTaskIds.has(data.task.id)) {
      this._completedTaskIds.add(data.task.id);
      if (data.task.status === 'completed') this._sessionTasksCompleted++;
    }
    this._scheduleOverviewRefresh();
  }

  // ========== Kanban Board ==========

  renderKanbanBoard() {
    const backlogEl = document.getElementById('kanbanBacklog');
    const todoEl = document.getElementById('kanbanTodo');
    const wipEl = document.getElementById('kanbanWip');
    const reviewEl = document.getElementById('kanbanReview');
    const doneEl = document.getElementById('kanbanDone');
    if (!backlogEl || !todoEl || !wipEl || !reviewEl || !doneEl) return;

    const queues = this.taskQueues || [];
    const allTasksRaw = queues.flatMap(q => (q.tasks || []).map(t => ({ ...t, _sessionId: q.sessionId })));
    // Apply the task-filter-bar filter so the board honors search + chips.
    const allTasks = this._applyTaskFilter(allTasksRaw);

    // 5-column status mapping:
    // pending        -> Backlog
    // acknowledged   -> To Do
    // in_progress    -> In Progress
    // blocked        -> Review
    // completed/failed -> Done
    const backlog = allTasks.filter(t => t.status === 'pending');
    const todo = allTasks.filter(t => t.status === 'acknowledged');
    const inProgress = allTasks.filter(t => t.status === 'in_progress');
    const review = allTasks.filter(t => t.status === 'blocked');
    const done = allTasks.filter(t => t.status === 'completed' || t.status === 'failed');

    // Resolve the owning agent's current status per task so kanban
    // cards in the "doing" column can light up with the border-beam
    // only when the agent is actually computing right now.
    const membersBySid = new Map((this.state.members || []).map(m => [m.sessionId, m]));

    const renderCards = (tasks) => {
      return tasks.map(t => {
        const isDone = t.status === 'completed' || t.status === 'failed';
        const isFailed = t.status === 'failed';
        const isBlocked = t.status === 'blocked';
        const owner = t._sessionId ? membersBySid.get(t._sessionId) : null;
        const ownerWorking = owner && owner.status === 'working';
        const isDoingAndWorking = t.status === 'in_progress' && ownerWorking;
        const cls = [
          t.priority === 'high' ? 'card-high' : '',
          isDone ? 'card-done' : '',
          isBlocked ? 'card-blocked' : '',
          'border-beam',
          isDoingAndWorking ? 'border-beam--active' : '',
        ].filter(Boolean).join(' ');
        const priority = t.priority || 'medium';
        const priorityBadge = `<span class="priority-badge priority-${priority}">${priority}</span>`;
        const failedBadge = isFailed
          ? `<span class="kanban-failed-badge">failed</span>`
          : '';
        const blockedBadge = (t.blockedBy && t.blockedBy.length > 0)
          ? `<span class="kanban-blocked-badge">blocked by ${t.blockedBy.length}</span>`
          : '';
        const clickHandler = t._sessionId
          ? `office.openTaskDetail('${this.escapeAttr(t._sessionId)}','${this.escapeAttr(t.id)}')`
          : `office.switchTab('tasks')`;
        // F2 · hover-only quick-edit strip for mutable tasks. Only
        // render if we know the owning session; bug tasks and orphans
        // don't get the buttons since the existing endpoints need
        // sessionId.
        let quickHtml = '';
        if (t._sessionId && !isDone) {
          const sid = this.escapeAttr(t._sessionId);
          const tid = this.escapeAttr(t.id);
          // Priority cycle: high -> medium -> low -> high
          const nextPri = priority === 'high' ? 'low' : (priority === 'medium' ? 'high' : 'medium');
          const buttons = [];
          buttons.push(`<button class="q-priority" data-pri="${this.escapeAttr(priority)}" title="Cycle priority (click to set to ${nextPri})" onclick="event.stopPropagation(); office._kanbanCyclePriority('${sid}','${tid}','${nextPri}')">${priority}</button>`);
          if (t.status === 'pending' || t.status === 'acknowledged') {
            buttons.push(`<button onclick="event.stopPropagation(); office._kanbanTaskStart('${sid}','${tid}')" title="Start this task">Start</button>`);
          }
          if (t.status === 'in_progress') {
            buttons.push(`<button onclick="event.stopPropagation(); office._kanbanTaskBlock('${sid}','${tid}')" title="Mark blocked">Block</button>`);
            buttons.push(`<button onclick="event.stopPropagation(); office._kanbanTaskComplete('${sid}','${tid}')" title="Mark complete">Done</button>`);
          }
          if (t.status === 'blocked') {
            buttons.push(`<button onclick="event.stopPropagation(); office._kanbanTaskStart('${sid}','${tid}')" title="Unblock / resume">Resume</button>`);
          }
          quickHtml = `<div class="kanban-card-quick">${buttons.join('')}</div>`;
        }
        return `<div class="kanban-card ${cls}" title="${this.escapeAttr(t.title || '')}" onclick="${clickHandler}">
          <div class="kanban-card-title">${this.escapeHtml(t.title || '')}</div>
          ${priorityBadge}${failedBadge}${blockedBadge}
          ${quickHtml}
        </div>`;
      }).join('');
    };

    backlogEl.innerHTML = renderCards(backlog);
    todoEl.innerHTML = renderCards(todo);
    wipEl.innerHTML = renderCards(inProgress);
    reviewEl.innerHTML = renderCards(review);
    doneEl.innerHTML = renderCards(done);

    // WIP limit: one active task per worker. Above that, the In
    // Progress column gets a warn badge (over-limit) so bottlenecks
    // are visible at a glance.
    const teamSize = (this.state && Array.isArray(this.state.members)) ? this.state.members.length : 0;
    const wipLimit = teamSize > 0 ? teamSize : null;

    // Update counts with a subtle pulse when the value changes; tracked
    // per-element so a re-render with unchanged counts stays quiet.
    const setCount = (el, next, limit) => {
      if (!el) return;
      const prev = parseInt(el.dataset.prevCount || '0', 10);
      const overLimit = limit && next > limit;
      el.textContent = overLimit ? `${next}/${limit}` : String(next);
      el.dataset.prevCount = String(next);
      el.classList.toggle('over-limit', !!overLimit);
      if (prev !== next) {
        el.classList.remove('count-pulse');
        // Force reflow so re-adding the class retriggers the animation.
        void el.offsetWidth;
        el.classList.add('count-pulse');
      }
    };
    setCount(document.getElementById('kanbanBacklogCount'), backlog.length, null);
    setCount(document.getElementById('kanbanTodoCount'), todo.length, null);
    setCount(document.getElementById('kanbanWipCount'), inProgress.length, wipLimit);
    setCount(document.getElementById('kanbanReviewCount'), review.length, null);
    setCount(document.getElementById('kanbanDoneCount'), done.length, null);
  }

  // ========== Ad-Hoc Meetings ==========

  handleMeetingStarted(meeting) {
    if (!this.state.meetings) this.state.meetings = [];
    this.state.meetings.push(meeting);
    const typeLabel = meeting.type === 'brainstorm' ? 'Brainstorm' : 'Meeting';
    this.addActivityEntry(
      meeting.type === 'brainstorm' ? 'brainstorm' : 'meeting',
      `${typeLabel} started: ${meeting.topic || meeting.participants.length + ' participants'}`
    );

    for (const sid of meeting.participants) {
      this.updateMemberStatusBySessionId(sid, 'meeting');
    }
  }

  handleMeetingEnded(meeting) {
    if (this.state.meetings) {
      this.state.meetings = this.state.meetings.filter(m => m.id !== meeting.id);
    }
    // Revert meeting participants back to idle status without a full state fetch
    if (meeting.participants) {
      for (const sid of meeting.participants) {
        const member = this.state.members.find(m => m.sessionId === sid);
        if (member && member.status === 'meeting') {
          member.status = 'idle';
          const card = document.querySelector(`.agent-card[data-session-id="${sid}"]`);
          if (card) this.updateAgentCard(card, member);
        }
      }
    }
    // Fallback: schedule a debounced full re-render in case members list is stale
    this._scheduleRender(() => this.renderAgentCards(), 'agents', 200);
  }

  // ========== File Locks ==========

  handleFileLockChanged(data) {
    this.fetchFileLocks();
    const { action, lock } = data;

    if (action === 'locked') {
      this.addActivityEntry('lock', `${lock.agentName} locked ${lock.filePath}`);
    } else if (action === 'unlocked') {
      this.addActivityEntry('lock', `${lock.agentName} unlocked ${lock.filePath}`);
    } else if (action === 'expired') {
      this.addActivityEntry('lock', `Lock expired: ${lock.filePath} (was ${lock.agentName})`);
    }

    if (action === 'locked') {
      this.showNotification(`${lock.agentName} locked ${lock.filePath}`, 'info');
    } else if (action === 'unlocked') {
      this.showNotification(`${lock.agentName} unlocked ${lock.filePath}`, 'info');
    } else if (action === 'expired') {
      this.showNotification(`Lock expired: ${lock.filePath} (was ${lock.agentName})`, 'warning');
    }
  }

  async fetchFileLocks() {
    try {
      const res = await fetch('/api/office/file-locks');
      const data = await res.json();
      this.state.fileLocks = data.success ? data.locks : [];
      if (this.activeTab === 'tasks') this.renderTasks();
    } catch (err) {
      console.error('Failed to fetch file locks:', err);
    }
  }

  renderFileLocks() {
    const locks = this.state.fileLocks || [];
    if (locks.length === 0) return '';

    let html = '<div class="tasks-section-title">File Locks</div>';
    html += locks.map(lock => {
      const remaining = Math.max(0, Math.round((lock.expiresAt - Date.now()) / 60000));
      return `
        <div class="file-lock-card">
          <div class="file-lock-header">
            <span class="file-lock-path">${this.escapeHtml(lock.filePath)}</span>
            <button class="btn-unlock" onclick="office.forceUnlock(decodeURIComponent('${encodeURIComponent(lock.filePath)}'))">Unlock</button>
          </div>
          <div class="file-lock-info">
            <span class="file-lock-agent">${this.escapeHtml(lock.agentName)}</span>
            <span class="file-lock-time">${remaining}m remaining</span>
          </div>
        </div>
      `;
    }).join('');

    return html;
  }

  async forceUnlock(filePath) {
    try {
      const data = await this.deleteJson(`/api/office/file-locks/${encodeURIComponent(filePath)}`);
      if (data && data.success) {
        this.fetchFileLocks();
      }
    } catch (err) {
      console.error('Failed to force unlock:', err);
    }
  }

  // ========== Brainstorm ==========

  async triggerBrainstorm() {
    try {
      const data = await this.postJson('/api/office/brainstorm', {});
      if (!data.success) {
        this.showNotification(data.error || 'Failed to start brainstorm', 'warning');
      }
    } catch (err) {
      console.error('Failed to trigger brainstorm:', err);
    }
  }

  // ========== Activity Feed ==========

  addActivityEntry(type, text) {
    const entry = {
      type,
      text,
      timestamp: Date.now(),
    };
    this.activityLog.push(entry);
    if (this.activityLog.length > this.maxActivityEntries) {
      this.activityLog = this.activityLog.slice(-this.maxActivityEntries);
      // Cap triggered -- reset render counter so next render rebuilds from scratch
      this._lastActivityRenderCount = 0;
    }
    this.persistActivityLog();
    if (this.activeTab === 'activity') {
      // Debounce so rapid bursts of events are coalesced into one DOM update
      this._scheduleRender(() => this.renderActivity(), 'activity', 80);
    }
  }

  persistActivityLog() {
    try {
      const toStore = this.activityLog.slice(-100);
      localStorage.setItem('office_activity_log', JSON.stringify(toStore));
    } catch (err) {
      console.warn('Failed to persist activity log:', err);
    }
  }

  restoreActivityLog() {
    try {
      const stored = localStorage.getItem('office_activity_log');
      if (stored) {
        const entries = JSON.parse(stored);
        if (Array.isArray(entries)) {
          this.activityLog = entries;
        }
      }
    } catch { /* corrupt data */ }
  }

  renderActivity() {
    const feed = document.getElementById('activityFeed');
    if (!feed) return;

    if (this.activityLog.length === 0) {
      feed.innerHTML = '<div class="activity-empty">No activity yet. Events will appear here in real time.</div>';
      this._lastActivityRenderCount = 0;
      return;
    }

    const iconMap = {
      meeting: '&#9679;',
      standup: '&#9650;',
      message: '&#9656;',
      task: '&#9632;',
      lock: '&#9670;',
      system: '&#9675;',
      brainstorm: '&#10038;',
    };

    // F2 · which activity types are drillable (system stays passive).
    const drillableTypes = new Set(['task', 'standup', 'message', 'lock', 'meeting', 'brainstorm']);

    const buildEntryEl = (entry, isNew) => {
      const el = document.createElement('div');
      const drillable = drillableTypes.has(entry.type);
      el.className = `activity-entry type-${entry.type}${isNew ? ' just-added' : ''}${drillable ? ' drillable' : ''}`;
      const time = new Date(entry.timestamp).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
      const icon = iconMap[entry.type] || '&#9675;';
      el.innerHTML = `<span class="activity-time">${time}</span><span class="activity-icon">${icon}</span><span class="activity-text">${this.escapeHtml(entry.text)}</span>`;
      if (drillable) {
        el.setAttribute('role', 'button');
        el.setAttribute('tabindex', '0');
        el.addEventListener('click', () => this._drillActivityEntry(entry));
        el.addEventListener('keydown', (e) => {
          if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); this._drillActivityEntry(entry); }
        });
      }
      return el;
    };

    const prevCount = this._lastActivityRenderCount;
    const currentCount = this.activityLog.length;

    // Capture scroll position BEFORE mutation so auto-tail only fires
    // when the operator was already watching the head of the feed.
    const wasNearTop = feed.scrollTop < 40;

    if (prevCount === 0 || feed.children.length === 0 || feed.querySelector('.activity-empty')) {
      // Full rebuild using documentFragment
      const fragment = document.createDocumentFragment();
      for (let i = currentCount - 1; i >= 0; i--) {
        fragment.appendChild(buildEntryEl(this.activityLog[i], false));
      }
      feed.innerHTML = '';
      feed.appendChild(fragment);
    } else if (currentCount > prevCount) {
      // Incremental prepend: new entries animate in via .just-added.
      const fragment = document.createDocumentFragment();
      for (let i = currentCount - 1; i >= prevCount; i--) {
        fragment.appendChild(buildEntryEl(this.activityLog[i], true));
      }
      feed.insertBefore(fragment, feed.firstChild);
    }

    // If the operator was already at the top, keep them there so new
    // entries remain visible. If they had scrolled down to read history,
    // preserve position -- don't yank them back.
    if (wasNearTop) {
      feed.scrollTop = 0;
    }

    // Re-apply the activity-type chip filter so new entries respect it.
    if (this._activityFilter && this._activityFilter !== 'all') {
      this._setActivityFilter(this._activityFilter);
    }

    this._lastActivityRenderCount = currentCount;
  }

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
  }

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
    const teamDailyBudget = el('settingTeamDailyBudget');
    if (teamDailyBudget) teamDailyBudget.value = settings.teamDailyBudget || 0;
    this._updateMissionCharCount();

    this.updateMissionBanner(settings.teamMission);
    // Round 9 · budget value may have changed → re-evaluate pill + bar.
    this._updateBudgetPill();
  }

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
  }

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
  }

  _updateMissionCharCount() {
    const ta = document.getElementById('settingTeamMission');
    const out = document.getElementById('missionCharCount');
    if (!ta || !out) return;
    const len = ta.value.length;
    const max = parseInt(ta.getAttribute('maxlength') || '1000', 10);
    out.textContent = `${len} / ${max}`;
    out.classList.toggle('near-limit', len >= max * 0.9 && len < max);
    out.classList.toggle('at-limit', len >= max);
  }

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
      case 'teamDailyBudget': {
        const raw = parseFloat(el('settingTeamDailyBudget').value);
        body.teamDailyBudget = Number.isFinite(raw) && raw >= 0 ? raw : 0;
        break;
      }
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
  }

  showSettingsSaved() {
    const status = document.getElementById('settingsSaveStatus');
    if (status) {
      status.textContent = 'Settings saved';
      setTimeout(() => { status.textContent = ''; }, 2000);
    }
  }

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
  }

  // ========== Connection Status & Health ==========

  updateConnectionStatus(status) {
    const dot = document.getElementById('connectionDot');
    const label = document.getElementById('connectionLabel');
    if (!dot || !label) return;

    dot.className = 'connection-dot ' + status;
    const labels = {
      connected: 'Connected',
      disconnected: 'Disconnected',
      reconnecting: 'Reconnecting...',
    };
    label.textContent = labels[status] || status;
  }

  startHealthCheck() {
    this.healthCheckInterval = setInterval(async () => {
      try {
        const res = await fetch('/api/office/health');
        const data = await res.json();
        if (data.success) {
          this.lastHealthCheck = Date.now();
          if (!this.sseConnected) {
            this.updateConnectionStatus('reconnecting');
            if (this.eventSource) this.eventSource.close();
            if (this._sseReconnectTimer) clearTimeout(this._sseReconnectTimer);
            this.sseReconnecting = false;
            this.sseReconnectDelay = 1000;
            this.connectSSE();
          }
        }
      } catch (err) {
        this.updateConnectionStatus('disconnected');
      }
    }, 30000);
  }

  // ========== Status Bar ==========

  getTeamStats() {
    const members = this.state.members || [];
    const working = members.filter(m => m.status === 'working').length;
    const idle = members.filter(m => m.status === 'idle').length;
    const meeting = members.filter(m => m.status === 'meeting').length;
    const stuck = members.filter(m => m.status === 'stuck').length;

    const queues = this.taskQueues || [];
    let totalTasks = 0;
    let completedTasks = 0;
    for (const q of queues) {
      totalTasks += q.tasks.length;
      completedTasks += q.tasks.filter(t => t.status === 'completed' || t.status === 'failed').length;
    }

    return { working, idle, meeting, stuck, totalTasks, completedTasks };
  }

  startStatusBarUpdates() {
    this.updateStatusBar();
    // Status bar is now primarily driven by SSE events. Keep a low-frequency
    // fallback interval (30s) only for the auto-standup countdown display which
    // needs to tick even when no SSE events arrive.
    this.statusBarInterval = setInterval(() => {
      this.updateStatusBar();
    }, 30000);
    // Update uptime counter every second (local timer -- no network needed)
    this.uptimeInterval = setInterval(() => {
      const uptimeEl = document.getElementById('teamActiveTime');
      if (uptimeEl && this.autoStandupConfig.teamSpawnedAt && this.state.teamSpawned) {
        const elapsed = Date.now() - this.autoStandupConfig.teamSpawnedAt;
        const hours = Math.floor(elapsed / 3600000);
        const mins = Math.floor((elapsed % 3600000) / 60000);
        const secs = Math.floor((elapsed % 60000) / 1000);
        uptimeEl.textContent = hours > 0 ? `${hours}h ${mins}m` : (mins > 0 ? `${mins}m ${secs}s` : `${secs}s`);
      }
    }, 1000);
  }

  updateStatusBar() {
    const stats = this.getTeamStats();

    const statWorking = document.getElementById('statWorking');
    const statIdle = document.getElementById('statIdle');
    const statMeeting = document.getElementById('statMeeting');
    const statStuck = document.getElementById('statStuck');
    const statTasks = document.getElementById('statTasks');
    const uptimeEl = document.getElementById('teamActiveTime');
    const standupEl = document.getElementById('autoStandupStatus');
    const brainstormEl = document.getElementById('brainstormCooldownStatus');

    if (statWorking) statWorking.textContent = stats.working;
    if (statIdle) statIdle.textContent = stats.idle;
    if (statMeeting) statMeeting.textContent = stats.meeting;
    if (statStuck) statStuck.textContent = stats.stuck;
    if (statTasks) statTasks.textContent = `${stats.completedTasks}/${stats.totalTasks}`;

    // Feature 6: update progress ring
    const ringFill = document.getElementById('progressRingFill');
    if (ringFill) {
      const pct = stats.totalTasks > 0 ? (stats.completedTasks / stats.totalTasks) * 100 : 0;
      const circumference = 2 * Math.PI * 14;
      const offset = circumference - (pct / 100) * circumference;
      ringFill.style.strokeDashoffset = offset.toFixed(2);
    }

    if (uptimeEl) {
      if (this.autoStandupConfig.teamSpawnedAt && this.state.teamSpawned) {
        const elapsed = Date.now() - this.autoStandupConfig.teamSpawnedAt;
        const hours = Math.floor(elapsed / 3600000);
        const mins = Math.floor((elapsed % 3600000) / 60000);
        uptimeEl.textContent = hours > 0 ? `${hours}h ${mins}m` : `${mins}m`;
      } else {
        uptimeEl.textContent = '--';
      }
    }

    if (standupEl) {
      if (this.state.standupActive) {
        standupEl.textContent = 'ACTIVE';
      } else if (this.autoStandupConfig.enabled && this.autoStandupConfig.nextAt) {
        const remaining = Math.max(0, this.autoStandupConfig.nextAt - Date.now());
        const mins = Math.floor(remaining / 60000);
        const hrs = Math.floor(mins / 60);
        const remMins = mins % 60;
        standupEl.textContent = hrs > 0 ? `in ${hrs}h ${remMins}m` : `in ${remMins}m`;
      } else if (this.autoStandupConfig.enabled) {
        standupEl.textContent = 'ON';
      } else {
        standupEl.textContent = 'OFF';
      }
    }

    if (brainstormEl && this.officeSettings) {
      const lastAt = this.officeSettings.lastBrainstormAt || 0;
      const cooldown = this.officeSettings.brainstormCooldownMs || 600000;
      const elapsed = Date.now() - lastAt;
      if (lastAt > 0 && elapsed < cooldown) {
        const remainMs = cooldown - elapsed;
        const remMins = Math.ceil(remainMs / 60000);
        brainstormEl.textContent = `${remMins}m cooldown`;
      } else {
        brainstormEl.textContent = 'Ready';
      }
    }
  }

  async fetchAutoStandupConfig() {
    try {
      const res = await fetch('/api/office/auto-standup');
      const data = await res.json();
      if (data.success) {
        this.autoStandupConfig = data;
        this.updateStatusBar();
      }
    } catch (err) {
      // Non-critical, ignore
    }
  }

  // ========== Whiteboard ==========

  renderWhiteboard(notes) {
    // No dedicated whiteboard in the new layout -- notes go to activity feed
    if (!notes || notes.length === 0) return;
    for (const note of notes) {
      this.addActivityEntry('system', `[Note] ${note.name}: ${note.text}`);
    }
  }

  handleWhiteboardUpdate(data) {
    if (!this.state.whiteboardNotes) this.state.whiteboardNotes = [];
    const idx = this.state.whiteboardNotes.findIndex(n => n.sessionId === data.sessionId);
    if (idx >= 0) {
      this.state.whiteboardNotes[idx] = data;
    } else {
      this.state.whiteboardNotes.push(data);
    }
    this.addActivityEntry('system', `[Note] ${data.name}: ${data.text}`);
  }

  async fetchWhiteboardNotes() {
    try {
      const res = await fetch('/api/office/whiteboard');
      const data = await res.json();
      if (data.success) {
        this.state.whiteboardNotes = data.notes;
      }
    } catch {
      // Silently fail -- whiteboard is non-critical
    }
  }

  // ========== Metrics Dashboard ==========

  async fetchAndRenderMetrics() {
    const dashboard = document.getElementById('metricsDashboard');
    if (!dashboard) return;

    try {
      const res = await fetch('/api/office/metrics');
      const data = await res.json();
      if (!data.success) {
        dashboard.innerHTML = '<div class="metrics-loading">Failed to load metrics</div>';
        return;
      }

      const m = data.metrics;
      const responseRatePct = Math.round((m.averageStandupResponseRate || 0) * 100);
      const costStr = m.totalCost >= 1 ? '$' + m.totalCost.toFixed(2) : (m.totalCost * 100).toFixed(0) + 'c';

      const uptimeEntries = Object.entries(m.agentUptime || {});
      let uptimeHtml = '';
      if (uptimeEntries.length > 0) {
        uptimeHtml = uptimeEntries.map(([sid, ms]) => {
          const member = (this.state.members || []).find(mem => mem.sessionId === sid);
          const name = member ? member.name : sid.slice(0, 8);
          const hours = (ms / 3600000).toFixed(1);
          return `<div class="metric-uptime-row"><span>${this.escapeHtml(name)}</span><span>${hours}h</span></div>`;
        }).join('');
      }

      // Token usage breakdown
      const inputTokens = m.totalInputTokens || 0;
      const outputTokens = m.totalOutputTokens || 0;
      const totalTokens = inputTokens + outputTokens;
      const inputPct = totalTokens > 0 ? Math.round((inputTokens / totalTokens) * 100) : 50;
      const outputPct = totalTokens > 0 ? 100 - inputPct : 50;
      const costFull = (m.totalCost || 0).toFixed(4);
      const inputK = totalTokens > 0 ? (inputTokens / 1000).toFixed(1) + 'k' : '0';
      const outputK = totalTokens > 0 ? (outputTokens / 1000).toFixed(1) + 'k' : '0';

      const tokenSectionHtml = `
        <div class="metric-card-wide token-usage-section">
          <div class="metric-label" style="margin-bottom:10px">Token Usage</div>
          <div class="token-bar">
            <div class="token-segment input" style="width:${inputPct}%" title="Input: ${inputTokens.toLocaleString()} tokens"></div>
            <div class="token-segment output" style="width:${outputPct}%" title="Output: ${outputTokens.toLocaleString()} tokens"></div>
          </div>
          <div class="token-legend">
            <span class="legend-item"><span class="legend-dot input"></span> Input: ${inputK}</span>
            <span class="legend-item"><span class="legend-dot output"></span> Output: ${outputK}</span>
            <span class="legend-item"><span class="legend-dot cost"></span> Cost: $${costFull}</span>
          </div>
        </div>
      `;

      dashboard.innerHTML = `
        <div class="metrics-grid">
          <div class="metric-card"><div class="metric-value">${m.totalStandupsCompleted}</div><div class="metric-label">Standups</div></div>
          <div class="metric-card"><div class="metric-value">${m.totalTasksCompleted}</div><div class="metric-label">Tasks Done</div></div>
          <div class="metric-card"><div class="metric-value">${m.totalBugTasksCreated}</div><div class="metric-label">Bugs Filed</div></div>
          <div class="metric-card"><div class="metric-value">${m.totalBugTasksResolved}</div><div class="metric-label">Bugs Fixed</div></div>
          <div class="metric-card"><div class="metric-value">${m.totalRecoveryAttempts}</div><div class="metric-label">Recoveries</div></div>
          <div class="metric-card"><div class="metric-value">${m.totalStuckAlerts}</div><div class="metric-label">Stuck Alerts</div></div>
          <div class="metric-card"><div class="metric-value">${responseRatePct}%</div><div class="metric-label">Avg Response</div></div>
          <div class="metric-card"><div class="metric-value">${costStr}</div><div class="metric-label">Total Cost</div></div>
        </div>
        ${tokenSectionHtml}
        ${uptimeHtml ? `<div class="metric-card-wide"><div class="metric-label" style="margin-bottom:8px">Agent Uptime</div>${uptimeHtml}</div>` : ''}
      `;
    } catch (err) {
      dashboard.innerHTML = '<div class="metrics-loading">Error loading metrics</div>';
    }
  }

  // ========== Standup History ==========

  async toggleStandupHistory() {
    const list = document.getElementById('standupHistoryList');
    if (!list) return;

    if (!list.classList.contains('hidden')) {
      list.classList.add('hidden');
      return;
    }

    list.innerHTML = '<div class="standup-history-loading">Loading...</div>';
    list.classList.remove('hidden');

    try {
      const res = await fetch('/api/office/standup/history');
      const data = await res.json();
      if (!data.success || !data.history || data.history.length === 0) {
        list.innerHTML = '<div class="standup-history-empty">No standup history yet.</div>';
        return;
      }

      list.innerHTML = data.history.map(s => {
        const date = new Date(s.startedAt);
        const dateStr = date.toLocaleDateString([], { month: 'short', day: 'numeric' });
        const timeStr = date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
        const ratePct = Math.round((s.responseRate || 0) * 100);
        // F2 · response-rate bar with tier colors so history is
        // visually scannable at a glance.
        let tier = 'ok';
        if (ratePct < 50) tier = 'alert';
        else if (ratePct < 75) tier = 'warn';
        return `<div class="standup-history-item" onclick="office.viewStandupDetail(decodeURIComponent('${encodeURIComponent(s.id)}'))">
          <div class="standup-history-item-row">
            <span class="standup-history-date">${dateStr} ${timeStr}</span>
            <span class="standup-history-meta">${s.participantCount} agents</span>
          </div>
          <div class="standup-response-bar">
            <div class="standup-response-bar-track">
              <div class="standup-response-bar-fill" data-tier="${tier}" style="width:${ratePct}%"></div>
            </div>
            <span class="standup-response-bar-pct">${ratePct}%</span>
          </div>
        </div>`;
      }).join('');
    } catch {
      list.innerHTML = '<div class="standup-history-empty">Error loading history.</div>';
    }
  }

  async viewStandupDetail(standupId) {
    const transcript = document.getElementById('standupTranscript');
    if (!transcript) return;

    const historyList = document.getElementById('standupHistoryList');
    if (historyList) historyList.classList.add('hidden');

    transcript.innerHTML = '<div class="standup-loading">Loading transcript...</div>';

    try {
      const res = await fetch(`/api/office/standup/${encodeURIComponent(standupId)}`);
      const data = await res.json();
      if (!data.success || !data.standup) {
        transcript.innerHTML = '<div class="standup-empty">Standup not found.</div>';
        return;
      }

      const s = data.standup;
      const dateStr = new Date(s.startedAt).toLocaleString();
      let html = `<div class="standup-history-header">
        <span class="standup-history-label">Standup from ${this.escapeHtml(dateStr)}</span>
      </div>`;

      for (const msg of s.transcript) {
        const time = new Date(msg.timestamp).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
        const cls = msg.type === 'takeaway' ? 'standup-msg-takeaway' : (msg.type === 'opening' || msg.type === 'closing' ? 'standup-msg-system' : '');
        html += `<div class="standup-message ${cls}">
          <span class="standup-msg-time">${time}</span>
          <span class="standup-msg-name">${this.escapeHtml(msg.speakerName)}</span>
          <div class="standup-msg-content">${this.escapeHtml(msg.content)}</div>
        </div>`;
      }

      transcript.innerHTML = html;
    } catch {
      transcript.innerHTML = '<div class="standup-empty">Error loading standup.</div>';
    }
  }

  // ========== Keyboard Shortcuts ==========

  setupKeyboardShortcuts() {
    document.addEventListener('keydown', (e) => {
      // Feature 4: Cmd+K opens command palette
      if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
        e.preventDefault();
        this.openCommandPalette();
        return;
      }

      if (e.key === 'Escape') {
        // Close command palette first if open (top of z-stack)
        const palette = document.getElementById('commandPalette');
        if (palette && !palette.classList.contains('hidden')) {
          this.closeCommandPalette();
          return;
        }
        const reviewModal = document.getElementById('reviewModal');
        if (reviewModal && !reviewModal.classList.contains('hidden')) {
          this.closeReview();
          return;
        }
        const modal = document.getElementById('memberModal');
        if (modal && !modal.classList.contains('hidden')) {
          this.closeMemberModal();
          return;
        }
        // Spawn / template-editor / task-detail modals also deserve
        // Escape handling -- previously the user had to click the X
        // or click outside, inconsistent with the three modals above.
        const spawnModal = document.getElementById('spawnConfigModal');
        if (spawnModal && !spawnModal.classList.contains('hidden')) {
          this.closeSpawnConfigModal();
          return;
        }
        const tplEditor = document.getElementById('templateEditorModal');
        if (tplEditor && !tplEditor.classList.contains('hidden')) {
          this.closeTemplateEditor();
          return;
        }
        const taskModal = document.getElementById('taskDetailModal');
        if (taskModal && !taskModal.classList.contains('hidden')) {
          this.closeTaskDetail();
          return;
        }
        this.closeDetail();
        return;
      }

      const tag = e.target.tagName;
      if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return;

      if ((e.ctrlKey || e.metaKey) && e.key >= '1' && e.key <= '7') {
        e.preventDefault();
        const tabs = ['agents', 'tasks', 'standup', 'messages', 'metrics', 'activity', 'settings'];
        const idx = parseInt(e.key) - 1;
        if (tabs[idx]) this.switchTab(tabs[idx]);
        return;
      }

      if (e.key === 's' && !e.ctrlKey && !e.metaKey) {
        if (!this.state.standupActive) {
          this.startStandup();
        }
        return;
      }

      if (e.key === 'b' && !e.ctrlKey && !e.metaKey) {
        this.triggerBrainstorm();
        return;
      }
    });
  }

  // ========== Command Palette ==========

  openCommandPalette() {
    const palette = document.getElementById('commandPalette');
    if (!palette) return;
    palette.classList.remove('hidden');
    this._cmdPaletteActive = -1;
    const input = document.getElementById('cmdInput');
    if (input) {
      input.value = '';
      input.focus();
    }
    this.renderCommandResults('');
  }

  closeCommandPalette() {
    const palette = document.getElementById('commandPalette');
    if (palette) palette.classList.add('hidden');
    this._cmdPaletteActive = -1;
  }

  getCommands() {
    // Category-tagged so renderCommandResults can group with headers.
    // Shortcut hints mirror the actual keybindings set up in
    // _installShortcutBindings.
    const tabs = [
      { tab: 'agents',   label: 'Go to Agents',   shortcut: 'g a' },
      { tab: 'tasks',    label: 'Go to Tasks',    shortcut: 'g k' },
      { tab: 'standup',  label: 'Go to Standup',  shortcut: 'g s' },
      { tab: 'messages', label: 'Go to Chat',     shortcut: 'g c' },
      { tab: 'timeline', label: 'Go to Timeline', shortcut: 'g t' },
      { tab: 'insights', label: 'Go to Insights', shortcut: 'g i' },
      { tab: 'meetings', label: 'Go to Meetings', shortcut: 'g m' },
      { tab: 'network',  label: 'Go to Network',  shortcut: 'g n' },
      { tab: 'reports',  label: 'Go to Reports',  shortcut: 'g r' },
      { tab: 'metrics',  label: 'Go to Metrics',  shortcut: '' },
      { tab: 'activity', label: 'Go to Activity', shortcut: '' },
      { tab: 'settings', label: 'Go to Settings', shortcut: 'g g' },
    ];

    const commands = [
      { category: 'Actions', name: 'Spawn Team',         icon: '+', shortcut: '', action: () => this.spawnTeam() },
      { category: 'Actions', name: 'Start Standup',      icon: '>', shortcut: '', action: () => { this.startStandup(); } },
      { category: 'Actions', name: 'Trigger Brainstorm', icon: '*', shortcut: '', action: () => this.triggerBrainstorm() },
      { category: 'Actions', name: 'Wrap Up All',        icon: '~', shortcut: '', action: () => this.wrapUpAll() },
      { category: 'Actions', name: 'Shutdown Team',      icon: '!', shortcut: '', action: () => this.shutdownTeam() },
    ];

    for (const t of tabs) {
      commands.push({ category: 'Navigation', name: t.label, icon: '#', shortcut: t.shortcut, action: () => this.switchTab(t.tab) });
    }

    // Per-agent open commands so fuzzy-search by name jumps straight to a profile.
    const members = this.state && this.state.members ? this.state.members : [];
    for (const m of members) {
      if (!m || !m.sessionId) continue;
      const name = m.name || m.sessionId;
      commands.push({
        category: 'Agents',
        name: `Open agent: ${name}`,
        icon: '@',
        shortcut: '',
        action: () => this.showDetail(m.sessionId),
      });
    }

    return commands;
  }

  renderCommandResults(query) {
    const resultsEl = document.getElementById('cmdResults');
    if (!resultsEl) return;

    const commands = this.getCommands();
    const q = query.trim().toLowerCase();

    const filtered = q
      ? commands.filter(c => c.name.toLowerCase().includes(q))
      : commands;

    if (filtered.length === 0) {
      resultsEl.innerHTML = '<div class="cmd-empty">No matching commands</div>';
      return;
    }

    // Group by category, preserving insertion order. When the user has
    // typed a query, skip grouping so the most relevant matches appear
    // flat at the top.
    const groupOrder = [];
    const byCat = new Map();
    for (const cmd of filtered) {
      const cat = cmd.category || 'Other';
      if (!byCat.has(cat)) { byCat.set(cat, []); groupOrder.push(cat); }
      byCat.get(cat).push(cmd);
    }

    const renderItem = (cmd, idx) => {
      const nameHtml = q
        ? cmd.name.replace(new RegExp(`(${q.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')})`, 'gi'),
          '<span class="cmd-result-match">$1</span>')
        : cmd.name;
      const activeClass = idx === this._cmdPaletteActive ? ' active' : '';
      const shortcutHtml = cmd.shortcut
        ? `<span class="cmd-result-shortcut">${this.escapeHtml(cmd.shortcut)}</span>`
        : '';
      return `<div class="cmd-result-item${activeClass}" data-cmd-idx="${idx}" onclick="office._executeCmdByIndex(${idx})">
        <span class="cmd-result-icon">${this.escapeHtml(cmd.icon)}</span>
        <span class="cmd-result-name">${nameHtml}</span>
        ${shortcutHtml}
      </div>`;
    };

    if (q) {
      // Flat list while searching -- user wants the best match front and center.
      resultsEl.innerHTML = filtered.map((cmd, idx) => renderItem(cmd, idx)).join('');
    } else {
      // Grouped with category headers. idx is the position in the
      // flat `filtered` array so keyboard nav still works.
      let idx = 0;
      const chunks = [];
      for (const cat of groupOrder) {
        chunks.push(`<div class="cmd-group-title">${this.escapeHtml(cat)}</div>`);
        for (const cmd of byCat.get(cat)) {
          chunks.push(renderItem(cmd, idx));
          idx++;
        }
      }
      resultsEl.innerHTML = chunks.join('');
    }

    this._cmdFilteredCommands = filtered;
  }

  _executeCmdByIndex(idx) {
    const cmds = this._cmdFilteredCommands || this.getCommands();
    if (cmds[idx]) {
      this.closeCommandPalette();
      cmds[idx].action();
    }
  }

  handleCommandPaletteKey(e) {
    const results = document.getElementById('cmdResults');
    if (!results) return;
    const items = results.querySelectorAll('.cmd-result-item');
    const count = items.length;

    if (e.key === 'ArrowDown') {
      e.preventDefault();
      this._cmdPaletteActive = Math.min(this._cmdPaletteActive + 1, count - 1);
      this.highlightCmdItem(items);
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      this._cmdPaletteActive = Math.max(this._cmdPaletteActive - 1, 0);
      this.highlightCmdItem(items);
    } else if (e.key === 'Enter') {
      e.preventDefault();
      const activeIdx = this._cmdPaletteActive >= 0 ? this._cmdPaletteActive : 0;
      this._executeCmdByIndex(activeIdx);
    }
  }

  highlightCmdItem(items) {
    items.forEach((item, idx) => {
      item.classList.toggle('active', idx === this._cmdPaletteActive);
    });
    if (this._cmdPaletteActive >= 0 && items[this._cmdPaletteActive]) {
      items[this._cmdPaletteActive].scrollIntoView({ block: 'nearest' });
    }
  }

  // ========== Feature: Session Report Dashboard ==========

  renderReportDashboard() {
    const el = document.getElementById('reportContent');
    if (!el) return;

    el.innerHTML = '<div class="report-grid"><div class="report-card skeleton" style="height:180px"></div><div class="report-card skeleton" style="height:180px"></div><div class="report-card skeleton" style="height:180px"></div></div>';

    Promise.all([
      fetch('/api/office/metrics').then(r => r.json()).catch(() => ({})),
      fetch('/api/office/status').then(r => r.json()).catch(() => ({})),
      fetch('/api/office/experiments').then(r => r.json()).catch(() => ({ experiments: [] })),
    ]).then(([metricsData, statusData, expData]) => {
      const m = metricsData?.metrics || {};
      const members = statusData?.members || [];
      const experiments = Array.isArray(expData) ? expData : (expData?.experiments || []);
      const working = members.filter(mem => mem.status === 'working').length;

      el.innerHTML = `
        <div class="report-header">
          <h3>Team Report</h3>
          <div class="report-meta">
            <span class="report-updated">Updated ${new Date().toLocaleTimeString()}</span>
            <button class="btn btn-sm" onclick="office.renderReportDashboard()">Refresh</button>
          </div>
        </div>
        <div class="report-grid">
          <div class="report-card">
            <h4>Overview</h4>
            <div class="report-stat-row"><span>Team Size</span><span>${members.length}</span></div>
            <div class="report-stat-row"><span>Active</span><span>${working}</span></div>
            <div class="report-stat-row"><span>Uptime</span><span>${m.uptime || '0s'}</span></div>
            <div class="report-stat-row"><span>Standups</span><span>${m.totalStandupsCompleted || 0}</span></div>
          </div>
          <div class="report-card">
            <h4>Tasks</h4>
            <div class="report-stat-row"><span>Total</span><span>${m.totalTasks || 0}</span></div>
            <div class="report-stat-row"><span>Completed</span><span>${m.totalTasksCompleted || 0}</span></div>
            <div class="report-stat-row"><span>Success Rate</span><span>${(m.totalTasks || 0) > 0 ? Math.round(((m.totalTasksCompleted || 0) / m.totalTasks) * 100) : 0}%</span></div>
          </div>
          <div class="report-card">
            <h4>Cost &amp; Tokens</h4>
            <div class="report-stat-row"><span>Input Tokens</span><span>${(m.totalInputTokens || 0).toLocaleString()}</span></div>
            <div class="report-stat-row"><span>Output Tokens</span><span>${(m.totalOutputTokens || 0).toLocaleString()}</span></div>
            <div class="report-stat-row"><span>Total Cost</span><span>$${(m.totalCost || 0).toFixed(2)}</span></div>
          </div>
          <div class="report-card">
            <h4>Quality</h4>
            <div class="report-stat-row"><span>Bugs Created</span><span>${m.totalBugTasksCreated || 0}</span></div>
            <div class="report-stat-row"><span>Bugs Resolved</span><span>${m.totalBugTasksResolved || 0}</span></div>
            <div class="report-stat-row"><span>Recoveries</span><span>${m.totalRecoveryAttempts || 0}</span></div>
            <div class="report-stat-row"><span>Stuck Alerts</span><span>${m.totalStuckAlerts || 0}</span></div>
          </div>
          <div class="report-card">
            <h4>Communication</h4>
            <div class="report-stat-row"><span>Messages</span><span>${m.totalMessages || 0}</span></div>
            <div class="report-stat-row"><span>Reflections</span><span>${m.totalReflections || 0}</span></div>
            <div class="report-stat-row"><span>Skills Discovered</span><span>${m.totalSkillsDiscovered || 0}</span></div>
            <div class="report-stat-row"><span>Curiosity Sessions</span><span>${m.totalCuriosity || 0}</span></div>
          </div>
          <div class="report-card report-card-wide">
            <h4>Team Health Assessment</h4>
            ${this._renderHealthScore(m, members)}
          </div>
          <div class="report-card report-card-wide">
            <h4>Agent Communication Graph</h4>
            <div id="agentGraphContainer" class="agent-graph-container"></div>
          </div>
          <div class="report-card report-card-wide">
            <h4>Experiment History</h4>
            ${this._renderExperimentHistory(experiments)}
          </div>
        </div>
      `;

      this.renderAgentGraph();
    }).catch(() => {
      el.innerHTML = '<div class="empty-state"><div class="empty-icon">!</div><div class="empty-text">Failed to load report</div><div class="empty-hint">Check your connection and try again</div><button class="btn btn-sm" style="margin-top:8px" onclick="office.renderReportDashboard()">Retry</button></div>';
    });
  }

  // F5 · build a markdown snapshot of the team state. Pure function on
  // this.state / this.taskQueues / this.officeSettings — no fetch, no
  // re-render — so the same text can power both Copy and Download.
  _buildReportMarkdown() {
    const now = new Date();
    const dateStr = now.toISOString().slice(0, 10);
    const timeStr = now.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
    const members = (this.state && Array.isArray(this.state.members)) ? this.state.members : [];
    const queues = this.taskQueues || [];
    const settings = this.officeSettings || {};

    // Totals across the team.
    const totalCost = members.reduce((s, m) => s + (Number(m.totalCost) || 0), 0);
    const counts = { working: 0, idle: 0, meeting: 0, stuck: 0 };
    for (const m of members) { if (counts[m.status] !== undefined) counts[m.status]++; }

    let totalTasks = 0, completedTasks = 0, blockedTasks = 0;
    const blockers = [];
    for (const q of queues) {
      const owner = members.find(m => m.sessionId === q.sessionId);
      for (const t of (q.tasks || [])) {
        totalTasks++;
        if (t.status === 'completed' || t.status === 'failed') completedTasks++;
        if (t.status === 'blocked') {
          blockedTasks++;
          blockers.push({ title: t.title || '', agent: owner ? owner.name : '', since: t.assignedAt });
        }
      }
    }

    const asciiBar = (pctRaw) => {
      const pct = Math.max(0, Math.min(100, Math.round(pctRaw)));
      const filled = Math.round((pct / 100) * 10);
      return '▰'.repeat(filled) + '▱'.repeat(10 - filled);
    };

    const lines = [];
    lines.push(`# Jarvis Office Report — ${dateStr} ${timeStr}`);
    lines.push('');
    if (settings.teamMission) {
      lines.push(`> **Mission:** ${settings.teamMission}`);
      lines.push('');
    }

    lines.push('## Team at a glance');
    lines.push('');
    lines.push(`- **Members:** ${members.length} (${counts.working} working · ${counts.idle} idle · ${counts.meeting} meeting · ${counts.stuck} stuck)`);
    lines.push(`- **Tasks:** ${completedTasks} / ${totalTasks} completed${totalTasks > 0 ? ` · ${Math.round((completedTasks / totalTasks) * 100)}%` : ''}`);
    if (blockedTasks > 0) lines.push(`- **Blocked:** ${blockedTasks} task${blockedTasks === 1 ? '' : 's'}`);
    lines.push(`- **Spend today:** $${totalCost.toFixed(4)}${settings.teamDailyBudget > 0 ? ` of $${settings.teamDailyBudget} budget (${asciiBar((totalCost / settings.teamDailyBudget) * 100)} ${Math.round((totalCost / settings.teamDailyBudget) * 100)}%)` : ''}`);
    lines.push('');

    lines.push('## Agents');
    lines.push('');
    lines.push('| Agent | Role | Status | Tasks | Spend |');
    lines.push('|---|---|---|---|---|');
    for (const m of members) {
      const q = queues.find(qq => qq.sessionId === m.sessionId);
      const ts = q ? q.tasks || [] : [];
      const done = ts.filter(t => t.status === 'completed' || t.status === 'failed').length;
      const cost = (Number(m.totalCost) || 0).toFixed(4);
      const star = this.isPinned(m) ? '⭐ ' : '';
      lines.push(`| ${star}${m.name || '—'} | ${m.role || '—'} | ${m.status || '—'} | ${done}/${ts.length} | $${cost} |`);
    }
    lines.push('');

    if (blockers.length > 0) {
      lines.push('## Top blockers');
      lines.push('');
      blockers.slice(0, 5).forEach((b, i) => {
        lines.push(`${i + 1}. **${b.title}** — ${b.agent || 'unassigned'}${b.since ? ` (since ${new Date(b.since).toLocaleString()})` : ''}`);
      });
      lines.push('');
    }

    // Recent standup response-rate, if history is cached.
    const lastStandup = this.lastCompletedStandup;
    if (lastStandup && typeof lastStandup.responseRate === 'number') {
      const pct = Math.round(lastStandup.responseRate * 100);
      lines.push('## Last standup');
      lines.push('');
      lines.push(`- **Response rate:** ${asciiBar(pct)} ${pct}%`);
      if (lastStandup.completedAt) lines.push(`- **Completed:** ${new Date(lastStandup.completedAt).toLocaleString()}`);
      lines.push('');
    }

    lines.push('---');
    lines.push(`_Generated from the Jarvis office dashboard._`);
    return lines.join('\n');
  }

  async exportReportMarkdown() {
    const md = this._buildReportMarkdown();
    const btn = document.getElementById('btnCopyReport');
    try {
      if (navigator.clipboard && navigator.clipboard.writeText) {
        await navigator.clipboard.writeText(md);
      } else {
        // Fallback: hidden textarea + execCommand('copy'). Works in
        // non-secure contexts but is being deprecated by browsers.
        const ta = document.createElement('textarea');
        ta.value = md;
        ta.style.position = 'fixed';
        ta.style.opacity = '0';
        document.body.appendChild(ta);
        ta.select();
        document.execCommand('copy');
        document.body.removeChild(ta);
      }
      if (btn) {
        const original = btn.textContent;
        btn.textContent = 'Copied ✓';
        btn.classList.add('copied');
        setTimeout(() => { btn.textContent = original; btn.classList.remove('copied'); }, 1200);
      }
      this.showToast('Report copied', 'success');
    } catch (err) {
      console.error('Copy failed:', err);
      this.showToast('Copy failed — try Download .md instead', 'error');
    }
  }

  downloadReportMarkdown() {
    const md = this._buildReportMarkdown();
    const dateStr = new Date().toISOString().slice(0, 10);
    const blob = new Blob([md], { type: 'text/markdown;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `jarvis-report-${dateStr}.md`;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    // Release the blob URL after a tick so the browser commits the download.
    setTimeout(() => URL.revokeObjectURL(url), 1000);
    this.showToast('Report downloaded', 'success');
  }

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
  }

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
  }

  // ========== Feature: Agent Relationship Mini-Graph ==========

  renderAgentGraph() {
    const container = document.getElementById('agentGraphContainer');
    if (!container) return;

    const members = this.state.members || [];
    if (members.length === 0) {
      container.innerHTML = '<div class="empty-state"><div class="empty-text">No agents to graph</div></div>';
      return;
    }

    const width = Math.min(container.clientWidth || 520, 520);
    const height = 280;
    const cx = width / 2;
    const cy = height / 2;
    const radius = Math.min(cx, cy) - 40;

    // Position agents in a circle
    const positions = members.map((member, i) => {
      const angle = (2 * Math.PI * i) / members.length - Math.PI / 2;
      const queue = (this.taskQueues || []).find(q => q.sessionId === member.sessionId);
      const taskCount = queue ? (queue.tasks || []).length : 0;
      const nodeRadius = 8 + Math.min(taskCount, 10);
      return {
        id: member.sessionId,
        name: member.name,
        initial: member.avatarInitial || member.name[0],
        x: cx + radius * Math.cos(angle),
        y: cy + radius * Math.sin(angle),
        r: nodeRadius,
        status: member.status,
      };
    });

    // Build edges from recent messages, tracking count + last timestamp per pair.
    const edgeMap = new Map();
    for (const msg of (this.state.recentMessages || [])) {
      const fromPos = positions.find(p => p.id === msg.fromSessionId);
      const toPos = positions.find(p => p.id === msg.toSessionId);
      if (!fromPos || !toPos) continue;
      const key = [fromPos.id, toPos.id].sort().join('::');
      const prev = edgeMap.get(key) || { count: 0, last: 0 };
      prev.count += 1;
      const ts = typeof msg.timestamp === 'number' ? msg.timestamp : new Date(msg.timestamp || 0).getTime();
      if (ts > prev.last) prev.last = ts;
      edgeMap.set(key, prev);
    }

    const statusColors = {
      working: '#3b82f6',
      idle: '#52525b',
      meeting: '#a855f7',
      stuck: '#ef4444',
      offline: '#27272a',
    };

    const edges = Array.from(edgeMap.entries()).map(([key, info]) => {
      const [id1, id2] = key.split('::');
      const p1 = positions.find(p => p.id === id1);
      const p2 = positions.find(p => p.id === id2);
      if (!p1 || !p2) return '';
      const count = info.count;
      if (count < 1) return '';
      const strokeWidth = Math.min(1 + Math.log(count + 1), 4).toFixed(1);
      const last = info.last ? this.formatTimeAgo(info.last) : 'never';
      const title = `${count} message${count === 1 ? '' : 's'} · last ${last}`;
      return `<line x1="${p1.x.toFixed(1)}" y1="${p1.y.toFixed(1)}" x2="${p2.x.toFixed(1)}" y2="${p2.y.toFixed(1)}" stroke="#3f3f46" stroke-width="${strokeWidth}" opacity="0.6"><title>${this.escapeHtml(title)}</title></line>`;
    }).join('');

    const nodes = positions.map(p => {
      const color = statusColors[p.status] || '#52525b';
      const labelX = p.x.toFixed(1);
      const labelY = (p.y + p.r + 12).toFixed(1);
      return `
        <g style="cursor:pointer" onclick="office.showDetail('${this.escapeAttr(p.id)}')">
          <circle cx="${p.x.toFixed(1)}" cy="${p.y.toFixed(1)}" r="${p.r}" fill="${color}" opacity="0.9" stroke="#09090b" stroke-width="2"><title>${this.escapeHtml(p.name)}</title></circle>
          <text x="${labelX}" y="${labelY}" text-anchor="middle" fill="#a1a1aa" font-size="10" font-family="-apple-system, sans-serif">${this.escapeHtml(p.name.slice(0, 8))}</text>
        </g>
      `;
    }).join('');

    container.innerHTML = `<svg width="${width}" height="${height}" viewBox="0 0 ${width} ${height}">${edges}${nodes}</svg>`;
  }

  // ========== Feature: Global Search ==========

  handleSearch(query) {
    if (!query || query.length < 2) {
      this.hideSearchResults();
      return;
    }
    const q = query.toLowerCase();
    const results = [];

    // Search agents
    for (const member of (this.state.members || [])) {
      if ((member.name || '').toLowerCase().includes(q) || (member.role || '').toLowerCase().includes(q)) {
        results.push({
          type: 'agent',
          icon: 'A',
          label: member.name,
          detail: member.role,
          action: () => this.showDetail(member.sessionId),
        });
      }
    }

    // Search tasks
    for (const queue of (this.taskQueues || [])) {
      for (const task of (queue.tasks || [])) {
        if ((task.title || '').toLowerCase().includes(q)) {
          results.push({
            type: 'task',
            icon: 'T',
            label: task.title,
            detail: task.status,
            action: () => this.switchTab('tasks'),
          });
        }
      }
    }

    // Search messages
    for (const msg of (this.state.recentMessages || [])) {
      if ((msg.content || '').toLowerCase().includes(q)) {
        results.push({
          type: 'message',
          icon: 'M',
          label: `${msg.fromName} to ${msg.toName}`,
          detail: (msg.content || '').slice(0, 60),
          action: () => this.switchTab('messages'),
        });
      }
    }

    this.renderSearchResults(results.slice(0, 10));
  }

  renderSearchResults(results) {
    const dropdown = document.getElementById('searchResults');
    if (!dropdown) return;

    if (results.length === 0) {
      dropdown.innerHTML = '<div class="search-result-empty">No results</div>';
      dropdown.classList.remove('hidden');
      return;
    }

    dropdown.innerHTML = results.map((r, idx) => {
      const typeColors = { agent: '#3b82f6', task: '#f97316', message: '#22c55e' };
      const color = typeColors[r.type] || '#71717a';
      return `<div class="search-result-item" onclick="office._executeSearchResult(${idx})">
        <span class="search-result-icon" style="color:${color}">${this.escapeHtml(r.icon)}</span>
        <span class="search-result-label">${this.escapeHtml(r.label)}</span>
        <span class="search-result-detail">${this.escapeHtml(r.detail || '')}</span>
      </div>`;
    }).join('');

    this._searchResults = results;
    dropdown.classList.remove('hidden');
  }

  _executeSearchResult(idx) {
    const result = this._searchResults && this._searchResults[idx];
    if (result) {
      this.hideSearchResults();
      const searchInput = document.getElementById('globalSearch');
      if (searchInput) searchInput.value = '';
      result.action();
    }
  }

  hideSearchResults() {
    const dropdown = document.getElementById('searchResults');
    if (dropdown) dropdown.classList.add('hidden');
  }

  // ========== Feature: Notification Center ==========

  addNotification(type, message, detail) {
    const notif = {
      id: Date.now() + '-' + Math.random().toString(36).slice(2),
      type,
      message,
      detail: detail || '',
      timestamp: Date.now(),
      read: false,
    };
    this._notifications.unshift(notif);
    if (this._notifications.length > 50) {
      this._notifications = this._notifications.slice(0, 50);
    }
    this._notifUnread++;
    this._updateNotifBadge();
    if (this._notifDropdownOpen) {
      this._renderNotifList();
    }
  }

  _updateNotifBadge() {
    const badge = document.getElementById('notifBadge');
    if (!badge) return;
    if (this._notifUnread > 0) {
      badge.textContent = this._notifUnread > 99 ? '99+' : String(this._notifUnread);
      badge.classList.remove('hidden');
    } else {
      badge.classList.add('hidden');
    }
  }

  toggleNotifications() {
    const dropdown = document.getElementById('notifDropdown');
    if (!dropdown) return;
    this._notifDropdownOpen = !this._notifDropdownOpen;
    dropdown.classList.toggle('hidden', !this._notifDropdownOpen);
    if (this._notifDropdownOpen) {
      this._notifUnread = 0;
      this._updateNotifBadge();
      this._renderNotifList();
    }
  }

  closeNotifications() {
    const dropdown = document.getElementById('notifDropdown');
    if (dropdown) dropdown.classList.add('hidden');
    this._notifDropdownOpen = false;
  }

  clearNotifications() {
    this._notifications = [];
    this._notifUnread = 0;
    this._updateNotifBadge();
    this._renderNotifList();
  }

  _renderNotifList() {
    const list = document.getElementById('notifList');
    if (!list) return;

    if (this._notifications.length === 0) {
      list.innerHTML = '<div class="notif-empty">No notifications yet</div>';
      return;
    }

    const typeIcons = { error: '!', success: '+', info: 'i', warning: '~' };
    const typeColors = {
      error: '#ef4444',
      success: '#22c55e',
      info: '#3b82f6',
      warning: '#eab308',
    };

    list.innerHTML = this._notifications.map(n => {
      const icon = typeIcons[n.type] || 'i';
      const color = typeColors[n.type] || '#71717a';
      const ago = this.formatTimeAgo(n.timestamp);
      return `<div class="notif-item">
        <span class="notif-type-icon" style="color:${color}">${icon}</span>
        <div class="notif-content">
          <div class="notif-message">${this.escapeHtml(n.message)}</div>
          ${n.detail ? `<div class="notif-detail">${this.escapeHtml(n.detail)}</div>` : ''}
          <div class="notif-time">${ago}</div>
        </div>
      </div>`;
    }).join('');
  }

  // ========== Toast Notifications ==========

  showToast(message, type = 'info') {
    let container = document.getElementById('toastContainer');
    if (!container) {
      container = document.createElement('div');
      container.id = 'toastContainer';
      container.className = 'toast-container';
      document.body.appendChild(container);
    }
    const toast = document.createElement('div');
    toast.className = `toast toast-${type}`;
    toast.textContent = message;
    container.appendChild(toast);
    setTimeout(() => {
      toast.style.animation = 'toastOut 0.3s ease-out forwards';
      setTimeout(() => toast.remove(), 300);
    }, 3000);
  }

  // ========== Notifications ==========

  showNotification(message, type = 'info') {
    const banner = document.createElement('div');
    banner.className = `office-notification office-notification-${type}`;
    banner.textContent = message;
    document.body.appendChild(banner);

    setTimeout(() => {
      banner.classList.add('fade-out');
      setTimeout(() => banner.remove(), 300);
    }, 8000);
  }

  // ========== Utilities ==========

  timeAgo(timestamp) {
    if (!timestamp) return '';
    const seconds = Math.floor((Date.now() - timestamp) / 1000);
    if (seconds < 10) return 'just now';
    if (seconds < 60) return `${seconds}s ago`;
    const minutes = Math.floor(seconds / 60);
    if (minutes < 60) return `${minutes}m ago`;
    const hours = Math.floor(minutes / 60);
    if (hours < 24) return `${hours}h ago`;
    const days = Math.floor(hours / 24);
    return `${days}d ago`;
  }

  // ============================================================================
  // Agent Network tab (force-directed knowledge graph)
  //
  // Nodes = agents, directed edges = messages between them. Runs a small
  // mass-spring simulation in requestAnimationFrame, then idles once kinetic
  // energy falls below a threshold. No external deps.
  // ============================================================================

  _pairKey(a, b) {
    return a < b ? a + '|' + b : b + '|' + a;
  }

  _isPairMatch(pair, a, b) {
    return (pair[0] === a && pair[1] === b) || (pair[0] === b && pair[1] === a);
  }

  renderAgentNetwork() {
    const svg = document.getElementById('networkCanvas');
    const wrap = document.getElementById('networkCanvasWrap');
    const empty = document.getElementById('networkEmpty');
    if (!svg || !wrap) return;

    const members = (this.state.members || []).filter(m => m && m.sessionId);
    const messages = this.state.recentMessages || [];

    // Filter states
    const hideIdle = document.getElementById('netHideIdle')?.checked;
    const activeOnly = document.getElementById('netActiveOnly')?.checked;

    // Build directed edge map: sender|recipient → {count, lastTs}
    const edgeMap = new Map();
    const tenMinAgo = Date.now() - 10 * 60 * 1000;
    for (const msg of messages) {
      if (!msg.fromSessionId || !msg.toSessionId) continue;
      const ts = typeof msg.timestamp === 'number' ? msg.timestamp : new Date(msg.timestamp).getTime();
      if (activeOnly && ts < tenMinAgo) continue;
      const key = msg.fromSessionId + '>' + msg.toSessionId;
      const prev = edgeMap.get(key) || { from: msg.fromSessionId, to: msg.toSessionId, count: 0, lastTs: 0 };
      prev.count += 1;
      if (ts > prev.lastTs) prev.lastTs = ts;
      edgeMap.set(key, prev);
    }

    // Build node activity counts (messages sent + received).
    const activity = new Map();
    for (const m of members) activity.set(m.sessionId, 0);
    for (const e of edgeMap.values()) {
      activity.set(e.from, (activity.get(e.from) || 0) + e.count);
      activity.set(e.to, (activity.get(e.to) || 0) + e.count);
    }

    // Visible nodes
    const visible = members.filter(m => !(hideIdle && m.status === 'idle' && (activity.get(m.sessionId) || 0) === 0));

    empty.classList.toggle('hidden', visible.length > 0 && edgeMap.size > 0);

    // Initialize or reuse node positions. When a node is first seen we seed
    // it on a ring around the canvas center; the simulation then settles it.
    if (!this._netNodes) this._netNodes = new Map();
    const rect = wrap.getBoundingClientRect();
    const width = Math.max(400, rect.width);
    const height = Math.max(320, rect.height);
    const cx = width / 2, cy = height / 2;
    const radius = Math.min(cx, cy) * 0.6;

    visible.forEach((m, i) => {
      let n = this._netNodes.get(m.sessionId);
      if (!n) {
        const angle = (2 * Math.PI * i) / Math.max(1, visible.length) - Math.PI / 2;
        n = { id: m.sessionId, x: cx + radius * Math.cos(angle), y: cy + radius * Math.sin(angle), vx: 0, vy: 0 };
        this._netNodes.set(m.sessionId, n);
      }
      n.member = m;
      n.activity = activity.get(m.sessionId) || 0;
    });

    // Keep only nodes that still exist (drop removed members).
    const visibleIds = new Set(visible.map(m => m.sessionId));
    for (const id of Array.from(this._netNodes.keys())) {
      if (!visibleIds.has(id)) this._netNodes.delete(id);
    }

    const nodes = Array.from(this._netNodes.values());
    const edges = Array.from(edgeMap.values())
      .filter(e => visibleIds.has(e.from) && visibleIds.has(e.to));

    // Run simulation.
    this._runForceSimulation(nodes, edges, width, height);

    // Paint.
    svg.setAttribute('viewBox', `0 0 ${width} ${height}`);
    svg.setAttribute('preserveAspectRatio', 'xMidYMid meet');
    const defs = `<defs>
      <marker id="net-arrow" viewBox="0 0 10 10" refX="10" refY="5" markerWidth="6" markerHeight="6" orient="auto-start-reverse">
        <path d="M 0 0 L 10 5 L 0 10 Z" fill="#52525b"/>
      </marker>
      <marker id="net-arrow-sel" viewBox="0 0 10 10" refX="10" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
        <path d="M 0 0 L 10 5 L 0 10 Z" fill="#3b82f6"/>
      </marker>
    </defs>`;

    const statusColor = {
      working: '#3b82f6', idle: '#52525b', meeting: '#a855f7',
      stuck: '#ef4444', offline: '#27272a',
    };
    const now = Date.now();
    const selEdge = this._networkEdgePair;
    const selNode = this._networkSelectedNode;

    const edgeHtml = edges.map(e => {
      const a = this._netNodes.get(e.from);
      const b = this._netNodes.get(e.to);
      if (!a || !b) return '';
      // Shorten so arrowhead lands on node edge, not center.
      const dx = b.x - a.x, dy = b.y - a.y;
      const dist = Math.sqrt(dx * dx + dy * dy) || 1;
      const rA = 12 + Math.min(8, Math.sqrt(a.activity));
      const rB = 12 + Math.min(8, Math.sqrt(b.activity));
      const ux = dx / dist, uy = dy / dist;
      const x1 = a.x + ux * rA;
      const y1 = a.y + uy * rA;
      const x2 = b.x - ux * rB;
      const y2 = b.y - uy * rB;
      const width = Math.min(1 + Math.log(e.count + 1), 4).toFixed(1);
      const ageMs = Math.max(0, now - e.lastTs);
      const recency = Math.max(0.2, Math.min(1, 1 - ageMs / (60 * 60 * 1000))); // 1h decay
      const isSel = selEdge && this._isPairMatch(selEdge, e.from, e.to);
      const cls = ['network-edge'];
      if (isSel) cls.push('selected');
      else if (selEdge || selNode) cls.push('faded');
      const marker = isSel ? 'net-arrow-sel' : 'net-arrow';
      const lastStr = e.lastTs ? this.formatTimeAgo(e.lastTs) : 'never';
      const title = `${a.member.name} → ${b.member.name} · ${e.count} msg${e.count === 1 ? '' : 's'} · last ${this._escapeHtml(lastStr)}`;
      return `<line class="${cls.join(' ')}" data-from="${this._escapeAttr(e.from)}" data-to="${this._escapeAttr(e.to)}"
        x1="${x1.toFixed(1)}" y1="${y1.toFixed(1)}" x2="${x2.toFixed(1)}" y2="${y2.toFixed(1)}"
        stroke="#60a5fa" stroke-opacity="${recency.toFixed(2)}" stroke-width="${width}"
        marker-end="url(#${marker})" onclick="office._openNetworkEdgeDrawer('${this._escapeAttr(e.from)}','${this._escapeAttr(e.to)}')"
        tabindex="0" aria-label="Conversation between agents"><title>${title}</title></line>`;
    }).join('');

    const nodeHtml = nodes.map(n => {
      const m = n.member;
      const color = statusColor[m.status] || '#52525b';
      const r = 12 + Math.min(8, Math.sqrt(n.activity));
      const initial = m.avatarInitial || (m.name || '?')[0];
      const faded = selEdge && !(selEdge[0] === m.sessionId || selEdge[1] === m.sessionId);
      const isSel = selNode === m.sessionId;
      const cls = ['network-node'];
      if (isSel) cls.push('selected');
      if (faded) cls.push('faded');
      const task = m.activeTask && m.activeTask.title ? m.activeTask.title : '';
      const title = task ? `${m.name} · ${task}` : `${m.name} · ${m.role}`;
      // Concentric rings: outer status-colored stroke for status, middle
      // neutral fill anchors the node against the canvas, inner circle
      // holds the avatar initial. Gives the graph visual depth instead
      // of flat dots.
      const rOuter = r + 4;
      const rMid = r;
      const rInner = r - 3;
      return `<g class="${cls.join(' ')}" data-id="${this._escapeAttr(m.sessionId)}"
        onclick="office._openNetworkNodeDrawer('${this._escapeAttr(m.sessionId)}')"
        tabindex="0" aria-label="Agent ${this._escapeAttr(m.name)}">
        <title>${this._escapeHtml(title)}</title>
        <circle class="node-ring-outer" cx="${n.x.toFixed(1)}" cy="${n.y.toFixed(1)}" r="${rOuter.toFixed(1)}" fill="none" stroke="${color}" stroke-width="2.5" opacity="0.55"/>
        <circle class="node-ring-mid" cx="${n.x.toFixed(1)}" cy="${n.y.toFixed(1)}" r="${rMid.toFixed(1)}" fill="#18181b" stroke="#27272a" stroke-width="1"/>
        <circle cx="${n.x.toFixed(1)}" cy="${n.y.toFixed(1)}" r="${rInner.toFixed(1)}" fill="${color}"/>
        <text x="${n.x.toFixed(1)}" y="${(n.y + 4).toFixed(1)}">${this._escapeHtml(initial)}</text>
        <text x="${n.x.toFixed(1)}" y="${(n.y + rOuter + 12).toFixed(1)}" style="font-size:10px;fill:#a1a1aa">${this._escapeHtml(m.name)}</text>
      </g>`;
    }).join('');

    svg.innerHTML = defs + edgeHtml + nodeHtml;
  }

  // Simple force-directed simulation. Runs a fixed number of ticks synchronously;
  // sufficient for teams under ~20 nodes without blocking the main thread.
  _runForceSimulation(nodes, edges, width, height) {
    const cx = width / 2, cy = height / 2;
    const repulsion = 1800;
    const spring = 0.04;
    const springLen = 110;
    const damping = 0.85;
    const centeringPull = 0.004;
    const ticks = 200;

    for (let t = 0; t < ticks; t++) {
      // Pairwise repulsion.
      for (let i = 0; i < nodes.length; i++) {
        for (let j = i + 1; j < nodes.length; j++) {
          const a = nodes[i], b = nodes[j];
          const dx = a.x - b.x, dy = a.y - b.y;
          const d2 = Math.max(400, dx * dx + dy * dy);
          const f = repulsion / d2;
          const d = Math.sqrt(d2);
          const fx = (dx / d) * f;
          const fy = (dy / d) * f;
          a.vx += fx; a.vy += fy;
          b.vx -= fx; b.vy -= fy;
        }
      }
      // Spring attraction along edges.
      const byId = new Map(nodes.map(n => [n.id, n]));
      for (const e of edges) {
        const a = byId.get(e.from), b = byId.get(e.to);
        if (!a || !b) continue;
        const dx = b.x - a.x, dy = b.y - a.y;
        const dist = Math.max(1, Math.sqrt(dx * dx + dy * dy));
        const f = spring * (dist - springLen);
        const fx = (dx / dist) * f;
        const fy = (dy / dist) * f;
        a.vx += fx; a.vy += fy;
        b.vx -= fx; b.vy -= fy;
      }
      // Weak centering + damping + integrate.
      for (const n of nodes) {
        n.vx += (cx - n.x) * centeringPull;
        n.vy += (cy - n.y) * centeringPull;
        n.vx *= damping; n.vy *= damping;
        n.x += n.vx; n.y += n.vy;
        // Keep inside canvas.
        const pad = 28;
        n.x = Math.max(pad, Math.min(width - pad, n.x));
        n.y = Math.max(pad, Math.min(height - pad, n.y));
      }
    }
  }

  relayoutNetwork() {
    // Drop cached positions so the next render re-seeds on the ring.
    this._netNodes = null;
    this.renderAgentNetwork();
  }

  // F4 · close network drawer, switch to Timeline tab, filter by agent.
  // Reuses the existing timeline agent-filter state + render path;
  // cross-tab integration in 4 lines.
  _focusAgentOnTimeline(sessionId) {
    this.closeNetworkDrawer();
    this._timelineAgent = sessionId;
    const sel = document.getElementById('timelineAgentFilter');
    if (sel) sel.value = sessionId;
    this.switchTab('timeline');
  }

  _openNetworkNodeDrawer(sessionId) {
    const m = (this.state.members || []).find(x => x.sessionId === sessionId);
    if (!m) return;
    this._networkSelectedNode = sessionId;
    this._networkEdgePair = null;
    document.getElementById('networkDrawer')?.classList.remove('hidden');
    document.querySelector('.network-layout')?.classList.add('drawer-open');

    const msgs = this.state.recentMessages || [];
    const partnerCounts = new Map();
    for (const msg of msgs) {
      if (msg.fromSessionId === sessionId) {
        partnerCounts.set(msg.toSessionId, (partnerCounts.get(msg.toSessionId) || 0) + 1);
      } else if (msg.toSessionId === sessionId) {
        partnerCounts.set(msg.fromSessionId, (partnerCounts.get(msg.fromSessionId) || 0) + 1);
      }
    }
    const byId = new Map((this.state.members || []).map(x => [x.sessionId, x]));
    const topPartners = Array.from(partnerCounts.entries())
      .sort((a, b) => b[1] - a[1]).slice(0, 5)
      .map(([id, n]) => ({ name: byId.get(id)?.name || id, id, count: n }));

    const usage = m.usage || {};
    const cost = (usage.totalCostUSD || m.totalCost || 0);
    const turns = usage.turnCount || 0;
    const task = m.activeTask && m.activeTask.title ? m.activeTask.title : '-';
    const lastActive = m.lastActivityAt ? this.formatTimeAgo(m.lastActivityAt) : 'never';

    const content = document.getElementById('networkDrawerContent');
    if (!content) return;
    content.innerHTML = `
      <h3>${this._escapeHtml(m.name)}</h3>
      <div class="drawer-sub">${this._escapeHtml(m.role)} · <span style="color:${{working:'#3b82f6',idle:'#52525b',stuck:'#ef4444',meeting:'#a855f7'}[m.status]||'#a1a1aa'}">${this._escapeHtml(m.status || 'unknown')}</span></div>
      <div class="drawer-stats">
        <div class="drawer-stat"><div class="drawer-stat-label">Cost</div><div class="drawer-stat-value">$${cost.toFixed(4)}</div></div>
        <div class="drawer-stat"><div class="drawer-stat-label">Turns</div><div class="drawer-stat-value">${turns}</div></div>
        <div class="drawer-stat"><div class="drawer-stat-label">Last active</div><div class="drawer-stat-value">${this._escapeHtml(lastActive)}</div></div>
        <div class="drawer-stat"><div class="drawer-stat-label">Active task</div><div class="drawer-stat-value" style="font-size:11px">${this._escapeHtml(task)}</div></div>
      </div>
      <div>
        <div class="drawer-stat-label" style="margin-top:6px">Top message partners</div>
        <div class="drawer-partners">${
          topPartners.length
            ? topPartners.map(p => `<div class="drawer-partner-row" onclick="office._openNetworkEdgeDrawer('${this._escapeAttr(sessionId)}','${this._escapeAttr(p.id)}')"><span>${this._escapeHtml(p.name)}</span><span>${p.count}</span></div>`).join('')
            : '<div class="drawer-sub">No messages yet.</div>'
        }</div>
      </div>
      <div style="display:flex;gap:6px;margin-top:10px;flex-wrap:wrap">
        <button class="panel-btn" onclick="office.showDetail('${this._escapeAttr(sessionId)}')">Open full detail</button>
        <button class="network-drawer-focus-btn" onclick="office._focusAgentOnTimeline('${this._escapeAttr(sessionId)}')" title="Jump to Timeline filtered by this agent">
          <svg aria-hidden="true"><use href="#icon-tool-agent"/></svg>
          Focus on ${this._escapeHtml(m.name)}
        </button>
      </div>
    `;
    this.renderAgentNetwork();
  }

  _openNetworkEdgeDrawer(fromId, toId) {
    const byId = new Map((this.state.members || []).map(x => [x.sessionId, x]));
    const a = byId.get(fromId), b = byId.get(toId);
    if (!a || !b) return;
    this._networkEdgePair = [fromId, toId];
    this._networkSelectedNode = null;
    document.getElementById('networkDrawer')?.classList.remove('hidden');
    document.querySelector('.network-layout')?.classList.add('drawer-open');

    const msgs = (this.state.recentMessages || [])
      .filter(m => (m.fromSessionId === fromId && m.toSessionId === toId) ||
                   (m.fromSessionId === toId && m.toSessionId === fromId))
      .slice(-50)
      .reverse();

    const content = document.getElementById('networkDrawerContent');
    if (!content) return;
    const rowsHtml = msgs.length
      ? msgs.map(m => {
          const cls = m.fromSessionId === fromId ? '' : 'reverse';
          const ts = typeof m.timestamp === 'number' ? m.timestamp : new Date(m.timestamp).getTime();
          return `<div class="conversation-row ${cls}" onclick="office.showDetail('${this._escapeAttr(m.fromSessionId)}')">
            <div class="conversation-row-meta"><span>${this._escapeHtml(m.fromName || m.fromSessionId)} → ${this._escapeHtml(m.toName || m.toSessionId)}</span><span>${this._escapeHtml(this.formatTimeAgo(ts))}</span></div>
            <div class="conversation-row-body">${this._escapeHtml((m.content || '').slice(0, 600))}</div>
          </div>`;
        }).join('')
      : '<div class="drawer-sub">No messages between these agents yet.</div>';

    content.innerHTML = `
      <h3>${this._escapeHtml(a.name)} ↔ ${this._escapeHtml(b.name)}</h3>
      <div class="drawer-sub">${msgs.length} message${msgs.length === 1 ? '' : 's'}</div>
      <div class="conversation-thread">${rowsHtml}</div>
      <div class="conversation-composer">
        <select id="convSender">
          <option value="${this._escapeAttr(fromId)}">${this._escapeHtml(a.name)}</option>
          <option value="${this._escapeAttr(toId)}">${this._escapeHtml(b.name)}</option>
        </select>
        <input type="text" id="convBody" placeholder="Type a message..." onkeydown="if(event.key==='Enter')office._sendConversationMessage('${this._escapeAttr(fromId)}','${this._escapeAttr(toId)}')" />
        <button onclick="office._sendConversationMessage('${this._escapeAttr(fromId)}','${this._escapeAttr(toId)}')">Send</button>
      </div>
    `;
    // Autofocus input for fast iteration.
    setTimeout(() => document.getElementById('convBody')?.focus(), 30);
    this.renderAgentNetwork();
  }

  async _sendConversationMessage(fromId, toId) {
    const sender = document.getElementById('convSender')?.value || fromId;
    const recipient = sender === fromId ? toId : fromId;
    const body = document.getElementById('convBody');
    const content = (body?.value || '').trim();
    if (!content) return;
    try {
      await this.postJson('/api/office/agent/message', {
        sessionId: sender, fromSessionId: sender, toSessionId: recipient, content,
      });
      if (body) body.value = '';
    } catch (err) {
      console.error('send message failed', err);
      this.showNotification?.('Failed to send message', 'error');
    }
  }

  closeNetworkDrawer() {
    this._networkSelectedNode = null;
    this._networkEdgePair = null;
    document.getElementById('networkDrawer')?.classList.add('hidden');
    document.querySelector('.network-layout')?.classList.remove('drawer-open');
    this.renderAgentNetwork();
  }

  // Flash an edge when a message arrives over it. Uses a transient animated
  // circle ("pulse") that travels from sender to recipient.
  _pulseEdgeForMessage(message) {
    if (!message.fromSessionId || !message.toSessionId) return;
    const svg = document.getElementById('networkCanvas');
    if (!svg || !this._netNodes) return;
    const a = this._netNodes.get(message.fromSessionId);
    const b = this._netNodes.get(message.toSessionId);
    if (!a || !b) return;
    const NS = 'http://www.w3.org/2000/svg';
    const pulse = document.createElementNS(NS, 'circle');
    pulse.setAttribute('class', 'network-pulse active');
    pulse.setAttribute('cx', a.x.toFixed(1));
    pulse.setAttribute('cy', a.y.toFixed(1));
    pulse.setAttribute('r', '4');
    svg.appendChild(pulse);
    // Animate via SVG's SMIL: simpler than rAF, declarative.
    const animX = document.createElementNS(NS, 'animate');
    animX.setAttribute('attributeName', 'cx');
    animX.setAttribute('from', a.x.toFixed(1));
    animX.setAttribute('to', b.x.toFixed(1));
    animX.setAttribute('dur', '0.9s');
    animX.setAttribute('fill', 'freeze');
    const animY = document.createElementNS(NS, 'animate');
    animY.setAttribute('attributeName', 'cy');
    animY.setAttribute('from', a.y.toFixed(1));
    animY.setAttribute('to', b.y.toFixed(1));
    animY.setAttribute('dur', '0.9s');
    animY.setAttribute('fill', 'freeze');
    pulse.appendChild(animX);
    pulse.appendChild(animY);
    setTimeout(() => pulse.remove(), 950);
  }

  // ============================================================================
  // Insights tab additions: Active Now + velocity + stage timing + blockers
  // ============================================================================

  async _renderActiveNowFeed(container) {
    const members = this.state.members || [];
    if (!members.length) {
      container.innerHTML = '<div style="color:#52525b;font-size:12px">No agents yet.</div>';
      return;
    }
    container.innerHTML = members.map(m => {
      const task = m.activeTask && m.activeTask.title ? m.activeTask.title : '-';
      const phase = m.status === 'working' ? 'working'
        : m.status === 'meeting' ? 'in meeting'
        : m.status === 'stuck' ? 'stuck' : 'idle';
      const tool = m.activeTool || '';
      const elapsed = m.lastActivityAt ? this.formatTimeAgo(m.lastActivityAt) : '';
      const beamCls = (m.status === 'working') ? ' border-beam border-beam--active' : ' border-beam';
      return `<div class="active-now-card status-${this._escapeAttr(m.status || 'idle')}${beamCls}" onclick="office.showDetail('${this._escapeAttr(m.sessionId)}')">
        <div class="active-now-card-header">
          <span class="active-now-name">${this._escapeHtml(m.name)}</span>
          <span class="active-now-phase">${this._escapeHtml(phase)}</span>
        </div>
        ${tool ? `<div class="active-now-tool">${this._escapeHtml(tool)}</div>` : ''}
        <div class="active-now-task" title="${this._escapeAttr(task)}">${this._escapeHtml(task)}</div>
        <div class="active-now-elapsed">${this._escapeHtml(elapsed)}</div>
      </div>`;
    }).join('');
  }

  async _renderTeamVelocity(container) {
    const signal = this._insightsAbortController ? this._insightsAbortController.signal : undefined;
    try {
      const res = await fetch('/api/office/timeline?type=task&limit=500', { signal });
      const data = res.ok ? await res.json() : { entries: [] };
      const entries = (data.entries || []).filter(e => {
        const tx = e.data && e.data.transition;
        return tx === 'completed';
      });
      const bucketMs = 60 * 60 * 1000; // 1h
      const now = Date.now();
      const buckets = new Array(24).fill(0);
      for (const e of entries) {
        const ts = new Date(e.timestamp).getTime();
        const age = now - ts;
        if (age < 0 || age > 24 * bucketMs) continue;
        const idx = 23 - Math.floor(age / bucketMs);
        if (idx >= 0 && idx < 24) buckets[idx]++;
      }
      const maxV = Math.max(1, ...buckets);
      const w = 260, h = 56, bw = w / 24;
      const bars = buckets.map((v, i) => {
        const bh = (v / maxV) * (h - 4);
        return `<rect x="${(i * bw).toFixed(1)}" y="${(h - bh).toFixed(1)}" width="${(bw - 1).toFixed(1)}" height="${bh.toFixed(1)}"><title>${v} completed</title></rect>`;
      }).join('');
      const total = buckets.reduce((a, b) => a + b, 0);
      // Give the chart x/y-axis context so raw bars are interpretable.
      container.innerHTML = `
        <div class="velocity-chart-wrap">
          <div class="velocity-yticks">
            <span class="t-mono">${maxV}</span>
            <span class="t-mono">0</span>
          </div>
          <svg class="velocity-chart" viewBox="0 0 ${w} ${h}" preserveAspectRatio="none">${bars}</svg>
        </div>
        <div class="velocity-xticks">
          <span class="t-meta">24h ago</span>
          <span class="t-meta">Now</span>
        </div>
        <div class="velocity-caption">${total} task${total === 1 ? '' : 's'} completed in last 24h</div>`;
    } catch (err) {
      if (err && err.name === 'AbortError') return;
      container.innerHTML = '<div style="color:#52525b;font-size:12px">Unable to load velocity.</div>';
    }
  }

  async _renderStageTimings(container) {
    const signal = this._insightsAbortController ? this._insightsAbortController.signal : undefined;
    try {
      const res = await fetch('/api/office/task-queues', { signal });
      const data = res.ok ? await res.json() : { queues: [] };
      const tasks = [];
      for (const q of (data.queues || [])) {
        for (const t of (q.tasks || [])) tasks.push(t);
      }
      const now = Date.now();
      const toMs = v => (typeof v === 'string' ? new Date(v).getTime() : (v || 0));
      // Durations in each stage for completed tasks.
      const waits = [], starts = [], runs = [];
      for (const t of tasks) {
        const a = toMs(t.assignedAt), s = toMs(t.startedAt), c = toMs(t.completedAt);
        if (!a) continue;
        if (s > a) waits.push(s - a);
        if (c > s && s > 0) runs.push(c - s);
      }
      const median = arr => {
        if (!arr.length) return null;
        const sorted = arr.slice().sort((x, y) => x - y);
        const mid = Math.floor(sorted.length / 2);
        return sorted.length % 2 ? sorted[mid] : (sorted[mid - 1] + sorted[mid]) / 2;
      };
      const fmt = ms => ms == null ? '—' : ms < 60000 ? `${Math.round(ms / 1000)}s` : `${Math.round(ms / 60000)}m`;
      container.innerHTML = `
        <div class="stage-timing-row"><span class="stage-name">Assigned → Started (median wait)</span><span class="stage-value">${fmt(median(waits))} · n=${waits.length}</span></div>
        <div class="stage-timing-row"><span class="stage-name">Started → Completed (median run)</span><span class="stage-value">${fmt(median(runs))} · n=${runs.length}</span></div>
        <div class="stage-timing-row"><span class="stage-name">Open tasks</span><span class="stage-value">${tasks.filter(t => t.status !== 'completed' && t.status !== 'failed').length}</span></div>
      `;
    } catch (err) {
      if (err && err.name === 'AbortError') return;
      container.innerHTML = '<div style="color:#52525b;font-size:12px">Unable to load stage timings.</div>';
    }
  }

  // Pure render: receives task-queue data already fetched by
  // renderInsights so we don't race a duplicate request against
  // _buildInsightsNarrative.
  _renderBlockerLeaderboard(container, queuesData) {
    if (!queuesData) {
      container.innerHTML = '<div style="color:#52525b;font-size:12px">Unable to load blockers.</div>';
      return;
    }
    const blocked = [];
    for (const q of (queuesData.queues || [])) {
      for (const t of (q.tasks || [])) {
        if (t.status === 'blocked') blocked.push({ ...t, sessionId: q.sessionId, agentName: q.agentName });
      }
    }
    // Sort by time in status (use assignedAt as a proxy if no explicit blockedAt).
    blocked.sort((a, b) => new Date(a.assignedAt).getTime() - new Date(b.assignedAt).getTime());
    container.innerHTML = blocked.length
      ? blocked.slice(0, 5).map((t, i) => `<div class="blocker-row" onclick="office.openTaskDetail('${this._escapeAttr(t.sessionId)}','${this._escapeAttr(t.id)}')">
          <span class="blocker-title"><span class="t-rank">#${i + 1}</span>${this._escapeHtml((t.title || '').slice(0, 60))}</span>
          <span class="blocker-meta t-mono">${this._escapeHtml(t.agentName || '')} · ${this.formatTimeAgo(new Date(t.assignedAt).getTime())}</span>
        </div>`).join('')
      : '<div style="color:#52525b;font-size:12px">No blocked tasks right now.</div>';
  }

  // ============================================================================
  // Round 9 · overview widget, activity drill, context menus, budget, quick-add
  // ============================================================================

  // F1 · compute a snapshot of team health from already-known state.
  // Pure function: reads from this.state.members + this.taskQueues + the
  // session-local throughput counter. Called on every overview render.
  _computeTeamSnapshot() {
    const members = (this.state && Array.isArray(this.state.members)) ? this.state.members : [];
    const counts = { working: 0, idle: 0, stuck: 0, meeting: 0 };
    let totalCost = 0;
    for (const m of members) {
      const s = m.status || 'idle';
      if (counts[s] !== undefined) counts[s]++;
      totalCost += Number(m.totalCost) || 0;
    }

    // Top blocker: walk all task queues, pick the oldest blocked or
    // stuck-agent task. Fall back to in_progress tasks on stuck agents
    // so the cell isn't empty when nothing is formally blocked.
    const queues = this.taskQueues || [];
    const blockedTasks = [];
    for (const q of queues) {
      const owner = members.find(m => m.sessionId === q.sessionId);
      for (const t of (q.tasks || [])) {
        if (t.status === 'blocked' || (owner && owner.status === 'stuck' && t.status === 'in_progress')) {
          const since = t.assignedAt ? new Date(t.assignedAt).getTime() : 0;
          blockedTasks.push({ task: t, owner, since });
        }
      }
    }
    blockedTasks.sort((a, b) => a.since - b.since);
    const topBlocker = blockedTasks[0] || null;

    // Standup schedule: derive last/next from autoStandupConfig if known,
    // plus a locally-tracked _lastStandupAt that mirrors recent SSE.
    let nextStandupMs = 0;
    if (this.autoStandupConfig && this.autoStandupConfig.enabled && this.autoStandupConfig.nextAt) {
      nextStandupMs = this.autoStandupConfig.nextAt;
    }
    const lastStandupMs = this._lastStandupAt || 0;

    // Budget: pulled from officeSettings. 0 / unset → no budget.
    const budget = (this.officeSettings && Number(this.officeSettings.teamDailyBudget)) || 0;

    return {
      total: members.length,
      counts,
      totalCost,
      topBlocker,
      lastStandupMs,
      nextStandupMs,
      throughput: this._sessionTasksCompleted,
      budget,
    };
  }

  // F1 · render the overview widget at the top of the Agents tab. Idempotent;
  // called on every team state change (members, tasks, cost).
  _renderOverviewWidget() {
    const root = document.getElementById('teamOverview');
    if (!root) return;
    const members = (this.state && Array.isArray(this.state.members)) ? this.state.members : [];
    if (members.length === 0) {
      root.classList.add('hidden');
      return;
    }
    root.classList.remove('hidden');

    const snap = this._computeTeamSnapshot();

    // Donut: 4 segments (working / idle / meeting / stuck). Skip empty
    // segments so zero-count statuses don't produce weird arcs.
    const segEl = document.getElementById('teamOverviewDonutSegments');
    const totalEl = document.getElementById('teamOverviewTotal');
    if (totalEl) totalEl.textContent = String(snap.total);
    if (segEl) {
      const segments = [
        { key: 'working', count: snap.counts.working, cls: 'seg-working' },
        { key: 'meeting', count: snap.counts.meeting, cls: 'seg-meeting' },
        { key: 'idle',    count: snap.counts.idle,    cls: 'seg-idle' },
        { key: 'stuck',   count: snap.counts.stuck,   cls: 'seg-stuck' },
      ];
      const total = snap.total || 1;
      const r = 18;
      const circumference = 2 * Math.PI * r;
      let offset = 0;
      const parts = [];
      for (const s of segments) {
        if (s.count <= 0) continue;
        const frac = s.count / total;
        const dash = frac * circumference;
        parts.push(`<circle class="team-overview-donut-seg ${s.cls}" cx="24" cy="24" r="${r}" fill="none" stroke-width="6" stroke-dasharray="${dash.toFixed(2)} ${(circumference - dash).toFixed(2)}" stroke-dashoffset="${(-offset).toFixed(2)}" transform="rotate(-90 24 24)"></circle>`);
        offset += dash;
      }
      segEl.innerHTML = parts.join('');
    }

    // Standup cell.
    const lastEl = document.getElementById('teamOverviewLastStandup');
    const nextEl = document.getElementById('teamOverviewNextStandup');
    if (lastEl) lastEl.textContent = snap.lastStandupMs > 0 ? this.formatTimeAgo(snap.lastStandupMs) : '—';
    if (nextEl) {
      if (snap.nextStandupMs > 0) {
        const remaining = Math.max(0, snap.nextStandupMs - Date.now());
        const mins = Math.round(remaining / 60000);
        const hrs = Math.floor(mins / 60);
        nextEl.textContent = hrs > 0 ? `Next: in ${hrs}h ${mins % 60}m` : `Next: in ${mins}m`;
      } else {
        nextEl.textContent = 'Next: —';
      }
    }

    // Blocker cell.
    const blockerEl = document.getElementById('teamOverviewBlocker');
    const blockerDurEl = document.getElementById('teamOverviewBlockerDuration');
    if (blockerEl && blockerDurEl) {
      if (snap.topBlocker) {
        const who = (snap.topBlocker.owner && snap.topBlocker.owner.name) || '—';
        const title = (snap.topBlocker.task.title || '').slice(0, 36);
        blockerEl.textContent = `${who} · ${title}`;
        blockerDurEl.textContent = snap.topBlocker.since ? this.formatTimeAgo(snap.topBlocker.since) : '';
      } else {
        blockerEl.textContent = 'None';
        blockerDurEl.textContent = '';
      }
    }

    // Cost cell + budget hint.
    const costEl = document.getElementById('teamOverviewCost');
    const costBudgetEl = document.getElementById('teamOverviewCostBudget');
    if (costEl) costEl.textContent = this._formatCost(snap.totalCost);
    if (costBudgetEl) {
      if (snap.budget > 0) {
        const pct = Math.min(999, Math.round((snap.totalCost / snap.budget) * 100));
        costBudgetEl.textContent = `${pct}% of $${snap.budget}`;
      } else {
        costBudgetEl.textContent = 'no budget set';
      }
    }

    // Throughput cell.
    const tpEl = document.getElementById('teamOverviewThroughput');
    if (tpEl) tpEl.textContent = String(snap.throughput);

    // Bind cell clicks once. The delegation sits on the root container
    // so re-renders don't duplicate listeners.
    if (!root.dataset.bound) {
      root.dataset.bound = '1';
      root.addEventListener('click', (e) => {
        const cell = e.target.closest('.team-overview-cell');
        if (!cell) return;
        const kind = cell.dataset.overviewCell;
        if (kind === 'blocker') this.switchTab('tasks');
        else if (kind === 'cost') this.switchTab('insights');
        else if (kind === 'throughput') { this.switchTab('timeline'); this.setTimelineKind('task'); }
        else if (kind === 'standup') this.switchTab('standup');
      });
      root.addEventListener('keydown', (e) => {
        if (e.key !== 'Enter' && e.key !== ' ') return;
        const cell = e.target.closest('.team-overview-cell');
        if (!cell) return;
        e.preventDefault();
        cell.click();
      });
    }
  }

  // Called from handleMemberUpdated / handleTaskQueueUpdated / handleMessage.
  _scheduleOverviewRefresh() {
    this._scheduleRender(() => this._renderOverviewWidget(), 'overview', 150);
  }

  // F2 · make activity-feed entries jump to their source surface. Invoked
  // from an onclick injected by renderActivity; see _makeActivityRowDrillable.
  _drillActivityEntry(entry) {
    if (!entry || !entry.type) return;
    const type = entry.type;
    if (type === 'system') return; // no target
    if (type === 'task') {
      this.switchTab('tasks');
      return;
    }
    if (type === 'standup') { this.switchTab('standup'); return; }
    if (type === 'message') {
      this.switchTab('messages');
      // best-effort: scroll to the bottom of message list so the recent
      // message is visible.
      setTimeout(() => {
        const list = document.getElementById('messageList');
        if (list) list.scrollTop = list.scrollHeight;
      }, 80);
      return;
    }
    if (type === 'lock') { this.switchTab('tasks'); return; }
    if (type === 'meeting' || type === 'brainstorm') { this.switchTab('meetings'); return; }
  }

  // F3 · reusable context menu. `items` is an array of
  //   { label, onClick, danger?: bool, separator?: bool }
  _showContextMenu(x, y, items) {
    const el = document.getElementById('ctxMenu');
    if (!el) return;
    el.innerHTML = items.map((it, i) => {
      if (it.separator) return '<div class="ctx-sep"></div>';
      const cls = it.danger ? 'ctx-danger' : '';
      return `<button class="${cls}" data-ctx-idx="${i}">${this.escapeHtml(it.label)}</button>`;
    }).join('');
    el.classList.remove('hidden');
    el.setAttribute('aria-hidden', 'false');

    // Measure then clamp to viewport so the menu never clips.
    el.style.visibility = 'hidden';
    el.style.left = '0px';
    el.style.top = '0px';
    const rect = el.getBoundingClientRect();
    const vw = window.innerWidth;
    const vh = window.innerHeight;
    const left = (x + rect.width > vw - 8) ? Math.max(8, vw - rect.width - 8) : x;
    const top  = (y + rect.height > vh - 8) ? Math.max(8, vh - rect.height - 8) : y;
    el.style.left = `${left}px`;
    el.style.top = `${top}px`;
    el.style.visibility = '';

    const close = () => {
      el.classList.add('hidden');
      el.setAttribute('aria-hidden', 'true');
      el.innerHTML = '';
      document.removeEventListener('click', onDocClick, true);
      document.removeEventListener('keydown', onKey, true);
      window.removeEventListener('resize', close);
      window.removeEventListener('scroll', close, true);
    };
    const onDocClick = (evt) => {
      if (!el.contains(evt.target)) close();
    };
    const onKey = (evt) => { if (evt.key === 'Escape') close(); };

    el.onclick = (evt) => {
      const btn = evt.target.closest('[data-ctx-idx]');
      if (!btn) return;
      const idx = parseInt(btn.dataset.ctxIdx, 10);
      const item = items[idx];
      close();
      if (item && typeof item.onClick === 'function') item.onClick();
    };

    // Defer the document-level dismisser so the right-click's own
    // mouseup doesn't immediately close the menu we just opened.
    setTimeout(() => {
      document.addEventListener('click', onDocClick, true);
      document.addEventListener('keydown', onKey, true);
      window.addEventListener('resize', close);
      window.addEventListener('scroll', close, true);
    }, 0);
  }

  // F3 · bind once: delegated contextmenu listeners for agent + kanban cards.
  _installContextMenus() {
    if (this._ctxMenusInstalled) return;
    this._ctxMenusInstalled = true;

    document.addEventListener('contextmenu', (e) => {
      // Agent card
      const agentEl = e.target.closest('.agent-card[data-session-id]');
      if (agentEl) {
        e.preventDefault();
        const sid = agentEl.dataset.sessionId;
        const member = (this.state.members || []).find(m => m.sessionId === sid);
        if (!member) return;
        const pinned = this.isPinned(member);
        const items = [
          { label: 'Open detail', onClick: () => this.showDetail(sid) },
          { label: 'Send prompt…', onClick: () => this.openMemberModal(sid) },
          { label: pinned ? 'Unpin' : 'Pin to top', onClick: () => this.togglePin(member) },
          { label: 'Start brainstorm', onClick: () => this.triggerBrainstorm() },
          { separator: true },
          { label: 'Open session (terminal)', onClick: () => { window.location.href = `/?id=${encodeURIComponent(sid)}`; } },
        ];
        this._showContextMenu(e.pageX, e.pageY, items);
        return;
      }
      // Kanban card — inspected via the existing kanban card DOM shape.
      const kanbanEl = e.target.closest('.kanban-card');
      if (kanbanEl) {
        // Pull the task ID / session from the click handler string on the
        // card: it's `office.openTaskDetail('<sid>','<tid>')`. We parse
        // rather than storing separately to avoid a second render pass.
        const onclick = kanbanEl.getAttribute('onclick') || '';
        const m = onclick.match(/openTaskDetail\('([^']+)','([^']+)'\)/);
        if (!m) return;
        e.preventDefault();
        const sid = m[1];
        const tid = m[2];
        // Figure out current status from DOM classes to gate actions.
        const isDone = kanbanEl.classList.contains('card-done');
        const isBlocked = kanbanEl.classList.contains('card-blocked');
        const items = [
          { label: 'Open detail', onClick: () => this.openTaskDetail(sid, tid) },
        ];
        if (!isDone) {
          if (isBlocked) {
            items.push({ label: 'Resume', onClick: () => this._kanbanTaskStart(sid, tid) });
          } else {
            items.push({ label: 'Start', onClick: () => this._kanbanTaskStart(sid, tid) });
            items.push({ label: 'Mark blocked', onClick: () => this._kanbanTaskBlock(sid, tid) });
          }
          items.push({ label: 'Mark complete', onClick: () => this._kanbanTaskComplete(sid, tid) });
          items.push({ separator: true });
          items.push({ label: 'Cycle priority', onClick: () => {
            // Read current priority from the inner `.q-priority` button's data-attr.
            const pri = kanbanEl.querySelector('.q-priority')?.dataset.pri || 'medium';
            const nextPri = pri === 'high' ? 'low' : (pri === 'medium' ? 'high' : 'medium');
            this._kanbanCyclePriority(sid, tid, nextPri);
          }});
        }
        this._showContextMenu(e.pageX, e.pageY, items);
      }
    });
  }

  // F4 · header budget pill + insights progress bar. Called on cost + settings
  // changes. Defensive against officeSettings not being fetched yet.
  _updateBudgetPill() {
    const pill = document.getElementById('budgetPill');
    const val = document.getElementById('budgetPillValue');
    const budget = (this.officeSettings && Number(this.officeSettings.teamDailyBudget)) || 0;
    const members = (this.state && Array.isArray(this.state.members)) ? this.state.members : [];
    const totalCost = members.reduce((s, m) => s + (Number(m.totalCost) || 0), 0);

    if (!pill || !val) return;
    if (budget <= 0) {
      pill.classList.add('hidden');
      pill.removeAttribute('data-tier');
      return;
    }
    pill.classList.remove('hidden');
    const pct = Math.max(0, (totalCost / budget) * 100);
    let tier = 'ok';
    if (pct >= 95) tier = 'alert';
    else if (pct >= 80) tier = 'warn';
    else if (pct >= 50) tier = 'mid';
    pill.setAttribute('data-tier', tier);
    pill.style.setProperty('--budget-pct', Math.min(100, pct).toFixed(0));
    val.textContent = `$${totalCost.toFixed(2)} / $${budget}`;
    pill.title = `Today: $${totalCost.toFixed(4)} of $${budget} (${pct.toFixed(0)}%)`;

    // Also refresh the Insights bar if the tab is mounted — lightweight.
    const bar = document.getElementById('insightBudgetBar');
    if (bar) {
      bar.setAttribute('data-tier', tier);
      const fill = bar.querySelector('.insight-budget-bar-fill');
      if (fill) {
        fill.style.width = `${Math.min(100, pct).toFixed(1)}%`;
        fill.setAttribute('data-tier', tier);
      }
      const label = bar.querySelector('.insight-budget-bar-label');
      if (label) label.textContent = `$${totalCost.toFixed(2)} / $${budget} · ${pct.toFixed(0)}%`;
    }
  }

  // F5 · quick-add task modal.
  openQuickAddModal() {
    const overlay = document.getElementById('quickAddModal');
    const assigneeSel = document.getElementById('qaAssignee');
    const titleInput = document.getElementById('qaTitle');
    const descInput = document.getElementById('qaDescription');
    const priInput = document.getElementById('qaPriority');
    if (!overlay || !assigneeSel) return;

    // Populate assignees from current roster.
    const roster = (this.state && Array.isArray(this.state.members)) ? this.state.members : [];
    if (roster.length === 0) {
      this.showToast('Spawn a team first', 'warning');
      return;
    }
    assigneeSel.innerHTML = '<option value="">Select a member...</option>' +
      roster.map(m => `<option value="${this.escapeAttr(m.sessionId)}">${this.escapeHtml(m.name || m.sessionId)} · ${this.escapeHtml(m.role || '')}</option>`).join('');

    if (titleInput) titleInput.value = '';
    if (descInput) descInput.value = '';
    if (priInput) priInput.value = 'medium';
    overlay.classList.remove('hidden');
    setTimeout(() => titleInput && titleInput.focus(), 40);
  }

  closeQuickAddModal() {
    const overlay = document.getElementById('quickAddModal');
    if (overlay) overlay.classList.add('hidden');
  }

  async submitQuickAddTask() {
    const assigneeSel = document.getElementById('qaAssignee');
    const titleInput = document.getElementById('qaTitle');
    const descInput = document.getElementById('qaDescription');
    const priInput = document.getElementById('qaPriority');
    const submitBtn = document.getElementById('btnQuickAddSubmit');
    if (!assigneeSel || !titleInput) return;

    const sessionId = assigneeSel.value.trim();
    const title = titleInput.value.trim();
    if (!sessionId) { this.showToast('Pick an assignee', 'warning'); return; }
    if (!title) { this.showToast('Title is required', 'warning'); return; }

    if (submitBtn) submitBtn.disabled = true;
    try {
      const data = await this.postJson('/api/office/agent/task/self-add', {
        sessionId,
        title,
        description: descInput ? descInput.value.trim() : '',
        priority: priInput ? priInput.value : 'medium',
      });
      if (data && data.success) {
        this.showToast('Task added', 'success');
        this.closeQuickAddModal();
        this._scheduleRender(() => this.fetchAndRenderTasks(), 'taskFetch', 100);
      } else {
        this.showToast((data && data.error) || 'Failed to add task', 'error');
      }
    } catch (err) {
      console.error('Quick-add task failed:', err);
      this.showToast('Network error adding task', 'error');
    } finally {
      if (submitBtn) submitBtn.disabled = false;
    }
  }

  // ============================================================================
  // Keyboard shortcuts overlay
  // ============================================================================

  _installShortcutBindings() {
    if (this._shortcutsInstalled) return;
    this._shortcutsInstalled = true;
    let lastKey = '';
    let lastKeyAt = 0;
    document.addEventListener('keydown', (e) => {
      // Ignore when typing in inputs/textareas/contenteditable/modifier chords.
      const t = e.target;
      const inField = t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.isContentEditable);
      if (e.metaKey || e.ctrlKey || e.altKey) return;

      // Esc closes the overlay if open.
      if (e.key === 'Escape' && !document.getElementById('shortcutsOverlay')?.classList.contains('hidden')) {
        this.closeShortcutsOverlay();
        return;
      }
      if (inField) return;

      if (e.key === '?' || (e.shiftKey && e.key === '/')) {
        e.preventDefault();
        this.openShortcutsOverlay();
        return;
      }

      // F5 · bare 't' opens the quick-add task modal. Only fires when
      // not typing in a field (already gated above) and not as part of
      // the "g t" chord sequence below.
      if (e.key === 't' && lastKey !== 'g') {
        e.preventDefault();
        this.openQuickAddModal();
        return;
      }

      // Chord: "g" then another letter jumps to a tab.
      const now = Date.now();
      if (lastKey === 'g' && now - lastKeyAt < 1200) {
        const map = { n: 'network', i: 'insights', t: 'timeline', m: 'meetings', a: 'agents',
                      k: 'tasks', s: 'standup', c: 'messages', r: 'reports', g: 'settings' };
        if (map[e.key]) {
          e.preventDefault();
          this.switchTab(map[e.key]);
          lastKey = '';
          return;
        }
      }
      if (e.key === 'g') {
        lastKey = 'g';
        lastKeyAt = now;
      } else {
        lastKey = '';
      }
    });
  }

  openShortcutsOverlay() {
    const grid = document.getElementById('shortcutsGrid');
    if (grid) {
      const rows = [
        ['Open command palette', 'Cmd/Ctrl + K'],
        ['Open this help', '?'],
        ['Close modal / overlay', 'Esc'],
        ['Go to Agents', 'g then a'],
        ['Go to Tasks', 'g then k'],
        ['Go to Standup', 'g then s'],
        ['Go to Chat', 'g then c'],
        ['Go to Timeline', 'g then t'],
        ['Go to Insights', 'g then i'],
        ['Go to Meetings', 'g then m'],
        ['Go to Agent Network', 'g then n'],
        ['Go to Reports', 'g then r'],
        ['Go to Settings', 'g then g'],
      ];
      grid.innerHTML = rows.map(([lbl, keys]) => {
        const k = keys.split(/\s+/).map(p => /^(then|\+|\/)$/i.test(p) ? `<span style="color:#52525b">${p}</span>` : `<kbd>${this._escapeHtml(p)}</kbd>`).join(' ');
        return `<div class="shortcut-row"><span class="shortcut-label">${this._escapeHtml(lbl)}</span><span class="shortcut-keys">${k}</span></div>`;
      }).join('');
    }
    document.getElementById('shortcutsOverlay')?.classList.remove('hidden');
  }

  closeShortcutsOverlay() {
    document.getElementById('shortcutsOverlay')?.classList.add('hidden');
  }

  // R15 · Fetch the full slot map + re-render the dashboard card. Called on
  // init + any office:devInstance* SSE event. Hits the browser-auth
  // mirror at /api/office/dev-instances; the agent-auth-only
  // /api/office/agent/dev-instance/all rejects the session cookie with 401.
  async refreshDevInstances() {
    try {
      const res = await fetch('/api/office/dev-instances', {
        credentials: 'include',
      });
      if (!res.ok) return;
      const data = await res.json();
      const slots = (data && data.slots) || {};
      this._renderDevInstancesCard(slots);
    } catch (err) {
      // Non-critical — the SSE listener will try again on the next event.
    }
  }

  // R15 · Render the team dev instances card. Hides when empty.
  _renderDevInstancesCard(slots) {
    const card = document.getElementById('devInstancesCard');
    const list = document.getElementById('devInstancesList');
    const count = document.getElementById('devInstancesCount');
    if (!card || !list) return;

    const names = Object.keys(slots || {}).sort();
    if (names.length === 0) {
      card.classList.add('hidden');
      list.innerHTML = '';
      return;
    }
    card.classList.remove('hidden');
    if (count) count.textContent = `${names.length} running`;

    const rows = names.map(name => {
      const s = slots[name] || {};
      const url = this.escapeHtml(s.url || '');
      const failures = s.healthFailures || 0;
      // Three-state health signal so the operator sees something meaningful
      // at every failure count: 0 = healthy, 1 = degraded (first miss; in
      // adaptive backoff), >=2 = failing (about to auto-clear at 3).
      const healthy = failures === 0;
      const degraded = failures === 1;
      const failing = failures >= 2;
      const forced = !!s.forceAnnounced;
      const credsSet = !!s.credsSet;

      const badges = [];
      if (forced) {
        badges.push('<span class="dev-instance-badge forced" title="Announced with --force">forced</span>');
      }
      if (failing) {
        badges.push(`<span class="dev-instance-badge failing" title="Health check failing (${failures} strikes)">failing</span>`);
      } else if (degraded) {
        badges.push('<span class="dev-instance-badge degraded" title="1 missed health check; re-checking with backoff">degraded</span>');
      } else if (healthy && s.url) {
        badges.push('<span class="dev-instance-badge healthy" title="Health check passing">healthy</span>');
      }
      if (credsSet) {
        badges.push('<span class="dev-instance-badge creds" title="Login credentials available via cm dev creds">creds</span>');
      }

      const metaParts = [];
      if (s.pid) metaParts.push(`pid ${s.pid}`);
      if (s.lastCheckAt) {
        // Server sends ISO8601; show relative time using our existing util.
        const ts = new Date(s.lastCheckAt).getTime();
        if (ts) metaParts.push(`last check ${this.formatTimeAgo(ts)}`);
      }
      const meta = metaParts.length ? `<div class="dev-instance-meta">${this.escapeHtml(metaParts.join(' · '))}</div>` : '';

      return `
        <div class="dev-instance-row">
          <span class="dev-instance-slot">${this.escapeHtml(name)}</span>
          ${s.url
            ? `<a class="dev-instance-url" href="${url}" target="_blank" rel="noopener noreferrer" title="${url}">${url}</a>`
            : '<span class="dev-instance-url" style="color:var(--text-faint)">(no URL)</span>'}
          <span class="dev-instance-badges">${badges.join('')}</span>
          ${meta}
        </div>
      `;
    });

    list.innerHTML = rows.join('');
  }

  escapeHtml(text) {
    return String(text)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;')
      .replace(/'/g, '&#39;');
  }

  escapeAttr(text) {
    return String(text).replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/'/g, '&#39;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
  }
}

// Initialize
const office = new OfficeApp();
document.addEventListener('DOMContentLoaded', () => office.init());
