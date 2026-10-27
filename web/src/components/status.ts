import type { Check, Instance, RequestSummaryOutcome as Outcome, RequestSummary } from "@api/generated/model";
import type { Tone } from "@components/ui/Chip";
import i18n from "@i18n/index";

/** Display helpers shared by the overview, Kodi, request log and devices pages. */

export function outcomeChip(request: Pick<RequestSummary, "outcome" | "target" | "intent">): {
  label: string;
  tone: Tone;
} {
  const tones: Record<Outcome, Tone> = {
    asked: "ask",
    error: "bad",
    not_found: "bad",
    ok: "ok",
    played: "ok",
    unlinked: "warn",
  };
  const tone = tones[request.outcome];
  if (tone) return { label: outcomeLabel(request.outcome), tone };
  // An outcome this UI doesn't know (from a newer server) shows as-is rather than breaking the page.
  return { label: request.outcome || outcomeLabel("ok"), tone: "neutral" };
}

function outcomeLabel(outcome: Outcome): string {
  return i18n.t(`outcome.${outcome}`);
}

/** The request log's outcome filter; labels are getters so they follow the interface language. */
export const OUTCOME_OPTIONS: Array<{ value: Outcome; label: string }> = (
  ["played", "asked", "not_found", "unlinked", "ok", "error"] as const
).map((value) => ({
  get label() {
    return outcomeLabel(value);
  },
  value,
}));

/** The connection test's checks, in the order the server runs them; an array, as object keys are kept sorted. */
export const CHECK_ORDER: Array<Check["name"]> = ["ping", "websocket", "auth", "library"];

/** Getters, so the labels follow the interface language. */
export const CHECK_LABELS: Record<Check["name"], string> = {
  get auth() {
    return i18n.t("check.auth");
  },
  get library() {
    return i18n.t("check.library");
  },
  get ping() {
    return i18n.t("check.ping");
  },
  websocket: "WebSocket",
};

/** A connection test line: the server's detail, or what was checked when it sends none. */
export function checkLabel(check: Pick<Check, "name" | "detail">): string {
  return check.detail || CHECK_LABELS[check.name];
}

/** "13.5.0" → "13.5" */
function shortApi(version: string | null | undefined): string {
  return version ? version.split(".").slice(0, 2).join(".") : "";
}

/** "Kodi 21.3 · API 13.5", leaving out what isn't known; Encore learns both once it reaches Kodi. */
export function versionLine(instance: Instance): string {
  const parts = [
    instance.status.kodi_version ? `Kodi ${instance.status.kodi_version}` : "",
    instance.status.api_version ? `API ${shortApi(instance.status.api_version)}` : "",
  ].filter(Boolean);
  return parts.length ? parts.join(" · ") : i18n.t("instance.versionUnknown");
}

export function instanceStatus(instance: Instance): { label: string; tone: Tone } {
  switch (instance.status.state) {
    case "connected":
    case "http_only":
      // Green while playing; amber while paused, which is waiting rather than going.
      if (instance.now_playing) {
        return instance.now_playing.paused
          ? { label: i18n.t("instance.paused"), tone: "warn" }
          : { label: i18n.t("instance.playing"), tone: "ok" };
      }
      return { label: i18n.t("instance.idle"), tone: "info" };
    case "asleep":
      return { label: i18n.t("instance.asleep"), tone: "neutral" };
    case "offline":
      return { label: i18n.t("instance.offline"), tone: "bad" };
  }
}

/** Connection pill on the Kodi page: "Connected · WebSocket", "Connected · HTTP only", "Asleep", "Offline". */
export function connectionStatus(instance: Instance): { label: string; tone: Tone } {
  switch (instance.status.state) {
    case "connected":
      return { label: i18n.t("instance.connectedWebSocket"), tone: "ok" };
    case "http_only":
      return { label: i18n.t("instance.connectedHttp"), tone: "warn" };
    case "asleep":
      return { label: i18n.t("instance.asleep"), tone: "neutral" };
    case "offline":
      return { label: i18n.t("instance.offline"), tone: "bad" };
  }
}

export function transportLabel(instance: Instance): string {
  if (instance.status.state === "connected") return "WebSocket";
  if (instance.status.state === "http_only") return i18n.t("instance.httpOnly");
  return instance.mac ? i18n.t("instance.macSaved") : i18n.t("instance.noMac");
}
