// Minimal, dependency-free API client.
//
// Tokens:
//   - access_token: memory only (lost on refresh; refresh endpoint recovers it)
//   - refresh_token: sessionStorage (cleared when tab closes; better than localStorage)
//
// CSRF: double-submit. The server sets `panel_csrf` cookie, we echo it
// in the `X-CSRF-Token` header on state-changing requests.

const REFRESH_KEY = "panel_refresh";

let accessToken: string | null = null;
let accessExpiresAt = 0;
let refreshPromise: Promise<void> | null = null;

export function setTokens(access: string, accessExp: string, refresh: string) {
  accessToken = access;
  accessExpiresAt = new Date(accessExp).getTime();
  sessionStorage.setItem(REFRESH_KEY, refresh);
}

export function clearTokens() {
  accessToken = null;
  accessExpiresAt = 0;
  sessionStorage.removeItem(REFRESH_KEY);
}

export function hasRefreshToken() {
  return !!sessionStorage.getItem(REFRESH_KEY);
}

function getCookie(name: string): string | null {
  const parts = document.cookie.split(";").map((s) => s.trim());
  for (const p of parts) {
    const eq = p.indexOf("=");
    if (eq > -1 && p.substring(0, eq) === name) {
      return decodeURIComponent(p.substring(eq + 1));
    }
  }
  return null;
}

async function refreshAccess(): Promise<void> {
  const refresh = sessionStorage.getItem(REFRESH_KEY);
  if (!refresh) throw new ApiError(401, "no refresh token");

  const csrf = getCookie("panel_csrf") ?? "";
  const res = await fetch("/api/auth/refresh", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "X-CSRF-Token": csrf,
    },
    credentials: "same-origin",
    body: JSON.stringify({ refresh_token: refresh }),
  });
  if (!res.ok) {
    clearTokens();
    throw new ApiError(res.status, "refresh failed");
  }
  const data = await res.json();
  setTokens(data.access_token, data.access_expires_at, data.refresh_token);
}

async function ensureAccess(): Promise<string | null> {
  if (accessToken && Date.now() < accessExpiresAt - 5000) return accessToken;
  if (!hasRefreshToken()) return null;
  if (!refreshPromise) {
    refreshPromise = refreshAccess().finally(() => {
      refreshPromise = null;
    });
  }
  try {
    await refreshPromise;
  } catch {
    return null;
  }
  return accessToken;
}

export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message);
  }
}

type Req = {
  method?: "GET" | "POST" | "PUT" | "DELETE";
  body?: unknown;
  auth?: boolean;
};

export async function api<T = unknown>(path: string, opts: Req = {}): Promise<T> {
  const method = opts.method ?? "GET";
  const needsAuth = opts.auth ?? true;

  const headers: Record<string, string> = {};
  if (opts.body !== undefined) headers["Content-Type"] = "application/json";

  if (method !== "GET") {
    const csrf = getCookie("panel_csrf");
    if (csrf) headers["X-CSRF-Token"] = csrf;
  }

  if (needsAuth) {
    const tok = await ensureAccess();
    if (!tok) throw new ApiError(401, "not authenticated");
    headers["Authorization"] = `Bearer ${tok}`;
  }

  const res = await fetch(path, {
    method,
    headers,
    credentials: "same-origin",
    body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
  });

  if (res.status === 401 && needsAuth && hasRefreshToken()) {
    // One retry after forced refresh.
    try {
      await refreshAccess();
      const tok = accessToken;
      if (!tok) throw new ApiError(401, "unauthorized");
      headers["Authorization"] = `Bearer ${tok}`;
      const res2 = await fetch(path, {
        method,
        headers,
        credentials: "same-origin",
        body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
      });
      if (!res2.ok) throw new ApiError(res2.status, await res2.text());
      return (await res2.json()) as T;
    } catch (e) {
      clearTokens();
      throw e;
    }
  }

  if (!res.ok) {
    let msg = res.statusText;
    try {
      const j = await res.json();
      if (j.error) msg = j.error;
    } catch {
      /* ignore */
    }
    throw new ApiError(res.status, msg);
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

let _devMode = false;
export function isDevMode(): boolean {
  return _devMode;
}

export async function prepareCsrf() {
  // Trigger Set-Cookie for panel_csrf and learn dev_mode from the same call.
  try {
    const res = await fetch("/api/health", { credentials: "same-origin" });
    if (res.ok) {
      const j = await res.json();
      _devMode = !!j.dev_mode;
    }
  } catch {
    /* ignore */
  }
}
