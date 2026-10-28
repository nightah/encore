import { type FormEvent, useState } from "react";

import { RefreshCw } from "lucide-react";
import { useTranslation } from "react-i18next";

import { ApiError, errorMessage } from "@api/client";
import type { Instance, TestResult as InstanceTest } from "@api/generated/model";
import { useCreateInstance, useDiscovered, useInstances, useTestInstance } from "@api/kodi";
import { useSaveSkillContent, useSkill } from "@api/skill";
import { CHECK_LABELS, CHECK_ORDER, checkLabel } from "@components/status";
import { Button } from "@components/ui/Button";
import { Checkbox, Field, FieldGrid, Input, RadioCards } from "@components/ui/Field";
import { Callout } from "@components/ui/States";
import { Searching } from "@features/kodi/Searching";
import { CheckList, StepFooter, StepHeading, StepPending, type StepProps } from "@features/setup/shared";
import { CONTENT, toggleContent } from "@features/skill/content";
import { formatDuration } from "@lib/format";

const MANUAL = "manual";

/** A typed-in address. The server leaves live updates off without a WebSocket URL, so assume Kodi's fixed WebSocket port (9090) on the same host; it can be changed on the Kodi page. */
function manualBody(name: string, url: string) {
  const body = { name: name.trim(), url: url.trim() };
  try {
    const { hostname } = new URL(body.url);
    return hostname ? { ...body, websocket_url: `ws://${hostname}:9090/jsonrpc` } : body;
  } catch {
    return body;
  }
}

