import type { Candidate, MatchResult, RequestDetail } from "@api/generated/model";
import { ago } from "@mocks/data";

const MIN = 60_000;
const HOUR = 60 * MIN;

type Breakdown = Candidate["breakdown"];

function cand(
  title: string,
  year: number,
  score: number,
  kind: string,
  media_id: number,
  breakdown: Partial<Breakdown> = {},
): Candidate {
  return {
    breakdown: {
      alias: "",
      boost: 0,
      jaro_winkler: score,
      osa: score,
      phonetic: score,
      token_set: score,
      ...breakdown,
    },
    kind,
    media_id,
    score,
    title,
    year,
  };
}

/** Six requests covering the typical outcomes, newest first. */
export function sampleRequests(now: number): RequestDetail[] {
  const list: Array<Omit<RequestDetail, "simulated">> = [
    {
      device_id: 1,
      device_name: "Lounge Echo Show",
      duration_ms: 148,
      heard: "play the next episode of severance",
      id: 1006,
      intent: "WatchNextEpisode",
      kodi_calls: [
        { method: "VideoLibrary.GetEpisodes", ms: 38, ok: true },
        { method: "Player.Open", ms: 84, ok: true },
      ],
      locale: "en-AU",
      match: {
        candidates: [
          cand("Severance", 2022, 1.0, "tvshow", 311, { alias: "severance" }),
          cand("Silo", 2023, 0.41, "tvshow", 402, { alias: "silo" }),
        ],
        decision: "play",
        error: "",
        margin: 0.59,
        normalized: "severance",
        target: "Lounge",
      },
      outcome: "played",
      slots: { Room: "big tv", Show: "severance" },
      speech: "Playing Severance, season 2, episode 4, Woe's Hollow, in the lounge.",
      target: "Lounge",
      time: ago(5 * MIN, now),
      timing: { kodi_ms: 122, match_ms: 2, verify_ms: 3 },
      via: { kind: "device", room: "Lounge", spoken: "big tv" },
    },
    {
      device_id: 1,
      device_name: "Lounge Echo Show",
      duration_ms: 61,
      heard: "play the new dune",
      id: 1005,
      intent: "PlayMedia",
      kodi_calls: [],
      locale: "en-AU",
      match: {
        candidates: [
          cand("Dune: Part Two", 2024, 0.87, "movie", 2101, {
            alias: "dune part two",
            boost: 0.05,
          }),
          cand("Dune", 2021, 0.84, "movie", 1877, {
            alias: "dune",
            boost: 0.03,
          }),
          cand("Dune", 1984, 0.71, "movie", 112, { alias: "dune" }),
        ],
        decision: "ask",
        error: "",
        margin: 0.03,
        normalized: "new dune",
        target: "Lounge",
      },
      outcome: "asked",
      slots: { Title: "the new dune" },
      speech: "Did you mean Dune Part Two from 2024, or Dune from 2021?",
      target: "Lounge",
      time: ago(34 * MIN, now),
      timing: { kodi_ms: 0, match_ms: 4, verify_ms: 3 },
      via: null,
    },
    {
      device_id: 3,
      device_name: "Kids' room Dot",
      duration_ms: 171,
      heard: "play blue y",
      id: 1004,
      intent: "PlayMedia",
      kodi_calls: [
        { method: "VideoLibrary.GetEpisodes", ms: 29, ok: true },
        { method: "Player.Open", ms: 97, ok: true },
      ],
      locale: "en-AU",
      match: {
        candidates: [
          cand("Bluey", 2018, 0.93, "tvshow", 88, { alias: "bluey", phonetic: 1.0 }),
          cand("Blue Planet II", 2017, 0.52, "tvshow", 91, { alias: "blue planet 2" }),
        ],
        decision: "play",
        error: "",
        margin: 0.41,
        normalized: "blue y",
        target: "Kids' room",
      },
      outcome: "played",
      slots: { Title: "blue y" },
      speech: "Playing Bluey, season 3, episode 12, Rain, in the kids' room.",
      target: "Kids' room",
      time: ago(48 * MIN, now),
      timing: { kodi_ms: 126, match_ms: 3, verify_ms: 3 },
      via: null,
    },
    {
      device_id: 5,
      device_name: "",
      duration_ms: 9,
      heard: "pause",
      id: 1003,
      intent: "AMAZON.PauseIntent",
      kodi_calls: [],
      locale: "en-AU",
      match: null,
      outcome: "unlinked",
      slots: {},
      speech: "This Echo isn't linked to a Kodi yet. Open Encore's settings to choose one.",
      target: "",
      time: ago(76 * MIN, now),
      timing: { kodi_ms: 0, match_ms: 1, verify_ms: 3 },
      via: null,
    },
    {
      device_id: 2,
      device_name: "Bedroom Echo",
      duration_ms: 6400,
      heard: "shuffle radiohead",
      id: 1002,
      intent: "ShuffleMedia",
      kodi_calls: [{ method: "Player.Open", ms: 140, ok: true }],
      locale: "en-AU",
      match: {
        candidates: [cand("Radiohead", 0, 1.0, "artist", 412, { alias: "radiohead" })],
        decision: "play",
        error: "",
        margin: 1,
        normalized: "radiohead",
        target: "Bedroom",
      },
      outcome: "played",
      slots: { Artist: "radiohead" },
      speech: "Waking the bedroom TV… Shuffling Radiohead.",
      target: "Bedroom",
      time: ago(2 * HOUR + 41 * MIN, now),
      timing: { kodi_ms: 6396, match_ms: 1, verify_ms: 3 },
      via: null,
    },
    {
      device_id: 1,
      device_name: "Lounge Echo Show",
      duration_ms: 212,
      heard: "play hot fuzzy",
      id: 1001,
      intent: "PlayMedia",
      kodi_calls: [{ method: "VideoLibrary.GetMovies", ms: 151, ok: true }],
      locale: "en-AU",
      match: {
        candidates: [
          cand("Hot Fuzz", 2007, 0.58, "movie", 733, { alias: "hot fuzz" }),
          cand("Hot Shots!", 1991, 0.44, "movie", 520, { alias: "hot shots" }),
        ],
        decision: "not_found",
        error: "",
        margin: 0.14,
        normalized: "hot fuzzy",
        target: "Lounge",
      },
      outcome: "not_found",
      slots: { Title: "hot fuzzy" },
      speech: "I couldn't find hot fuzzy in your library. The closest is Hot Fuzz. Say “play Hot Fuzz” to watch it.",
      target: "Lounge",
      time: ago(3 * HOUR + 24 * MIN, now),
      timing: { kodi_ms: 151, match_ms: 6, verify_ms: 3 },
      via: null,
    },
  ];
  return list.map((r) => ({ ...r, simulated: false }));
}

