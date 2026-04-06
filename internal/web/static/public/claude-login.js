// Claude Login UI Module
// Handles authentication flow through the web UI
// Version: 2026-01-30-v3

(function() {
  'use strict';
  console.log('[ClaudeLogin] Module loaded (v4 - with CSRF support)');

  let authCheckInterval = null;
  let loginStatusInterval = null;
  const AUTH_SESSION_ID = '__claude_auth__';
  let csrfToken = null;

  // Fetch CSRF token for POST requests
  async function getCsrfToken() {
    if (csrfToken) return csrfToken;
    try {
      const res = await fetch('/api/csrf-token');
      if (res.ok) {
        const data = await res.json();
        csrfToken = data.token;
        return csrfToken;
      }
    } catch (err) {
      console.error('[ClaudeLogin] Failed to fetch CSRF token:', err);
    }
    return null;
  }

  // Check auth status on page load
  async function checkAuthStatus() {
    try {
      const res = await fetch('/api/claude/auth-status');
      const data = await res.json();

      if (!data.authenticated) {
        showLoginBanner(data.reason);
      } else {
        hideLoginBanner();
        // Clean up any auth session if we're now authenticated
        cleanupAuthSession();
        // Stop polling once authenticated - no need to keep checking
        if (authCheckInterval) {
          clearInterval(authCheckInterval);
          authCheckInterval = null;
        }
      }

      return data;
    } catch (err) {
      console.error('[ClaudeLogin] Error checking auth:', err);
      return { authenticated: false, reason: 'error' };
    }
  }

  function showLoginBanner(reason) {
    let banner = document.getElementById('claudeLoginBanner');
    if (!banner) {
      banner = document.createElement('div');
      banner.id = 'claudeLoginBanner';
      banner.className = 'claude-login-banner';
      banner.innerHTML = `
        <div class="login-banner-content">
          <span class="login-banner-icon">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <circle cx="12" cy="12" r="10"/>
              <line x1="12" y1="8" x2="12" y2="12"/>
              <line x1="12" y1="16" x2="12.01" y2="16"/>
            </svg>
          </span>
          <span class="login-banner-text">Claude Code authentication required</span>
          <button class="login-banner-btn" onclick="window.claudeLogin.startLogin()">Login to Claude</button>
        </div>
      `;

      // Insert after header
      const header = document.querySelector('header');
      if (header && header.nextSibling) {
        header.parentNode.insertBefore(banner, header.nextSibling);
      } else {
        document.body.insertBefore(banner, document.body.firstChild);
      }
    }

    // Update message based on reason
    const text = banner.querySelector('.login-banner-text');
    if (text) {
      switch (reason) {
        case 'token_expired':
          text.textContent = 'Claude Code session expired - please re-authenticate';
          break;
        case 'no_credentials':
        case 'no_token':
          text.textContent = 'Claude Code authentication required';
          break;
        default:
          text.textContent = 'Claude Code authentication required';
      }
    }

    banner.style.display = 'flex';
  }

  function hideLoginBanner() {
    const banner = document.getElementById('claudeLoginBanner');
    if (banner) {
      banner.style.display = 'none';
    }
  }

  async function cleanupAuthSession() {
    try {
      // Delete the auth session if it exists
      const sessionId = window.__claudeAuthSessionId || AUTH_SESSION_ID;
      const token = await getCsrfToken();
      const headers = {};
      if (token) {
        headers['X-CSRF-Token'] = token;
      }
      await fetch(`/api/sessions/${sessionId}`, { method: 'DELETE', headers });
      window.__claudeAuthSessionId = null;
    } catch {
      // Ignore errors - session may not exist
    }
  }

  async function startLogin() {
    try {
      // First, check if auth session already exists (search by name since server generates UUIDs)
      const sessionsRes = await fetch('/api/sessions');
      const sessions = await sessionsRes.json();

      let authSession = sessions.find(s => s.name === 'Claude Authentication');

      // Track the actual session ID (server generates its own UUID)
      let actualSessionId = null;

      if (!authSession) {
        // Fetch CSRF token first
        const token = await getCsrfToken();

        // Create a new auth session using the existing session API
        const headers = { 'Content-Type': 'application/json' };
        if (token) {
          headers['X-CSRF-Token'] = token;
        }

        const createRes = await fetch('/api/sessions', {
          method: 'POST',
          headers,
          body: JSON.stringify({
            name: 'Claude Authentication',
            workingDir: '/home/jarvis',
            provider: 'claude-subscription'
          })
        });

        if (!createRes.ok) {
          const error = await createRes.text();
          throw new Error('Failed to create auth session: ' + error);
        }

        authSession = await createRes.json();
      }

      // Extract actual session ID from response (server generates UUID)
      actualSessionId = authSession.session?.id || authSession.id;
      console.log('[ClaudeLogin] Auth session ID:', actualSessionId);

      // Store the actual session ID for cleanup later
      window.__claudeAuthSessionId = actualSessionId;

      // Wait for session to be registered via SSE
      await new Promise(resolve => setTimeout(resolve, 500));

      // Select the auth session FIRST (this sets activeSessionId for terminal updates)
      // Use window.app to avoid TDZ errors with lexical declarations in app.js
      console.log('[ClaudeLogin] Checking app global:', typeof window.app, window.app ? 'exists' : 'undefined');
      if (window.app) {
        console.log('[ClaudeLogin] app.selectSession:', typeof window.app.selectSession);
        console.log('[ClaudeLogin] app.activeSessionId before:', window.app.activeSessionId);

        if (window.app.selectSession) {
          console.log('[ClaudeLogin] Selecting session:', actualSessionId);
          await window.app.selectSession(actualSessionId);
          console.log('[ClaudeLogin] selectSession complete, activeSessionId:', window.app.activeSessionId);
        } else {
          // Fallback: directly set activeSessionId
          console.warn('[ClaudeLogin] selectSession not found, setting activeSessionId directly');
          window.app.activeSessionId = actualSessionId;
        }
      } else {
        console.error('[ClaudeLogin] app global not available - keyboard input will not work!');
      }

      // Wait a moment for UI to update
      await new Promise(resolve => setTimeout(resolve, 200));

      // Now start the interactive session
      const session = window.app ? window.app.sessions?.get(actualSessionId) : null;
      if (!session || session.pid === null) {
        console.log('[ClaudeLogin] Starting interactive session:', actualSessionId);
        const interactiveToken = await getCsrfToken();
        const interactiveHeaders = {};
        if (interactiveToken) {
          interactiveHeaders['X-CSRF-Token'] = interactiveToken;
        }
        const interactiveRes = await fetch(`/api/sessions/${actualSessionId}/interactive`, {
          method: 'POST',
          headers: interactiveHeaders
        });
        if (!interactiveRes.ok) {
          console.warn('[ClaudeLogin] Failed to start interactive session:', await interactiveRes.text());
        }
      }

      // Wait for Claude to start up and emit terminal data
      await new Promise(resolve => setTimeout(resolve, 1500));

      // Show info modal
      showLoginInfoModal();

      // Start polling for auth completion
      startLoginStatusCheck();

    } catch (err) {
      console.error('[ClaudeLogin] Error starting login:', err);
      alert('Failed to start login: ' + err.message);
    }
  }

  function showLoginInfoModal() {
    // Don't show a blocking modal - just show a small notification banner instead
    // The user needs to interact with the terminal to login
    let notification = document.getElementById('claudeLoginNotification');
    if (!notification) {
      notification = document.createElement('div');
      notification.id = 'claudeLoginNotification';
      notification.style.cssText = `
        position: fixed;
        bottom: 20px;
        right: 20px;
        background: var(--bg-secondary, #1a1a1a);
        border: 1px solid var(--accent, #3b82f6);
        border-radius: 8px;
        padding: 16px 20px;
        max-width: 320px;
        z-index: 1000;
        box-shadow: 0 4px 12px rgba(0, 0, 0, 0.3);
      `;
      notification.innerHTML = `
        <div style="display: flex; justify-content: space-between; align-items: start; margin-bottom: 8px;">
          <strong style="color: var(--text, #eee); font-size: 14px;">Login to Claude</strong>
          <button onclick="this.parentElement.parentElement.remove()" style="background: none; border: none; color: var(--text-dim, #888); cursor: pointer; font-size: 18px; line-height: 1;">&times;</button>
        </div>
        <p style="color: var(--text-dim, #888); font-size: 13px; margin: 0 0 12px 0; line-height: 1.4;">
          Use the terminal to complete authentication. Select a login method and follow the prompts.
        </p>
        <div class="claude-login-status" id="claudeLoginStatus" style="display: flex; align-items: center; gap: 8px; padding: 8px; background: var(--bg, #0a0a0a); border-radius: 4px; font-size: 12px; color: var(--text-dim, #888);">
          <span class="status-spinner" style="width: 12px; height: 12px; border: 2px solid var(--border, #333); border-top-color: var(--accent, #3b82f6); border-radius: 50%; animation: spin 0.8s linear infinite;"></span>
          <span>Waiting for authentication...</span>
        </div>
      `;
      document.body.appendChild(notification);
    }
  }

  function closeModal() {
    const modal = document.getElementById('claudeLoginModal');
    if (modal) {
      modal.style.display = 'none';
    }
    const notification = document.getElementById('claudeLoginNotification');
    if (notification) {
      notification.remove();
    }
    stopLoginStatusCheck();
  }

  function startLoginStatusCheck() {
    stopLoginStatusCheck();
    loginStatusInterval = setInterval(async () => {
      try {
        const res = await fetch('/api/claude/auth-status');
        const data = await res.json();

        const status = document.getElementById('claudeLoginStatus');

        if (data.authenticated) {
          if (status) {
            status.innerHTML = `
              <span class="status-success">✓</span>
              <span>Successfully authenticated!</span>
            `;
          }
          stopLoginStatusCheck();

          // Close modal and hide banner after a brief delay
          setTimeout(async () => {
            closeModal();
            hideLoginBanner();

            // Clean up auth session
            await cleanupAuthSession();
          }, 1500);
        }
      } catch (err) {
        console.error('[ClaudeLogin] Error checking status:', err);
      }
    }, 2000);
  }

  function stopLoginStatusCheck() {
    if (loginStatusInterval) {
      clearInterval(loginStatusInterval);
      loginStatusInterval = null;
    }
  }

  // Start periodic auth check
  function startAuthCheck() {
    // Set interval first so it can be cleared if the first check succeeds
    authCheckInterval = setInterval(checkAuthStatus, 30000);

    // Then check immediately
    checkAuthStatus();
  }

  // Initialize when DOM is ready
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', startAuthCheck);
  } else {
    startAuthCheck();
  }

  // Export functions for use in HTML
  window.claudeLogin = {
    checkAuthStatus,
    startLogin,
    closeModal
  };
})();
