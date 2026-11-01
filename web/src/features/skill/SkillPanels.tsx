import { useState } from "react";

import { Link } from "@tanstack/react-router";
import { Check, LoaderCircle, X } from "lucide-react";
import { Trans, useTranslation } from "react-i18next";

import { errorMessage } from "@api/client";
import type { Deploy, LastDeploy, SkillInfo as Skill } from "@api/generated/model";
import { getAuthorize, skillSettingsFrom, useDisconnectAmazon, useSaveSkillSettings } from "@api/skill";
import { Button } from "@components/ui/Button";
import { Card, KeyValues, Section } from "@components/ui/Card";
import { Chip, ToggleChip } from "@components/ui/Chip";
import { cn } from "@components/ui/cn";
import { Dialog } from "@components/ui/Dialog";
import { Meter } from "@components/ui/Meter";
import { Callout } from "@components/ui/States";
import { Table, Td, Th } from "@components/ui/Table";
import { CONTENT, type Content, toggleContent } from "@features/skill/content";
import { type MarkState, buildTimeline, shortId } from "@features/skill/timeline";
import { formatDayOrClock, formatList, formatMegabytes, formatNumber } from "@lib/format";
import { pageLocation } from "@lib/pageLocation";

const markStyles: Record<MarkState, string> = {
  busy: "bg-accent text-primary-ink",
  fail: "bg-bad text-surface",
  ok: "bg-[#166534] text-white",
  pending: "bg-line text-ink",
};

function Mark({ state }: { state: MarkState }) {
  const Icon = { busy: LoaderCircle, fail: X, ok: Check, pending: null }[state];
  return (
    <span
      aria-hidden="true"
      className={cn("flex size-5.5 flex-none items-center justify-center rounded-full", markStyles[state])}
    >
      {Icon ? <Icon className={cn("size-3.5", state === "busy" && "animate-spin")} strokeWidth={3} /> : null}
    </span>
  );
}

export function Timeline({ skill, deploy }: { skill: Skill; deploy: Deploy | undefined }) {
  const { t } = useTranslation("skill");
  const items = buildTimeline(skill, deploy);
  return (
    <section aria-label={t("timeline.label")}>
      <Card className="grid grid-cols-[repeat(auto-fit,minmax(200px,1fr))] gap-4 px-5 py-4.5">
        <ol className="contents">
          {items.map((item) => (
            <li key={item.title} className="flex items-start gap-2.5">
              <Mark state={item.state} />
              <div className="flex min-w-0 flex-col">
                <span className="font-semibold">
                  {item.title}
                  <span className="sr-only">: {t(`timeline.state.${item.state}`)}</span>
                </span>
                <span className="wrap-anywhere font-mono text-muted text-xs">{item.detail}</span>
              </div>
            </li>
          ))}
        </ol>
      </Card>
    </section>
  );
}

const STATUS_TONE = {
  failed: "bad",
  in_progress: "info",
  succeeded: "ok",
} as const;

