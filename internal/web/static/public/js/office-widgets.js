/* P13 widget helpers — activity strip + per-agent cost sparkline + the
 * shared SVG sparkline renderer. Extracted from office.js to keep that
 * file scannable; behavior is unchanged.
 *
 * Loaded as a sibling <script> AFTER office.js in office.html. Uses
 * Object.assign on OfficeApp.prototype so methods are live on the
 * already-constructed `office` instance — JS prototype lookups are
 * dynamic, so this works even though `const office = new OfficeApp()`
 * runs at the bottom of office.js before this file evaluates.
 */

(function () {
  'use strict';
  if (typeof OfficeApp === 'undefined') {
    console.error('office-widgets.js loaded before OfficeApp; check script order in office.html');
    return;
  }

  Object.assign(OfficeApp.prototype, {

    // Activity strip — three rolling-window widgets (cost-7d,
    // throughput-24h, agent-health). Pulls from in-memory state +
    // task-completion ring buffers; no extra endpoints. Idempotent.
    _renderActivityStrip() {
      const root = document.getElementById('activityStrip');
      if (!root) return;
      const members = (this.state && Array.isArray(this.state.members)) ? this.state.members : [];
      if (members.length === 0) {
        root.classList.add('hidden');
        return;
      }
      root.classList.remove('hidden');

      const snap = this._computeTeamSnapshot();

      // ---- Cost widget (session-cumulative; sparkline from per-member cost) ----
      const cost7dEl = document.getElementById('activityCost7d');
      const costTodayEl = document.getElementById('activityCostToday');
      const costAvgEl = document.getElementById('activityCostAvg');
      if (cost7dEl) cost7dEl.textContent = this._formatCost(snap.totalCost);
      if (costTodayEl) costTodayEl.textContent = `${this._formatCost(snap.totalCost)} today`;
      if (costAvgEl) {
        const perMember = members.length > 0 ? snap.totalCost / members.length : 0;
        costAvgEl.textContent = `${this._formatCost(perMember)} avg/agent`;
      }
      const costSeries = members
        .map((m) => Number(m.totalCost) || 0)
        .sort((a, b) => a - b);
      this._drawSparkline('activityCostSpark', costSeries, { fill: true });

      // ---- Throughput widget (24h task-completion histogram) ----
      // Source: ring buffers updated in the task-status SSE handler.
      const now = Date.now();
      const dayMs = 24 * 60 * 60 * 1000;
      const bucketCount = 24;
      const bucketMs = dayMs / bucketCount;
      const buckets = new Array(bucketCount).fill(0);
      const completions = Array.isArray(this._taskCompletionTimestamps) ? this._taskCompletionTimestamps : [];
      let completed24h = 0;
      for (const ts of completions) {
        if (typeof ts !== 'number') continue;
        const age = now - ts;
        if (age < 0 || age >= dayMs) continue;
        const idx = Math.min(bucketCount - 1, Math.floor((dayMs - age) / bucketMs));
        buckets[idx]++;
        completed24h++;
      }
      const blockers = Array.isArray(this._taskBlockedTimestamps) ? this._taskBlockedTimestamps : [];
      let blocked24h = 0;
      for (const ts of blockers) {
        if (typeof ts !== 'number') continue;
        if (now - ts >= dayMs) continue;
        blocked24h++;
      }
      const tp24El = document.getElementById('activityThroughput24h');
      const tpDoneEl = document.getElementById('activityThroughputCompleted');
      const tpBlockedEl = document.getElementById('activityThroughputBlocked');
      if (tp24El) tp24El.textContent = String(completed24h);
      if (tpDoneEl) tpDoneEl.textContent = `${completed24h} done`;
      if (tpBlockedEl) tpBlockedEl.textContent = `${blocked24h} blocked`;
      this._drawSparkline('activityThroughputSpark', buckets, { fill: true });

      // ---- Agent health widget ----
      const total = snap.total || 1;
      const setBarWidth = (status, count) => {
        const el = root.querySelector(`.activity-health-bar[data-status="${status}"]`);
        if (el) el.style.setProperty('--w', `${Math.round((count / total) * 100)}%`);
      };
      setBarWidth('working', snap.counts.working);
      setBarWidth('idle',    snap.counts.idle);
      setBarWidth('meeting', snap.counts.meeting);
      setBarWidth('stuck',   snap.counts.stuck);
      const healthTotalEl = document.getElementById('activityHealthTotal');
      if (healthTotalEl) healthTotalEl.textContent = String(snap.total);
      const healthWorkingEl = document.getElementById('activityHealthWorking');
      const healthStuckEl = document.getElementById('activityHealthStuck');
      if (healthWorkingEl) healthWorkingEl.textContent = String(snap.counts.working);
      if (healthStuckEl) healthStuckEl.textContent = String(snap.counts.stuck);
    },

    // Tiny SVG sparkline renderer. Maps `values` into the viewBox of
    // the target <svg>, drawing a 1.5px line plus (optionally) a
    // translucent area fill. Empty / flat data renders a baseline so
    // the widget still looks alive.
    _drawSparkline(svgId, values, opts) {
      const svg = document.getElementById(svgId);
      if (!svg) return;
      const vbW = 120;
      const vbH = 32;
      const fill = opts && opts.fill;
      if (!values || values.length === 0) {
        svg.innerHTML = `<path d="M0 ${vbH - 1} L${vbW} ${vbH - 1}"/>`;
        return;
      }
      const max = Math.max(...values, 1);
      const min = Math.min(...values, 0);
      const range = Math.max(1, max - min);
      const stepX = values.length > 1 ? vbW / (values.length - 1) : vbW;
      const pts = values.map((v, i) => {
        const x = i * stepX;
        const y = vbH - 2 - ((v - min) / range) * (vbH - 4);
        return `${x.toFixed(1)},${y.toFixed(1)}`;
      });
      const linePath = `M${pts.join(' L')}`;
      const fillPath = fill
        ? `<path class="spark-fill" d="${linePath} L${vbW.toFixed(1)},${vbH} L0,${vbH} Z"/>`
        : '';
      svg.innerHTML = `${fillPath}<path d="${linePath}"/>`;
    },

    // Fetch /api/office/agent/<sid>/usage-series and render the
    // sparkline + summary into the detail panel. Silent failure so a
    // missing turn history doesn't disrupt the panel.
    async _loadDetailUsageSeries(sessionId) {
      const statsEl = document.getElementById('detailUsageStats');
      if (!statsEl) return; // panel closed before fetch returned
      try {
        const res = await fetch(`/api/office/agent/${encodeURIComponent(sessionId)}/usage-series`, { credentials: 'include' });
        if (!res.ok) {
          statsEl.textContent = res.status === 404 ? 'No turns recorded yet.' : `Usage unavailable (${res.status})`;
          return;
        }
        const data = await res.json();
        const turns = Array.isArray(data.recentTurnCosts) ? data.recentTurnCosts : [];
        this._drawSparkline('detailUsageSpark', turns, { fill: true });
        const avgC = (data.costPerTurnAvg || 0) * 100;
        const totalUSD = data.totalCostUSD || 0;
        const totalStr = totalUSD >= 1 ? '$' + totalUSD.toFixed(2) : (totalUSD * 100).toFixed(0) + 'c';
        statsEl.textContent = `${data.turnCount || 0} turns · ${totalStr} total · ${avgC.toFixed(1)}c avg/turn`;
      } catch (err) {
        statsEl.textContent = 'Usage unavailable';
      }
    },

  });
})();
