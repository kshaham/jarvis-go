/* office-tasks.js — task detail modal, labels editor, dependency
 * graph, kanban board. Extracted from office.js via the
 * prototype-extension pattern proven in js/office-widgets.js. No
 * behavior change.
 *
 * Loaded as a sibling <script> after office.js. Methods are added
 * to OfficeApp.prototype so the singleton `office` instance picks
 * them up live (JS prototype lookups are dynamic).
 */

(function () {
  'use strict';
  if (typeof OfficeApp === 'undefined') {
    console.error('office-tasks.js loaded before OfficeApp; check script order');
    return;
  }

  Object.assign(OfficeApp.prototype, {

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
        <div class="task-modal-labels" id="taskModalLabels">${this._renderTaskLabelsEditor(sessionId, taskId, t.labels || [])}</div>
        <div id="taskModalScreenshots" class="task-modal-screenshots"></div>
        ${progress}
      `;
      // R34 · If agents uploaded a screenshot via `cm task complete
      // --screenshot`, render thumbnails so operators see real
      // evidence instead of just a text citation. Fire-and-forget --
      // failure just hides the section.
      this._loadTaskScreenshots(taskId);
    } catch (err) {
      body.innerHTML = '<div class="task-modal-loading">Failed to load task.</div>';
    }
  },

  // R34 · Fetch the task-screenshots directory index via a grep-able
  // per-task list. There's no dedicated listing endpoint today, so
  // we piggyback on the SSE payload and keep an in-memory index.
  // For the happy path, the task detail modal also accepts a direct
  // filename list from the progress log if we've observed it there.
  async _loadTaskScreenshots(taskId) {
    const el = document.getElementById('taskModalScreenshots');
    if (!el) return;
    // Prefer the in-memory index populated by the SSE listener; fall
    // back to empty. Operators refresh the modal by reopening it.
    const index = this._taskScreenshotIndex || {};
    const filenames = index[taskId] || [];
    if (!filenames.length) {
      el.innerHTML = '';
      return;
    }
    el.innerHTML = `
      <div class="task-modal-screenshots-header">
        <strong>Visual verification</strong>
        <span class="task-modal-screenshots-hint">${filenames.length} screenshot${filenames.length === 1 ? '' : 's'}</span>
      </div>
      <div class="task-modal-screenshot-grid">
        ${filenames.map(fn => `
          <a class="task-modal-screenshot-link" href="/api/office/task-screenshots/${encodeURIComponent(fn)}" target="_blank" rel="noopener">
            <img class="task-modal-screenshot-thumb" src="/api/office/task-screenshots/${encodeURIComponent(fn)}" alt="${this._escapeAttr(fn)}" loading="lazy" />
          </a>
        `).join('')}
      </div>`;
  },

  closeTaskDetail() {
    const modal = document.getElementById('taskDetailModal');
    if (modal) modal.classList.add('hidden');
  },


  // ========== R36 · Labels editor on task detail modal ==========

  _renderTaskLabelsEditor(sessionId, taskId, labels) {
    const sid = this._escapeAttr(sessionId);
    const tid = this._escapeAttr(taskId);
    const pills = (labels || []).map(l => `
      <span class="task-label-pill">
        ${this._escapeHtml(l)}
        <button class="task-label-pill-x" onclick="office._removeTaskLabel('${sid}','${tid}','${this._escapeAttr(l)}')" title="Remove label">×</button>
      </span>`).join('');
    return `
      <div class="task-modal-field"><strong>Labels:</strong></div>
      <div class="task-label-row" id="taskLabelRow-${tid}">${pills}</div>
      <div class="task-label-addrow">
        <input type="text" class="task-label-input" id="taskLabelInput-${tid}"
               placeholder="add label (lowercase, - allowed)"
               onkeydown="if(event.key==='Enter'){office._addTaskLabelFromInput('${sid}','${tid}');event.preventDefault();}">
        <button class="btn btn-sm" onclick="office._addTaskLabelFromInput('${sid}','${tid}')">Add</button>
      </div>`;
  },

  async _addTaskLabelFromInput(sessionId, taskId) {
    const input = document.getElementById('taskLabelInput-' + taskId);
    if (!input) return;
    const val = (input.value || '').trim();
    if (!val) return;
    try {
      const resp = await this.postJson('/api/office/agent/task/labels', {
        sessionId, taskId, add: [val],
      });
      input.value = '';
      if (resp && Array.isArray(resp.labels)) {
        this._refreshTaskLabelRow(sessionId, taskId, resp.labels);
      }
    } catch (err) {
      this.showToast('Failed to add label: ' + (err.message || err), 'error');
    }
  },

  async _removeTaskLabel(sessionId, taskId, label) {
    try {
      const resp = await this.postJson('/api/office/agent/task/labels', {
        sessionId, taskId, remove: [label],
      });
      if (resp && Array.isArray(resp.labels)) {
        this._refreshTaskLabelRow(sessionId, taskId, resp.labels);
      }
    } catch (err) {
      this.showToast('Failed to remove label: ' + (err.message || err), 'error');
    }
  },

  _refreshTaskLabelRow(sessionId, taskId, labels) {
    const row = document.getElementById('taskLabelRow-' + taskId);
    if (!row) return;
    const sid = this._escapeAttr(sessionId);
    const tid = this._escapeAttr(taskId);
    row.innerHTML = (labels || []).map(l => `
      <span class="task-label-pill">
        ${this._escapeHtml(l)}
        <button class="task-label-pill-x" onclick="office._removeTaskLabel('${sid}','${tid}','${this._escapeAttr(l)}')" title="Remove label">×</button>
      </span>`).join('');
  },

  _taskTimestamps(t) {
    const rows = [];
    if (t.assignedAt) rows.push(['Assigned', t.assignedAt]);
    if (t.startedAt && !t.startedAt.startsWith('0001')) rows.push(['Started', t.startedAt]);
    if (t.completedAt && !t.completedAt.startsWith('0001')) rows.push(['Completed', t.completedAt]);
    return rows.map(([label, ts]) => `<div class="task-modal-field"><strong>${label}:</strong> ${this._escapeHtml(this._formatRelativeTime(new Date(ts)))}</div>`).join('');
  },

  _findMemberBySession(sessionId) {
    const roster = (this.state && Array.isArray(this.state.members)) ? this.state.members : [];
    return roster.find(m => m.sessionId === sessionId);
  },

  _escapeHtml(s) {
    return String(s == null ? '' : s)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  },
  _escapeAttr(s) { return this._escapeHtml(s).replace(/'/g, '&#39;'); },

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
  },


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
      // R30 · If the graph view is currently active, refresh the
      // dependency graph on the same tick. Cheap — one extra GET
      // that shares the tasks we just queued above.
      if (this._taskView === 'graph') this.fetchAndRenderTaskGraph();
    } catch (err) {
      if (err && err.name === 'AbortError') return;
      console.error('Failed to fetch tasks:', err);
    }
  },


  // ========== R30 · Task dependency graph ==========
  //
  // View toggle state + layout. The graph shares the Tasks tab with
  // the kanban — only one is visible at a time. Layout is computed
  // from the backend's topological depth + agent attribution.

  setTaskView(view) {
    if (view !== 'kanban' && view !== 'graph') return;
    this._taskView = view;
    const kanban = document.getElementById('kanbanBoard');
    const graph = document.getElementById('taskGraphWrap');
    const hideDone = document.getElementById('taskGraphHideDone');
    const download = document.getElementById('taskGraphDownload');
    // querySelector on .task-view-chip hits every chip including the
    // download button -- only set `active` based on the data-task-view
    // attribute (which only the kanban/graph chips carry).
    document.querySelectorAll('.task-view-chip').forEach(el => {
      if (!el.dataset.taskView) return;
      el.classList.toggle('active', el.dataset.taskView === view);
    });
    if (view === 'graph') {
      if (kanban) kanban.classList.add('hidden');
      if (graph) graph.classList.remove('hidden');
      if (hideDone) hideDone.classList.remove('hidden');
      if (download) download.classList.remove('hidden');
      this.fetchAndRenderTaskGraph();
    } else {
      if (kanban) kanban.classList.remove('hidden');
      if (graph) graph.classList.add('hidden');
      if (hideDone) hideDone.classList.add('hidden');
      if (download) download.classList.add('hidden');
    }
  },

  // R32 · Graph export. Clones the live SVG, inlines computed styles
  // per element (so the downloaded file renders without the
  // dashboard's CSS), serializes to XML, and triggers a Blob download.
  exportTaskGraphSvg() {
    const svg = document.getElementById('taskGraphSvg');
    if (!svg) return;
    const clone = svg.cloneNode(true);
    // Copy computed style into each element so the file renders
    // correctly outside the dashboard's CSS.
    const inlineStyles = (live, clonedEl) => {
      const cs = window.getComputedStyle(live);
      // Only properties that affect visual output -- avoid flooding the
      // file with every single CSS property (there are ~350).
      const props = ['fill', 'stroke', 'stroke-width', 'stroke-dasharray',
        'opacity', 'font-family', 'font-size', 'font-weight'];
      const decls = [];
      for (const p of props) {
        const v = cs.getPropertyValue(p);
        if (v && v !== 'none' && v !== 'normal') decls.push(p + ':' + v);
      }
      if (decls.length) clonedEl.setAttribute('style', decls.join(';'));
    };
    const liveNodes = svg.querySelectorAll('*');
    const clonedNodes = clone.querySelectorAll('*');
    for (let i = 0; i < liveNodes.length && i < clonedNodes.length; i++) {
      inlineStyles(liveNodes[i], clonedNodes[i]);
    }
    // Also set a background so SVG renders well on a light viewer.
    clone.setAttribute('style', 'background:#0a0a0a');

    const xml = '<?xml version="1.0" encoding="UTF-8"?>\n' +
      new XMLSerializer().serializeToString(clone);
    const blob = new Blob([xml], { type: 'image/svg+xml;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const now = new Date();
    const pad = (n) => String(n).padStart(2, '0');
    const filename = `task-graph-${now.getFullYear()}-${pad(now.getMonth()+1)}-${pad(now.getDate())}T${pad(now.getHours())}${pad(now.getMinutes())}.svg`;
    const a = document.createElement('a');
    a.href = url;
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
  },

  setGraphHideCompleted(hide) {
    this._graphHideCompleted = hide;
    this.renderTaskGraph(this._lastGraphData);
  },

  async fetchAndRenderTaskGraph() {
    try {
      const res = await fetch('/api/office/task-graph');
      if (!res.ok) throw new Error('HTTP ' + res.status);
      const data = await res.json();
      this._lastGraphData = data;
      this.renderTaskGraph(data);
    } catch (err) {
      console.error('Failed to fetch task graph:', err);
    }
  },

  renderTaskGraph(data) {
    if (!data) return;
    const svg = document.getElementById('taskGraphSvg');
    const empty = document.getElementById('taskGraphEmpty');
    const banner = document.getElementById('taskGraphCycleBanner');
    if (!svg || !empty || !banner) return;

    // Cycle banner.
    if (data.cycleDetected && data.cycleNodeIds && data.cycleNodeIds.length) {
      banner.classList.remove('hidden');
      banner.textContent = `Cycle detected in: ${data.cycleNodeIds.join(' → ')}. Resolve via the task detail modal or cm task blocked.`;
    } else {
      banner.classList.add('hidden');
      banner.textContent = '';
    }

    // Apply the hide-completed filter.
    const hideDone = this._graphHideCompleted !== false; // default true
    let nodes = (data.nodes || []).slice();
    if (hideDone) {
      // Keep a completed task if any dependent (forward edge) exists,
      // since it anchors an in-flight chain.
      const hasDependent = new Set();
      for (const n of nodes) {
        for (const b of (n.blockedBy || [])) hasDependent.add(b);
      }
      nodes = nodes.filter(n => n.status !== 'completed' || hasDependent.has(n.id));
    }

    // Count edges to decide empty state.
    let edgeCount = 0;
    for (const n of nodes) edgeCount += (n.blockedBy || []).length;

    if (nodes.length === 0 || edgeCount === 0) {
      empty.classList.remove('hidden');
      svg.innerHTML = '';
      svg.setAttribute('viewBox', '0 0 100 100');
      return;
    }
    empty.classList.add('hidden');

    // Layer nodes by depth.
    const byDepth = new Map();
    for (const n of nodes) {
      const d = n.depth || 0;
      if (!byDepth.has(d)) byDepth.set(d, []);
      byDepth.get(d).push(n);
    }
    const depths = Array.from(byDepth.keys()).sort((a, b) => a - b);

    // Geometry.
    const NODE_W = 180;
    const NODE_H = 48;
    const COL_GAP = 64;
    const ROW_GAP = 14;
    const PAD = 24;

    // Stable per-agent colour. Reuse the member accent if available.
    const agentColor = (sessionId) => {
      const m = (this.state && this.state.members) ? this.state.members.find(x => x.sessionId === sessionId) : null;
      return (m && m.color) || '#6366f1';
    };

    // Position nodes: column = depth, row = sort within column.
    const pos = new Map();
    let maxRow = 0;
    depths.forEach((d, colIdx) => {
      const col = byDepth.get(d);
      col.sort((a, b) => {
        if ((a.agentName || '') !== (b.agentName || '')) return (a.agentName || '').localeCompare(b.agentName || '');
        const pr = { high: 0, medium: 1, low: 2 };
        const pa = pr[a.priority] ?? 3;
        const pb = pr[b.priority] ?? 3;
        if (pa !== pb) return pa - pb;
        return a.id.localeCompare(b.id);
      });
      col.forEach((n, rowIdx) => {
        pos.set(n.id, {
          x: PAD + colIdx * (NODE_W + COL_GAP),
          y: PAD + rowIdx * (NODE_H + ROW_GAP),
        });
        if (rowIdx > maxRow) maxRow = rowIdx;
      });
    });

    const width = PAD + depths.length * (NODE_W + COL_GAP);
    const height = PAD * 2 + (maxRow + 1) * (NODE_H + ROW_GAP);
    svg.setAttribute('viewBox', `0 0 ${width} ${height}`);
    svg.setAttribute('width', width);
    svg.setAttribute('height', height);

    // Build edges first (drawn underneath nodes). Direction is
    // prereq → dependent, so we iterate dependents' blockedBy lists.
    const critSet = new Set((data.criticalPath || []));
    const edgeFrags = [];
    const arrowFrags = [];
    const visibleIds = new Set(nodes.map(n => n.id));
    for (const n of nodes) {
      for (const prereqId of (n.blockedBy || [])) {
        if (!pos.has(prereqId) || !pos.has(n.id)) continue;
        if (!visibleIds.has(prereqId) || !visibleIds.has(n.id)) continue;
        const a = pos.get(prereqId);
        const b = pos.get(n.id);
        const x1 = a.x + NODE_W;
        const y1 = a.y + NODE_H / 2;
        const x2 = b.x;
        const y2 = b.y + NODE_H / 2;
        const mx = x1 + (x2 - x1) / 2;
        const path = `M${x1},${y1} C${mx},${y1} ${mx},${y2} ${x2},${y2}`;
        const prereqNode = nodes.find(x => x.id === prereqId);
        const isBlocking = (n.status === 'in_progress' || n.status === 'blocked')
          && prereqNode && prereqNode.status !== 'completed';
        const isCrit = critSet.has(prereqId) && critSet.has(n.id);
        const classes = ['task-graph-edge'];
        if (isBlocking) classes.push('blocking');
        if (isCrit) classes.push('on-critical');
        edgeFrags.push(
          `<path class="${classes.join(' ')}" d="${path}" data-from="${this.escapeAttr(prereqId)}" data-to="${this.escapeAttr(n.id)}"${n.blockerDescription ? ` data-tooltip="${this.escapeAttr(n.blockerDescription)}"` : ''}/>`
        );
        // Arrow head.
        const arrowSize = 5;
        const ah = `${x2},${y2} ${x2 - arrowSize},${y2 - arrowSize} ${x2 - arrowSize},${y2 + arrowSize}`;
        arrowFrags.push(
          `<polygon class="task-graph-edge-arrow${isBlocking ? ' blocking' : ''}" points="${ah}" fill="${isBlocking ? '#ef4444' : '#52525b'}"/>`
        );
      }
    }

    // Build nodes.
    const nodeFrags = [];
    for (const n of nodes) {
      const p = pos.get(n.id);
      if (!p) continue;
      const cls = ['task-graph-node', `status-${n.status || 'pending'}`];
      if (n.onCriticalPath) cls.push('on-critical');
      if (data.cycleDetected && data.cycleNodeIds && data.cycleNodeIds.includes(n.id)) cls.push('cycle');
      const color = n.unknown ? '#52525b' : agentColor(n.sessionId);
      const title = this.escapeHtml(this._truncateTaskTitle(n.title || n.id, 28));
      const critGlyph = n.onCriticalPath ? ' 🔥' : '';
      const meta = n.unknown ? 'missing' : (n.agentName || '');
      const onclickAttr = n.unknown
        ? ''
        : ` onclick="office.openTaskDetail('${this.escapeAttr(n.sessionId)}','${this.escapeAttr(n.id)}')"`;
      nodeFrags.push(`
        <g class="${cls.join(' ')}${n.unknown ? '' : ' task-graph-node-click'}" data-id="${this.escapeAttr(n.id)}" transform="translate(${p.x},${p.y})"${onclickAttr}>
          <rect width="${NODE_W}" height="${NODE_H}" rx="6" ry="6"/>
          <rect class="task-graph-node-band" x="0" y="0" width="4" height="${NODE_H}" fill="${color}" rx="2" ry="2"/>
          <text class="task-graph-node-title" x="12" y="18">${title}${critGlyph}</text>
          <text class="task-graph-node-meta" x="12" y="36">${this.escapeHtml(meta)} · ${this.escapeHtml(n.status || '')}</text>
        </g>`);
    }

    svg.innerHTML = edgeFrags.join('') + arrowFrags.join('') + nodeFrags.join('');

    // Hook hover to lowlight non-connected nodes + edges. Cheap DOM
    // traversal — task counts stay bounded by team size, so doing it
    // on every mouseenter is fine.
    const reach = this._buildGraphReach(nodes);
    svg.querySelectorAll('.task-graph-node').forEach(el => {
      el.addEventListener('mouseenter', () => this._graphHoverEnter(el.dataset.id, reach));
      el.addEventListener('mouseleave', () => this._graphHoverLeave());
    });
  },

  _truncateTaskTitle(s, max) {
    if (!s) return '';
    return s.length > max ? s.substring(0, max - 1) + '…' : s;
  },

  // _buildGraphReach returns maps of ancestors + descendants for every
  // node, keyed by node ID. Used for the hover-highlight behaviour so
  // the visual "reachable" set is computed once per render instead of
  // on every mouseenter.
  _buildGraphReach(nodes) {
    const forward = new Map(); // id → Set(dependents)
    const backward = new Map(); // id → Set(prereqs)
    for (const n of nodes) {
      if (!backward.has(n.id)) backward.set(n.id, new Set());
      for (const b of (n.blockedBy || [])) {
        if (!forward.has(b)) forward.set(b, new Set());
        forward.get(b).add(n.id);
        backward.get(n.id).add(b);
      }
    }
    const ancestors = new Map();
    const descendants = new Map();
    const walk = (graph, start) => {
      const out = new Set();
      const stack = [start];
      while (stack.length) {
        const cur = stack.pop();
        const next = graph.get(cur);
        if (!next) continue;
        for (const v of next) {
          if (!out.has(v)) { out.add(v); stack.push(v); }
        }
      }
      return out;
    };
    for (const n of nodes) {
      ancestors.set(n.id, walk(backward, n.id));
      descendants.set(n.id, walk(forward, n.id));
    }
    return { ancestors, descendants };
  },

  _graphHoverEnter(id, reach) {
    const svg = document.getElementById('taskGraphSvg');
    if (!svg) return;
    const keep = new Set([id]);
    (reach.ancestors.get(id) || new Set()).forEach(x => keep.add(x));
    (reach.descendants.get(id) || new Set()).forEach(x => keep.add(x));
    svg.querySelectorAll('.task-graph-node').forEach(el => {
      const nid = el.dataset.id;
      el.classList.toggle('lowlight', !keep.has(nid));
      el.classList.toggle('highlight', nid === id);
    });
    svg.querySelectorAll('.task-graph-edge').forEach(el => {
      const from = el.dataset.from, to = el.dataset.to;
      const visible = keep.has(from) && keep.has(to);
      el.classList.toggle('lowlight', !visible);
    });
  },

  _graphHoverLeave() {
    const svg = document.getElementById('taskGraphSvg');
    if (!svg) return;
    svg.querySelectorAll('.lowlight, .highlight').forEach(el => {
      el.classList.remove('lowlight');
      el.classList.remove('highlight');
    });
  },

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
  },

  _onTaskFilterInput(q) {
    this._taskFilter.q = q || '';
    this.renderTasks();
    this.renderKanbanBoard();
  },

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
  },

  _clearTaskFilters() {
    this._taskFilter = { q: '', priority: new Set(), status: new Set() };
    const input = document.getElementById('taskFilterSearch');
    if (input) input.value = '';
    document.querySelectorAll('.task-filter-chip.active').forEach(b => b.classList.remove('active'));
    this.renderTasks();
    this.renderKanbanBoard();
  },

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
  },
  async _kanbanTaskStart(sessionId, taskId) {
    try {
      await this.postJson('/api/office/agent/task-ack', { sessionId, taskId });
      await this.postJson('/api/office/agent/task-start', { sessionId, taskId });
    } catch (err) {
      this.showNotification('Failed to start task', 'error');
    }
  },
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
  },
  async _kanbanTaskComplete(sessionId, taskId) {
    try {
      await this.postJson('/api/office/agent/task-complete', { sessionId, taskId });
    } catch (err) {
      this.showNotification('Failed to complete task', 'error');
    }
  },

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
        html += queue.tasks.map(t => {
          // R29 · Render handoff pill + operator force-resolve controls
          // when a task is handoff_pending. The click target of the card
          // still opens the detail modal — buttons stop propagation.
          const isHandoff = t.status === 'handoff_pending' && t.handoff;
          const handoffPill = isHandoff
            ? `<span class="task-status status-pill--handoff">→ ${this.escapeHtml(t.handoff.toName || 'agent')}</span>`
            : `<span class="task-status task-status-${t.status}">${t.status}</span>`;
          const handoffCtx = isHandoff && t.handoff.context
            ? `<div class="task-card-handoff-ctx">${this.escapeHtml(t.handoff.context)}</div>`
            : '';
          const handoffControls = isHandoff
            ? `<div class="task-card-handoff-actions">
                <button class="btn-handoff-accept" onclick="event.stopPropagation(); office.forceAcceptHandoff('${this.escapeAttr(t.id)}')">Force accept</button>
                <button class="btn-handoff-reject" onclick="event.stopPropagation(); office.forceRejectHandoff('${this.escapeAttr(t.id)}')">Force reject</button>
              </div>`
            : '';
          const labelPills = (t.labels && t.labels.length)
            ? `<div class="task-card-labels">${t.labels.map(l => `<span class="task-label-pill task-label-pill--small">${this.escapeHtml(l)}</span>`).join('')}</div>`
            : '';
          return `
          <div class="task-card priority-${t.priority} task-card-clickable" onclick="office.openTaskDetail('${this.escapeAttr(queue.sessionId)}','${this.escapeAttr(t.id)}')">
            <div class="task-card-header">
              <span class="task-priority">${t.priority.toUpperCase()}</span>
              ${handoffPill}
            </div>
            <div class="task-card-desc">${this.escapeHtml(t.title)}</div>
            ${labelPills}
            ${handoffCtx}
            ${handoffControls}
          </div>
        `;}).join('');
      }
    }

    list.innerHTML = html;
  },

  // R29 · Operator-only handoff controls. Used when the receiving
  // agent is unresponsive — the operator decides whether to hand the
  // task off (accept) or send it back to the sender (reject).
  async forceAcceptHandoff(taskId) {
    if (!confirm('Force-accept this handoff? Task will move to the receiver as if they had accepted it.')) return;
    try {
      await this.postJson('/api/office/handoffs/force-accept', { taskId });
      this.fetchAndRenderTasks();
    } catch (err) {
      console.error('force-accept handoff failed:', err);
      this.showNotification('Force-accept failed: ' + (err.message || err), 'error');
    }
  },

  async forceRejectHandoff(taskId) {
    const reason = prompt('Reason for rejecting this handoff?', 'operator override');
    if (reason === null) return;
    try {
      await this.postJson('/api/office/handoffs/force-reject', { taskId, reason: reason || 'operator override' });
      this.fetchAndRenderTasks();
    } catch (err) {
      console.error('force-reject handoff failed:', err);
      this.showNotification('Force-reject failed: ' + (err.message || err), 'error');
    }
  },

  async resolveBugTask(taskId) {
    try {
      await this.putJson(`/api/office/bug-tasks/${taskId}`, { status: 'resolved' });
      this.fetchAndRenderTasks();
    } catch (err) {
      console.error('Failed to resolve bug task:', err);
    }
  },

  handleBugTaskCreated(task) {
    if (!this.bugTasks) this.bugTasks = [];
    this.bugTasks.push(task);
    if (this.activeTab === 'tasks') this.renderTasks();
    this.updateStatusBar();
  },

  handleBugTaskUpdated(task) {
    if (!this.bugTasks) return;
    const idx = this.bugTasks.findIndex(t => t.id === task.id);
    if (idx >= 0) {
      this.bugTasks[idx] = task;
    }
    if (this.activeTab === 'tasks') this.renderTasks();
    this.updateStatusBar();
  },

  handleBugTaskResolved(task) {
    if (!this.bugTasks) return;
    const idx = this.bugTasks.findIndex(t => t.id === task.id);
    if (idx >= 0) this.bugTasks[idx] = task;
    if (this.activeTab === 'tasks') this.renderTasks();
    this.updateStatusBar();
  },

  handleTaskQueueGenerated(data) {
    // Re-fetch full task queues from API (debounced to avoid rapid re-fetches)
    this._scheduleRender(() => this.fetchAndRenderTasks(), 'taskFetch', 300);
    const totalTasks = data.taskCount || 0;
    this.addActivityEntry('task', `Task queues generated: ${totalTasks} tasks for ${data.agentName || 'agent'}`);
    this.addNotification('info', 'Tasks generated', `${totalTasks} tasks for ${data.agentName || 'agent'}`);
    this._scheduleRender(() => this.renderAgentCards(), 'agents', 100);
    this.updateStatusBar();
  },

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
      if (data.task.status === 'completed') {
        this._sessionTasksCompleted++;
        this._taskCompletionTimestamps.push(Date.now());
        if (this._taskCompletionTimestamps.length > 500) {
          this._taskCompletionTimestamps = this._taskCompletionTimestamps.slice(-500);
        }
      }
    }
    // Track blocked transitions (independent of dedupe — a task can be
    // re-blocked after unblock).
    if (data && data.task && data.task.status === 'blocked') {
      this._taskBlockedTimestamps.push(Date.now());
      if (this._taskBlockedTimestamps.length > 500) {
        this._taskBlockedTimestamps = this._taskBlockedTimestamps.slice(-500);
      }
    }
    this._scheduleOverviewRefresh();
  },


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
  },


  });
})();
