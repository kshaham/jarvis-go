// P13.5 · Thin fetch wrapper mirroring jarvis-mobile's apiFetch shape.
// Bearer cookie is set by the existing /api/auth/login flow; the SPA
// reuses it transparently via credentials: 'include'.

const CSRF_META_SELECTOR = 'meta[name="csrf-token"]';

function getCSRFToken(): string {
  const meta = document.querySelector(CSRF_META_SELECTOR);
  if (meta) return meta.getAttribute('content') || '';
  const m = document.cookie.match(/(?:^|;\s*)csrf_token=([^;]+)/);
  return m ? decodeURIComponent(m[1]) : '';
}

export interface RequestOptions {
  method?: 'GET' | 'POST' | 'PUT' | 'DELETE';
  body?: unknown;
  signal?: AbortSignal;
}

export async function fetchJSON<T>(url: string, opts: RequestOptions = {}): Promise<T> {
  const headers: Record<string, string> = {
    Accept: 'application/json',
  };
  const init: RequestInit = {
    method: opts.method || 'GET',
    credentials: 'include',
    signal: opts.signal,
    headers,
  };
  if (opts.body !== undefined) {
    headers['Content-Type'] = 'application/json';
    init.body = JSON.stringify(opts.body);
  }
  if (init.method !== 'GET' && init.method !== 'HEAD') {
    headers['X-CSRF-Token'] = getCSRFToken();
  }
  const res = await fetch(url, init);
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
