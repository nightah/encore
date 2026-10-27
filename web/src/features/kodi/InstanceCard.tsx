import type { ReactNode } from "react";

import { Trans, useTranslation } from "react-i18next";

import type { Instance, Library } from "@api/generated/model";
import { useMakeDefaultInstance, useSyncInstance, useTestInstance, useWakeInstance } from "@api/kodi";
import { checkLabel, connectionStatus, versionLine } from "@components/status";
import { Button } from "@components/ui/Button";
import { Dot, KeyValues, Label } from "@components/ui/Card";
import { Chip } from "@components/ui/Chip";
import { Callout, ErrorState } from "@components/ui/States";
import { formatClock, formatRelative, hostPort } from "@lib/format";

// Instance names are user data: escape them before Trans parses its tags, then unescape the text.
const ESCAPE_VALUES = { shouldUnescape: true, tOptions: { interpolation: { escapeValue: true } } } as const;

interface InstanceCardProps {
  instance: Instance;
  library: Library | undefined;
  onEdit: () => void;
}

export function InstanceCard({ instance, library, onEdit }: InstanceCardProps) {
  const test = useTestInstance();
  const { t } = useTranslation("kodi");
  const sync = useSyncInstance();
  const wake = useWakeInstance();
  const makeDefault = useMakeDefaultInstance();
  const connection = connectionStatus(instance);
  const { state } = instance.status;
  const awake = state === "connected" || state === "http_only";
  const shared = instance.library.shared;
  const others = library?.members.filter((m) => m.instance_id !== instance.id).map((m) => m.name) ?? [];
  const sharedWith = others.join(t("card.and"));
  const showTest = test.isSuccess || test.isError || test.isPending;
  const headingId = `instance-${instance.id}-h`;

  return (
    <article aria-labelledby={headingId} className="flex flex-col gap-4 rounded-xl border border-line bg-surface p-5">
      <div className="flex flex-wrap items-center justify-between gap-2.5">
        <div className="flex flex-wrap items-center gap-2.5">
          <h2 id={headingId} className="font-sans font-semibold text-lg">
            {instance.name}
          </h2>
          <Chip tone={connection.tone}>{connection.label}</Chip>
          {shared ? (
            <Chip tone="link">{instance.library.is_source ? t("card.sharedSource") : t("card.shared")}</Chip>
          ) : null}
          {instance.default_for_unlinked ? <Chip tone="neutral">{t("card.defaultForUnlinked")}</Chip> : null}
        </div>
        <div className="flex flex-wrap gap-2">
          {awake ? (
            <Button onClick={() => (test.isIdle ? test.mutate(instance.id) : test.reset())} pending={test.isPending}>
              {showTest && !test.isPending ? t("card.hideTest") : t("card.test")}
            </Button>
          ) : null}
          {awake ? (
            <Button onClick={() => sync.mutate(instance.id)} pending={sync.isPending}>
              {t("card.sync")}
            </Button>
          ) : null}
          {!awake && instance.mac ? (
            <Button onClick={() => wake.mutate(instance.id)} pending={wake.isPending} disabled={wake.isSuccess}>
              {wake.isSuccess ? t("card.waking") : t("card.wake")}
            </Button>
          ) : null}
          {!instance.default_for_unlinked ? (
            <Button onClick={() => makeDefault.mutate(instance.id)} pending={makeDefault.isPending}>
              {t("card.useForUnlinked")}
            </Button>
          ) : null}
          <Button aria-label={t("card.editNamed", { name: instance.name })} onClick={onEdit}>
            {t("card.edit")}
          </Button>
        </div>
      </div>

      {state === "http_only" ? (
        <Callout>
          <Dot tone="accent" className="mt-1.75 size-2" />
          <span>
            {shared && !instance.library.is_source ? (
              <Trans
                t={t}
                i18nKey="card.httpOnly.viaSource"
                values={{
                  name: instance.name,
                  source:
                    library?.members.find((m) => m.instance_id === library.source_instance_id)?.name ??
                    t("card.httpOnly.theSource"),
                }}
                components={{ b: <b /> }}
                {...ESCAPE_VALUES}
              />
            ) : (
              <Trans
                t={t}
                i18nKey="card.httpOnly.scheduled"
                values={{ name: instance.name }}
                components={{ b: <b /> }}
                {...ESCAPE_VALUES}
              />
            )}
          </span>
        </Callout>
      ) : null}
      {state === "offline" ? (
        <Callout tone="bad">
          <span>{t("card.offline", { name: instance.name })}</span>
        </Callout>
      ) : null}

      <KeyValues
        items={[
          { label: t("card.address"), mono: true, value: hostPort(instance.url) },
          {
            label: t("card.version"),
            value: versionLine(instance),
          },
          { label: t("card.mac"), mono: !!instance.mac, value: instance.mac || t("card.notSet") },
          state === "asleep"
            ? { label: t("card.wakeTimeout.label"), value: t("card.wakeTimeout.value") }
            : {
                label: t("card.library.label"),
                value: shared ? t("card.library.sharedWith", { names: sharedWith }) : t("card.library.own"),
              },
        ]}
      />

      {state === "connected" ? (
        <div className="grid grid-cols-[repeat(auto-fit,minmax(150px,1fr))] gap-2.5 rounded-[10px] bg-soft px-4 py-3.5">
          <Stat label={t("card.library.label")}>
            {t("card.stats.movies", { count: instance.library.movies })} ·{" "}
            {t("card.stats.shows", { count: instance.library.shows })}
            {shared ? ` · ${t("card.stats.sharedWith", { names: sharedWith })}` : ""}
          </Stat>
          <Stat label={t("card.stats.lastEvent")}>
            {instance.status.last_event_at ? formatRelative(instance.status.last_event_at) : t("card.stats.noneYet")}
          </Stat>
          <Stat label={t("card.stats.resync")}>
            {instance.status.next_sync_at
              ? t("card.stats.resyncNext", { time: formatClock(instance.status.next_sync_at) })
              : t("card.stats.resyncEvery")}
          </Stat>
          <Stat label={t("card.stats.newest")}>
            {instance.library.newest_title
              ? `${instance.library.newest_title.title}${instance.library.newest_title.year ? ` (${instance.library.newest_title.year})` : ""} · ${instance.library.newest_title.findable ? t("card.stats.findable") : t("card.stats.notFindable")}`
              : "-"}
          </Stat>
        </div>
      ) : null}

      <div aria-live="polite">
        {test.isPending ? <span className="sr-only">{t("card.testing", { name: instance.name })}</span> : null}
        {test.isError ? <ErrorState error={test.error} /> : null}
        {test.data ? (
          <ul
            aria-label={t("card.testResults")}
            className="flex flex-col gap-1.5 rounded-[10px] border border-line px-3.5 py-3"
          >
            {test.data.checks.map((check) => (
              <li key={check.name} className="flex items-center gap-2.5">
                <Dot tone={check.ok ? "ok" : "bad"} />
                <span>
                  <span className="sr-only">{`${check.ok ? t("card.passed") : t("card.failed")} `}</span>
                  {checkLabel(check)}
                </span>
                {check.ok ? (
                  <span className="ml-auto font-mono text-[13px] text-muted">{t("card.ms", { ms: check.ms })}</span>
                ) : null}
              </li>
            ))}
          </ul>
        ) : null}
      </div>
    </article>
  );
}

function Stat({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex flex-col">
      <Label>{label}</Label>
      <span>{children}</span>
    </div>
  );
}