export function LocalesPanel({ skill, deploying }: { skill: Skill; deploying: boolean }) {
  const { t } = useTranslation("skill");
  const save = useSaveSkillSettings(t("locales.saved"));
  const [note, setNote] = useState<string>();
  const enabled = skill.locales_enabled;
  const rows = skill.last_deploy?.locales ?? [];
  // Amazon's limit applies to each locale's model on its own.
  const limit = skill.model_size?.limit_bytes ?? 1_500_000;

  function toggle(locale: string) {
    const on = enabled.includes(locale);
    if (on && enabled.length === 1) {
      setNote(t("locales.enabled.keepOne"));
      return;
    }
    setNote(undefined);
    const next = skill.locales_available.filter((code) => (code === locale ? !on : enabled.includes(code)));
    save.mutate(skillSettingsFrom(skill, { locales_enabled: next }));
  }

  function toggleGroup(group: Content) {
    const on = skill.content.includes(group);
    if (on && skill.content.length === 1) {
      setNote(t("locales.content.keepOne"));
      return;
    }
    setNote(undefined);
    save.mutate(skillSettingsFrom(skill, { content: toggleContent(skill.content, group, !on) }));
  }

  return (
    <section
      aria-labelledby="loc-h"
      className="flex min-w-0 flex-[3_1_560px] flex-col rounded-xl border border-line bg-surface"
    >
      <div className="flex flex-wrap items-center justify-between gap-2.5 px-4.5 pt-4 pb-1.5">
        <h2 id="loc-h" className="font-display font-semibold text-lg">
          {t("locales.title")}
        </h2>
      </div>
      {rows.length ? (
        <Table aria-label={t("locales.title")}>
          <thead>
            <tr>
              <Th>{t("locales.columns.locale")}</Th>
              <Th>{t("locales.columns.build")}</Th>
              <Th>{t("locales.columns.titles")}</Th>
              <Th>{t("locales.columns.size", { limit: formatMegabytes(limit) })}</Th>
              <Th>{t("locales.columns.built")}</Th>
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => {
              const status = deploying ? "in_progress" : row.status;
              // A status this UI doesn't know shows as-is rather than breaking the page.
              const known = status in STATUS_TONE ? (status as keyof typeof STATUS_TONE) : undefined;
              const chip = known
                ? { label: t(`locales.build.${known}`), tone: STATUS_TONE[known] }
                : { label: status, tone: "neutral" as const };
              return (
                <tr key={row.locale}>
                  <Td label={t("locales.columns.locale")} className="font-mono text-[13px]">
                    {row.locale}
                  </Td>
                  <Td label={t("locales.columns.build")} className="max-sm:flex-wrap">
                    <span className="flex flex-wrap gap-1.5">
                      <Chip tone={chip.tone}>{chip.label}</Chip>
                      {row.degraded && !deploying ? <Chip tone="warn">{t("locales.build.degraded")}</Chip> : null}
                    </span>
                    {row.errors?.length ? (
                      <ul className="mt-1.5 flex flex-col gap-0.5 text-[13px] text-bad">
                        {row.errors.map((error) => (
                          <li key={error}>{error}</li>
                        ))}
                      </ul>
                    ) : null}
                  </Td>
                  <Td label={t("locales.columns.titles")} className="font-mono text-[13px]">
                    {row.status !== "succeeded"
                      ? "-"
                      : row.degraded
                        ? t("locales.degradedTitles", {
                            kept: formatNumber(row.titles),
                            total: formatNumber(row.degraded.titles_asked_for),
                          })
                        : formatNumber(row.titles)}
                  </Td>
                  <Td
                    label={t("locales.columns.size", { limit: formatMegabytes(limit) })}
                    className="font-mono text-[13px]"
                  >
                    {row.size_bytes ? (
                      <span className="flex items-center gap-2.5">
                        <Meter
                          value={row.size_bytes}
                          max={limit}
                          height="h-1.5"
                          className="w-16 flex-none"
                          label={t("locales.sizeUsed", {
                            limit: formatMegabytes(limit),
                            locale: row.locale,
                            used: formatMegabytes(row.size_bytes),
                          })}
                        />
                        {formatMegabytes(row.size_bytes)}
                      </span>
                    ) : (
                      "-"
                    )}
                  </Td>
                  <Td label={t("locales.columns.built")} className="text-muted">
                    {formatDayOrClock(row.built_at)}
                  </Td>
                </tr>
              );
            })}
          </tbody>
        </Table>
      ) : (
        <p className="px-4.5 pt-1 pb-3 text-muted">{t("locales.empty")}</p>
      )}
      <div className="mx-4.5 mt-1.5 mb-4.5 flex flex-col gap-2.5 border-line-soft border-t pt-3.5">
        <fieldset className="m-0 flex flex-wrap gap-2 border-0 p-0">
          <legend className="mb-2.5 font-semibold">{t("locales.enabled.legend")}</legend>
          {skill.locales_available.map((code) => (
            <ToggleChip
              key={code}
              pressed={enabled.includes(code)}
              onClick={() => toggle(code)}
              disabled={save.isPending}
            >
              {code}
            </ToggleChip>
          ))}
        </fieldset>
        <span className="text-[13px] text-muted">{t("locales.enabled.hint")}</span>
        <fieldset className="m-0 mt-2 flex flex-wrap gap-2 border-0 p-0">
          <legend className="mb-2.5 font-semibold">{t("locales.content.legend")}</legend>
          {CONTENT.map((c) => (
            <ToggleChip
              key={c.value}
              pressed={skill.content.includes(c.value)}
              onClick={() => toggleGroup(c.value)}
              disabled={save.isPending}
            >
              {c.label}
            </ToggleChip>
          ))}
        </fieldset>
        <span className="text-[13px] text-muted">{t("locales.content.hint")}</span>
        <div aria-live="polite" className="text-[13px]">
          {note ? <span className="text-warn">{note}</span> : null}
          {save.error ? <span className="text-bad">{errorMessage(save.error)}</span> : null}
          {!note && !save.error && skill.needs_deploy ? (
            <span className="text-warn">
              {skill.auto_deploy && !skill.auto_deploy_paused
                ? t("locales.needsDeploy.automatic")
                : skill.update_reason === "library"
                  ? t("locales.needsDeploy.library")
                  : t("locales.needsDeploy.settings")}
            </span>
          ) : null}
        </div>
      </div>
    </section>
  );
}

