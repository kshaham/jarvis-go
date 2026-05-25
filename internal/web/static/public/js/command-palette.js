/* P13.3 · Vanilla command palette. Triggered by ⌘K / Ctrl+K. Renders
 * an overlay with a fuzzy-search input + keyboard-driven result list.
 *
 * Self-contained: no framework, no global state, no external deps.
 * Mounts on first ⌘K press to avoid paying setup cost when unused.
 * Exposes window.CommandPalette.{open, close, register} for any
 * page-specific actions that want to extend the default set.
 */

(function () {
  'use strict';

  // ----- Default action registry --------------------------------------
  // Each entry: { id, label, group, hint?, run() }.
  // Actions are surfaced as fuzzy-search candidates. The `run` callback
  // fires on Enter / click. Always `await`able so the palette can close
  // after async work (e.g. CSRF-required POST).
  const defaultActions = [
    {
      id: 'nav-office',
      label: 'Go to Office dashboard',
      group: 'Navigate',
      hint: '/office',
      run: () => { window.location.href = '/office'; },
    },
    {
      id: 'nav-terminal',
      label: 'Go to Terminal sessions',
      group: 'Navigate',
      hint: '/',
      run: () => { window.location.href = '/'; },
    },
    {
      id: 'spawn-team',
      label: 'Open spawn team modal',
      group: 'Team',
      hint: 'or click "Spawn team" in the header',
      run: () => {
        // Trigger an existing spawn-modal button if present, else
        // bounce to /office where the spawn flow lives.
        const btn = document.getElementById('spawnTeamBtn')
                  || document.querySelector('[data-action="spawn-team"]');
        if (btn) btn.click();
        else window.location.href = '/office?spawn=1';
      },
    },
    {
      id: 'team-pause',
      label: 'Pause current team',
      group: 'Team',
      hint: 'no new tasks; in-flight turns complete',
      run: async () => {
        await postWithCSRF('/api/office/pause', { reason: 'operator (⌘K)' });
      },
    },
    {
      id: 'team-resume',
      label: 'Resume current team',
      group: 'Team',
      run: async () => {
        await postWithCSRF('/api/office/resume', {});
      },
    },
    {
      id: 'open-snapshots',
      label: 'Open snapshots',
      group: 'Snapshots',
      run: () => { window.location.href = '/office#snapshots'; },
    },
    {
      id: 'open-tool-policy',
      label: 'View tool policy (denylist)',
      group: 'Security',
      hint: 'GET /api/office/tool-policy',
      run: async () => {
        const resp = await fetch('/api/office/tool-policy', { credentials: 'include' });
        const text = await resp.text();
        alert('Tool policy:\n\n' + text);
      },
    },
    {
      id: 'open-handoffs',
      label: 'Open handoffs queue',
      group: 'Office',
      run: () => { window.location.href = '/office#handoffs'; },
    },
    {
      id: 'open-whiteboard',
      label: 'Open whiteboard',
      group: 'Office',
      run: () => { window.location.href = '/office#whiteboard'; },
    },
    {
      id: 'search-team',
      label: 'Search messages + tasks…',
      group: 'Search',
      hint: 'P13.8 team-wide search',
      run: () => {
        // Convert the palette into search mode by prefilling and
        // re-rendering against the new backend endpoint. For first
        // ship, just deep-link to the office page with a search hash.
        window.location.href = '/office#search';
      },
    },
  ];

  let mounted = false;
  let backdrop;        // .cmdk-backdrop
  let panel;           // .cmdk-panel
  let input;           // .cmdk-input
  let resultsEl;       // .cmdk-results
  let activeIndex = 0;
  let visibleItems = []; // filtered results in render order
  const userActions = [];
  let previousFocus = null;

  // ----- Public API --------------------------------------------------
  const api = {
    open() { ensureMounted(); openInternal(); },
    close() { if (backdrop) closeInternal(); },
    register(action) {
      if (!action || !action.id || !action.label || typeof action.run !== 'function') {
        console.warn('CommandPalette.register: invalid action', action);
        return;
      }
      userActions.push(action);
    },
  };
  window.CommandPalette = api;

  // ----- Global keybinding -------------------------------------------
  document.addEventListener('keydown', (e) => {
    const isCmdK = (e.key === 'k' || e.key === 'K') && (e.metaKey || e.ctrlKey);
    if (isCmdK) {
      e.preventDefault();
      if (backdrop && backdrop.classList.contains('open')) closeInternal();
      else api.open();
      return;
    }
    if (e.key === 'Escape' && backdrop && backdrop.classList.contains('open')) {
      e.preventDefault();
      closeInternal();
    }
  });

  // ----- Lazy mount --------------------------------------------------
  function ensureMounted() {
    if (mounted) return;
    mounted = true;
    backdrop = document.createElement('div');
    backdrop.className = 'cmdk-backdrop';
    backdrop.innerHTML = [
      '<div class="cmdk-panel" role="dialog" aria-label="Command palette">',
      '  <div class="cmdk-input-wrap">',
      '    <svg class="cmdk-icon" viewBox="0 0 16 16" fill="currentColor" aria-hidden="true">',
      '      <path d="M11.742 10.344a6.5 6.5 0 1 0-1.397 1.398l2.85 2.85a1 1 0 0 0 1.415-1.415l-2.868-2.833zM12 6.5a5.5 5.5 0 1 1-11 0 5.5 5.5 0 0 1 11 0z"/>',
      '    </svg>',
      '    <input class="cmdk-input" type="text" autocomplete="off" spellcheck="false" placeholder="Type a command or search…" aria-label="Command palette input"/>',
      '    <span class="cmdk-kbd-hint">esc</span>',
      '  </div>',
      '  <div class="cmdk-results" role="listbox"></div>',
      '  <div class="cmdk-footer">',
      '    <span>Press <kbd>⌘K</kbd> to open · <kbd>esc</kbd> to close</span>',
      '    <span class="cmdk-footer-keys">',
      '      <span class="cmdk-footer-key"><kbd>↑↓</kbd> nav</span>',
      '      <span class="cmdk-footer-key"><kbd>↵</kbd> run</span>',
      '    </span>',
      '  </div>',
      '</div>',
    ].join('');
    panel = backdrop.querySelector('.cmdk-panel');
    input = backdrop.querySelector('.cmdk-input');
    resultsEl = backdrop.querySelector('.cmdk-results');

    document.body.appendChild(backdrop);

    // Click outside the panel closes.
    backdrop.addEventListener('mousedown', (e) => {
      if (!panel.contains(e.target)) closeInternal();
    });

    input.addEventListener('input', renderResults);
    input.addEventListener('keydown', onInputKey);
    resultsEl.addEventListener('click', onResultClick);
  }

  // ----- Open / close ------------------------------------------------
  function openInternal() {
    previousFocus = document.activeElement;
    backdrop.classList.add('open');
    input.value = '';
    activeIndex = 0;
    renderResults();
    // Defer focus until the transition starts so iOS doesn't kill it.
    requestAnimationFrame(() => input.focus());
  }

  function closeInternal() {
    backdrop.classList.remove('open');
    if (previousFocus && typeof previousFocus.focus === 'function') {
      previousFocus.focus();
    }
    previousFocus = null;
  }

  // ----- Rendering ---------------------------------------------------
  function renderResults() {
    const q = (input.value || '').trim().toLowerCase();
    const all = defaultActions.concat(userActions);
    const ranked = q
      ? all.map((a) => ({ a, score: fuzzyScore(q, a.label.toLowerCase()) }))
           .filter((x) => x.score > 0)
           .sort((x, y) => y.score - x.score)
           .map((x) => x.a)
      : all.slice();
    visibleItems = ranked;
    activeIndex = 0;
    if (ranked.length === 0) {
      resultsEl.innerHTML = '<div class="cmdk-empty">No commands match.</div>';
      return;
    }
    // Group by .group for readability, preserving ranked order within
    // each group.
    const byGroup = new Map();
    ranked.forEach((a) => {
      const g = a.group || 'Other';
      if (!byGroup.has(g)) byGroup.set(g, []);
      byGroup.get(g).push(a);
    });
    let html = '';
    let flatIndex = 0;
    byGroup.forEach((items, group) => {
      html += '<div class="cmdk-group-header">' + escapeHTML(group) + '</div>';
      items.forEach((a) => {
        html += renderItem(a, flatIndex);
        flatIndex++;
      });
    });
    resultsEl.innerHTML = html;
    setActive(0);
  }

  function renderItem(a, idx) {
    const hint = a.hint ? '<span class="cmdk-item-hint">' + escapeHTML(a.hint) + '</span>' : '';
    return [
      '<div class="cmdk-item" data-idx="' + idx + '" role="option">',
      '  <span class="cmdk-item-label">' + escapeHTML(a.label) + '</span>',
      hint,
      '</div>',
    ].join('');
  }

  function setActive(idx) {
    if (visibleItems.length === 0) return;
    activeIndex = Math.max(0, Math.min(idx, visibleItems.length - 1));
    resultsEl.querySelectorAll('.cmdk-item').forEach((el, i) => {
      el.classList.toggle('active', i === activeIndex);
      if (i === activeIndex) el.scrollIntoView({ block: 'nearest' });
    });
  }

  // ----- Input keymap ------------------------------------------------
  function onInputKey(e) {
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      setActive(activeIndex + 1);
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      setActive(activeIndex - 1);
    } else if (e.key === 'Enter') {
      e.preventDefault();
      runActive();
    } else if (e.key === 'Tab') {
      // Trap focus.
      e.preventDefault();
    }
  }

  function onResultClick(e) {
    const row = e.target.closest('.cmdk-item');
    if (!row) return;
    const idx = parseInt(row.getAttribute('data-idx') || '-1', 10);
    if (idx < 0 || idx >= visibleItems.length) return;
    setActive(idx);
    runActive();
  }

  async function runActive() {
    const a = visibleItems[activeIndex];
    if (!a) return;
    closeInternal();
    try {
      await a.run();
    } catch (err) {
      console.error('CommandPalette: action failed', a.id, err);
      alert('Command failed: ' + (err && err.message ? err.message : String(err)));
    }
  }

  // ----- Helpers -----------------------------------------------------

  // fuzzyScore returns a positive score when every char of `q` appears
  // in `text` in order; higher score for tighter matches + prefix
  // hits. 0 = no match.
  function fuzzyScore(q, text) {
    if (!q) return 0.5;
    let score = 0;
    let qi = 0;
    let lastIdx = -1;
    for (let i = 0; i < text.length && qi < q.length; i++) {
      if (text[i] === q[qi]) {
        score += 1;
        if (lastIdx >= 0 && i === lastIdx + 1) score += 1; // consecutive
        if (i === 0 || text[i - 1] === ' ') score += 2;   // word start
        lastIdx = i;
        qi++;
      }
    }
    if (qi < q.length) return 0; // not all chars matched
    return score;
  }

  async function postWithCSRF(url, body) {
    const csrf = getCSRFToken();
    const resp = await fetch(url, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'X-CSRF-Token': csrf,
      },
      credentials: 'include',
      body: JSON.stringify(body || {}),
    });
    if (!resp.ok) {
      const text = await resp.text().catch(() => '');
      throw new Error('HTTP ' + resp.status + ': ' + text);
    }
    return resp;
  }

  function getCSRFToken() {
    // jarvis sets the CSRF token on a meta tag on every page that
    // needs it. Fall back to the cookie if the tag isn't present.
    const meta = document.querySelector('meta[name="csrf-token"]');
    if (meta) return meta.getAttribute('content') || '';
    const m = document.cookie.match(/(?:^|;\s*)csrf_token=([^;]+)/);
    return m ? decodeURIComponent(m[1]) : '';
  }

  function escapeHTML(s) {
    return String(s)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;')
      .replace(/'/g, '&#39;');
  }
})();
