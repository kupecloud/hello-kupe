export type Feature = {
  id: string;
  title: string;
  description: string;
  detail: string;
  link: string;
};

export function presentFeature(feature: Feature, embedded: boolean): Feature {
  if (!embedded) return feature;
  const details: Record<string, string> = {
    observability:
      "Starting a game, flipping a card and saving a score all make real application requests. Use monitoring dashboards to explore logs, request counts and response times.",
    gitops:
      "Keep the game's image version and settings in Git. GitOps delivery keeps releases and cluster settings in sync, making updates repeatable. Saved scores can live beyond an app release when a database is connected.",
  };
  return details[feature.id]
    ? { ...feature, detail: details[feature.id] }
    : feature;
}
export type Card = { index: number; feature?: string; matched: boolean };
export type Game = {
  id: string;
  cards: Card[];
  moves: number;
  matches: number;
  elapsed_ms: number;
  complete: boolean;
  saved: boolean;
  reveal_ms: number;
};
export type Score = { name: string; moves: number; duration_ms: number };
export type Board = { scores: Score[]; persistent: boolean };

export async function request<T>(path: string, body?: unknown): Promise<T> {
  const response = await fetch(path, {
    method: body === undefined ? "GET" : "POST",
    headers:
      body === undefined ? undefined : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
    cache: "no-store",
  });
  const result = await response.json().catch(() => null);
  if (!response.ok)
    throw new Error(result?.error || "Unable to connect. Please try again.");
  return result as T;
}
export function formatTime(ms: number) {
  const seconds = Math.floor(ms / 1000);
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, "0")}`;
}
export function better(a: Score, b: Score) {
  return (
    a.moves < b.moves || (a.moves === b.moves && a.duration_ms < b.duration_ms)
  );
}