/** Older routine requests so the log has more than one page. */
export function olderRequests(now: number, count = 70): RequestDetail[] {
  const pool = [
    {
      device: [1, "Lounge Echo Show", "Lounge"] as const,
      heard: "pause",
      intent: "AMAZON.PauseIntent",
    },
    {
      device: [2, "Bedroom Echo", "Bedroom"] as const,
      heard: "resume",
      intent: "AMAZON.ResumeIntent",
    },
    {
      device: [1, "Lounge Echo Show", "Lounge"] as const,
      heard: "what's playing",
      intent: "WhatsPlaying",
    },
    {
      device: [3, "Kids' room Dot", "Kids' room"] as const,
      heard: "play bluey",
      intent: "PlayMedia",
    },
    {
      device: [4, "Study Echo Spot", "Lounge"] as const,
      heard: "skip forward thirty seconds",
      intent: "SeekForward",
    },
  ];
  return Array.from({ length: count }, (_, index) => {
    const entry = pool[index % pool.length] ?? pool[0];
    if (!entry) throw new Error("unreachable");
    const [deviceId, deviceName, target] = entry.device;
    return {
      device_id: deviceId,
      device_name: deviceName,
      duration_ms: 40 + ((index * 37) % 160),
      heard: entry.heard,
      id: 1000 - index,
      intent: entry.intent,
      kodi_calls: [{ method: "Player.GetActivePlayers", ms: 12, ok: true }],
      locale: "en-AU",
      match: null,
      outcome: entry.intent === "PlayMedia" ? "played" : "ok",
      simulated: false,
      slots: {},
      speech: entry.intent === "WhatsPlaying" ? "Severance is playing in the lounge." : "",
      target,
      time: ago(4 * HOUR + index * 47 * MIN, now),
      timing: { kodi_ms: 12, match_ms: 1, verify_ms: 3 },
      via: null,
    };
  });
}

