// Theme management — keeps things dependency-free and FOUC-safe.
// The inline script in index.html applies the initial theme before React mounts;
// this module is for runtime changes.

export type Theme = "system" | "light" | "dark";
const KEY = "theme";

export function getStoredTheme(): Theme {
  try {
    const v = localStorage.getItem(KEY);
    if (v === "light" || v === "dark" || v === "system") return v;
  } catch {
    /* storage blocked */
  }
  return "system";
}

export function resolveTheme(t: Theme): "light" | "dark" {
  if (t === "system") {
    return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
  }
  return t;
}

export function applyTheme(t: Theme) {
  try {
    localStorage.setItem(KEY, t);
  } catch {
    /* storage blocked */
  }
  const dark = resolveTheme(t) === "dark";
  document.documentElement.classList.toggle("dark", dark);
}

export function watchSystemTheme(cb: () => void): () => void {
  const mq = window.matchMedia("(prefers-color-scheme: dark)");
  const handler = () => cb();
  mq.addEventListener("change", handler);
  return () => mq.removeEventListener("change", handler);
}
