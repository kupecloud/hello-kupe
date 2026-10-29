"use client";

import {
  memo,
  useEffect,
  useRef,
  useState,
  type CSSProperties,
  type FormEvent,
} from "react";
import {
  ArrowRight,
  ArrowUpRight,
  Check,
  Cloud,
  Database,
  ExternalLink,
  GitBranch,
  Globe2,
  Info,
  Layers3,
  Maximize2,
  RotateCcw,
  Sparkles,
  Trophy,
  X,
  Activity,
  Clock3,
  MousePointer2,
  Moon,
  Sun,
} from "lucide-react";
import { useTheme } from "./useTheme";
import {
  better,
  formatTime,
  presentFeature,
  request,
  type Board,
  type Feature,
  type Game,
  type Score,
} from "./api";

const icons = {
  domains: Globe2,
  database: Database,
  registry: Layers3,
  scaling: Maximize2,
  observability: Activity,
  gitops: GitBranch,
};
const errorText = (error: unknown) =>
  error instanceof Error
    ? error.message
    : "Something went wrong. Please try again.";

function Mark({ small = false }: { small?: boolean }) {
  return (
    <span className={`kupe-mark ${small ? "small" : ""}`} aria-hidden="true">
      <img src="/kupe-mark.svg" alt="" width="34" height="34" />
    </span>
  );
}
function FeatureIcon({ id, size = 24 }: { id: string; size?: number }) {
  const Icon = icons[id as keyof typeof icons] || Cloud;
  return <Icon size={size} strokeWidth={1.65} aria-hidden="true" />;
}

type Burst = { id: number; origins: { index: number; x: number; y: number }[] };

const MatchConfetti = memo(function MatchConfetti({ burst }: { burst: Burst }) {
  return (
    <div className="match-confetti" key={burst.id} aria-hidden="true">
      {burst.origins.flatMap((origin, originIndex) =>
        Array.from({ length: 112 }, (_, piece) => {
          const angle = (piece / 112) * Math.PI * 2;
          const distance = 180 + ((piece * 37) % 230);
          return (
            <i
              key={`${originIndex}-${piece}`}
              className="confetti-piece"
              style={
                {
                  left: origin.x,
                  top: origin.y,
                  "--confetti-x": `${Math.cos(angle) * distance}px`,
                  "--confetti-y": `${Math.sin(angle) * distance - 100}px`,
                  "--confetti-turn": `${(piece % 2 ? 1 : -1) * (360 + piece * 21)}deg`,
                  "--confetti-colour": [
                    "#087f75",
                    "#7350b9",
                    "#5ee0c5",
                    "#bba0e5",
                    "#e5b85c",
                  ][piece % 5],
                  "--confetti-delay": `${(piece % 11) * 14}ms`,
                  width: 5 + (piece % 5),
                  height: 8 + (piece % 8),
                } as CSSProperties
              }
            />
          );
        }),
      )}
    </div>
  );
});

