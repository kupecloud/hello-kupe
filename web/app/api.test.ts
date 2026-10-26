import { describe, expect, it } from "vitest";
import { better, formatTime } from "./api";

describe("leaderboard display", () => {
  it("keeps minutes and seconds readable across minute boundaries", () => {
    expect(formatTime(59999)).toBe("0:59");
    expect(formatTime(60000)).toBe("1:00");
    expect(formatTime(125000)).toBe("2:05");
  });
  it("ranks moves first and time only as a tie breaker", () => {
    const best = { name: "Player", moves: 10, duration_ms: 60000 };
    expect(better({ ...best, moves: 9, duration_ms: 120000 }, best)).toBe(true);
    expect(better({ ...best, moves: 11, duration_ms: 10000 }, best)).toBe(
      false,
    );
    expect(better({ ...best, duration_ms: 59000 }, best)).toBe(true);
    expect(better(best, best)).toBe(false);
  });
});
