// Expand Mode Feature Module for Jarvis
(function() {
  'use strict';

  // Current session ID (set when session modal opens)
  var currentSessionId = null;
  var pollInterval = null;
  var lastKnownState = null;

  // Wait for app to be available
  function waitForApp(callback) {
    if (typeof window.app !== 'undefined') {
      callback(window.app);
    } else {
      setTimeout(function() { waitForApp(callback); }, 100);
    }
  }

  // Get CSRF token from app
  async function getCsrfToken() {
    if (window.app && typeof window.app.getCsrfToken === 'function') {
      return await window.app.getCsrfToken();
    }
    return null;
  }

  // Format timestamp to readable time
  function formatTime(timestamp) {
    if (!timestamp) return '-';
    var date = new Date(timestamp);
    return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  }

  // Format duration in ms to readable string
  function formatDuration(ms) {
    if (!ms) return '-';
    var seconds = Math.floor(ms / 1000);
    var minutes = Math.floor(seconds / 60);
    seconds = seconds % 60;
    return minutes + 'm ' + seconds + 's';
  }

  // Parse comma-separated string to array
  function parseCommaSeparated(str) {
    if (!str || !str.trim()) return [];
    return str.split(',').map(function(s) { return s.trim(); }).filter(function(s) { return s; });
  }

  // Array to comma-separated string
  function toCommaSeparated(arr) {
    if (!arr || !Array.isArray(arr)) return '';
    return arr.join(', ');
  }

  // Load expand mode status and settings for a session
  function loadExpandStatus(sessionId) {
    if (!sessionId) return;

    fetch('/api/sessions/' + sessionId + '/expand')
      .then(function(response) { if (!response.ok) throw new Error('HTTP ' + response.status); return response.json(); })
      .then(function(data) {
        if (data.error) {
          console.error('[Expand] Failed to load status:', data.error);
          return;
        }
        updateUI(data.state, data.settings);
        lastKnownState = data.state;
        if (data.state && data.state.active) {
          startPolling();
        }
      })
      .catch(function(err) {
        console.error('[Expand] Error loading status:', err);
      });
  }

  // Load expand history for a session
  function loadExpandHistory(sessionId) {
    if (!sessionId) return;

    fetch('/api/sessions/' + sessionId + '/expand/history')
      .then(function(response) { if (!response.ok) throw new Error('HTTP ' + response.status); return response.json(); })
      .then(function(data) {
        if (data.error) {
          console.error('[Expand] Failed to load history:', data.error);
          return;
        }
        updateHistoryUI(data.history || []);
      })
      .catch(function(err) {
        console.error('[Expand] Error loading history:', err);
      });
  }

  // Update the UI with current state and settings
  function updateUI(state, settings) {
    // Update status indicator
    var statusDot = document.getElementById('expandStatusDot');
    var statusText = document.getElementById('expandStatusText');
    var expandStats = document.getElementById('expandStats');
    var startBtn = document.getElementById('expandStartBtn');
    var stopBtn = document.getElementById('expandStopBtn');
    var currentCycle = document.getElementById('expandCurrentCycle');

    if (state && state.active) {
      // Active state
      statusDot.className = 'status-dot ' + (state.stopRequested ? 'stopping' : 'active');
      statusText.textContent = state.stopRequested ? 'Stopping...' : 'Running - Cycle ' + (state.currentCycle || state.cyclesCompleted + 1);
      expandStats.style.display = 'grid';
      startBtn.style.display = 'none';
      stopBtn.style.display = 'inline-flex';
      currentCycle.style.display = 'block';

      // Update stats
      document.getElementById('expandCyclesCompleted').textContent = state.cyclesCompleted || 0;
      document.getElementById('expandStartedAt').textContent = formatTime(state.startedAt);
      document.getElementById('expandEndsAt').textContent = state.scheduledEndTime ? formatTime(state.scheduledEndTime) : 'Indefinite';

      // Update current cycle phase
      if (state.lastCycleResult) {
        document.getElementById('expandCurrentPhase').textContent = 'Processing...';
        var progress = Math.min(100, ((Date.now() - state.lastCycleResult.timestamp) / (5 * 60 * 1000)) * 100);
        document.getElementById('expandProgressFill').style.width = progress + '%';
      }
    } else {
      // Inactive state
      statusDot.className = 'status-dot inactive';
      statusText.textContent = state && state.cyclesCompleted > 0 ?
        'Completed ' + state.cyclesCompleted + ' cycles' : 'Inactive';
      expandStats.style.display = state && state.cyclesCompleted > 0 ? 'grid' : 'none';
      startBtn.style.display = 'inline-flex';
      stopBtn.style.display = 'none';
      currentCycle.style.display = 'none';

      if (state && state.cyclesCompleted > 0) {
        document.getElementById('expandCyclesCompleted').textContent = state.cyclesCompleted;
        document.getElementById('expandStartedAt').textContent = formatTime(state.startedAt);
        document.getElementById('expandEndsAt').textContent = '-';
      }
    }

    // Populate settings if available
    if (settings) {
      var durationInput = document.getElementById('expandDuration');
      var intervalInput = document.getElementById('expandInterval');
      var maxChangesInput = document.getElementById('expandMaxChanges');
      var focusAreasInput = document.getElementById('expandFocusAreas');
      var excludePatternsInput = document.getElementById('expandExcludePatterns');
      var autoCommitInput = document.getElementById('expandAutoCommit');
      var commitPrefixInput = document.getElementById('expandCommitPrefix');

      if (durationInput) durationInput.value = settings.duration || 30;
      if (intervalInput) intervalInput.value = settings.intervalBetweenCycles || 5;
      if (maxChangesInput) maxChangesInput.value = settings.maxChangesPerCycle || 5;
      if (focusAreasInput) focusAreasInput.value = toCommaSeparated(settings.focusAreas);
      if (excludePatternsInput) excludePatternsInput.value = toCommaSeparated(settings.excludePatterns);
      if (autoCommitInput) autoCommitInput.checked = settings.autoCommit !== false;
      if (commitPrefixInput) commitPrefixInput.value = settings.commitPrefix || 'expand:';

      updateCommitPrefixVisibility();
    }
  }

  // Update history UI
  function updateHistoryUI(history) {
    var emptyDiv = document.getElementById('expandHistoryEmpty');
    var listDiv = document.getElementById('expandHistoryList');

    if (!history || history.length === 0) {
      emptyDiv.style.display = 'block';
      listDiv.style.display = 'none';
      return;
    }

    emptyDiv.style.display = 'none';
    listDiv.style.display = 'flex';

    // Build history HTML (newest first)
    var html = '';
    var reversedHistory = history.slice().reverse();

    for (var i = 0; i < reversedHistory.length && i < 20; i++) {
      var entry = reversedHistory[i];
      var statusClass = entry.status || 'success';

      html += '<div class="history-entry ' + statusClass + '">';
      html += '  <div class="history-header">';
      html += '    <span class="history-cycle-num">Cycle ' + entry.cycleNumber + '</span>';
      html += '    <span class="history-time">' + formatTime(entry.timestamp) + ' (' + formatDuration(entry.duration) + ')</span>';
      html += '  </div>';
      html += '  <div class="history-details">';
      html += '    ' + entry.changesApplied + ' change(s) applied';
      if (entry.error) {
        var safeError = String(entry.error).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');
        html += ' - <span style="color: #ef4444;">' + safeError + '</span>';
      }
      html += '  </div>';

      if (entry.filesModified && entry.filesModified.length > 0) {
        html += '  <div class="history-files">';
        html += '    <div class="history-files-label">Files modified:</div>';
        html += '    <div class="history-file-list">' + entry.filesModified.slice(0, 5).join(', ');
        if (entry.filesModified.length > 5) {
          html += ' ... +' + (entry.filesModified.length - 5) + ' more';
        }
        html += '</div>';
        html += '  </div>';
      }

      if (entry.commitHash) {
        html += '  <div class="history-commit">Committed: ' + entry.commitHash.substring(0, 7) + '</div>';
      }

      html += '</div>';
    }

    listDiv.innerHTML = html;
  }

  // Update commit prefix visibility based on auto-commit checkbox
  function updateCommitPrefixVisibility() {
    var autoCommit = document.getElementById('expandAutoCommit');
    var commitPrefixRow = document.getElementById('commitPrefixRow');
    if (autoCommit && commitPrefixRow) {
      commitPrefixRow.style.display = autoCommit.checked ? 'block' : 'none';
    }
  }

  // Get current settings from form
  function getSettingsFromForm() {
    return {
      duration: parseInt(document.getElementById('expandDuration')?.value) || 30,
      intervalBetweenCycles: parseInt(document.getElementById('expandInterval')?.value) || 5,
      maxChangesPerCycle: parseInt(document.getElementById('expandMaxChanges')?.value) || 5,
      focusAreas: parseCommaSeparated(document.getElementById('expandFocusAreas')?.value),
      excludePatterns: parseCommaSeparated(document.getElementById('expandExcludePatterns')?.value),
      autoCommit: document.getElementById('expandAutoCommit')?.checked !== false,
      commitPrefix: document.getElementById('expandCommitPrefix')?.value || 'expand:'
    };
  }

  // Start expand mode
  function startExpandMode() {
    if (!currentSessionId) {
      alert('No session selected');
      return;
    }

    var settings = getSettingsFromForm();

    getCsrfToken().then(function(csrfToken) {
      var headers = { 'Content-Type': 'application/json' };
      if (csrfToken) {
        headers['X-CSRF-Token'] = csrfToken;
      }

      fetch('/api/sessions/' + currentSessionId + '/expand/start', {
        method: 'POST',
        headers: headers,
        body: JSON.stringify(settings)
      })
      .then(function(response) { if (!response.ok) throw new Error('HTTP ' + response.status); return response.json(); })
      .then(function(data) {
        if (data.error) {
          alert('Failed to start: ' + data.error);
          return;
        }
        updateUI(data.state, data.settings);
        startPolling();
        console.log('[Expand] Started for session', currentSessionId);
      })
      .catch(function(err) {
        console.error('[Expand] Error starting:', err);
        alert('Error starting expand mode');
      });
    });
  }

  // Stop expand mode
  function stopExpandMode() {
    if (!currentSessionId) return;

    getCsrfToken().then(function(csrfToken) {
      var headers = { 'Content-Type': 'application/json' };
      if (csrfToken) {
        headers['X-CSRF-Token'] = csrfToken;
      }

      fetch('/api/sessions/' + currentSessionId + '/expand/stop', {
        method: 'POST',
        headers: headers
      })
      .then(function(response) { if (!response.ok) throw new Error('HTTP ' + response.status); return response.json(); })
      .then(function(data) {
        if (data.error) {
          alert('Failed to stop: ' + data.error);
          return;
        }
        console.log('[Expand] Stop requested for session', currentSessionId);
        // UI will update on next poll
      })
      .catch(function(err) {
        console.error('[Expand] Error stopping:', err);
      });
    });
  }

  // Save configuration
  function saveConfiguration() {
    if (!currentSessionId) {
      alert('No session selected');
      return;
    }

    var settings = getSettingsFromForm();

    getCsrfToken().then(function(csrfToken) {
      var headers = { 'Content-Type': 'application/json' };
      if (csrfToken) {
        headers['X-CSRF-Token'] = csrfToken;
      }

      fetch('/api/sessions/' + currentSessionId + '/expand/config', {
        method: 'POST',
        headers: headers,
        body: JSON.stringify(settings)
      })
      .then(function(response) { if (!response.ok) throw new Error('HTTP ' + response.status); return response.json(); })
      .then(function(data) {
        if (data.error) {
          showSaveStatus('Error: ' + data.error, false);
          return;
        }
        showSaveStatus('Configuration saved!', true);
      })
      .catch(function(err) {
        console.error('[Expand] Error saving config:', err);
        showSaveStatus('Error saving configuration', false);
      });
    });
  }

  // Show save status message
  function showSaveStatus(message, success) {
    var status = document.getElementById('expandSaveStatus');
    if (status) {
      status.textContent = message;
      status.style.color = success ? '#4caf50' : '#ef4444';
      setTimeout(function() { status.textContent = ''; }, 3000);
    }
  }

  // Start polling for status updates
  function startPolling() {
    stopPolling();
    pollInterval = setInterval(function() {
      if (currentSessionId) {
        loadExpandStatus(currentSessionId);
        loadExpandHistory(currentSessionId);
      }
    }, 5000); // Poll every 5 seconds
    // Register with app timer registry if available
    if (window.app && window.app.registerTimer) {
      window.app.registerTimer(pollInterval);
    }
  }

  // Stop polling
  function stopPolling() {
    if (pollInterval) {
      // Unregister from app timer registry if available
      if (window.app && window.app.unregisterTimer) {
        window.app.unregisterTimer(pollInterval);
      }
      clearInterval(pollInterval);
      pollInterval = null;
    }
  }

  // Initialize UI when session modal opens
  function initExpandUI(sessionId) {
    currentSessionId = sessionId;
    lastKnownState = null;
    loadExpandStatus(sessionId);
    loadExpandHistory(sessionId);
  }

  // Cleanup when modal closes
  function cleanupExpandUI() {
    currentSessionId = null;
    stopPolling();
  }

  // Set up event listeners
  function setupEventListeners() {
    // Start button
    var startBtn = document.getElementById('expandStartBtn');
    if (startBtn) {
      startBtn.addEventListener('click', startExpandMode);
    }

    // Stop button
    var stopBtn = document.getElementById('expandStopBtn');
    if (stopBtn) {
      stopBtn.addEventListener('click', stopExpandMode);
    }

    // Save config button
    var saveBtn = document.getElementById('expandSaveConfigBtn');
    if (saveBtn) {
      saveBtn.addEventListener('click', saveConfiguration);
    }

    // Auto-commit checkbox
    var autoCommit = document.getElementById('expandAutoCommit');
    if (autoCommit) {
      autoCommit.addEventListener('change', updateCommitPrefixVisibility);
    }

    // Watch for session modal open/close
    // Scope observer to specific modal element instead of entire document.body
    var modalEl = document.getElementById('sessionModal') || document.querySelector('.session-modal');
    if (modalEl) {
      var observer = new MutationObserver(function(mutations) {
        mutations.forEach(function(mutation) {
          if (mutation.type === 'attributes') {
            var isVisible = !modalEl.classList.contains('hidden') &&
                           window.getComputedStyle(modalEl).display !== 'none';
            if (isVisible) {
              waitForApp(function(app) {
                var sessionId = app.selectedSessionId || app.currentSession?.id;
                if (sessionId) {
                  initExpandUI(sessionId);
                }
              });
            } else {
              cleanupExpandUI();
            }
          }
        });
      });
      observer.observe(modalEl, { attributes: true, attributeFilter: ['class', 'style'] });
    }

    console.log('[Expand] Event listeners set up');
  }

  // Initialize module
  function init() {
    setupEventListeners();
    updateCommitPrefixVisibility();
    console.log('[Expand] Module initialized');
  }

  // Initialize when DOM is loaded
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', function() {
      setTimeout(init, 500);
    });
  } else {
    setTimeout(init, 500);
  }

  // Expose functions for external use
  window.expandMode = {
    init: initExpandUI,
    cleanup: cleanupExpandUI,
    start: startExpandMode,
    stop: stopExpandMode,
    saveConfig: saveConfiguration
  };

})();

