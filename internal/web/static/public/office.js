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

    // Restore view mode preference
    if (this._viewMode === 'list') {
      this.setViewMode('list');
    }

    // Refresh CSRF token periodically
    this.csrfInterval = setInterval(() => this.fetchCsrfToken(), 45 * 60 * 1000);

    // Periodic state sync -- safety net to self-correct any missed SSE updates
    this._stateRefreshInterval = setInterval(() => {
      if (this.state.teamSpawned && this.state.members && this.state.members.length > 0) {
        this.fetchInitialState().then(() => this.renderAgentCards());
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
      this._sseReconnectTimer = setTimeout(() => this.connectSSE(), this.sseReconnectDelay);
      this.sseReconnectDelay = Math.min(this.sseReconnectDelay * 2, this.sseMaxReconnectDelay);
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
  }

  handleMemberJoined(member) {
    if (!this.state.members.find(m => m.sessionId === member.sessionId)) {
      this.state.members.push(member);
    }
    this.state.teamSpawned = true;
    this.render();
    this.addActivityEntry('system', `${member.name} joined the office`);
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
    this.addActivityEntry('message', `${message.fromName} -> ${message.toName}: ${message.content.slice(0, 60)}`);
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
    this.updateStatusBar();
  }

  updateSpawnButton() {
    const btn = document.getElementById('btnSpawnTeam');
    if (btn) {
      btn.disabled = this.state.teamSpawned;
      btn.textContent = this.state.teamSpawned ? 'Team Active' : 'Spawn Team';
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
    const visibleMembers = filter === 'all'
      ? this.state.members
      : this.state.members.filter(m => m.status === filter);

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

    for (const member of visibleMembers) {
      const existing = existingCards.get(member.sessionId);
      if (existing) {
        this.updateAgentCard(existing, member);
      } else {
        const card = this.createAgentCard(member);
        grid.appendChild(card);
      }
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
    card.className = `agent-card status-${member.status}`;
    card.dataset.sessionId = member.sessionId;
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
    return card;
  }

  fillAgentCard(card, member) {
    // Defensive defaults -- prevent "undefined" rendering
    if (!member.name) member.name = member.configId || 'Agent';
    if (!member.role) member.role = '';
    if (!member.status) member.status = 'idle';
    if (!member.avatarColor) member.avatarColor = member.color || 'blue';

    const queue = (this.taskQueues || []).find(q => q.sessionId === member.sessionId);
    const tasks = queue ? queue.tasks || [] : [];
    const total = tasks.length;
    const completed = tasks.filter(t => t.status === 'completed' || t.status === 'failed').length;
    const pct = total > 0 ? Math.round((completed / total) * 100) : 0;
    const currentTask = tasks.find(t => t.status === 'in_progress') || tasks.find(t => t.status === 'pending');
    const costStr = (member.totalCost || 0) >= 1
      ? '$' + (member.totalCost || 0).toFixed(2)
      : (((member.totalCost || 0) * 100).toFixed(0)) + 'c';

    const badgeClass = `badge-${member.status}`;
    const statusLabel = member.status.charAt(0).toUpperCase() + member.status.slice(1);
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
        activeToolHtml = `<div class="agent-active-tool"><span class="tool-spinner"></span><span>${this.escapeHtml(String(activeTool).slice(0, 24))}</span></div>`;
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

    // Quick action buttons (visible on hover)
    const quickActionsHtml = `
      <div class="agent-card-actions">
        <button class="agent-quick-btn" onclick="event.stopPropagation(); office.openMemberModal('${member.sessionId}')" title="Details">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="15 3 21 3 21 9"/><line x1="10" y1="14" x2="21" y2="3"/></svg>
        </button>
        <button class="agent-quick-btn" onclick="event.stopPropagation(); office.quickPrompt('${member.sessionId}')" title="Send prompt">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="22" y1="2" x2="11" y2="13"/><polygon points="22 2 15 22 11 13 2 9 22 2"/></svg>
        </button>
        <button class="agent-quick-btn" onclick="event.stopPropagation(); office.navigateToSessionDirect('${member.sessionId}')" title="Terminal">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="4 17 10 11 4 5"/><line x1="12" y1="19" x2="20" y2="19"/></svg>
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
        <span class="agent-card-cost">${lastActiveHtml}${costStr}</span>
      </div>
      <div class="agent-card-badges">${autonomyBadgeHtml}${learningsBadgeHtml}</div>
    `;
  }

  updateAgentCard(card, member) {
    // Update CSS status class; include no-anim to suppress fadeSlideIn on re-render
    card.className = `agent-card status-${member.status} no-anim`;
    card.setAttribute('aria-label', `${member.name} - ${member.role} (${member.status})`);
    // Re-fill content to reflect new state
    this.fillAgentCard(card, member);
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
        ${this.escapeHtml(msg.content.slice(0, 120))}
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

    // Feature 1: fetch terminal buffer with short-lived cache, then build session analysis timeline
    const detailContent = document.getElementById('detailPanelContent');
    this._fetchTerminalPreview(sessionId)
      .then(data => {
        const termEl = detailContent ? detailContent.querySelector('.detail-terminal') : null;
        if (termEl && data.terminalBuffer) {
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

  // ========== Tab Navigation ==========

  switchTab(tab) {
    this.activeTab = tab;

    // Update nav icons
    document.querySelectorAll('.nav-icon').forEach(btn => {
      btn.classList.toggle('active', btn.dataset.tab === tab);
    });

    // Show/hide panels
    const panels = ['agents', 'tasks', 'standup', 'messages', 'metrics', 'activity', 'reports', 'settings'];
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
  }

  // ========== Standup ==========

  renderStandup() {
    const transcript = document.getElementById('standupTranscript');
    if (!transcript) return;

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
      this.lastRenderedStandupCount = 0;
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

    list.innerHTML = threads.map(thread => {
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
            <span>${this.escapeHtml(msg.fromName)} to ${this.escapeHtml(msg.toName)}</span>
            <span>${time}</span>
          </div>
          ${this.escapeHtml(msg.content)}
        </div>`;
      }).join('');

      return `<div class="message-thread" data-thread-key="${this.escapeAttr(thread.key)}">
        <div class="message-thread-header" onclick="office._toggleMessageThread(this)">
          <span class="message-thread-participants">${this.escapeHtml(thread.participants)}</span>
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
          ${this.escapeHtml(msg.content.slice(0, 200))}
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
      const res = await fetch(`/api/sessions/${this.selectedMemberId}/input`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': this.csrfToken || '',
        },
        body: JSON.stringify({ input: prompt }),
      });

      if (res.ok) {
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
        // Start new file section
        if (line.startsWith('diff --git')) {
          if (currentFile) files.push(currentFile);
          const match = line.match(/diff --git a\/(.+) b\/(.+)/);
          currentFile = {
            name: match ? match[2] : line,
            lines: [],
            adds: 0,
            dels: 0,
          };
        }
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

  quickPrompt(sessionId) {
    const member = this.state.members.find(m => m.sessionId === sessionId);
    const name = member ? member.name : 'Agent';
    const prompt = window.prompt(`Send prompt to ${name}:`);
    if (prompt && prompt.trim()) {
      const headers = { 'Content-Type': 'application/json' };
      if (this.csrfToken) headers['X-CSRF-Token'] = this.csrfToken;
      fetch(`/api/sessions/${sessionId}/input`, {
        method: 'POST',
        headers: headers,
        body: JSON.stringify({ input: prompt.trim() + '\n' }),
      }).then(res => {
        if (res.ok) this.showNotification(`Prompt sent to ${name}`, 'success');
        else this.showNotification(`Failed to send prompt to ${name}`, 'error');
      }).catch(() => {
        this.showNotification(`Failed to send prompt to ${name}`, 'error');
      });
    }
  }

  setViewMode(mode) {
    this._viewMode = mode;
    localStorage.setItem('office-view-mode', mode);
    const grid = document.getElementById('agentGrid');
    if (grid) {
      grid.classList.toggle('view-list', mode === 'list');
    }
    document.querySelectorAll('.view-mode-btn').forEach(btn => {
      btn.classList.toggle('active', btn.dataset.mode === mode);
    });
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
      const [templatesRes, settingsRes] = await Promise.all([
        fetch('/api/office/templates'),
        fetch('/api/settings/accounts'),
      ]);
      const templatesData = await templatesRes.json();
      const settings = await settingsRes.json();

      this._spawnTemplates = templatesData.templates || [];
      this._spawnSettings = settings;
      this._selectedTemplateId = null;

      document.getElementById('spawnStep1').classList.remove('hidden');
      document.getElementById('spawnStep2').classList.add('hidden');

      this.renderTemplateGrid();
      document.getElementById('spawnConfigModal').classList.remove('hidden');
    } catch (err) {
      console.error('Failed to open spawn config:', err);
      this.showNotification('Failed to load team templates', 'error');
    }
  }

  renderTemplateGrid() {
    const grid = document.getElementById('templateGrid');
    if (!grid || !this._spawnTemplates) return;

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

    grid.innerHTML = this._spawnTemplates.map(tpl => {
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

      return `<div class="template-card" data-template-id="${this.escapeAttr(tpl.id)}" onclick="office.selectTemplate('${this.escapeAttr(tpl.id)}')">
        <div class="tpl-card-header">
          <span class="tpl-card-icon">${icon}</span>
          <div class="tpl-card-title-group">
            <h3 class="tpl-card-title">${this.escapeHtml(tpl.name)}</h3>
            <span class="tpl-card-count">${tpl.memberCount} agents</span>
          </div>
        </div>
        <p class="tpl-card-desc">${this.escapeHtml(tpl.description)}</p>
        <div class="tpl-card-avatars">${memberAvatars}</div>
        <div class="tpl-card-roster">${memberList}</div>
        <button class="btn-select-tpl" onclick="event.stopPropagation(); office.selectTemplate('${this.escapeAttr(tpl.id)}')">Select &amp; Configure</button>
      </div>`;
    }).join('');
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

    document.getElementById('spawnStep1').classList.add('hidden');
    document.getElementById('spawnStep2').classList.remove('hidden');

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
  }

  getDefaultModelForProvider(provider, settings) {
    const models = OfficeApp.PROVIDER_MODELS[provider];
    if (!models || models.length === 0) return '';
    if (provider === 'zai-glm' && settings.zaiModel) return settings.zaiModel;
    return models[0].value;
  }

  closeSpawnConfigModal() {
    document.getElementById('spawnConfigModal').classList.add('hidden');
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

  renderSpawnMemberRows(defaultProvider, defaultModel) {
    const container = document.getElementById('spawnConfigMembers');
    const membersInfo = this._currentMembers || OfficeApp.TEAM_MEMBERS_INFO;
    const avatarColors = {
      blue: 'var(--session-blue, #3b82f6)',
      purple: 'var(--session-purple, #a855f7)',
      pink: 'var(--session-pink, #ec4899)',
      green: 'var(--session-green, #22c55e)',
      orange: 'var(--session-orange, #f97316)',
      yellow: 'var(--session-yellow, #eab308)',
      red: 'var(--session-red, #ef4444)',
    };

    container.innerHTML = membersInfo.map(member => {
      const bgColor = avatarColors[member.color] || 'var(--text-dim)';
      const initial = member.name[0];

      const providers = [
        { value: 'claude-subscription', label: 'Claude Sub' },
        { value: 'claude-api', label: 'Claude API' },
        { value: 'zai-glm', label: 'Z.AI GLM' },
      ];
      const providerOptions = providers.map(p =>
        `<option value="${p.value}"${p.value === defaultProvider ? ' selected' : ''}>${p.label}</option>`
      ).join('');

      const models = OfficeApp.PROVIDER_MODELS[defaultProvider] || [];
      const modelOptions = models.map(m =>
        `<option value="${m.value}"${m.value === defaultModel ? ' selected' : ''}>${m.label}</option>`
      ).join('');

      return `<div class="spawn-member-row">
        <div class="spawn-member-avatar" style="background: ${bgColor}">${initial}</div>
        <div class="spawn-member-info">
          <div class="spawn-member-name">${this.escapeHtml(member.name)}</div>
          <div class="spawn-member-role">${this.escapeHtml(member.role)}</div>
        </div>
        <div class="spawn-member-selects">
          <select class="spawn-config-select" id="spawn-provider-${this.escapeAttr(member.id)}" onchange="office.updateMemberProvider('${this.escapeAttr(member.id)}')">${providerOptions}</select>
          <select class="spawn-config-select" id="spawn-model-${this.escapeAttr(member.id)}">${modelOptions}</select>
        </div>
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

    const workingDir = document.getElementById('spawnWorkingDir').value.trim();
    if (!workingDir) {
      this.showNotification('Working directory is required', 'warning');
      if (btn) { btn.disabled = false; btn.textContent = 'Spawn Team'; }
      if (spawnHeaderBtn) { spawnHeaderBtn.disabled = false; spawnHeaderBtn.textContent = 'Spawn Team'; }
      return;
    }

    const membersInfo = this._currentMembers || OfficeApp.TEAM_MEMBERS_INFO;
    const memberOverrides = membersInfo.map(member => {
      const provider = document.getElementById(`spawn-provider-${member.id}`)?.value || 'claude-subscription';
      const model = document.getElementById(`spawn-model-${member.id}`)?.value || 'sonnet';
      return { id: member.id, provider, model };
    });

    const telegramEnabled = document.getElementById('spawnTelegramEnabled')?.checked || false;
    const telegramIntervalMin = parseInt(document.getElementById('spawnTelegramInterval')?.value || '60', 10);

    try {
      const mission = document.getElementById('spawnMission')?.value?.trim() || '';
      const perpetualMode = document.getElementById('spawnPerpetualMode')?.checked ?? true;
      const data = await this.postJson('/api/office/spawn-team', {
        workingDir,
        mission,
        memberOverrides,
        perpetualMode,
        templateId: this._selectedTemplateId || undefined,
        telegramEnabled,
        telegramIntervalMin,
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
      this.showNotification('Failed to spawn team', 'error');
      this.showToast('Failed to spawn team', 'error');
      if (btn) { btn.disabled = false; btn.textContent = 'Spawn Team'; }
      if (spawnHeaderBtn) { spawnHeaderBtn.disabled = false; spawnHeaderBtn.textContent = 'Spawn Team'; }
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
              await fetch(`/api/sessions/${sessionId}`, {
                method: 'DELETE',
                headers: { 'X-CSRF-Token': this.csrfToken || '' },
              });
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
    fetch(`/api/sessions/${sessionId}`, {
      method: 'DELETE',
      headers: { 'X-CSRF-Token': this.csrfToken || '' },
    }).catch(() => {});
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
    try {
      const [bugRes, queueRes, lockRes] = await Promise.all([
        fetch('/api/office/bug-tasks'),
        fetch('/api/office/task-queues'),
        fetch('/api/office/file-locks'),
      ]);
      const bugData = await bugRes.json();
      const queueData = await queueRes.json();
      const lockData = await lockRes.json();

      this.bugTasks = bugData.success ? bugData.tasks : [];
      this.taskQueues = queueData.success ? queueData.queues : [];
      this.state.fileLocks = lockData.success ? lockData.locks : [];
      this.renderTasks();
      this.renderKanbanBoard();
    } catch (err) {
      console.error('Failed to fetch tasks:', err);
    }
  }

  renderTasks() {
    const list = document.getElementById('tasksList');
    if (!list) return;

    const bugTasks = this.bugTasks || [];
    const taskQueues = this.taskQueues || [];
    const fileLocks = this.state.fileLocks || [];

    if (bugTasks.length === 0 && taskQueues.length === 0 && fileLocks.length === 0) {
      list.innerHTML = '<div class="tasks-empty">No tasks yet. Tasks appear after a standup or when Lint reports bugs.</div>';
      return;
    }

    let html = '';

    html += this.renderFileLocks();

    const openBugs = bugTasks.filter(t => t.status !== 'resolved');
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
          <div class="task-card priority-${t.priority}">
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
    const allTasks = queues.flatMap(q => q.tasks || []);

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

    const renderCards = (tasks) => {
      return tasks.map(t => {
        const isDone = t.status === 'completed' || t.status === 'failed';
        const isFailed = t.status === 'failed';
        const isBlocked = t.status === 'blocked';
        const cls = [
          t.priority === 'high' ? 'card-high' : '',
          isDone ? 'card-done' : '',
          isBlocked ? 'card-blocked' : '',
        ].filter(Boolean).join(' ');
        const priority = t.priority || 'medium';
        const priorityBadge = `<span class="priority-badge priority-${priority}">${priority}</span>`;
        const failedBadge = isFailed
          ? `<span class="kanban-failed-badge">failed</span>`
          : '';
        const blockedBadge = (t.blockedBy && t.blockedBy.length > 0)
          ? `<span class="kanban-blocked-badge">blocked by ${t.blockedBy.length}</span>`
          : '';
        return `<div class="kanban-card ${cls}" title="${this.escapeAttr(t.title || '')}" onclick="office.switchTab('tasks')">
          <div class="kanban-card-title">${this.escapeHtml((t.title || '').slice(0, 40))}</div>
          ${priorityBadge}${failedBadge}${blockedBadge}
        </div>`;
      }).join('');
    };

    backlogEl.innerHTML = renderCards(backlog);
    todoEl.innerHTML = renderCards(todo);
    wipEl.innerHTML = renderCards(inProgress);
    reviewEl.innerHTML = renderCards(review);
    doneEl.innerHTML = renderCards(done);

    // Update counts
    const backlogCount = document.getElementById('kanbanBacklogCount');
    const todoCount = document.getElementById('kanbanTodoCount');
    const wipCount = document.getElementById('kanbanWipCount');
    const reviewCount = document.getElementById('kanbanReviewCount');
    const doneCount = document.getElementById('kanbanDoneCount');
    if (backlogCount) backlogCount.textContent = backlog.length;
    if (todoCount) todoCount.textContent = todo.length;
    if (wipCount) wipCount.textContent = inProgress.length;
    if (reviewCount) reviewCount.textContent = review.length;
    if (doneCount) doneCount.textContent = done.length;
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
      const res = await fetch(`/api/office/file-locks/${encodeURIComponent(filePath)}`, {
        method: 'DELETE',
        headers: { 'X-CSRF-Token': this.csrfToken || '' },
      });
      const data = await res.json();
      if (data.success) {
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

    const buildEntryEl = (entry) => {
      const el = document.createElement('div');
      el.className = `activity-entry type-${entry.type}`;
      const time = new Date(entry.timestamp).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
      const icon = iconMap[entry.type] || '&#9675;';
      el.innerHTML = `<span class="activity-time">${time}</span><span class="activity-icon">${icon}</span><span class="activity-text">${this.escapeHtml(entry.text)}</span>`;
      return el;
    };

    const prevCount = this._lastActivityRenderCount;
    const currentCount = this.activityLog.length;

    if (prevCount === 0 || feed.children.length === 0 || feed.querySelector('.activity-empty')) {
      // Full rebuild using documentFragment
      const fragment = document.createDocumentFragment();
      for (let i = currentCount - 1; i >= 0; i--) {
        fragment.appendChild(buildEntryEl(this.activityLog[i]));
      }
      feed.innerHTML = '';
      feed.appendChild(fragment);
    } else if (currentCount > prevCount) {
      // Incremental prepend: only add new entries at the top
      const fragment = document.createDocumentFragment();
      for (let i = currentCount - 1; i >= prevCount; i--) {
        fragment.appendChild(buildEntryEl(this.activityLog[i]));
      }
      feed.insertBefore(fragment, feed.firstChild);
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

    if (autoEnabled) autoEnabled.checked = settings.autoStandup.enabled;
    if (autoInterval) autoInterval.value = Math.round(settings.autoStandup.intervalMs / 3600000 * 10) / 10;
    if (brainstormCooldown) brainstormCooldown.value = Math.round(settings.brainstormCooldownMs / 60000);
    if (brainstormResponseTimeout) brainstormResponseTimeout.value = Math.round(settings.brainstormResponseTimeoutMs / 1000);
    if (idleThreshold) idleThreshold.value = Math.round(settings.idleThresholdMs / 60000);
    if (meetingTimeout) meetingTimeout.value = Math.round(settings.meetingInactivityTimeoutMs / 1000);
    if (standupTimeout) standupTimeout.value = Math.round(settings.standupResponseTimeoutMs / 1000);
    if (fileLockExpiry) fileLockExpiry.value = Math.round(settings.fileLockExpiryMs / 60000);
    if (teamMission) teamMission.value = settings.teamMission || '';

    this.updateMissionBanner(settings.teamMission);
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
      text.textContent = mission.trim();
      banner.classList.remove('hidden');
    } else {
      banner.classList.add('hidden');
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
        return `<div class="standup-history-item" onclick="office.viewStandupDetail(decodeURIComponent('${encodeURIComponent(s.id)}'))">
          <span class="standup-history-date">${dateStr} ${timeStr}</span>
          <span class="standup-history-meta">${s.participantCount} agents, ${ratePct}% response</span>
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
        // Close command palette first if open
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
    const tabs = [
      { tab: 'agents', label: 'Go to Agents' },
      { tab: 'tasks', label: 'Go to Tasks' },
      { tab: 'standup', label: 'Go to Standup' },
      { tab: 'messages', label: 'Go to Chat' },
      { tab: 'metrics', label: 'Go to Metrics' },
      { tab: 'activity', label: 'Go to Activity' },
      { tab: 'settings', label: 'Go to Settings' },
    ];

    const commands = [
      { name: 'Spawn Team', icon: '+', shortcut: '', action: () => this.spawnTeam() },
      { name: 'Start Standup', icon: '>', shortcut: 'S', action: () => { this.startStandup(); } },
      { name: 'Trigger Brainstorm', icon: '*', shortcut: 'B', action: () => this.triggerBrainstorm() },
      { name: 'Wrap Up All', icon: '~', shortcut: '', action: () => this.wrapUpAll() },
      { name: 'Shutdown Team', icon: '!', shortcut: '', action: () => this.shutdownTeam() },
    ];

    for (const t of tabs) {
      commands.push({ name: t.label, icon: '#', shortcut: '', action: () => this.switchTab(t.tab) });
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

    resultsEl.innerHTML = filtered.map((cmd, idx) => {
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
    }).join('');

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

    // Build edges from recent messages
    const edgeMap = new Map();
    for (const msg of (this.state.recentMessages || [])) {
      const fromPos = positions.find(p => p.id === msg.fromSessionId);
      const toPos = positions.find(p => p.id === msg.toSessionId);
      if (!fromPos || !toPos) continue;
      const key = [fromPos.id, toPos.id].sort().join('::');
      edgeMap.set(key, (edgeMap.get(key) || 0) + 1);
    }

    const statusColors = {
      working: '#3b82f6',
      idle: '#52525b',
      meeting: '#a855f7',
      stuck: '#ef4444',
      offline: '#27272a',
    };

    const edges = Array.from(edgeMap.entries()).map(([key, count]) => {
      const [id1, id2] = key.split('::');
      const p1 = positions.find(p => p.id === id1);
      const p2 = positions.find(p => p.id === id2);
      if (!p1 || !p2) return '';
      const strokeWidth = Math.min(1 + Math.log(count + 1), 4).toFixed(1);
      return `<line x1="${p1.x.toFixed(1)}" y1="${p1.y.toFixed(1)}" x2="${p2.x.toFixed(1)}" y2="${p2.y.toFixed(1)}" stroke="#3f3f46" stroke-width="${strokeWidth}" opacity="0.6"/>`;
    }).join('');

    const nodes = positions.map(p => {
      const color = statusColors[p.status] || '#52525b';
      const labelX = p.x.toFixed(1);
      const labelY = (p.y + p.r + 12).toFixed(1);
      return `
        <circle cx="${p.x.toFixed(1)}" cy="${p.y.toFixed(1)}" r="${p.r}" fill="${color}" opacity="0.9" stroke="#09090b" stroke-width="2"/>
        <text x="${labelX}" y="${labelY}" text-anchor="middle" fill="#a1a1aa" font-size="10" font-family="-apple-system, sans-serif">${this.escapeHtml(p.name.slice(0, 8))}</text>
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
