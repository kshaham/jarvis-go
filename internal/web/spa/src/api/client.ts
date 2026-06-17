// P13.5 · Thin fetch wrapper mirroring jarvis-mobile's apiFetch shape.
// Bearer cookie is set by the existing /api/auth/login flow; the SPA
// reuses it transparently via credentials: 'include'.

const CSRF_META_SELECTOR = 'meta[name="csrf-token"]';

// The CSRF token is server-issued (validated against a server-side store) and
// delivered ONLY in the JSON body of GET /api/csrf-token — the SPA's static
// index.html carries no meta tag and the server sets no csrf_token cookie. So
// we fetch it on demand and cache it in memory; the meta/cookie fallbacks cover
// the classic server-rendered shell. Without this, every mutating request would
// send an empty X-CSRF-Token and the server would reject it with 403.
let cachedCSRFToken: string | null = null;

function readDocumentCSRFToken(): string {
  const meta = document.querySelector(CSRF_META_SELECTOR);
  if (meta) return meta.getAttribute('content') || '';
  const m = document.cookie.match(/(?:^|;\s*)csrf_token=([^;]+)/);
  return m ? decodeURIComponent(m[1]) : '';
}

// Fetch a fresh token from the server and cache it. Returns '' on failure (the
// request then proceeds tokenless and surfaces the server's error).
async function refreshCSRFToken(signal?: AbortSignal): Promise<string> {
  try {
    const res = await fetch('/api/csrf-token', { credentials: 'include', signal });
    if (!res.ok) return '';
    const data = (await res.json()) as { token?: unknown };
    cachedCSRFToken = typeof data?.token === 'string' ? data.token : '';
    return cachedCSRFToken;
  } catch {
    return '';
  }
}

function currentCSRFToken(): string {
  return cachedCSRFToken || readDocumentCSRFToken();
}

export interface RequestOptions {
  method?: 'GET' | 'POST' | 'PUT' | 'DELETE';
  body?: unknown;
  signal?: AbortSignal;
}

export async function fetchJSON<T>(url: string, opts: RequestOptions = {}): Promise<T> {
  const method = opts.method || 'GET';
  const isMutation = method !== 'GET';

  // Ensure we hold a token before the first mutation (nothing else seeds it).
  if (isMutation && !currentCSRFToken()) {
    await refreshCSRFToken(opts.signal);
  }

  const send = (): Promise<Response> => {
    const headers: Record<string, string> = { Accept: 'application/json' };
    const init: RequestInit = { method, credentials: 'include', signal: opts.signal, headers };
    if (opts.body !== undefined) {
      headers['Content-Type'] = 'application/json';
      init.body = JSON.stringify(opts.body);
    }
    if (isMutation) {
      headers['X-CSRF-Token'] = currentCSRFToken();
    }
    return fetch(url, init);
  };

  let res = await send();
  // A 403 on a mutation usually means the token is missing/expired — refresh
  // once and retry (mirrors the classic UI's CSRF auto-retry).
  if (isMutation && res.status === 403) {
    await refreshCSRFToken(opts.signal);
    res = await send();
  }

  if (!res.ok) {
    const text = await res.text().catch(() => '');
    throw new Error(`HTTP ${res.status}: ${text || res.statusText}`);
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

export async function fetchText(url: string, opts: RequestOptions = {}): Promise<string> {
  const res = await fetch(url, {
    method: opts.method || 'GET',
    credentials: 'include',
    signal: opts.signal,
  });
  if (!res.ok) {
    throw new Error(`HTTP ${res.status}: ${res.statusText}`);
  }
  return await res.text();
}
