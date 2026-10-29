import { runInNewContext } from "node:vm";
import { describe, expect, it } from "vitest";
import { resolveTheme, themeBootstrap } from "./theme";

describe("theme preferences", () => {
  it("gives the embed's requested theme priority over the standalone choice", () => {
    expect(
      resolveTheme({
        embedded: true,
        requested: "light",
        saved: "dark",
        systemDark: true,
      }),
    ).toBe("light");
    expect(
      resolveTheme({
        embedded: true,
        requested: "dark",
        saved: "light",
        systemDark: false,
      }),
    ).toBe("dark");
  });

  it("keeps the standalone choice independent of the embed URL", () => {
    expect(
      resolveTheme({
        embedded: false,
        requested: "dark",
        saved: "light",
        systemDark: true,
      }),
    ).toBe("light");
  });

  it("falls back to the system preference for absent or invalid choices", () => {
    expect(
      resolveTheme({
        embedded: false,
        requested: null,
        saved: "invalid",
        systemDark: true,
      }),
    ).toBe("dark");
    expect(
      resolveTheme({
        embedded: true,
        requested: "invalid",
        saved: "dark",
        systemDark: false,
      }),
    ).toBe("light");
  });

  it.each([
    {
      path: "/",
      search: "",
      saved: "dark",
      systemDark: false,
      blocked: false,
      expected: "dark",
    },
    {
      path: "/embed/",
      search: "?theme=light",
      saved: "dark",
      systemDark: true,
      blocked: false,
      expected: "light",
    },
    {
      path: "/embed/",
      search: "?theme=dark",
      saved: null,
      systemDark: false,
      blocked: true,
      expected: "dark",
    },
    {
      path: "/",
      search: "",
      saved: null,
      systemDark: true,
      blocked: true,
      expected: "dark",
    },
  ])(
    "sets the initial palette before hydration: $path $expected",
    ({ path, search, saved, systemDark, blocked, expected }) => {
      const root = {
        dataset: {} as Record<string, string>,
        style: {} as Record<string, string>,
      };
      runInNewContext(themeBootstrap, {
        location: { pathname: path, search },
        URLSearchParams,
        document: { documentElement: root },
        localStorage: {
          getItem: () => {
            if (blocked) throw new Error("Storage blocked");
            return saved;
          },
        },
        matchMedia: () => ({ matches: systemDark }),
      });
      expect(root.dataset.theme).toBe(expected);
      expect(root.style.colorScheme).toBe(expected);
    },
  );
});