/** Precomputed match tester results for the example pills and the request log queries. */
const MATCH_CASES: Record<string, MatchResult> = {
  amelie: {
    candidates: [
      cand("Amélie", 2001, 1.0, "movie", 301, {
        alias: "amelie",
        jaro_winkler: 1,
        phonetic: 1,
        token_set: 1,
      }),
    ],
    decision: "play",
    margin: 1,
    normalized: "amelie",
  },
  "blue y": {
    candidates: [
      cand("Bluey", 2018, 0.93, "tvshow", 88, { alias: "bluey", phonetic: 1.0 }),
      cand("Blue Planet II", 2017, 0.52, "tvshow", 91, { alias: "blue planet 2" }),
    ],
    decision: "play",
    margin: 0.41,
    normalized: "blue y",
  },
  conclave: {
    candidates: [
      cand("Conclave", 2024, 1.0, "movie", 2184, {
        alias: "conclave",
        boost: 0.03,
        jaro_winkler: 1,
        phonetic: 1,
        token_set: 1,
      }),
    ],
    decision: "play",
    margin: 1,
    normalized: "conclave",
  },
  "hot fuzzy": {
    candidates: [
      cand("Hot Fuzz", 2007, 0.58, "movie", 733, { alias: "hot fuzz" }),
      cand("Hot Shots!", 1991, 0.44, "movie", 520, { alias: "hot shots" }),
    ],
    decision: "not_found",
    margin: 0.14,
    normalized: "hot fuzzy",
  },
  "rocky two": {
    candidates: [
      cand("Rocky II", 1979, 0.96, "movie", 1201, {
        alias: "rocky 2",
        jaro_winkler: 0.97,
        phonetic: 1.0,
        token_set: 1.0,
      }),
      cand("Rocky", 1976, 0.65, "movie", 1200, {
        alias: "rocky",
        jaro_winkler: 0.91,
        phonetic: 0.5,
        token_set: 0.8,
      }),
      cand("Rocky IV", 1985, 0.58, "movie", 1203, {
        alias: "rocky 4",
        jaro_winkler: 0.89,
        phonetic: 0.5,
        token_set: 0.67,
      }),
    ],
    decision: "play",
    margin: 0.31,
    normalized: "rocky 2",
  },
  seinfield: {
    candidates: [
      cand("Seinfeld", 1989, 0.93, "tvshow", 140, {
        alias: "seinfeld",
        jaro_winkler: 0.96,
        phonetic: 1.0,
        token_set: 0.88,
      }),
      cand("Shetland", 2013, 0.44, "tvshow", 233, {
        alias: "shetland",
        jaro_winkler: 0.62,
        phonetic: 0.25,
        token_set: 0.47,
      }),
    ],
    decision: "play",
    margin: 0.49,
    normalized: "seinfield",
  },
  "the new dune": {
    candidates: [
      cand("Dune: Part Two", 2024, 0.87, "movie", 2101, {
        alias: "dune part two",
        boost: 0.05,
      }),
      cand("Dune", 2021, 0.84, "movie", 1877, {
        alias: "dune",
        boost: 0.03,
      }),
      cand("Dune", 1984, 0.71, "movie", 112, { alias: "dune" }),
    ],
    decision: "ask",
    margin: 0.03,
    normalized: "new dune",
  },
  "the office": {
    candidates: [
      cand("The Office (US)", 2005, 0.99, "tvshow", 75, {
        alias: "office",
        boost: 0.02,
        jaro_winkler: 1,
        phonetic: 1,
        token_set: 1,
      }),
      cand("The Office (UK)", 2001, 0.97, "tvshow", 74, {
        alias: "office",
        jaro_winkler: 1,
        phonetic: 1,
        token_set: 1,
      }),
    ],
    decision: "ask",
    margin: 0.02,
    normalized: "office",
  },
};

