import {
  type Candidate,
  CreateAliasRequestKind,
  type MatchResult,
  type MatchResultDecision,
  type RecentTitle,
  type TestMatchRequestKindsItem,
} from "@api/generated/model";
import type { Tone } from "@components/ui/Chip";
import i18n from "@i18n/index";
import { formatScore } from "@lib/format";

const KINDS = ["album", "artist", "episode", "movie", "musicvideo", "playlist", "song", "tvshow"] as const;

function isKnownKind(kind: string): kind is (typeof KINDS)[number] {
  return (KINDS as readonly string[]).includes(kind);
}

export function kindLabel(kind: string): string {
  return isKnownKind(kind) ? i18n.t(`library:kinds.${kind}`) : kind.charAt(0).toUpperCase() + kind.slice(1);
}

/** The candidate's kind as POST /aliases types it, or undefined for a kind the API doesn't list. */
export function aliasKind(kind: string): CreateAliasRequestKind | undefined {
  return (Object.values(CreateAliasRequestKind) as string[]).includes(kind)
    ? (kind as CreateAliasRequestKind)
    : undefined;
}

export function titleWithYear(item: { title: string; year?: number | null | undefined }): string {
  return item.year ? `${item.title} (${item.year})` : item.title;
}

/** Labelled by library:matchTester.types.<value>. */
export const MEDIA_TYPES: Array<{
  value: "any" | "movies" | "shows" | "music";
  kinds?: TestMatchRequestKindsItem[];
}> = [
  { value: "any" },
  { kinds: ["movie"], value: "movies" },
  { kinds: ["tvshow", "episode"], value: "shows" },
  { kinds: ["artist", "album", "song", "musicvideo"], value: "music" },
];

export const EXAMPLES: Array<{ query: string; type: string }> = [
  { query: "rocky two", type: "movies" },
  { query: "seinfield", type: "shows" },
  { query: "the office", type: "shows" },
  { query: "amelie", type: "movies" },
  { query: "conclave", type: "movies" },
];

export function verdict(decision: MatchResultDecision): { label: string; tone: Tone } {
  const label = i18n.t(`library:verdict.${decision}`);
  switch (decision) {
    case "play":
      return { label, tone: "ok" };
    case "ask":
      return { label, tone: "ask" };
    case "not_found":
      return { label, tone: "bad" };
  }
}

/** One sentence explaining the decision, e.g. "Rocky II scores 0.96, 0.31 ahead of the next." */
export function explain(result: Pick<MatchResult, "candidates" | "decision" | "margin">): string {
  const [top, second] = result.candidates;
  if (!top) return i18n.t("library:explain.nothing");
  const margin = formatScore(result.margin || (second ? top.score - second.score : null));
  const score = formatScore(top.score);
  switch (result.decision) {
    case "play":
      return second
        ? i18n.t("library:explain.playTwo", { margin, score, top: top.title })
        : i18n.t("library:explain.playOne", { score, top: top.title });
    case "ask":
      return second
        ? i18n.t("library:explain.askTwo", { margin, second: second.title, top: top.title })
        : i18n.t("library:explain.askOne", { score, top: top.title });
    case "not_found":
      return i18n.t("library:explain.notFound", { score, top: top.title });
  }
}

export function boostText(candidate: Candidate): string {
  const boost = candidate.breakdown.boost;
  if (!boost) return "-";
  return `${boost > 0 ? "+" : ""}${boost.toFixed(2)}`;
}

export function recentStatus(item: RecentTitle): { label: string; tone: Tone } {
  if (item.in_skill_model) return { label: i18n.t("library:recent.status.inModel"), tone: "neutral" };
  if (item.findable) return { label: i18n.t("library:recent.status.findable"), tone: "ok" };
  return { label: i18n.t("library:recent.status.waiting"), tone: "warn" };
}
