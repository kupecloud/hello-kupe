export type Theme = "light" | "dark";

export function isTheme(value: unknown): value is Theme {
  return value === "light" || value === "dark";
}

export function resolveTheme({
  embedded,
  requested,
  saved,
  systemDark,
}: {
  embedded: boolean;
  requested: unknown;
  saved: unknown;
  systemDark: boolean;
}): Theme {
  if (embedded && isTheme(requested)) return requested;
  if (!embedded && isTheme(saved)) return saved;
  return systemDark ? "dark" : "light";
}

// Run before the page paints so a saved theme does not flash the other palette.
export const themeBootstrap = `(() => {
  const embedded = /^\\/embed(?:\\/|$)/.test(location.pathname);
  const requested = new URLSearchParams(location.search).get("theme");
  let saved;
  try { saved = localStorage.getItem("kupe-theme"); } catch {}
  const valid = value => value === "light" || value === "dark";
  const theme = embedded && valid(requested) ? requested
    : !embedded && valid(saved) ? saved
    : matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
  document.documentElement.dataset.theme = theme;
  document.documentElement.style.colorScheme = theme;
})();`;
