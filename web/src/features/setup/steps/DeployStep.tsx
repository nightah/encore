import { type FormEvent, useId, useState } from "react";

import { Radio } from "@base-ui/react/radio";
import { RadioGroup } from "@base-ui/react/radio-group";
import { useTranslation } from "react-i18next";

import { ApiError, errorMessage } from "@api/client";
import type { Privacy } from "@api/generated/model";
import {
  skillSettingsFrom,
  useActiveDeploy,
  useDeploy,
  useSaveSkillSettings,
  useSkill,
  useStartDeploy,
} from "@api/skill";
import { Button } from "@components/ui/Button";
import { cn, focusRing } from "@components/ui/cn";
import { Checkbox, FieldGrid } from "@components/ui/Field";
import { InfoTip } from "@components/ui/InfoTip";
import { Progress } from "@components/ui/Meter";
import { Callout } from "@components/ui/States";
import { StepFooter, StepHeading, StepPending, type StepProps } from "@features/setup/shared";
import { CONTENT, type Content, toggleContent } from "@features/skill/content";
import { deployFraction } from "@features/skill/timeline";

/** Amazon's questions, in the order Amazon asks them. Each has a label and a plain explanation of what it asks under deploy.privacy.<key>; they describe, they don't answer for the user. */
const PRIVACY_QUESTIONS: Array<keyof Privacy> = [
  "uses_personal_info",
  "is_child_directed",
  "is_export_compliant",
  "contains_ads",
  "allows_purchases",
];

type Answers = Record<keyof Privacy, boolean | null>;

type Invocation = "encore" | "kodi";

