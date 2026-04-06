// Accounts Feature Module for Jarvis
// Security improvements: CSRF protection, no sensitive data in localStorage
// Code quality: DOM caching, debouncing, timeout, proper logging
(function() {
  'use strict';

  var MODULE_NAME = 'Accounts';
  var TIMING = {
    POLL_INTERVAL_MS: 100,
    MAX_WAIT_FOR_APP_MS: 10000,
    DEBOUNCE_INPUT_MS: 150,
    STATUS_MESSAGE_TIMEOUT_MS: 3000
  };

  // CSRF token management
  var csrfToken = null;

  // DOM element cache for performance
  var domCache = {};

  // Get cached DOM element or query and cache it
  function getElement(id) {
    if (!domCache[id]) {
      domCache[id] = document.getElementById(id);
    }
    return domCache[id];
  }

  // Clear DOM cache (call on page reload/navigation)
  function clearDomCache() {
    domCache = {};
  }

  // Fetch CSRF token from server
  function fetchCsrfToken() {
    return fetch('/api/csrf-token')
      .then(function(response) { if (!response.ok) throw new Error('HTTP ' + response.status); return response.json(); })
      .then(function(data) {
        csrfToken = data.token;
        return csrfToken;
      })
      .catch(function(err) {
        ClientLogger.error(MODULE_NAME, 'Failed to fetch CSRF token', err);
        return null;
      });
  }

  // Wait for app to be available with timeout
  function waitForApp(callback, timeoutMs) {
    var maxWait = timeoutMs || TIMING.MAX_WAIT_FOR_APP_MS;
    var startTime = Date.now();

    function poll() {
      if (typeof window.app !== 'undefined') {
        callback(window.app);
      } else if (Date.now() - startTime >= maxWait) {
        ClientLogger.warn(MODULE_NAME, 'Timed out waiting for app after ' + maxWait + 'ms');
        // Still try to initialize without app reference
        callback(null);
      } else {
        setTimeout(poll, TIMING.POLL_INTERVAL_MS);
      }
    }
    poll();
  }

  // Update provider settings visibility
  function updateProviderSettings() {
    var provider = getElement('appSettingsModelProvider');
    var claudeApiSettings = getElement('claudeApiSettings');
    var zaiSettings = getElement('zaiSettings');

    if (provider && claudeApiSettings) {
      claudeApiSettings.style.display = provider.value === 'claude-api' ? 'block' : 'none';
    }
    if (provider && zaiSettings) {
      zaiSettings.style.display = provider.value === 'zai-glm' ? 'block' : 'none';
    }
  }

  // Update current profile name display (internal implementation)
  function updateProfileNameDisplayImpl() {
    var activeProfile = getElement('appSettingsActiveProfile');
    var profileNameSpan = getElement('currentProfileName');
    var profile1Name = getElement('appSettingsProfile1Name');
    var profile2Name = getElement('appSettingsProfile2Name');

    if (activeProfile && profileNameSpan) {
      var profileValue = activeProfile.value;
      var displayName = 'Default';

      if (profileValue === 'profile1') {
        displayName = (profile1Name && profile1Name.value) ? profile1Name.value : 'Profile 1';
      } else if (profileValue === 'profile2') {
        displayName = (profile2Name && profile2Name.value) ? profile2Name.value : 'Profile 2';
      }

      profileNameSpan.textContent = displayName;
    }
  }

  // Debounced version for input handlers (prevents excessive updates)
  var updateProfileNameDisplay = typeof debounce === 'function'
    ? debounce(updateProfileNameDisplayImpl, TIMING.DEBOUNCE_INPUT_MS)
    : updateProfileNameDisplayImpl;

  // Update Claude mode settings visibility
  function updateClaudeModeSettings() {
    var claudeMode = getElement('appSettingsClaudeMode');
    var allowedToolsSettings = getElement('allowedToolsSettings');

    if (claudeMode && allowedToolsSettings) {
      allowedToolsSettings.style.display = claudeMode.value === 'allowed-tools' ? 'block' : 'none';
    }
  }

  // Load settings from server first, then localStorage as fallback (non-sensitive only)
  function loadAccountSettings(callback) {
    fetch('/api/settings/accounts')
      .then(function(response) { if (!response.ok) throw new Error('HTTP ' + response.status); return response.json(); })
      .then(function(data) {
        ClientLogger.debug(MODULE_NAME, 'Loaded settings from server');
        callback(data);
      })
      .catch(function(err) {
        ClientLogger.warn(MODULE_NAME, 'Failed to load from server, using localStorage fallback', err);
        try {
          // Only load non-sensitive settings from localStorage as fallback
          var stored = localStorage.getItem('jarvis-account-settings-nonsensitive');
          var fallback = stored ? JSON.parse(stored) : getDefaultSettings();
          // Never restore API keys from localStorage
          fallback.claudeApiKey = '';
          fallback.zaiApiKey = '';
          callback(fallback);
        } catch (e) {
          ClientLogger.error(MODULE_NAME, 'localStorage fallback failed', e);
          callback(getDefaultSettings());
        }
      });
  }

  function getDefaultSettings() {
    return {
      provider: 'claude-subscription',
      claudeApiKey: '',
      zaiApiKey: '',
      zaiModel: 'glm-5.1',
      zaiSonnetModel: 'glm-4.7',
      zaiHaikuModel: 'glm-4.5-air',
      activeProfile: 'default',
      profile1Name: '',
      profile2Name: '',
      defaultWorkingDir: '',
      claudeMode: 'skip-permissions',
      allowedTools: ''
    };
  }

  // Populate form with settings
  function populateSettings(settings) {
    // Clear DOM cache to ensure fresh elements
    clearDomCache();

    var providerSelect = getElement('appSettingsModelProvider');
    if (providerSelect) providerSelect.value = settings.provider || 'claude-subscription';

    var claudeApiKey = getElement('appSettingsClaudeApiKey');
    if (claudeApiKey) claudeApiKey.value = settings.claudeApiKey || '';

    var zaiApiKey = getElement('appSettingsZaiApiKey');
    if (zaiApiKey) zaiApiKey.value = settings.zaiApiKey || '';

    var zaiModel = getElement('appSettingsZaiModel');
    if (zaiModel) zaiModel.value = settings.zaiModel || 'glm-5';

    var zaiSonnetModel = getElement('appSettingsZaiSonnetModel');
    if (zaiSonnetModel) zaiSonnetModel.value = settings.zaiSonnetModel || 'glm-4.7';

    var zaiHaikuModel = getElement('appSettingsZaiHaikuModel');
    if (zaiHaikuModel) zaiHaikuModel.value = settings.zaiHaikuModel || 'glm-4.5-air';

    var activeProfile = getElement('appSettingsActiveProfile');
    if (activeProfile) activeProfile.value = settings.activeProfile || 'default';

    var profile1Name = getElement('appSettingsProfile1Name');
    if (profile1Name) profile1Name.value = settings.profile1Name || '';

    var profile2Name = getElement('appSettingsProfile2Name');
    if (profile2Name) profile2Name.value = settings.profile2Name || '';

    var defaultWorkingDir = getElement('appSettingsDefaultWorkingDir');
    if (defaultWorkingDir) defaultWorkingDir.value = settings.defaultWorkingDir || '';

    var claudeMode = getElement('appSettingsClaudeMode');
    if (claudeMode) claudeMode.value = settings.claudeMode || 'skip-permissions';

    var allowedTools = getElement('appSettingsAllowedTools');
    if (allowedTools) allowedTools.value = settings.allowedTools || '';

    updateProviderSettings();
    updateClaudeModeSettings();
    updateProfileNameDisplayImpl(); // Use non-debounced for initial populate
  }

  // Save settings with CSRF protection
  function saveSettings() {
    var providerEl = getElement('appSettingsModelProvider');
    var settings = {
      provider: providerEl ? providerEl.value : 'claude-subscription',
      claudeApiKey: (getElement('appSettingsClaudeApiKey') || {}).value || '',
      zaiApiKey: (getElement('appSettingsZaiApiKey') || {}).value || '',
      zaiModel: (getElement('appSettingsZaiModel') || {}).value || 'glm-5.1',
      zaiSonnetModel: (getElement('appSettingsZaiSonnetModel') || {}).value || 'glm-4.7',
      zaiHaikuModel: (getElement('appSettingsZaiHaikuModel') || {}).value || 'glm-4.5-air',
      activeProfile: (getElement('appSettingsActiveProfile') || {}).value || 'default',
      profile1Name: (getElement('appSettingsProfile1Name') || {}).value || '',
      profile2Name: (getElement('appSettingsProfile2Name') || {}).value || '',
      defaultWorkingDir: (getElement('appSettingsDefaultWorkingDir') || {}).value || '',
      claudeMode: (getElement('appSettingsClaudeMode') || {}).value || 'skip-permissions',
      allowedTools: (getElement('appSettingsAllowedTools') || {}).value || ''
    };

    // Save ONLY non-sensitive settings to localStorage (no API keys)
    try {
      var nonSensitiveSettings = {
        provider: settings.provider,
        zaiModel: settings.zaiModel,
        zaiSonnetModel: settings.zaiSonnetModel,
        zaiHaikuModel: settings.zaiHaikuModel,
        activeProfile: settings.activeProfile,
        profile1Name: settings.profile1Name,
        profile2Name: settings.profile2Name,
        defaultWorkingDir: settings.defaultWorkingDir,
        claudeMode: settings.claudeMode,
        allowedTools: settings.allowedTools
      };
      localStorage.setItem('jarvis-account-settings-nonsensitive', JSON.stringify(nonSensitiveSettings));
      // Remove old key that may contain sensitive data
      localStorage.removeItem('jarvis-account-settings');
    } catch (e) {
      ClientLogger.error(MODULE_NAME, 'Failed to save to localStorage', e);
    }

    // Update status message
    function showStatus(message, isSuccess) {
      var status = getElement('accountSettingsSaveStatus');
      if (status) {
        status.textContent = message;
        status.style.color = isSuccess ? '#4caf50' : '#ff5252';
        setTimeout(function() { status.textContent = ''; }, TIMING.STATUS_MESSAGE_TIMEOUT_MS);
      }
    }

    // Ensure we have a CSRF token before saving
    var saveToServer = function(token, isRetry) {
      var headers = { 'Content-Type': 'application/json' };
      if (token) {
        headers['X-CSRF-Token'] = token;
      }

      fetch('/api/settings/accounts', {
        method: 'POST',
        headers: headers,
        body: JSON.stringify(settings)
      })
      .then(function(response) {
        // On 403 (stale CSRF token), refresh token and retry once
        if (response.status === 403 && !isRetry) {
          csrfToken = null;
          fetchCsrfToken().then(function(newToken) { saveToServer(newToken, true); });
          return;
        }
        if (!response.ok) throw new Error('HTTP ' + response.status);
        return response.json();
      })
      .then(function(data) {
        if (!data) return; // Handled by 403 retry
        if (data.success) {
          ClientLogger.info(MODULE_NAME, 'Settings saved successfully');
          showStatus('Saved! New sessions will use these settings.', true);
        } else {
          ClientLogger.warn(MODULE_NAME, 'Save failed: ' + (data.error || 'Unknown error'));
          showStatus(data.error || 'Failed to save settings', false);
        }
      })
      .catch(function(err) {
        ClientLogger.error(MODULE_NAME, 'Failed to save to server', err);
        showStatus('Error saving to server', false);
      });
    };

    // Always fetch a fresh CSRF token before saving
    fetchCsrfToken().then(function(token) { saveToServer(token, false); });
  }

  // Initialize when DOM is ready
  function initAccountsUI() {
    // Clear any stale DOM cache
    clearDomCache();

    // Fetch CSRF token on initialization
    fetchCsrfToken();

    // Profile change handler (immediate update for dropdown changes)
    var activeProfile = getElement('appSettingsActiveProfile');
    if (activeProfile) {
      activeProfile.addEventListener('change', updateProfileNameDisplayImpl);
    }

    // Profile name input handlers (debounced for typing)
    var profile1Name = getElement('appSettingsProfile1Name');
    var profile2Name = getElement('appSettingsProfile2Name');
    if (profile1Name) {
      profile1Name.addEventListener('input', updateProfileNameDisplay);
    }
    if (profile2Name) {
      profile2Name.addEventListener('input', updateProfileNameDisplay);
    }

    // Provider change handler
    var providerSelect = getElement('appSettingsModelProvider');
    if (providerSelect) {
      providerSelect.addEventListener('change', updateProviderSettings);
    }

    // Claude mode change handler
    var claudeModeSelect = getElement('appSettingsClaudeMode');
    if (claudeModeSelect) {
      claudeModeSelect.addEventListener('change', updateClaudeModeSettings);
    }

    // Save button handler
    var saveBtn = getElement('saveAccountSettingsBtn');
    if (saveBtn) {
      saveBtn.addEventListener('click', saveSettings);
    }

    // Load and populate settings
    loadAccountSettings(populateSettings);

    ClientLogger.info(MODULE_NAME, 'UI initialized with CSRF protection and DOM caching');
  }

  // Initialize when DOM is loaded
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', function() {
      setTimeout(initAccountsUI, 500);
    });
  } else {
    setTimeout(initAccountsUI, 500);
  }
})();