export function KodiStep({ nav }: StepProps) {
  const { t } = useTranslation("setup");
  const discovery = useDiscovered();
  const discovered = discovery.data ?? [];
  // mDNS takes a few seconds; show the search while nothing has been found yet, including "Search again".
  const searching = discovery.isFetching && discovered.length === 0;
  const { data: instances = [] } = useInstances();
  const create = useCreateInstance();
  const test = useTestInstance();
  const skillQuery = useSkill();
  const skill = skillQuery.data;
  const saveContent = useSaveSkillContent();
  const content = skill?.content ?? [];

  const [choice, setChoice] = useState<string | null>(null);
  const [manual, setManual] = useState({ name: "", url: "" });
  const [credentials, setCredentials] = useState({ password: "", username: "kodi" });
  // Set as soon as "Add and test" is pressed, so the checks show while Kodi is added and tested; instance arrives once it's added.
  const [added, setAdded] = useState<{
    name: string;
    instance?: Instance;
    test?: InstanceTest;
  } | null>(null);

  const selected = choice ?? (discovered[0] ? discovered[0].host : MANUAL);
  const fieldErrors = create.error instanceof ApiError ? create.error.fieldErrors : {};

  // Back to the form for the next Kodi; the credentials stay, as instances often share them, and discovery leaves out the one just added.
  const addAnother = () => {
    setAdded(null);
    setChoice(null);
    setManual({ name: "", url: "" });
    create.reset();
    test.reset();
    void discovery.refetch();
  };

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    const found = discovered.find((d) => d.host === selected);
    const body = found
      ? {
          name: found.name,
          url: `http://${found.host}:${found.http_port}/jsonrpc`,
          ...(found.ws_port ? { websocket_url: `ws://${found.host}:${found.ws_port}/jsonrpc` } : {}),
        }
      : manualBody(manual.name, manual.url);
    setAdded({ name: body.name || "Kodi" });
    let instance: Instance;
    try {
      instance = await create.mutateAsync({
        ...body,
        ...(credentials.username ? { username: credentials.username } : {}),
        ...(credentials.password ? { password: credentials.password } : {}),
      });
    } catch {
      // Back to the form, where the errors render inline.
      setAdded(null);
      return;
    }
    setAdded({ instance, name: instance.name });
    try {
      setAdded({ instance, name: instance.name, test: await test.mutateAsync(instance.id) });
    } catch {
      // The test error renders below the checks.
    }
  };

  if (added) {
    const allOk = added.test?.checks.every((c) => c.ok);
    const running = !added.test && !test.error;
    return (
      <div className="flex flex-col gap-4.5">
        <StepHeading>{t("kodi.heading")}</StepHeading>
        {added.instance ? (
          <Callout tone="ok">{t("kodi.added", { name: added.instance.name })}</Callout>
        ) : (
          <Callout tone="soft">{t("kodi.adding", { name: added.name })}</Callout>
        )}
        <div aria-live="polite">
          <CheckList
            label={t("kodi.results")}
            checks={
              added.test
                ? added.test.checks.map((c) => ({
                    detail: checkLabel(c),
                    key: c.name,
                    level: c.ok ? "ok" : "fail",
                    meta: c.ok ? formatDuration(c.ms) : undefined,
                  }))
                : CHECK_ORDER.map((key) => ({
                    detail: CHECK_LABELS[key],
                    key,
                    level: running ? "pending" : "fail",
                  }))
            }
          />
          {test.error ? (
            <p role="alert" className="mt-2 text-[13px] text-bad">
              {t("kodi.testFailed", { error: errorMessage(test.error) })}
            </p>
          ) : null}
          {added.test && !allOk ? <p className="mt-2 text-[13px] text-muted">{t("kodi.fixLater")}</p> : null}
        </div>
        <StepFooter
          nav={nav}
          primary={
            <>
              <Button onClick={addAnother} disabled={running}>
                {t("kodi.addAnother")}
              </Button>
              <Button variant="primary" onClick={nav.done} disabled={running}>
                {t("nav.continue")}
              </Button>
            </>
          }
        />
      </div>
    );
  }

  return (
    <form onSubmit={submit} noValidate className="flex flex-col gap-4.5">
      <StepHeading
        intro={
          <>
            {searching ? t("kodi.searching") : discovered.length ? t("kodi.found") : t("kodi.notFound")}
            <br />
            {t("kodi.remoteControl")}
          </>
        }
      >
        {t("kodi.heading")}
      </StepHeading>

      {instances.length ? (
        <Callout tone="soft">{t("kodi.alreadyAdded", { names: instances.map((i) => i.name).join(", ") })}</Callout>
      ) : null}

      <fieldset className="flex min-w-0 flex-col gap-1.5">
        <legend className="mb-1.5 font-medium">{t("content.legend")}</legend>
        {/* What's ticked comes from the skill, so the boxes wait for it rather than showing nothing ticked. */}
        {skill ? (
          <div className="flex flex-col gap-2.5">
            {CONTENT.map((c) => (
              <Checkbox
                key={c.value}
                label={c.label}
                hint={c.hint}
                checked={content.includes(c.value)}
                disabled={content.length === 1 && content[0] === c.value}
                onCheckedChange={(checked) => saveContent.mutate(toggleContent(content, c.value, checked))}
              />
            ))}
          </div>
        ) : (
          <StepPending error={skillQuery.error} onRetry={() => skillQuery.refetch()} />
        )}
        <span className="text-[13px] text-muted">{t("kodi.contentNote")}</span>
        {saveContent.error ? (
          <span role="alert" className="text-[13px] text-bad">
            {errorMessage(saveContent.error)}
          </span>
        ) : null}
      </fieldset>

      {searching ? (
        <Searching />
      ) : discovered.length === 0 ? (
        <div className="flex">
          <Button icon={<RefreshCw aria-hidden className="size-4" />} onClick={() => discovery.refetch()}>
            {t("kodi.searchAgain")}
          </Button>
        </div>
      ) : null}

      <RadioCards
        legend={t("kodi.instance")}
        value={selected}
        onValueChange={setChoice}
        columns="grid-cols-1"
        options={[
          ...discovered.map((d) => ({
            hint: (
              <span className="font-mono">
                {[d.host, d.version ? `Kodi ${d.version}` : "", d.ws_port ? `WebSocket :${d.ws_port}` : ""]
                  .filter(Boolean)
                  .join(" · ")}
              </span>
            ),
            label: d.name,
            value: d.host,
          })),
          {
            hint: t("kodi.manual.hint"),
            label: t("kodi.manual.label"),
            value: MANUAL,
          },
        ]}
      />

      {selected === MANUAL ? (
        <FieldGrid className="grid-cols-[repeat(auto-fit,minmax(200px,1fr))] gap-3.5">
          <Field label={t("kodi.name.label")} hint={t("kodi.name.hint")} error={fieldErrors.name}>
            <Input value={manual.name} onChange={(e) => setManual({ ...manual, name: e.target.value })} />
          </Field>
          <Field label={t("kodi.rpc")} hint="http://192.168.1.20:8080/jsonrpc" error={fieldErrors.url}>
            <Input
              mono
              inputMode="url"
              value={manual.url}
              onChange={(e) => setManual({ ...manual, url: e.target.value })}
            />
          </Field>
        </FieldGrid>
      ) : null}

      <FieldGrid className="grid-cols-[repeat(auto-fit,minmax(200px,1fr))] gap-3.5">
        <Field label={t("kodi.username")} error={fieldErrors.username}>
          <Input
            autoComplete="off"
            value={credentials.username}
            onChange={(e) => setCredentials({ ...credentials, username: e.target.value })}
          />
        </Field>
        <Field label={t("kodi.password.label")} hint={t("kodi.password.hint")} error={fieldErrors.password}>
          <Input
            type="password"
            autoComplete="new-password"
            value={credentials.password}
            onChange={(e) => setCredentials({ ...credentials, password: e.target.value })}
          />
        </Field>
      </FieldGrid>

      {create.error && Object.keys(fieldErrors).length === 0 ? (
        <p role="alert" className="rounded-[10px] bg-bad-bg px-3.5 py-2.5 text-bad">
          {errorMessage(create.error)}
        </p>
      ) : null}

      <StepFooter
        nav={nav}
        primary={
          <Button type="submit" variant="primary" pending={create.isPending}>
            {t("kodi.submit")}
          </Button>
        }
      />
    </form>
  );
}