export default function GameView({ embedded = false }: { embedded?: boolean }) {
  const { theme, toggleTheme } = useTheme(embedded);
  const shell = useRef<HTMLDivElement>(null);
  const gamePanel = useRef<HTMLElement>(null);
  const [features, setFeatures] = useState<Feature[]>([]);
  const [game, setGame] = useState<Game | null>(null);
  const [board, setBoard] = useState<Board | null>(null);
  const [busy, setBusy] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [boardError, setBoardError] = useState(false);
  const [elapsed, setElapsed] = useState(0);
  const [lesson, setLesson] = useState<string | null>(null);
  const [name, setName] = useState("");
  const [best, setBest] = useState<Score | null>(null);
  const [modal, setModal] = useState<Feature | null>(null);
  const [hostname, setHostname] = useState("your Kupe app");
  const [burst, setBurst] = useState<Burst | null>(null);
  const burstId = useRef(0);
  const grid = useRef<HTMLDivElement>(null);
  const dialog = useRef<HTMLDialogElement>(null);
  const cardRefs = useRef<(HTMLButtonElement | null)[]>([]);
  const revealedFaces = useRef(new Map<number, string>());
  const nameInput = useRef<HTMLInputElement>(null);
  const completionReplay = useRef<HTMLButtonElement>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const inFlight = useRef(false);
  const clock = useRef({ base: 0, at: 0, running: false });

  useEffect(() => {
    if (!embedded || !shell.current || window.parent === window) return;
    const element = shell.current;
    let previousHeight = 0;
    const reportSize = () => {
      const height = Math.ceil(element.getBoundingClientRect().height);
      if (height === previousHeight) return;
      previousHeight = height;
      // Only layout dimensions are shared, so embeds also work on previews.
      window.parent.postMessage({ type: "kupe:game-size", height }, "*");
    };
    const observer = new ResizeObserver(reportSize);
    observer.observe(element);
    reportSize();
    return () => observer.disconnect();
  }, [embedded]);

  useEffect(() => {
    const panel = gamePanel.current;
    const workspace = panel?.parentElement;
    if (!panel || !workspace) return;
    const measure = () =>
      workspace.style.setProperty(
        "--completion-board-height",
        `${panel.offsetHeight}px`,
      );
    const observer = new ResizeObserver(measure);
    observer.observe(panel);
    measure();
    return () => observer.disconnect();
  }, []);

  async function refreshBoard() {
    try {
      setBoard(await request<Board>("/api/leaderboard"));
      setBoardError(false);
    } catch {
      setBoardError(true);
    }
  }
  function apply(next: Game) {
    // Retain observed faces while their cards animate closed.
    for (const card of next.cards) {
      if (card.feature) revealedFaces.current.set(card.index, card.feature);
    }
    setGame(next);
    setElapsed(next.elapsed_ms);
    clock.current = {
      base: next.elapsed_ms,
      at: Date.now(),
      running:
        !next.complete &&
        (next.elapsed_ms > 0 || next.cards.some((c) => c.feature)),
    };
    if (timer.current) clearTimeout(timer.current);
    if (next.reveal_ms > 0) {
      timer.current = setTimeout(() => {
        setGame((current) =>
          current?.id === next.id
            ? {
                ...current,
                reveal_ms: 0,
                cards: current.cards.map((c) =>
                  c.matched ? c : { ...c, feature: undefined },
                ),
              }
            : current,
        );
      }, next.reveal_ms + 40);
    }
  }
  useEffect(() => {
    let live = true;
    setHostname(window.location.host);
    try {
      const saved = localStorage.getItem("kupe-best");
      if (saved) setBest(JSON.parse(saved) as Score);
    } catch {
      /* Storage is optional. */
    }
    async function load() {
      try {
        const data = await request<Feature[]>("/api/features");
        if (live) setFeatures(data);
        let id: string | null = null;
        try {
          id = sessionStorage.getItem("kupe-game");
        } catch {
          /* Storage is optional. */
        }
        if (id) {
          try {
            const resumed = await request<Game>(`/api/games/${id}`);
            if (live) {
              apply(resumed);
              setLesson(
                resumed.cards.find((card) => card.matched)?.feature ?? null,
              );
            }
          } catch {
            try {
              sessionStorage.removeItem("kupe-game");
            } catch {
              /* Storage is optional. */
            }
          }
        }
      } catch (err) {
        if (live) setError(errorText(err));
      } finally {
        if (live) setLoading(false);
      }
    }
    void load();
    void refreshBoard();
    const tick = setInterval(() => {
      if (clock.current.running)
        setElapsed(clock.current.base + Date.now() - clock.current.at);
    }, 250);
    return () => {
      live = false;
      clearInterval(tick);
      if (timer.current) clearTimeout(timer.current);
    };
  }, []);
  useEffect(() => {
    if (modal) dialog.current?.showModal();
    else dialog.current?.close();
  }, [modal]);

  useEffect(() => {
    if (!game?.complete) return;
    const frame = requestAnimationFrame(() => {
      const target = game.saved ? completionReplay.current : nameInput.current;
      target?.focus({ preventScroll: true });
    });
    return () => cancelAnimationFrame(frame);
  }, [game?.complete, game?.saved]);

  useEffect(() => {
    if (!burst) return;
    const timeout = setTimeout(() => setBurst(null), 2200);
    return () => clearTimeout(timeout);
  }, [burst]);

  function celebrate(indices: number[]) {
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return;
    const bounds = grid.current?.getBoundingClientRect();
    if (!bounds) return;
    const origins = indices.flatMap((index) => {
      const card = cardRefs.current[index]?.getBoundingClientRect();
      return card
        ? [
            {
              index,
              x: card.left - bounds.left + card.width / 2,
              y: card.top - bounds.top + card.height / 2 + 420,
            },
          ]
        : [];
    });
    setBurst({ id: ++burstId.current, origins });
  }

  async function start(firstCard?: number) {
    if (inFlight.current) return;
    inFlight.current = true;
    setBusy(true);
    setError("");
    setBurst(null);
    try {
      if (!features.length)
        setFeatures(await request<Feature[]>("/api/features"));
      const next = await request<Game>("/api/games", {});
      revealedFaces.current.clear();
      apply(next);
      setLesson(null);
      setName("");
      try {
        sessionStorage.setItem("kupe-game", next.id);
      } catch {
        /* Storage is optional. */
      }
      if (firstCard !== undefined) {
        apply(
          await request<Game>(`/api/games/${next.id}/flip`, {
            index: firstCard,
          }),
        );
      }
      requestAnimationFrame(() =>
        cardRefs.current[firstCard ?? 0]?.focus({ preventScroll: true }),
      );
    } catch (err) {
      setError(errorText(err));
    } finally {
      inFlight.current = false;
      setBusy(false);
    }
  }
  async function flip(index: number) {
    if (!game) {
      if (!loading) await start(index);
      return;
    }
    if (
      inFlight.current ||
      game.complete ||
      game.cards[index].matched ||
      (Boolean(game.cards[index].feature) && game.reveal_ms === 0)
    )
      return;
    inFlight.current = true;
    setBusy(true);
    setError("");
    try {
      const next = await request<Game>(`/api/games/${game.id}/flip`, { index });
      const newlyMatched = next.cards.filter(
        (c, i) => c.matched && !game.cards[i].matched,
      );
      if (newlyMatched[0]?.feature) {
        celebrate(newlyMatched.map((card) => card.index));
        setLesson(newlyMatched[0].feature);
        requestAnimationFrame(() => {
          if (!next.complete) {
            const remaining = next.cards.filter((card) => !card.matched);
            const target =
              remaining.find((card) => card.index > index) || remaining[0];
            cardRefs.current[target.index]?.focus({ preventScroll: true });
          }
        });
      }
      apply(next);
    } catch (err) {
      setError(errorText(err));
    } finally {
      inFlight.current = false;
      setBusy(false);
    }
  }
  async function save(event: FormEvent) {
    event.preventDefault();
    if (!game || inFlight.current) return;
    inFlight.current = true;
    setBusy(true);
    setError("");
    try {
      const next = await request<Game>(`/api/games/${game.id}/score`, {
        name: name.trim(),
      });
      apply(next);
      const score: Score = {
        name: name.trim(),
        moves: next.moves,
        duration_ms: next.elapsed_ms,
      };
      if (!best || better(score, best)) {
        setBest(score);
        try {
          localStorage.setItem("kupe-best", JSON.stringify(score));
        } catch {
          /* Storage is optional. */
        }
      }
      await refreshBoard();
    } catch (err) {
      setError(errorText(err));
    } finally {
      inFlight.current = false;
      setBusy(false);
    }
  }

  const matchedFeature = features.find((f) => f.id === lesson);
  const currentFeature = matchedFeature
    ? presentFeature(matchedFeature, embedded)
    : undefined;
  const cards =
    game?.cards ||
    Array.from({ length: 12 }, (_, index) => ({
      index,
      matched: false,
      feature: undefined,
    }));
  const domain = features.find((f) => f.id === "domains");
  const localPreview =
    hostname === "your Kupe app" ||
    /^(localhost|127\.0\.0\.1|\[::1\])(?::\d+)?$/.test(hostname);
  const domainHostname = localPreview
    ? "hello-kupe.demo.your-tenant.clusters.kupe.cloud"
    : hostname;

  return (
    <>
      {!embedded && (
        <div className="domain-banner">
          <div className="banner-content">
            <span className="banner-icon">
              <Globe2 size={16} />
            </span>
            <span>
              A new home for your app? <strong>Make this URL your own.</strong>
            </span>
            <button
              onClick={() => domain && setModal(domain)}
              disabled={!domain}
              aria-label="Learn about custom domains"
            >
              <Info size={17} />
              <span>Here’s how</span>
              <ArrowUpRight size={15} />
            </button>
          </div>
        </div>
      )}
      <div className={`page-shell${embedded ? " embed-view" : ""}`} ref={shell}>
        {!embedded && (
          <header className="site-header">
            <a className="brand" href="/" aria-label="Hello Kupe home">
              <Mark />
              <span>
                kupe<span className="brand-dot">.</span>
              </span>
            </a>
            <nav aria-label="Main navigation">
              <a
                href="https://docs.kupe.cloud/"
                target="_blank"
                rel="noreferrer"
              >
                The docs <ArrowUpRight size={15} />
              </a>
              <a
                className="console-link"
                href="https://console.kupe.cloud/"
                target="_blank"
                rel="noreferrer"
              >
                Open console <ArrowUpRight size={15} />
              </a>
              <button
                type="button"
                className="icon-button theme-toggle"
                onClick={toggleTheme}
                aria-label="Toggle dark mode"
                aria-pressed={theme === "dark"}
              >
                <Moon
                  className="theme-icon-light"
                  size={20}
                  aria-hidden="true"
                />
                <Sun className="theme-icon-dark" size={20} aria-hidden="true" />
              </button>
            </nav>
          </header>
        )}
        <main>
          {!embedded && (
            <section className="intro" aria-labelledby="title">
              <div>
                <div className="eyebrow">
                  <span className="live-dot" /> HELLO KUPE
                </div>
                <h1 id="title">
                  Match a little. <span>Discover a lot.</span>
                </h1>
              </div>
              <div className="intro-note">
                <span className="note-icon">
                  <Cloud size={22} aria-hidden="true" />
                </span>
                <p>
                  A tiny game.
                  <br />
                  <strong>A real cloud application.</strong>
                </p>
              </div>
            </section>
          )}
          <div className={`workspace${game?.complete ? " is-complete" : ""}`}>
            <section
              className="game-panel"
              ref={gamePanel}
              aria-labelledby="game-title"
              inert={Boolean(game?.complete)}
            >
              <div className="game-toolbar">
                <div>
                  <h2 id="game-title">Find six pairs.</h2>
                  <p className="game-description">
                    Discover what you can build with Kupe.
                  </p>
                </div>
                {game && (
                  <button
                    className="icon-button"
                    onClick={() => void start()}
                    disabled={busy || loading}
                    aria-label="Start a new game"
                  >
                    <RotateCcw size={18} />
                  </button>
                )}
              </div>
              {!game && (
                <p className="game-instructions">
                  {loading
                    ? "Getting your cards ready…"
                    : "Pick a card. Find its pair."}
                </p>
              )}
              <div className="stats">
                <div>
                  <MousePointer2 size={16} />
                  <span>Moves</span>
                  <strong>{game?.moves || 0}</strong>
                </div>
                <div>
                  <Clock3 size={16} />
                  <span>Time</span>
                  <strong>{formatTime(elapsed)}</strong>
                </div>
                <div className="matches-stat">
                  <span>Pairs found</span>
                  <strong>
                    {game?.matches || 0}
                    <span> / 6</span>
                  </strong>
                  <div className="progress-track">
                    <i
                      style={{ width: `${((game?.matches || 0) / 6) * 100}%` }}
                    />
                  </div>
                </div>
              </div>
              <div
                className="card-grid"
                ref={grid}
                aria-label="Matching cards"
                aria-busy={busy}
              >
                {burst && <MatchConfetti burst={burst} />}
                {cards.map((card) => {
                  const faceUp = Boolean(card.feature);
                  const feature = features.find(
                    (f) =>
                      f.id ===
                      (card.feature || revealedFaces.current.get(card.index)),
                  );
                  return (
                    <button
                      key={card.index}
                      className={`memory-card ${faceUp ? "flipped" : ""} ${card.matched ? "matched" : ""} ${burst?.origins.some((origin) => origin.index === card.index) ? "celebrating" : ""} tone-${card.index % 3 === 0 ? "purple" : "teal"}`}
                      ref={(node) => {
                        cardRefs.current[card.index] = node;
                      }}
                      disabled={
                        loading || Boolean(game?.complete) || card.matched
                      }
                      aria-disabled={
                        loading ||
                        busy ||
                        Boolean(game?.complete) ||
                        card.matched ||
                        (faceUp && (game?.reveal_ms ?? 0) === 0)
                      }
                      onClick={() => void flip(card.index)}
                      aria-label={`Card ${card.index + 1}${faceUp && feature ? `: ${feature.title}${card.matched ? ", matched" : ""}` : ": face down"}`}
                    >
                      <span className="card-inner">
                        <span className="card-back" aria-hidden="true">
                          <span
                            className="card-watermark"
                            style={
                              {
                                "--mark-size": `${140 + ((card.index * 17) % 85)}%`,
                                "--mark-angle": `${((card.index * 47) % 80) - 40}deg`,
                                "--mark-x": `${-70 + ((card.index * 13) % 60)}%`,
                                "--mark-y": `${-85 + ((card.index * 19) % 80)}%`,
                              } as CSSProperties
                            }
                          />
                          <Mark small />
                          <span className="card-back-word">
                            {card.index % 2 === 0 ? "hello" : "kupe"}
                          </span>
                        </span>
                        <span
                          className={`card-front tone-${feature?.id || "database"}`}
                          aria-hidden="true"
                        >
                          {feature && (
                            <>
                              <FeatureIcon id={feature.id} size={32} />
                              <span>{feature.title}</span>
                              {card.matched && (
                                <span className="match-tick">
                                  <Check size={12} />
                                </span>
                              )}
                            </>
                          )}
                        </span>
                      </span>
                    </button>
                  );
                })}
              </div>
              {game && (
                <div className="playing-footer" aria-hidden={game.complete}>
                  <span className="live-dot" />
                  <p>
                    {game.reveal_ms > 0
                      ? "Not quite a pair. Remember them for next time."
                      : game.matches
                        ? "Nice work. There’s more to discover."
                        : game.moves > 0 || elapsed > 0
                          ? "Keep those possibilities in mind."
                          : "Your timer starts with your first flip."}
                  </p>
                  <span>
                    {game.matches === 0
                      ? "You’ve got this."
                      : `${6 - game.matches} pairs to go`}
                  </span>
                </div>
              )}
              {error && !game?.complete && (
                <div className="error-message" role="alert">
                  <Info size={17} />
                  <span>{error}</span>
                  <button
                    className="text-button"
                    onClick={() => void start()}
                    disabled={busy}
                  >
                    New game
                  </button>
                </div>
              )}
            </section>
            <aside className="sidebar" inert={Boolean(game?.complete)}>
              <section
                className="leaderboard"
                aria-labelledby="leaderboard-title"
              >
                <div className="leaderboard-heading">
                  <span className="trophy-icon">
                    <Trophy size={23} />
                  </span>
                  <div>
                    <span className="section-kicker">
                      A LITTLE FRIENDLY COMPETITION
                    </span>
                    <h2 id="leaderboard-title">The hall of fame</h2>
                  </div>
                </div>
                <div className="leaderboard-body">
                  <div className="table-labels">
                    <span>PLAYER</span>
                    <span>MOVES / TIME</span>
                  </div>
                  {boardError ? (
                    <div className="empty-board">
                      <Cloud size={28} />
                      <p>The scoreboard is taking a moment.</p>
                      <button
                        className="text-button"
                        onClick={() => void refreshBoard()}
                      >
                        Try again <RotateCcw size={14} />
                      </button>
                    </div>
                  ) : !board ? (
                    <p className="board-loading">Fetching the latest scores…</p>
                  ) : board.scores.length === 0 ? (
                    <div className="empty-board">
                      <Trophy size={30} strokeWidth={1.2} />
                      <h3>Your name could be first.</h3>
                      <p>
                        Find all six pairs and leave
                        <br />
                        your mark on the board.
                      </p>
                    </div>
                  ) : (
                    <ol className="scores">
                      {board.scores.map((score, index) => (
                        <li key={`${index}-${score.name}`}>
                          <span className={`rank rank-${index}`}>
                            {String(index + 1).padStart(2, "0")}
                          </span>
                          <span className="player-name">{score.name}</span>
                          <span className="score-result">
                            <strong>{score.moves}</strong>
                            <span>{formatTime(score.duration_ms)}</span>
                          </span>
                        </li>
                      ))}
                    </ol>
                  )}
                  <p className="ranking-note">
                    Fewest moves wins. Time breaks the tie.
                  </p>
                  {best && (
                    <div className="personal-best">
                      <Sparkles size={15} />
                      <span>Your best on this device</span>
                      <strong>
                        {best.moves} / {formatTime(best.duration_ms)}
                      </strong>
                    </div>
                  )}
                </div>
                <div className="database-note">
                  <Database size={16} />
                  <span>
                    {board?.persistent
                      ? "Kept safe in managed PostgreSQL"
                      : "Practice mode · scores reset on restart"}
                  </span>
                  {board?.persistent && <span className="live-dot" />}
                </div>
              </section>
              <section
                className={`discovery ${currentFeature ? "unlocked" : ""}`}
                aria-live="polite"
              >
                <span className="section-kicker">
                  {currentFeature
                    ? "PAIR FOUND · HOW THIS GAME WORKS"
                    : "THE PLATFORM BEHIND THIS GAME"}
                </span>
                <div className="discovery-icon">
                  {currentFeature ? (
                    <FeatureIcon id={currentFeature.id} size={27} />
                  ) : (
                    <Sparkles size={27} strokeWidth={1.5} />
                  )}
                </div>
                <h3>
                  {currentFeature?.title || "Every pair has a part to play."}
                </h3>
                <p>
                  {currentFeature?.detail ||
                    "Match a pair to see how Kupe can run this game, store its scores and deliver its next release."}
                </p>
                {currentFeature && (
                  <button
                    className="text-button"
                    aria-expanded={
                      embedded ? modal?.id === currentFeature.id : undefined
                    }
                    onClick={() =>
                      setModal(
                        embedded && modal?.id === currentFeature.id
                          ? null
                          : currentFeature,
                      )
                    }
                  >
                    {embedded && modal?.id === currentFeature.id
                      ? "Show less"
                      : "Explore this feature"}
                    <ArrowUpRight size={15} />
                  </button>
                )}
                {embedded &&
                  currentFeature &&
                  modal?.id === currentFeature.id && (
                    <p className="discovery-more">
                      {currentFeature.description}
                    </p>
                  )}
                <div
                  className="discovery-dots"
                  aria-label={`${game?.matches || 0} of 6 capabilities discovered`}
                >
                  {Array.from({ length: 6 }, (_, i) => (
                    <i
                      key={i}
                      className={i < (game?.matches || 0) ? "filled" : ""}
                    />
                  ))}
                </div>
              </section>
            </aside>
            {game?.complete && (
              <div className="completion-overlay">
                <section
                  className="completion"
                  role="region"
                  aria-labelledby="completion-title"
                  aria-live="polite"
                >
                  <div className="completion-title">
                    <span className="success-icon" aria-hidden="true">
                      <Trophy size={42} />
                    </span>
                    <div>
                      <h3 id="completion-title">Six pairs. Well played!</h3>
                      <p>
                        {game.moves} moves · {formatTime(game.elapsed_ms)}
                      </p>
                    </div>
                  </div>
                  {game.saved ? (
                    <div className="saved-row">
                      <Check size={24} />
                      <span>Your score is on the board.</span>
                    </div>
                  ) : (
                    <form onSubmit={save}>
                      <label htmlFor="player-name">Your display name</label>
                      <div className="completion-fields">
                        <input
                          id="player-name"
                          ref={nameInput}
                          value={name}
                          onChange={(e) => setName(e.target.value)}
                          maxLength={24}
                          placeholder="e.g. Alex"
                          required
                          disabled={busy}
                          autoComplete="nickname"
                        />
                        <button
                          className="primary-button"
                          type="submit"
                          disabled={busy || !name.trim()}
                        >
                          {busy ? "Saving…" : "Save my score"}
                          <ArrowRight size={22} />
                        </button>
                      </div>
                      <small>
                        Your name and result will be public.{" "}
                        {board?.persistent
                          ? "Saved to Kupe managed PostgreSQL."
                          : "Practice scores are temporary."}
                      </small>
                    </form>
                  )}
                  {error && (
                    <div className="error-message" role="alert">
                      <Info size={22} />
                      <span>{error}</span>
                    </div>
                  )}
                  <button
                    className="text-button completion-replay"
                    ref={completionReplay}
                    onClick={() => void start()}
                    disabled={busy}
                  >
                    Play again
                    <RotateCcw size={20} />
                  </button>
                </section>
              </div>
            )}
          </div>
        </main>
        {!embedded && (
          <footer>
            <div>
              <Mark small />
              <span>Build something great.</span>
            </div>
            <span className="host-label">
              <span className="live-dot" />
              {hostname}
            </span>
          </footer>
        )}
      </div>
      {!embedded && (
        <dialog
          ref={dialog}
          className="feature-dialog"
          aria-labelledby="feature-dialog-title"
          aria-describedby="feature-dialog-description"
          onCancel={() => setModal(null)}
          onClick={(event) => {
            if (event.target === dialog.current) setModal(null);
          }}
          onClose={() => setModal(null)}
        >
          {modal && (
            <>
              <button
                className="dialog-close icon-button"
                onClick={() => setModal(null)}
                aria-label="Close hint"
                autoFocus
              >
                <X size={20} />
              </button>
              <span className={`modal-icon tone-${modal.id}`}>
                <FeatureIcon id={modal.id} size={32} />
              </span>
              <div className="eyebrow">A LITTLE KUPE KNOW-HOW</div>
              <h2 id="feature-dialog-title">{modal.title}</h2>
              <p className="modal-tagline">{modal.description}</p>
              <p id="feature-dialog-description">{modal.detail}</p>
              {modal.id === "domains" && (
                <div className="url-example">
                  <span>
                    {localPreview
                      ? "EXAMPLE KUPE ADDRESS"
                      : "YOUR KUPE ADDRESS"}
                  </span>
                  <code>{domainHostname}</code>
                  <ArrowRight size={17} />
                  <span>MAKE IT YOURS</span>
                  <code>play.your-domain.com</code>
                </div>
              )}
              {modal.id === "database" && (
                <div className="flow-example">
                  <span>Next.js</span>
                  <ArrowRight size={14} />
                  <span>Go API</span>
                  <ArrowRight size={14} />
                  <span>PostgreSQL</span>
                </div>
              )}
              <a
                className="primary-button"
                href={modal.link}
                target="_blank"
                rel="noreferrer"
              >
                Explore the docs <ExternalLink size={16} />
              </a>
            </>
          )}
        </dialog>
      )}
    </>
  );
}