/** A small catalog for queries that aren't precomputed. */
const CATALOG: Array<{
  title: string;
  year: number;
  kind: string;
  media_id: number;
}> = [
  { kind: "tvshow", media_id: 311, title: "Severance", year: 2022 },
  { kind: "tvshow", media_id: 512, title: "Slow Horses", year: 2022 },
  { kind: "tvshow", media_id: 530, title: "The Bear", year: 2022 },
  { kind: "tvshow", media_id: 88, title: "Bluey", year: 2018 },
  { kind: "movie", media_id: 2184, title: "Conclave", year: 2024 },
  { kind: "movie", media_id: 2190, title: "The Wild Robot", year: 2024 },
  { kind: "movie", media_id: 2177, title: "Flow", year: 2024 },
  { kind: "movie", media_id: 733, title: "Hot Fuzz", year: 2007 },
  { kind: "movie", media_id: 2101, title: "Dune: Part Two", year: 2024 },
  { kind: "movie", media_id: 301, title: "Amélie", year: 2001 },
  { kind: "movie", media_id: 1201, title: "Rocky II", year: 1979 },
  { kind: "artist", media_id: 412, title: "Radiohead", year: 0 },
];

function normalise(text: string): string {
  return text
    .toLowerCase()
    .normalize("NFD")
    .replace(/[̀-ͯ]/g, "")
    .replace(/[^a-z0-9 ]/g, "")
    .replace(/^(the|a|an) /, "")
    .trim();
}

function tokenSet(a: string, b: string): number {
  const left = new Set(a.split(" ").filter(Boolean));
  const right = new Set(b.split(" ").filter(Boolean));
  if (left.size === 0 || right.size === 0) return 0;
  let shared = 0;
  for (const token of left) if (right.has(token)) shared += 1;
  return (2 * shared) / (left.size + right.size);
}

function prefixSimilarity(a: string, b: string): number {
  let i = 0;
  while (i < a.length && i < b.length && a[i] === b[i]) i += 1;
  return i / Math.max(a.length, b.length, 1);
}

/** Rough stand-in for the server's matcher, good enough for exploring the UI. */
export function fuzzyMatch(query: string, kinds?: string[]): MatchResult {
  const known = MATCH_CASES[query.trim().toLowerCase()];
  if (known) return known;
  const normalized = normalise(query);
  const scored = CATALOG.filter((item) => !kinds?.length || kinds.includes(item.kind))
    .map((item) => {
      const title = normalise(item.title);
      const ts = tokenSet(normalized, title);
      const jw = 0.6 + 0.4 * prefixSimilarity(normalized, title);
      const score = Math.round((0.6 * ts + 0.4 * (jw - 0.6) * 2.5) * 100) / 100;
      return cand(item.title, item.year, Math.min(1, score), item.kind, item.media_id, {
        alias: title,
        jaro_winkler: Math.round(jw * 100) / 100,
        phonetic: ts > 0.5 ? 1 : 0.25,
        token_set: Math.round(ts * 100) / 100,
      });
    })
    .filter((c) => c.score > 0.2)
    .sort((a, b) => b.score - a.score)
    .slice(0, 5);
  const top = scored[0]?.score ?? 0;
  const margin = Math.round((top - (scored[1]?.score ?? 0)) * 100) / 100;
  const decision = top < 0.6 ? "not_found" : margin < 0.1 ? "ask" : "play";
  return { candidates: scored, decision, margin, normalized };
}