/** Titles in the model: the same for every locale, whose sizes are in the locale table. */
export function ModelTitlesCard({ skill }: { skill: Skill }) {
  const { t } = useTranslation("skill");
  const size = skill.model_size;
  const count = <span className="text-ink" />;
  return (
    <Section id="size-h" title={t("titles.title")}>
      {size ? (
        <>
          <ul className="flex list-disc flex-col gap-0.5 ps-4.5 text-[13px] text-muted">
            <li>
              <Trans
                t={t}
                i18nKey="titles.inModel"
                count={size.titles_in_model}
                values={{ n: formatNumber(size.titles_in_model) }}
                components={{ count }}
              />
            </li>
            <li>
              <Trans
                t={t}
                i18nKey="titles.serverOnly"
                count={size.titles_server_only}
                values={{ n: formatNumber(size.titles_server_only) }}
                components={{ count }}
              />
            </li>
          </ul>
          <p className="text-[13px] text-muted">{t("titles.limit", { limit: formatMegabytes(size.limit_bytes) })}</p>
        </>
      ) : (
        <p className="text-[13px] text-muted">{t("titles.empty")}</p>
      )}
    </Section>
  );
}

export function AmazonCard({ skill }: { skill: Skill }) {
  const { t } = useTranslation("skill");
  const disconnect = useDisconnectAmazon();
  const [confirming, setConfirming] = useState(false);
  const [authorizing, setAuthorizing] = useState(false);
  const [authError, setAuthError] = useState<string>();
  const { amazon } = skill;

  async function reauthorise() {
    setAuthorizing(true);
    setAuthError(undefined);
    try {
      const { url } = await getAuthorize();
      pageLocation.assign(url);
    } catch (error) {
      setAuthError(errorMessage(error));
      setAuthorizing(false);
    }
  }

  return (
    <Section id="amazon-h" title={t("amazon.title")}>
      {amazon.connected ? (
        <>
          {amazon.needs_reauthorise ? <Callout tone="warnStrong">{t("amazon.needsReauthorise")}</Callout> : null}
          <KeyValues
            items={[
              { label: t("amazon.vendor"), mono: true, value: amazon.vendor_id || "-" },
              {
                label: t("amazon.profile"),
                mono: true,
                value: amazon.profile_client_id ? shortId(amazon.profile_client_id) : "-",
              },
            ]}
          />
          <div className="flex flex-wrap gap-2">
            <Button
              variant={amazon.needs_reauthorise ? "primary" : "default"}
              onClick={reauthorise}
              pending={authorizing}
            >
              {t("amazon.reauthorise")}
            </Button>
            <Button onClick={() => setConfirming(true)}>{t("amazon.disconnect")}</Button>
          </div>
        </>
      ) : (
        <>
          <p className="text-[13px] text-muted">{t("amazon.notConnected")}</p>
          <div>
            <Link
              to="/setup"
              className="inline-flex min-h-11 items-center text-link underline-offset-2 hover:underline"
            >
              {t("amazon.connect")}
            </Link>
          </div>
        </>
      )}
      {authError ? (
        <p role="alert" className="text-[13px] text-bad">
          {authError}
        </p>
      ) : null}
      <Dialog
        open={confirming}
        onClose={() => setConfirming(false)}
        title={t("amazon.disconnectDialog.title")}
        description={t("amazon.disconnectDialog.description")}
        footer={
          <>
            <Button onClick={() => setConfirming(false)}>{t("amazon.disconnectDialog.cancel")}</Button>
            <Button
              variant="danger"
              pending={disconnect.isPending}
              onClick={() => disconnect.mutate(undefined, { onSuccess: () => setConfirming(false) })}
            >
              {t("amazon.disconnectDialog.confirm")}
            </Button>
          </>
        }
      />
    </Section>
  );
}