export function DeployStep({ nav }: StepProps) {
  const { t } = useTranslation("setup");
  const skillQuery = useSkill();
  const skill = skillQuery.data;
  const save = useSaveSkillSettings(null);
  const start = useStartDeploy();
  const [deployId, setDeployId] = useActiveDeploy(skill);
  const deploy = useDeploy(deployId);

  const [invocation, setInvocation] = useState<Invocation | null>(null);
  const [locales, setLocales] = useState<string[] | null>(null);
  const [content, setContent] = useState<Content[] | null>(null);
  const [privacy, setPrivacy] = useState<Answers | null>(null);

  if (!skill) {
    return (
      <div className="flex flex-col gap-4.5">
        <StepHeading>{t("deploy.heading")}</StepHeading>
        <StepPending error={skillQuery.error} onRetry={() => skillQuery.refetch()} />
        <StepFooter nav={nav} primary={null} />
      </div>
    );
  }

  const currentInvocation = invocation ?? skill.invocation;
  const currentLocales = locales ?? skill.locales_enabled;
  const currentContent = content ?? skill.content;
  // Amazon's questions are the user's to answer: nothing is pre-selected, and an earlier answer is only ever the user's own.
  const currentPrivacy =
    privacy ?? (Object.fromEntries(PRIVACY_QUESTIONS.map((key) => [key, skill.privacy[key] ?? null])) as Answers);
  const unanswered = PRIVACY_QUESTIONS.filter((key) => currentPrivacy[key] === null).length;

  const fieldErrors = save.error instanceof ApiError ? save.error.fieldErrors : {};
  // Errors this step shows beside a field; any other (such as the stored endpoint's) goes in the alert.
  const shownBesideField = ["invocation", "locales_enabled", "content"].some((key) => key in fieldErrors);
  const events = deploy.data?.events ?? [];
  const failed = deploy.data?.state === "failed" ? events.find((e) => e.state === "failed") : undefined;
  const succeeded = deploy.data?.state === "succeeded";
  const running = !!deployId && !failed && !succeeded;
  const latest = events.at(-1);
  const percent = deployFraction(deploy.data);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    try {
      await save.mutateAsync(
        skillSettingsFrom(skill, {
          content: currentContent,
          invocation: currentInvocation,
          locales_enabled: currentLocales,
          privacy: currentPrivacy,
        }),
      );
      const { id } = await start.mutateAsync(undefined);
      setDeployId(id);
    } catch {
      // Shown inline.
    }
  };

  if (!skill.amazon.connected) {
    return (
      <div className="flex flex-col gap-4.5">
        <StepHeading>{t("deploy.heading")}</StepHeading>
        <Callout tone="warnStrong">{t("deploy.connectFirst")}</Callout>
        <StepFooter
          nav={nav}
          primary={
            <Button variant="primary" onClick={() => nav.goTo(4)}>
              {t("deploy.goToAmazon")}
            </Button>
          }
        />
      </div>
    );
  }

  const locked = running || succeeded;

  return (
    <form onSubmit={submit} noValidate className="flex flex-col gap-4.5">
      <StepHeading>{t("deploy.heading")}</StepHeading>
      <FieldGrid className="grid-cols-[repeat(auto-fit,minmax(220px,1fr))] gap-3.5">
        <InvocationChoice
          value={currentInvocation}
          disabled={locked}
          french={currentLocales.includes("fr-FR")}
          error={fieldErrors.invocation}
          onChange={setInvocation}
        />
        <fieldset className="flex min-w-0 flex-col gap-1.5" disabled={locked}>
          <legend className="mb-1.5 font-medium">{t("deploy.locales.legend")}</legend>
          <div className="flex flex-wrap gap-x-4 gap-y-2.5">
            {skill.locales_available.map((locale) => (
              <Checkbox
                key={locale}
                label={<span className="font-mono text-[13px]">{locale}</span>}
                checked={currentLocales.includes(locale)}
                disabled={locked}
                onCheckedChange={(checked) =>
                  setLocales(
                    checked
                      ? skill.locales_available.filter((l) => l === locale || currentLocales.includes(l))
                      : currentLocales.filter((l) => l !== locale),
                  )
                }
              />
            ))}
          </div>
          <span className="text-[13px] text-muted">
            {t("deploy.locales.match")}
            <br />
            {t("deploy.locales.faster")}
          </span>
          {fieldErrors.locales_enabled ? (
            <span className="text-[13px] text-bad">{fieldErrors.locales_enabled}</span>
          ) : null}
        </fieldset>
        <fieldset className="col-span-full flex min-w-0 flex-col gap-1.5" disabled={locked}>
          <legend className="mb-1.5 font-medium">{t("content.legend")}</legend>
          <div className="flex flex-col gap-2.5">
            {CONTENT.map((c) => (
              <Checkbox
                key={c.value}
                label={c.label}
                hint={c.hint}
                checked={currentContent.includes(c.value)}
                disabled={locked || (currentContent.length === 1 && currentContent[0] === c.value)}
                onCheckedChange={(checked) => setContent(toggleContent(currentContent, c.value, checked))}
              />
            ))}
          </div>
          <span className="text-[13px] text-muted">{t("deploy.contentNote")}</span>
          {fieldErrors.content ? <span className="text-[13px] text-bad">{fieldErrors.content}</span> : null}
        </fieldset>
      </FieldGrid>

      <fieldset disabled={locked} className="flex flex-col gap-2 rounded-[10px] border border-line px-4 pt-1 pb-3.5">
        <legend className="px-1.5 font-semibold">{t("deploy.privacy.legend")}</legend>
        {PRIVACY_QUESTIONS.map((key) => (
          <YesNo
            key={key}
            label={t(`deploy.privacy.${key}.label`)}
            about={t(`deploy.privacy.${key}.about`)}
            value={currentPrivacy[key]}
            disabled={locked}
            onChange={(answer) => setPrivacy({ ...currentPrivacy, [key]: answer })}
          />
        ))}
        {unanswered > 0 ? (
          <span className="text-[13px] text-muted">{t("deploy.privacy.unanswered", { count: unanswered })}</span>
        ) : null}
      </fieldset>

      <div aria-live="polite" className="flex flex-col gap-1.5">
        {running ? (
          <>
            <span className="font-semibold">{t("deploy.deploying")}</span>
            <Progress value={percent} label={t("deploy.progress", { percent })} />
            <span className="text-[13px] text-muted">
              {latest?.message || t("deploy.starting")} · {t("deploy.duration", { count: currentLocales.length })}
            </span>
          </>
        ) : null}
        {succeeded ? <Callout tone="ok">{t("deploy.succeeded")}</Callout> : null}
        {failed ? (
          <Callout tone="bad">
            {t("deploy.failed", { message: failed.message || t("deploy.noDetails"), step: failed.step })}
          </Callout>
        ) : null}
        {(save.error && !shownBesideField) || start.error ? (
          <p role="alert" className="rounded-[10px] bg-bad-bg px-3.5 py-2.5 text-bad">
            {errorMessage(save.error ?? start.error)}
          </p>
        ) : null}
      </div>

      <StepFooter
        nav={nav}
        primary={
          succeeded ? (
            <Button key="continue" variant="primary" onClick={nav.done}>
              {t("nav.continue")}
            </Button>
          ) : (
            <Button
              key="deploy"
              type="submit"
              variant="primary"
              pending={save.isPending || start.isPending || running}
              disabled={currentLocales.length === 0 || unanswered > 0}
            >
              {failed ? t("deploy.tryAgain") : running ? t("deploy.deploying") : t("deploy.deploy")}
            </Button>
          )
        }
      />
    </form>
  );
}

