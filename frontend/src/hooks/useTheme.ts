import { useEffect, useState } from "react";
import { applyTheme, getStoredTheme, Theme, watchSystemTheme } from "../lib/theme";

export function useTheme(): [Theme, (t: Theme) => void] {
  const [theme, setThemeState] = useState<Theme>(() => getStoredTheme());

  const setTheme = (t: Theme) => {
    setThemeState(t);
    applyTheme(t);
  };

  // When in "system" mode, react to OS-level preference changes.
  useEffect(() => {
    if (theme !== "system") return;
    return watchSystemTheme(() => applyTheme("system"));
  }, [theme]);

  return [theme, setTheme];
}
