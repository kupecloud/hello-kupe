"use client";

import { useEffect, useState } from "react";
import { isTheme, resolveTheme, type Theme } from "./theme";

function savedTheme() {
  try {
    return localStorage.getItem("kupe-theme");
  } catch {
    return null;
  }
}

function applyTheme(theme: Theme) {
  document.documentElement.dataset.theme = theme;
  document.documentElement.style.colorScheme = theme;
}

export function useTheme(embedded: boolean) {
  const [theme, setTheme] = useState<Theme>("light");

  useEffect(() => {
    const system = matchMedia("(prefers-color-scheme: dark)");
    let requested = new URLSearchParams(location.search).get("theme");
    const update = (next: Theme) => {
      applyTheme(next);
      setTheme(next);
    };
    const followPreference = () =>
      update(
        resolveTheme({
          embedded,
          requested,
          saved: savedTheme(),
          systemDark: system.matches,
        }),
      );
    const onMessage = (event: MessageEvent) => {
      if (
        !embedded ||
        window.parent === window ||
        event.source !== window.parent
      )
        return;
      if (event.data?.type !== "kupe:theme" || !isTheme(event.data.theme))
        return;
      requested = event.data.theme;
      update(event.data.theme);
    };
    const onStorage = (event: StorageEvent) => {
      if (!embedded && (event.key === "kupe-theme" || event.key === null))
        followPreference();
    };
    followPreference();
    system.addEventListener("change", followPreference);
    window.addEventListener("message", onMessage);
    window.addEventListener("storage", onStorage);
    return () => {
      system.removeEventListener("change", followPreference);
      window.removeEventListener("message", onMessage);
      window.removeEventListener("storage", onStorage);
    };
  }, [embedded]);

  const toggleTheme = () => {
    if (embedded) return;
    const next = theme === "dark" ? "light" : "dark";
    applyTheme(next);
    setTheme(next);
    try {
      localStorage.setItem("kupe-theme", next);
    } catch {
      // Theme controls still work when browser storage is unavailable.
    }
  };

  return { theme, toggleTheme };
}
