import { type FormEvent, useState } from "react";

import { useTranslation } from "react-i18next";

import { useCheckReachability } from "@api/auth";
import { ApiError, errorMessage } from "@api/client";
import { skillSettingsFrom, useSaveSkillSettings, useSkill } from "@api/skill";
import { Button } from "@components/ui/Button";
import { Field, Input } from "@components/ui/Field";
import { Callout, ErrorState, LoadingBlock } from "@components/ui/States";
import { CheckList, StepFooter, StepHeading, type StepProps } from "@features/setup/shared";

function originOf(url: string | undefined): string {
  if (!url) return "";
  try {
    return new URL(url).origin;
  } catch {
    return "";
  }
}

export function AddressStep({ nav }: StepProps) {
  const { t } = useTranslation("setup");
  const skillQuery = useSkill();
  const skill = skillQuery.data;
  const check = useCheckReachability();
  const save = useSaveSkillSettings(null);
  const [url, setUrl] = useState<string | null>(null);
  const value =
    url ?? (originOf(skill?.endpoint.url) || (window.location.protocol === "https:" ? window.location.origin : ""));

  const result = check.data;
  const failed = result?.checks.some((c) => c.level === "fail") ?? false;
  const unsupported = check.error instanceof ApiError && [404, 405, 501].includes(check.error.status);
  const fieldError = check.error instanceof ApiError ? check.error.fieldErrors.url : undefined;

  const runCheck = async (event: FormEvent) => {
    event.preventDefault();
    const trimmed = value.trim().replace(/\/$/, "");
    try {
      await check.mutateAsync({ url: trimmed });
    } catch {
      return;
    }
    // Store the endpoint the skill will point at; the deploy step uses it.
    if (skill) {
      save.mutate(
        skillSettingsFrom(skill, {
          endpoint: {
            url: `${trimmed}/api/v1/alexa`,
          },
        }),
      );
    }
  };

  const checked = !!result || unsupported;

  return (
    <form onSubmit={runCheck} noValidate className="flex flex-col gap-4.5">
      <StepHeading intro={t("address.intro")}>{t("address.heading")}</StepHeading>
      {/* The address starts from the skill's saved endpoint, so the field waits for the skill rather than showing empty and then filling in. */}
      {!skill ? (
        skillQuery.error ? (
          <ErrorState error={skillQuery.error} onRetry={() => skillQuery.refetch()} />
        ) : (
          <LoadingBlock />
        )
      ) : (
        <>
          <Field
            label={t("address.url.label")}
            hint={t("address.url.hint", { url: "https://encore.example.net" })}
            error={fieldError ?? (check.error && !unsupported && !fieldError ? errorMessage(check.error) : undefined)}
          >
            <Input
              type="url"
              inputMode="url"
              placeholder="https://encore.example.net"
              value={value}
              onChange={(event) => {
                setUrl(event.target.value);
                check.reset();
              }}
            />
          </Field>

          <div aria-live="polite" className="flex flex-col gap-3">
            {check.isPending ? <p className="text-muted">{t("address.checking")}</p> : null}
            {result ? (
              <CheckList
                title={t("address.results.title")}
                label={t("address.results.label")}
                checks={result.checks.map((c) => ({
                  detail: c.detail,
                  key: c.name,
                  level: c.level,
                  note: c.note,
                }))}
              />
            ) : null}
            {unsupported ? <Callout tone="soft">{t("address.unsupported")}</Callout> : null}
          </div>
        </>
      )}

      <StepFooter
        nav={nav}
        primary={
          !skill ? null : checked && !failed ? (
            <Button key="continue" variant="primary" onClick={nav.done}>
              {t("nav.continue")}
            </Button>
          ) : (
            <Button key="check" type="submit" variant="primary" pending={check.isPending} disabled={!value.trim()}>
              {failed ? t("address.checkAgain") : t("address.check")}
            </Button>
          )
        }
      />
    </form>
  );
}
