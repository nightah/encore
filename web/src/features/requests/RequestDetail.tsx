import { useState } from "react";

import { Trans, useTranslation } from "react-i18next";

import { errorMessage } from "@api/client";
import {
  type RequestDetail as Detail,
  type Match as MatchDetail,
  type Replay,
  RoomAliasKind,
  type Via,
} from "@api/generated/model";
import { useReplayRequest, useRequest } from "@api/requests";
import { Heard, requestSource } from "@components/Heard";
import { KindPill } from "@components/KindPill";
import { Button } from "@components/ui/Button";
import { Label } from "@components/ui/Card";
import { cn } from "@components/ui/cn";
import { Meter, SegmentBar } from "@components/ui/Meter";
import { ErrorState, Skeleton } from "@components/ui/States";
import { explain, titleWithYear } from "@features/library/labels";
import i18n from "@i18n/index";
import { formatClock, formatDuration, formatScore } from "@lib/format";

/** `Title = "the new dune"` for each slot that has a value: what Alexa heard. The Room slot also holds devices ("on the TV"), so it reads Device when it named one. */
function describeSlots(slots: Detail["slots"], via: Via | null): string {
  const entries = Object.entries(slots).filter(([, value]) => value);
  const device = via?.kind === RoomAliasKind.device;
  return entries.length
    ? entries.map(([name, value]) => `${device && name === "Room" ? "Device" : name} = "${value}"`).join(", ")
    : "-";
}

/** The room or device the request named: a device in a room, another name for a room, or the room itself. */
function ViaLine({ via }: { via: Via }) {
  const room = <KindPill kind={RoomAliasKind.room} className="pr-2.5" />;
  if (via.kind === RoomAliasKind.room && via.spoken === via.room) {
    return (
      <KindPill kind={RoomAliasKind.room} className="pr-2.5">
        {via.room}
      </KindPill>
    );
  }
  return (
    <span className="flex flex-wrap items-center gap-1.5 text-[13px] text-muted">
      <Trans
        ns="requests"
        i18nKey={via.kind === RoomAliasKind.device ? "detail.via.device" : "detail.via.alias"}
        values={{ room: via.room, spoken: via.spoken }}
        components={{ room, spoken: <KindPill kind={via.kind} className="pr-2.5" /> }}
      />
    </span>
  );
}

/** verify / match / Kodi split; falls back to Kodi call time vs the rest for requests logged without timing. */
/** No-break spaces inside each "verify 0 ms" part, so a narrow screen wraps the line only between parts. */
function keepPartsWhole(label: string): string {
  return label
    .split(" · ")
    .map((part) => part.replaceAll(" ", "\u00a0"))
    .join(" · ");
}

function timingParts(detail: Detail) {
  const t = i18n.getFixedT(null, "requests", "detail.timing");
  if (detail.timing) {
    const { verify_ms, match_ms, kodi_ms } = detail.timing;
    return {
      label: t("split", {
        kodi: formatDuration(kodi_ms),
        match: formatDuration(match_ms),
        total: formatDuration(detail.duration_ms),
        verify: formatDuration(verify_ms),
      }),
      segments: [
        { className: "bg-slate", value: verify_ms },
        { className: "bg-accent", value: match_ms },
        { className: "bg-dot-ok", value: kodi_ms },
      ],
    };
  }
  const kodi = detail.kodi_calls.reduce((sum, call) => sum + call.ms, 0);
  const encore = Math.max(0, detail.duration_ms - kodi);
  return {
    label: t("fallback", {
      encore: formatDuration(encore),
      kodi: formatDuration(kodi),
      total: formatDuration(detail.duration_ms),
    }),
    segments: [
      { className: "bg-accent", value: encore },
      { className: "bg-dot-ok", value: kodi },
    ],
  };
}

function Candidates({ match }: { match: MatchDetail }) {
  const { t } = useTranslation("requests");
  return (
    <div className="flex flex-col gap-2">
      <Label>{t("detail.candidates.label")}</Label>
      {match.candidates.length ? (
        <ul className="flex flex-col gap-2">
          {match.candidates.map((candidate) => (
            <li
              key={`${candidate.title}-${candidate.year}-${candidate.media_id}`}
              className="grid grid-cols-[minmax(0,1fr)_minmax(80px,140px)_48px] items-center gap-3"
            >
              <span className="truncate">{titleWithYear(candidate)}</span>
              <Meter
                value={candidate.score * 100}
                label={t("detail.candidates.score", {
                  score: formatScore(candidate.score),
                  title: titleWithYear(candidate),
                })}
                height="h-2"
              />
              <span className="text-right font-mono text-[13px]">{formatScore(candidate.score)}</span>
            </li>
          ))}
        </ul>
      ) : (
        <span className="text-[13px] text-muted">{t("detail.candidates.none")}</span>
      )}
      <span className="text-[13px] text-muted">{explain(match)}</span>
    </div>
  );
}

