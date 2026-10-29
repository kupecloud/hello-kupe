import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import Game from "./Game";

describe("game presentation", () => {
  it("renders only the three panels in the embed, including before hydration", () => {
    const html = renderToStaticMarkup(<Game embedded />);
    expect(html).toContain('class="game-panel"');
    expect(html).toContain('class="leaderboard"');
    expect(html).toContain('class="discovery "');
    expect(html.match(/class="memory-card /g)).toHaveLength(12);
    expect(html).not.toContain('class="site-header"');
    expect(html).not.toContain('class="domain-banner"');
    expect(html).not.toContain('class="intro"');
    expect(html).not.toContain("Open console");
    expect(html).not.toContain("<footer");
    expect(html).not.toContain("<dialog");
  });

  it("keeps the full page branding and navigation in the normal view", () => {
    const html = renderToStaticMarkup(<Game />);
    expect(html).toContain('class="site-header"');
    expect(html).toContain('class="domain-banner"');
    expect(html).toContain('class="intro"');
    expect(html).toContain("Open console");
    expect(html).toContain("<footer");
    expect(html).toContain("<dialog");
  });
});