/** Shown while alexa.auto_deploy is on but paused by a failure, until a deploy started by hand succeeds. */
export function AutoDeployPausedCard({ skill }: { skill: Skill }) {
  const { t } = useTranslation("skill");
  if (!skill.auto_deploy_paused) return null;
  return (
    <Callout tone="bad" role="alert" className="flex-col gap-1">
      <span className="font-semibold">{t("autoDeployPaused.title")}</span>
      <span>{skill.auto_deploy_paused}</span>
      <span>{t("autoDeployPaused.resume")}</span>
    </Callout>
  );
}

/** Shown while a failed automatic deploy waits to be tried again, before automatic deploys would pause. */
export function AutoDeployRetryCard({ skill }: { skill: Skill }) {
  const { t } = useTranslation("skill");
  const retry = skill.auto_deploy_retry;
  if (!retry || skill.auto_deploy_paused) return null;
  return (
    <Callout tone="warn" role="status" className="flex-col gap-1">
      <span className="font-semibold">{t("autoDeployRetry.title")}</span>
      <span className="wrap-anywhere">{retry.error}</span>
      <span>
        {t("autoDeployRetry.next", {
          attempts: retry.attempts,
          max: retry.max_attempts,
          time: formatDayOrClock(retry.next_at),
        })}
      </span>
    </Callout>
  );
}

/** The last deploy's warnings, the locales it built with fewer library titles than asked for first, with what Amazon said about them. */
export function WarningsCard({ deploy }: { deploy: LastDeploy | null | undefined }) {
  const { t } = useTranslation("skill");
  const warnings = deploy?.warnings ?? [];
  const degraded = (deploy?.locales ?? []).flatMap(({ locale, status, titles, degraded: d }) =>
    status === "succeeded" && d ? [{ d, locale, titles }] : [],
  );
  if (!warnings.length && !degraded.length) return null;
  return (
    <Callout tone="warn" role="note" className="flex-col gap-1">
      <span className="font-semibold">{t("warnings.title")}</span>
      <ul className="flex list-disc flex-col gap-0.5 ps-4.5">
        {degraded.map(({ locale, titles, d }) => (
          <li key={locale}>
            {d.dropped.length
              ? t("warnings.dropped", { locale, titles: formatList(d.dropped) })
              : t("warnings.thinned", {
                  kept: formatNumber(titles),
                  locale,
                  total: formatNumber(d.titles_asked_for),
                })}
            {d.reasons.length ? (
              <ul className="mt-0.5 flex list-none flex-col gap-0.5 ps-0 text-[13px] wrap-anywhere">
                {d.reasons.map((reason) => (
                  <li key={reason}>{reason}</li>
                ))}
              </ul>
            ) : null}
          </li>
        ))}
        {warnings.map((warning) => (
          <li key={warning}>{warning}</li>
        ))}
      </ul>
    </Callout>
  );
}