function ReplayResult({ replay }: { replay: Replay }) {
  const { t } = useTranslation("requests");
  const top = replay.match?.candidates[0];
  return (
    <section
      aria-label={t("detail.replayResult.label")}
      className="flex flex-col gap-2.5 rounded-[10px] border border-line px-3.5 py-3"
    >
      <span className="font-semibold">{t("detail.replayResult.title")}</span>
      {replay.match ? (
        <span className="text-[13px]">
          {top
            ? `${t("detail.replayResult.topMatch", { score: formatScore(top.score), title: titleWithYear(top) })} `
            : ""}
          {explain(replay.match)}
        </span>
      ) : null}
      {replay.speech ? (
        <span className="text-[13px]">{t("detail.replayResult.wouldSay", { speech: replay.speech })}</span>
      ) : null}
      <div className="flex flex-col gap-1.5">
        <Label>{t("detail.replayResult.wouldCall")}</Label>
        {replay.would_call.length ? (
          replay.would_call.map((call, index) => (
            <code
              // biome-ignore lint/suspicious/noArrayIndexKey: calls are positional
              key={index}
              className="wrap-anywhere rounded-lg bg-soft px-3 py-2 font-mono text-[13px]"
            >
              {call.method} {JSON.stringify(call.params)}
            </code>
          ))
        ) : (
          <span className="text-[13px] text-muted">{t("detail.replayResult.noCalls")}</span>
        )}
      </div>
    </section>
  );
}

/** A request in full. `inline` under its row in the list, whose time, Echo and words are already shown above it, so they are only read out. */
export function RequestDetailPane({ id, inline = false }: { id: number; inline?: boolean }) {
  const { t } = useTranslation("requests");
  const { data: detail, error, refetch, isPending } = useRequest(id);
  const replay = useReplayRequest();
  const [copied, setCopied] = useState(false);

  if (isPending) {
    return (
      <div role="status" className="flex flex-col gap-3">
        <span className="sr-only">{t("detail.loading")}</span>
        <Skeleton className="h-8 w-2/3" />
        <Skeleton className="h-24" />
      </div>
    );
  }
  if (error) return <ErrorState error={error} onRetry={() => refetch()} />;

  const slots = describeSlots(detail.slots, detail.via);
  const timing = timingParts(detail);

  async function copy() {
    try {
      await navigator.clipboard.writeText(JSON.stringify(detail, null, 2));
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      setCopied(false);
    }
  }

  return (
    <div className="flex flex-col gap-4.5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className={cn("flex min-w-0 flex-col gap-1", inline && "sr-only")}>
          <Label>
            {formatClock(detail.time)} · {requestSource(detail)} · {detail.locale}
          </Label>
          <h2 id="det-h" className="font-display font-semibold text-[22px]">
            <Heard request={detail} />
          </h2>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button onClick={() => replay.mutate(detail.id)} pending={replay.isPending}>
            {t("detail.replay")}
          </Button>
          <Button onClick={copy} aria-live="polite">
            {copied ? t("detail.copied") : t("detail.copy")}
          </Button>
        </div>
      </div>

      <div aria-live="polite" className="empty:hidden">
        {replay.data ? <ReplayResult replay={replay.data} /> : null}
        {replay.error ? <p className="text-[13px] text-bad">{errorMessage(replay.error)}</p> : null}
      </div>

      <div className="grid grid-cols-[repeat(auto-fit,minmax(160px,1fr))] gap-3">
        <div className="flex flex-col">
          <Label>{t("detail.intent")}</Label>
          <span className="wrap-anywhere font-mono text-[13px]">{detail.intent}</span>
        </div>
        <div className="flex flex-col">
          <Label>{t("detail.slots")}</Label>
          <span className="wrap-anywhere font-mono text-[13px]">{slots}</span>
        </div>
        <div className="flex flex-col items-start gap-1">
          <Label>{t("detail.target.label")}</Label>
          <span>
            {detail.target || (detail.outcome === "unlinked" ? t("detail.target.unlinked") : t("detail.target.none"))}
          </span>
          {detail.via ? <ViaLine via={detail.via} /> : null}
        </div>
      </div>

      {detail.match ? (
        <Candidates match={detail.match} />
      ) : detail.outcome === "unlinked" ? (
        <div className="flex flex-col gap-2">
          <Label>{t("detail.candidates.label")}</Label>
          <span className="text-[13px] text-muted">{t("detail.candidates.unlinked")}</span>
        </div>
      ) : null}
      {detail.match?.error ? <p className="text-[13px] text-bad">{detail.match.error}</p> : null}

      <div className="flex flex-col gap-1.5">
        <Label>{t("detail.kodiCalls")}</Label>
        {detail.kodi_calls.length ? (
          <ul className="flex flex-col gap-1.5">
            {detail.kodi_calls.map((call, index) => (
              <li
                // biome-ignore lint/suspicious/noArrayIndexKey: calls are positional
                key={index}
                className="flex justify-between gap-3 rounded-lg bg-soft px-3 py-2 font-mono text-[13px]"
              >
                <span className="wrap-anywhere">
                  {call.method}
                  {call.ok ? null : <span className="text-bad"> {t("detail.callFailed")}</span>}
                </span>
                <span className="text-muted">{formatDuration(call.ms)}</span>
              </li>
            ))}
          </ul>
        ) : (
          <span className="text-[13px] text-muted">{t("detail.noCalls")}</span>
        )}
      </div>

      <div className="flex flex-col gap-1.5">
        <Label>{t("detail.alexaSaid")}</Label>
        <p className="text-[15px]">{detail.speech ? `“${detail.speech}”` : t("detail.silent")}</p>
      </div>

      <div className="flex flex-col gap-1.5">
        <Label>{t("detail.timing.label")}</Label>
        <SegmentBar segments={timing.segments} label={timing.label} />
        <span className="font-mono text-muted text-xs">{keepPartsWhole(timing.label)}</span>
      </div>
    </div>
  );
}
