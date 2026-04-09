// Telegram Feature Module for Jarvis
(function() {
  'use strict';

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

  // Default settings
  function getDefaultSettings() {
    return {
      enabled: false,
      botToken: '',
      chatId: '',
      mode: 'polling',
      webhookUrl: '',
      webhookSecret: '',
      notifications: {
        sessionStart: true,
        sessionComplete: true,
        sessionError: true,
        agentSpawn: true,
        agentComplete: true,
        progressUpdates: false,
        progressInterval: 60
      },
      allowedChatIds: [],
      requireChatIdVerification: true,
      parseMode: 'HTML',
      rateLimitPerMinute: 20
    };
  }

  // Update mode settings visibility
  function updateModeSettings() {
    var mode = document.getElementById('telegramMode');
    var webhookSettings = document.getElementById('telegramWebhookSettings');

    if (mode && webhookSettings) {
      if (mode.value === 'webhook') {
        webhookSettings.classList.remove('hidden');
      } else {
        webhookSettings.classList.add('hidden');
      }
    }
  }

  // Update progress interval visibility
  function updateProgressIntervalSettings() {
    var progressCheckbox = document.getElementById('notifyProgressUpdates');
    var progressSettings = document.getElementById('progressIntervalSettings');

    if (progressCheckbox && progressSettings) {
      if (progressCheckbox.checked) {
        progressSettings.classList.remove('hidden');
      } else {
        progressSettings.classList.add('hidden');
      }
    }
  }

  // Update enabled label
  function updateEnabledLabel() {
    var enabledCheckbox = document.getElementById('telegramEnabled');
    var enabledLabel = document.getElementById('telegramEnabledLabel');

    if (enabledCheckbox && enabledLabel) {
      enabledLabel.textContent = enabledCheckbox.checked ? 'Enabled' : 'Disabled';
    }
  }

  // Update connection status display
  function updateConnectionStatus(status) {
    var statusEl = document.getElementById('telegramConnectionStatus');
    if (!statusEl) return;

    var indicator = statusEl.querySelector('.status-indicator');
    var text = statusEl.querySelector('.status-text');

    statusEl.classList.remove('telegram-status-connected', 'telegram-status-disconnected', 'telegram-status-error');

    if (status.connected) {
      statusEl.classList.add('telegram-status-connected');
      text.textContent = 'Connected (' + (status.mode || 'polling') + ' mode)';
    } else if (status.lastError) {
      statusEl.classList.add('telegram-status-error');
      text.textContent = 'Error: ' + status.lastError;
    } else {
      statusEl.classList.add('telegram-status-disconnected');
      text.textContent = 'Not Connected';
    }
  }

  // Toggle bot token visibility
  function toggleBotTokenVisibility() {
    var tokenInput = document.getElementById('telegramBotToken');
    var toggleBtn = document.getElementById('toggleBotTokenVisibility');

    if (tokenInput) {
      if (tokenInput.type === 'password') {
        tokenInput.type = 'text';
        if (toggleBtn) toggleBtn.title = 'Hide';
      } else {
        tokenInput.type = 'password';
        if (toggleBtn) toggleBtn.title = 'Show';
      }
    }
  }

  // Show chat ID help
  function showChatIdHelp() {
    var botToken = document.getElementById('telegramBotToken')?.value || '';
    var message = 'How to get your Chat ID:\n\n';
    message += '1. Start a chat with your bot on Telegram\n';
    message += '2. Send any message to the bot\n';
    message += '3. Visit this URL in your browser:\n';

    if (botToken && !botToken.startsWith('****')) {
      var masked = botToken.substring(0, 5) + '***' + botToken.slice(-4);
      message += '\nhttps://api.telegram.org/bot' + masked + '/getUpdates\n';
      message += '\n(Full URL has been copied to your clipboard)\n';
    } else {
      message += '\nhttps://api.telegram.org/bot{YOUR_BOT_TOKEN}/getUpdates\n';
    }

    message += '\n4. Look for "chat":{"id":XXXXXXX} in the response\n';
    message += '5. Copy the ID number (including - for groups)';

    // Copy full URL to clipboard instead of showing token in alert
    if (botToken && !botToken.startsWith('****')) {
      try {
        navigator.clipboard.writeText('https://api.telegram.org/bot' + botToken + '/getUpdates');
      } catch (e) { /* clipboard not available */ }
    }
    alert(message);
  }

  // Load settings from server
  function loadTelegramSettings(callback) {
    fetch('/api/settings/telegram')
      .then(function(response) { if (!response.ok) throw new Error('HTTP ' + response.status); return response.json(); })
      .then(function(data) {
        callback(data);
      })
      .catch(function(err) {
        console.error('Failed to load telegram settings:', err);
        callback(getDefaultSettings());
      });
  }

  // Populate form with settings
  function populateSettings(settings) {
    // Enable toggle
    var enabledCheckbox = document.getElementById('telegramEnabled');
    if (enabledCheckbox) enabledCheckbox.checked = settings.enabled || false;

    // Bot token (don't overwrite if masked)
    var botToken = document.getElementById('telegramBotToken');
    if (botToken && settings.botToken) botToken.value = settings.botToken;

    // Chat ID
    var chatId = document.getElementById('telegramChatId');
    if (chatId) chatId.value = settings.chatId || '';

    // Mode
    var mode = document.getElementById('telegramMode');
    if (mode) mode.value = settings.mode || 'polling';

    // Webhook settings
    var webhookUrl = document.getElementById('telegramWebhookUrl');
    if (webhookUrl) webhookUrl.value = settings.webhookUrl || '';

    var webhookSecret = document.getElementById('telegramWebhookSecret');
    if (webhookSecret && settings.webhookSecret) webhookSecret.value = settings.webhookSecret;

    // Notifications
    var notifications = settings.notifications || {};
    var sessionStart = document.getElementById('notifySessionStart');
    if (sessionStart) sessionStart.checked = notifications.sessionStart !== false;

    var sessionComplete = document.getElementById('notifySessionComplete');
    if (sessionComplete) sessionComplete.checked = notifications.sessionComplete !== false;

    var sessionError = document.getElementById('notifySessionError');
    if (sessionError) sessionError.checked = notifications.sessionError !== false;

    var agentSpawn = document.getElementById('notifyAgentSpawn');
    if (agentSpawn) agentSpawn.checked = notifications.agentSpawn !== false;

    var agentComplete = document.getElementById('notifyAgentComplete');
    if (agentComplete) agentComplete.checked = notifications.agentComplete !== false;

    var progressUpdates = document.getElementById('notifyProgressUpdates');
    if (progressUpdates) progressUpdates.checked = notifications.progressUpdates || false;

    var progressInterval = document.getElementById('telegramProgressInterval');
    if (progressInterval) progressInterval.value = notifications.progressInterval || 60;

    // Advanced settings
    var parseMode = document.getElementById('telegramParseMode');
    if (parseMode) parseMode.value = settings.parseMode || 'HTML';

    var rateLimit = document.getElementById('telegramRateLimit');
    if (rateLimit) rateLimit.value = settings.rateLimitPerMinute || 20;

    var requireVerification = document.getElementById('telegramRequireChatVerification');
    if (requireVerification) requireVerification.checked = settings.requireChatIdVerification !== false;

    var allowedChatIds = document.getElementById('telegramAllowedChatIds');
    if (allowedChatIds) {
      var ids = settings.allowedChatIds || [];
      allowedChatIds.value = Array.isArray(ids) ? ids.join(', ') : '';
    }

    // Update UI state
    updateEnabledLabel();
    updateModeSettings();
    updateProgressIntervalSettings();

    // Update connection status
    if (settings.status) {
      updateConnectionStatus(settings.status);
    }
  }

  // Collect settings from form
  function collectSettings() {
    var allowedChatIdsInput = document.getElementById('telegramAllowedChatIds')?.value || '';
    var allowedChatIds = allowedChatIdsInput
      .split(',')
      .map(function(id) { return id.trim(); })
      .filter(function(id) { return id.length > 0; });

    return {
      enabled: document.getElementById('telegramEnabled')?.checked || false,
      botToken: document.getElementById('telegramBotToken')?.value || '',
      chatId: document.getElementById('telegramChatId')?.value || '',
      mode: document.getElementById('telegramMode')?.value || 'polling',
      webhookUrl: document.getElementById('telegramWebhookUrl')?.value || '',
      webhookSecret: document.getElementById('telegramWebhookSecret')?.value || '',
      notifications: {
        sessionStart: document.getElementById('notifySessionStart')?.checked || false,
        sessionComplete: document.getElementById('notifySessionComplete')?.checked || false,
        sessionError: document.getElementById('notifySessionError')?.checked || false,
        agentSpawn: document.getElementById('notifyAgentSpawn')?.checked || false,
        agentComplete: document.getElementById('notifyAgentComplete')?.checked || false,
        progressUpdates: document.getElementById('notifyProgressUpdates')?.checked || false,
        progressInterval: parseInt(document.getElementById('telegramProgressInterval')?.value || '60', 10)
      },
      allowedChatIds: allowedChatIds,
      requireChatIdVerification: document.getElementById('telegramRequireChatVerification')?.checked || false,
      parseMode: document.getElementById('telegramParseMode')?.value || 'HTML',
      rateLimitPerMinute: parseInt(document.getElementById('telegramRateLimit')?.value || '20', 10)
    };
  }

  // Save settings
  async function saveSettings() {
    var settings = collectSettings();
    var status = document.getElementById('telegramSaveStatus');

    try {
      var csrfToken = await getCsrfToken();
      var headers = { 'Content-Type': 'application/json' };
      if (csrfToken) {
        headers['X-CSRF-Token'] = csrfToken;
      }

      var response = await fetch('/api/settings/telegram', {
        method: 'POST',
        headers: headers,
        body: JSON.stringify(settings)
      });
      if (!response.ok) throw new Error('HTTP ' + response.status);
      var data = await response.json();

      if (data.success) {
        if (status) {
          status.textContent = 'Settings saved successfully!';
          status.style.color = '#4caf50';
          setTimeout(function() { status.textContent = ''; }, 3000);
        }
        // Refresh settings to get updated status
        loadTelegramSettings(function(newSettings) {
          if (newSettings.status) {
            updateConnectionStatus(newSettings.status);
          }
        });
      } else {
        throw new Error(data.error || 'Unknown error');
      }
    } catch (err) {
      console.error('Failed to save telegram settings:', err);
      if (status) {
        status.textContent = 'Error: ' + err.message;
        status.style.color = '#f44336';
        setTimeout(function() { status.textContent = ''; }, 5000);
      }
    }
  }

  // Test connection (saves settings first to ensure server has the token)
  async function testConnection() {
    var status = document.getElementById('telegramSaveStatus');
    var botToken = document.getElementById('telegramBotToken')?.value || '';

    if (!botToken || botToken.startsWith('****')) {
      if (status) {
        status.textContent = 'Please enter a valid bot token first';
        status.style.color = '#ff9800';
        setTimeout(function() { status.textContent = ''; }, 3000);
      }
      return;
    }

    if (status) {
      status.textContent = 'Saving settings and testing connection...';
      status.style.color = '#ff9800';
    }

    try {
      var csrfToken = await getCsrfToken();
      var headers = { 'Content-Type': 'application/json' };
      if (csrfToken) {
        headers['X-CSRF-Token'] = csrfToken;
      }

      // Save settings first, then test
      var settings = collectSettings();
      var saveResponse = await fetch('/api/settings/telegram', {
        method: 'POST',
        headers: headers,
        body: JSON.stringify(settings)
      });
      if (!saveResponse.ok) throw new Error('HTTP ' + saveResponse.status);
      var saveData = await saveResponse.json();

      if (!saveData.success) {
        throw new Error('Failed to save settings');
      }

      // Now test the connection
      var testResponse = await fetch('/api/telegram/test', {
        method: 'POST',
        headers: headers
      });
      var data = await testResponse.json();

      if (data.success) {
        if (status) {
          status.textContent = 'Connection successful! Bot: @' + (data.botInfo?.username || 'unknown');
          status.style.color = '#4caf50';
          setTimeout(function() { status.textContent = ''; }, 5000);
        }
        updateConnectionStatus({ connected: true, mode: 'test' });
      } else {
        throw new Error(data.error || 'Connection failed');
      }
    } catch (err) {
      console.error('Connection test failed:', err);
      if (status) {
        status.textContent = 'Connection failed: ' + err.message;
        status.style.color = '#f44336';
        setTimeout(function() { status.textContent = ''; }, 5000);
      }
      updateConnectionStatus({ connected: false, lastError: err.message });
    }
  }

  // Send test message (saves settings first to ensure server has the token and chat ID)
  async function sendTestMessage() {
    var status = document.getElementById('telegramSaveStatus');
    var botToken = document.getElementById('telegramBotToken')?.value || '';
    var chatId = document.getElementById('telegramChatId')?.value;

    if (!botToken || botToken.startsWith('****')) {
      if (status) {
        status.textContent = 'Please enter a valid bot token first';
        status.style.color = '#ff9800';
        setTimeout(function() { status.textContent = ''; }, 3000);
      }
      return;
    }

    if (!chatId) {
      if (status) {
        status.textContent = 'Please enter a Chat ID first';
        status.style.color = '#ff9800';
        setTimeout(function() { status.textContent = ''; }, 3000);
      }
      return;
    }

    if (status) {
      status.textContent = 'Saving settings and sending test message...';
      status.style.color = '#ff9800';
    }

    try {
      var csrfToken = await getCsrfToken();
      var headers = { 'Content-Type': 'application/json' };
      if (csrfToken) {
        headers['X-CSRF-Token'] = csrfToken;
      }

      // Save settings first, then send test message
      var settings = collectSettings();
      // Ensure enabled is true for the test
      settings.enabled = true;

      var saveResponse = await fetch('/api/settings/telegram', {
        method: 'POST',
        headers: headers,
        body: JSON.stringify(settings)
      });
      if (!saveResponse.ok) throw new Error('HTTP ' + saveResponse.status);
      var saveData = await saveResponse.json();

      if (!saveData.success) {
        throw new Error('Failed to save settings');
      }

      // Now send the test message
      var sendResponse = await fetch('/api/telegram/send-test', {
        method: 'POST',
        headers: headers,
        body: JSON.stringify({ message: 'Test message from Jarvis at ' + new Date().toLocaleTimeString() })
      });
      var data = await sendResponse.json();

      if (data.success) {
        if (status) {
          status.textContent = 'Test message sent! Check your Telegram.';
          status.style.color = '#4caf50';
          setTimeout(function() { status.textContent = ''; }, 5000);
        }
        // Update enabled checkbox to reflect the state
        var enabledCheckbox = document.getElementById('telegramEnabled');
        if (enabledCheckbox) enabledCheckbox.checked = true;
        updateEnabledLabel();
      } else {
        throw new Error(data.error || 'Failed to send message');
      }
    } catch (err) {
      console.error('Failed to send test message:', err);
      if (status) {
        status.textContent = 'Failed to send: ' + err.message;
        status.style.color = '#f44336';
        setTimeout(function() { status.textContent = ''; }, 5000);
      }
    }
  }

  // Initialize when DOM is ready
  function initTelegramUI() {
    // Enable toggle handler
    var enabledCheckbox = document.getElementById('telegramEnabled');
    if (enabledCheckbox) {
      enabledCheckbox.addEventListener('change', updateEnabledLabel);
    }

    // Mode change handler
    var modeSelect = document.getElementById('telegramMode');
    if (modeSelect) {
      modeSelect.addEventListener('change', updateModeSettings);
    }

    // Progress updates checkbox handler
    var progressCheckbox = document.getElementById('notifyProgressUpdates');
    if (progressCheckbox) {
      progressCheckbox.addEventListener('change', updateProgressIntervalSettings);
    }

    // Toggle token visibility handler
    var toggleBtn = document.getElementById('toggleBotTokenVisibility');
    if (toggleBtn) {
      toggleBtn.addEventListener('click', toggleBotTokenVisibility);
    }

    // Chat ID help handler
    var helpBtn = document.getElementById('getChatIdHelp');
    if (helpBtn) {
      helpBtn.addEventListener('click', showChatIdHelp);
    }

    // Save button handler
    var saveBtn = document.getElementById('saveTelegramSettings');
    if (saveBtn) {
      saveBtn.addEventListener('click', saveSettings);
    }

    // Test connection handler
    var testBtn = document.getElementById('testTelegramConnection');
    if (testBtn) {
      testBtn.addEventListener('click', testConnection);
    }

    // Send test message handler
    var sendTestBtn = document.getElementById('sendTestMessage');
    if (sendTestBtn) {
      sendTestBtn.addEventListener('click', sendTestMessage);
    }

    // Load and populate settings
    loadTelegramSettings(populateSettings);

    console.log('[Telegram] UI initialized');
  }

  // Initialize when DOM is loaded
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', function() {
      setTimeout(initTelegramUI, 500);
    });
  } else {
    setTimeout(initTelegramUI, 500);
  }
})();
