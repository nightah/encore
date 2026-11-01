import { Link, createFileRoute, useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import type { Attention, Instance, Library, Overview } from "@api/generated/model";
import { useLibraries } from "@api/kodi";
import { useOverview } from "@api/overview";
import { useInvocationName } from "@api/skill";
import { Cover } from "@components/Cover";
import { Heard, requestSource } from "@components/Heard";
import { Page } from "@components/Page";
import { instanceStatus, outcomeChip, transportLabel, versionLine } from "@components/status";
import { ButtonLink } from "@components/ui/Button";
import { Card, Dot, StatTile } from "@components/ui/Card";
import { Chip } from "@components/ui/Chip";
import { cn } from "@components/ui/cn";
import { Meter } from "@components/ui/Meter";
import { EmptyState, ErrorState, LoadingBlock } from "@components/ui/States";
import { Table, Td, Th } from "@components/ui/Table";
import { formatClock, formatPosition, formatRelative, hostPort } from "@lib/format";

export const Route = createFileRoute("/_shell/")({
  component: OverviewPage,
});

function OverviewPage() {
  const { data, error, refetch, isPending } = useOverview();
  const { data: libraries } = useLibraries();
  const { t } = useTranslation("overview");

  return (
    <Page
      title={t("title")}
      description={t("description")}
      actions={<ButtonLink to="/request-log">{t("openRequestLog")}</ButtonLink>}
    >
      {isPending ? <LoadingBlock /> : null}
      {error ? <ErrorState error={error} onRetry={() => refetch()} /> : null}
      {data ? (
        <>
          <StatusTiles overview={data} libraries={libraries} />
          <section aria-labelledby="kodi-h" className="flex flex-col gap-3.5">
            <h2 id="kodi-h" className="font-display font-semibold text-xl">
              {t("instances.title")}
            </h2>
            {data.instances.length ? (
              <div className="grid grid-cols-[repeat(auto-fit,minmax(280px,1fr))] gap-3.5 min-[1800px]:grid-cols-[repeat(auto-fill,minmax(420px,1fr))]">
                {data.instances.map((instance) => (
                  <InstanceCard key={instance.id} instance={instance} />
                ))}
              </div>
            ) : (
              <EmptyState
                title={t("instances.empty.title")}
                action={<ButtonLink to="/kodi">{t("instances.empty.action")}</ButtonLink>}
              >
                {t("instances.empty.body")}
              </EmptyState>
            )}
          </section>
          <div className="flex flex-wrap items-start gap-3.5">
            <RecentRequests overview={data} />
            <AttentionList items={data.attention} />
          </div>
        </>
      ) : null}
    </Page>
  );
}

function StatusTiles({ overview, libraries }: { overview: Overview; libraries: Library[] | undefined }) {
  const { t } = useTranslation("overview");
  const { skill, endpoint, requests_today: today } = overview;
  const skillDot = { deploying: "warn", failed: "bad", live: "ok", not_deployed: "muted" } as const;
  // The stage is an API value ("development" or "live"); an unknown one shows as it comes, capitalised.
  const stage =
    skill.stage === "development" || skill.stage === "live"
      ? t(`status.skill.stage.${skill.stage}`)
      : t("status.skill.stage.other", { stage: skill.stage.charAt(0).toUpperCase() + skill.stage.slice(1) });
  const shared = libraries?.find((l) => l.members.length > 1);
  const sharedText = shared
    ? t("status.libraries.shareOne", { names: shared.members.map((m) => m.name).join(t("status.libraries.and")) })
    : overview.libraries.shared_groups
      ? t("status.libraries.shared", { count: overview.libraries.shared_groups })
      : t("status.libraries.noneShared");
  const skillDetail = [
    stage,
    t("status.skill.locales", { count: skill.locales.length }),
    ...(skill.needs_deploy ? [t("status.skill.updatePending")] : []),
  ].join(" · ");

  return (
    <section aria-label={t("status.label")} className="grid grid-cols-[repeat(auto-fit,minmax(210px,1fr))] gap-3.5">
      <StatTile
        label={t("status.skill.label")}
        value={t(`status.skill.state.${skill.state}`)}
        dot={skillDot[skill.state]}
        detail={skillDetail}
      />
      <StatTile
        label={t("status.endpoint.label")}
        value={endpoint.reachable ? t("ms", { ms: endpoint.latency_ms }) : t("status.endpoint.unreachable")}
        dot={endpoint.reachable ? "ok" : "bad"}
        detail={
          endpoint.reachable
            ? t("status.endpoint.reachable", { host: hostPort(endpoint.url) })
            : t("status.endpoint.notReachable", { host: hostPort(endpoint.url) })
        }
      />
      <StatTile
        label={t("status.libraries.label")}
        value={
          overview.libraries.synced_at
            ? t("status.libraries.synced", { when: formatRelative(overview.libraries.synced_at) })
            : t("status.libraries.notSynced")
        }
        dot={overview.libraries.synced_at ? "ok" : "muted"}
        detail={`${t("status.libraries.count", { count: overview.libraries.count })} · ${sharedText}`}
      />
      <StatTile
        label={t("status.requests.label")}
        value={String(today.total)}
        dot={today.failed > 0 ? "warn" : "ok"}
        detail={t("status.requests.detail", { failed: today.failed, ms: today.median_ms })}
      />
    </section>
  );
}

function InstanceCard({ instance }: { instance: Instance }) {
  const { t } = useTranslation("overview");
  const status = instanceStatus(instance);
  const playing = instance.now_playing;
  const progress = playing && playing.duration_s > 0 ? (playing.position_s / playing.duration_s) * 100 : 0;
  return (
    <Card className="flex flex-col gap-3.5 p-4.5">
      <div className="flex items-center justify-between gap-2">
        <h3 className="font-sans font-semibold text-base">{instance.name}</h3>
        <Chip tone={status.tone}>{status.label}</Chip>
      </div>
      {playing ? (
        <>
          <div className="flex items-center gap-3.5">
            <Cover artUrl={playing.art_url} title={playing.show || playing.title} />
            <div className="flex min-w-0 flex-col gap-0.5">
              <span className="font-semibold">{playing.show || playing.title}</span>
              {playing.show ? (
                <span className="text-[13px] text-muted">
                  {t("instances.episode", {
                    episode: playing.episode,
                    season: playing.season,
                    title: playing.title,
                  })}
                </span>
              ) : null}
              <span className="font-mono text-[13px] text-muted">
                {formatPosition(playing.position_s)} / {formatPosition(playing.duration_s)}
              </span>
            </div>
          </div>
          <Meter value={progress} label={t("instances.progress", { percent: Math.round(progress) })} height="h-1.5" />
        </>
      ) : (
        <div className="flex min-h-22.5 flex-col justify-center gap-2.5">
          <IdleText instance={instance} />
        </div>
      )}
      <div className="mt-auto flex justify-between gap-3 font-mono text-[12.5px] text-muted">
        <span>{versionLine(instance)}</span>
        <span>{transportLabel(instance)}</span>
      </div>
    </Card>
  );
}

function IdleText({ instance }: { instance: Instance }) {
  const { t } = useTranslation("overview");
  switch (instance.status.state) {
    case "asleep":
      return (
        <>
          <span className="text-muted">
            {instance.status.last_event_at
              ? t("instances.asleep.since", { time: formatClock(instance.status.last_event_at) })
              : t("instances.asleep.plain")}{" "}
            {instance.mac ? t("instances.asleep.willWake") : t("instances.asleep.addMac")}
          </span>
          <Link to="/kodi" className="text-[13.5px] text-link hover:text-ink">
            {t("instances.asleep.manage")}
          </Link>
        </>
      );
    case "offline":
      return (
        <>
          <span className="text-muted">{t("instances.offline.text")}</span>
          <Link to="/kodi" className="text-[13.5px] text-link hover:text-ink">
            {t("instances.offline.test")}
          </Link>
        </>
      );
    default:
      return <span className="text-muted">{t("instances.idle")}</span>;
  }
}

function RecentRequests({ overview }: { overview: Overview }) {
  const navigate = useNavigate();
  const invocation = useInvocationName();
  const { t } = useTranslation("overview");
  return (
    <section
      aria-labelledby="recent-h"
      className="flex min-w-0 flex-[3_1_520px] flex-col rounded-xl border border-line bg-surface"
    >
      <div className="flex items-center justify-between px-4.5 pt-4 pb-1.5">
        <h2 id="recent-h" className="font-display font-semibold text-lg">
          {t("recent.title")}
        </h2>
        <Link to="/request-log" className="inline-flex min-h-11 items-center text-[13.5px] text-link hover:text-ink">
          {t("recent.viewAll")}
        </Link>
      </div>
      {overview.recent_requests.length ? (
        <Table aria-label={t("recent.title")} aria-live="polite">
          <thead>
            <tr>
              <Th>{t("recent.columns.time")}</Th>
              <Th>{t("recent.columns.echo")}</Th>
              <Th>{t("recent.columns.heard")}</Th>
              <Th>{t("recent.columns.result")}</Th>
            </tr>
          </thead>
          <tbody>
            {overview.recent_requests.map((request) => {
              const chip = outcomeChip(request);
              return (
                // The whole row opens the request in the log; the time is its link for keyboards and screen readers.
                <tr
                  key={request.id}
                  className="cursor-pointer hover:bg-soft"
                  onClick={() => void navigate({ search: { id: request.id }, to: "/request-log" })}
                >
                  <Td label={t("recent.columns.time")} className="whitespace-nowrap font-mono text-muted">
                    <Link
                      to="/request-log"
                      search={{ id: request.id }}
                      aria-label={t("recent.open", { time: formatClock(request.time) })}
                      className="hover:text-ink"
                      onClick={(event) => event.stopPropagation()}
                    >
                      {formatClock(request.time)}
                    </Link>
                  </Td>
                  <Td label={t("recent.columns.echo")} className="whitespace-nowrap">
                    {requestSource(request)}
                  </Td>
                  <Td label={t("recent.columns.heard")}>
                    <Heard request={request} />
                  </Td>
                  <Td label={t("recent.columns.result")}>
                    <Chip tone={chip.tone}>{chip.label}</Chip>
                  </Td>
                </tr>
              );
            })}
          </tbody>
        </Table>
      ) : (
        <p className="px-4.5 pt-2 pb-5 text-muted">
          {invocation ? t("recent.empty", { invocation }) : t("recent.emptyNoInvocation")}
        </p>
      )}
    </section>
  );
}

function AttentionList({ items }: { items: Attention[] }) {
  const { t } = useTranslation("overview");
  const linkLabel = (kind: string) =>
    kind === "needs_deploy"
      ? t("attention.links.needsDeploy")
      : kind === "unlinked_device"
        ? t("attention.links.unlinkedDevice")
        : t("attention.links.open");
  return (
    <section
      aria-labelledby="attn-h"
      className="flex min-w-0 flex-[2_1_320px] flex-col gap-3 rounded-xl border border-line bg-surface px-4.5 py-4"
    >
      <h2 id="attn-h" className="font-display font-semibold text-lg">
        {t("attention.title")}
      </h2>
      {items.length === 0 ? <p className="text-muted">{t("attention.empty")}</p> : null}
      <ul aria-live="polite" className="flex flex-col gap-3">
        {items.map((item, index) => {
          const urgent = index === 0 && item.kind === "unlinked_device";
          return (
            <li
              key={`${item.kind}-${item.title}`}
              className={cn("flex gap-3 rounded-[10px] p-3", urgent ? "bg-warn-soft" : "bg-soft")}
            >
              <Dot tone={urgent ? "accent" : "muted"} className="mt-1.75 size-2" />
              <div className="flex flex-col gap-1">
                <span className="font-semibold">{item.title}</span>
                <span className="text-[13px] text-muted">{item.detail}</span>
                {item.link ? (
                  <Link to={item.link} className="w-fit text-[13.5px] text-link hover:text-ink">
                    {linkLabel(item.kind)}
                  </Link>
                ) : null}
              </div>
            </li>
          );
        })}
      </ul>
    </section>
  );
}