/** A yes/no question with no default answer. */
function YesNo({
  label,
  about,
  value,
  disabled,
  onChange,
}: {
  label: string;
  about: string;
  value: boolean | null;
  disabled?: boolean;
  onChange: (answer: boolean) => void;
}) {
  const { t } = useTranslation("setup");
  const id = useId();
  return (
    <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1.5 py-1">
      <span className="flex items-center gap-1.5">
        <span id={id}>{label}</span>
        <InfoTip label={label}>{about}</InfoTip>
      </span>
      <RadioGroup
        aria-labelledby={id}
        value={value === null ? "" : value ? "yes" : "no"}
        onValueChange={(next) => onChange(next === "yes")}
        disabled={disabled}
        className="flex gap-4"
      >
        {(["yes", "no"] as const).map((option) => (
          <label key={option} htmlFor={`${id}-${option}`} className="flex cursor-pointer items-center gap-2">
            <Radio.Root
              id={`${id}-${option}`}
              value={option}
              aria-label={t(option === "yes" ? "deploy.answer.yesLabel" : "deploy.answer.noLabel", { question: label })}
              className={cn(
                "flex size-4.5 shrink-0 items-center justify-center rounded-full border border-input-border bg-surface data-checked:border-primary",
                focusRing,
              )}
            >
              <Radio.Indicator className="size-2.5 rounded-full bg-primary data-unchecked:hidden" />
            </Radio.Root>
            {option === "yes" ? t("deploy.answer.yes") : t("deploy.answer.no")}
          </label>
        ))}
      </RadioGroup>
    </div>
  );
}

/** The invocation name: encore or kodi, nothing else. */
function InvocationChoice({
  value,
  disabled,
  french,
  error,
  onChange,
}: {
  value: Invocation;
  disabled?: boolean;
  french: boolean;
  error?: string;
  onChange: (value: Invocation) => void;
}) {
  const { t } = useTranslation("setup");
  const id = useId();
  return (
    <div className="flex min-w-0 flex-col gap-1.5">
      <span id={id} className="font-medium">
        {t("deploy.invocation.label")}
      </span>
      <RadioGroup
        aria-labelledby={id}
        value={value}
        onValueChange={(next) => onChange(next as Invocation)}
        disabled={disabled}
        className="inline-flex w-fit rounded-[10px] border border-line p-1"
      >
        {(["encore", "kodi"] as const).map((option) => (
          <Radio.Root
            key={option}
            value={option}
            className={cn(
              "inline-flex min-h-9 cursor-pointer items-center justify-center rounded-lg px-4 font-medium font-mono text-[13px] text-muted data-checked:bg-primary data-checked:text-primary-ink data-disabled:cursor-not-allowed data-disabled:opacity-70",
              focusRing,
            )}
          >
            {option}
          </Radio.Root>
        ))}
      </RadioGroup>
      <span className="text-[13px] text-muted">
        {t("deploy.invocation.example", { name: value })}
        {value === "encore" && french ? (
          <>
            <br />
            {t("deploy.invocation.french")}
          </>
        ) : null}
      </span>
      {error ? <span className="text-[13px] text-bad">{error}</span> : null}
    </div>
  );
}
