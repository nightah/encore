import { useState } from "react";

import { useQuery } from "@tanstack/react-query";
import { LoaderCircle } from "lucide-react";
import { useTranslation } from "react-i18next";

import { ApiError } from "@api/client";
import { useUpdateDevice } from "@api/devices";
import { useServerEvent } from "@api/events";
import type { RequestSummary } from "@api/generated/model";
import { listRequests } from "@api/requests";
import { useInvocationName, useSkill } from "@api/skill";
import { Button } from "@components/ui/Button";
import { Dot } from "@components/ui/Card";
import { Field, Input } from "@components/ui/Field";
import { StepFooter, StepHeading, StepPending, type StepProps } from "@features/setup/shared";
import { formatClock } from "@lib/format";
import { getSetupProgress, saveSetupProgress } from "@lib/setupProgress";

/** How far back a request still counts as "the first one" when this step opens (e.g. it arrived while the deploy finished). */
const GRACE_MS = 10 * 60_000;

export function HelloStep({ nav }: StepProps) {
  const { t } = useTranslation("setup");
  const [since] = useState(() => {
    const saved = getSetupProgress();
    if (saved?.waitingSince) return saved.waitingSince;
    const now = new Date(Date.now() - GRACE_MS).toISOString();
    if (saved) saveSetupProgress({ ...saved, waitingSince: now });
    return now;
  });
  const [live, setLive] = useState<RequestSummary | null>(null);

  // A request may already have arrived before this step opened; check the newest one once.
  const latest = useQuery({
    // From an Echo: the deploy's smoke test talks to Encore through Amazon's simulator just before this step.
    queryFn: ({ signal }) => listRequests({ echo: true, limit: 1 }, { signal }),
    queryKey: ["setup", "latest-request"],
    staleTime: Number.POSITIVE_INFINITY,
  });
  const earlier = latest.data?.items[0];
  const received = live ?? (earlier && new Date(earlier.time).getTime() >= new Date(since).getTime() ? earlier : null);

  useServerEvent("request", (request) => {
    if (!request.simulated) setLive((current) => current ?? request);
  });

  const update = useUpdateDevice(t("hello.named"));
  const [name, setName] = useState("");
  const skillQuery = useSkill();
  const invocation = useInvocationName();

  const finish = async () => {
    if (received?.device_id && name.trim()) {
      try {
        await update.mutateAsync({ body: { name: name.trim() }, id: received.device_id });
      } catch {
        return;
      }
    }
    nav.done();
  };

  const nameError =
    update.error instanceof ApiError ? (update.error.fieldErrors.name ?? update.error.message) : undefined;

  return (
    <form
      onSubmit={(event) => {
        event.preventDefault();
        void finish();
      }}
      noValidate
      className="flex flex-col gap-4.5"
    >
      <StepHeading intro={t("hello.intro")}>{t("hello.heading")}</StepHeading>
      {/* The phrase names the skill as chosen, so it waits for the skill rather than guessing. */}
      {invocation ? (
        <p className="font-display font-semibold text-[26px] leading-tight sm:text-[30px]">
          {t("hello.phrase", { invocation })}
        </p>
      ) : (
        <StepPending error={skillQuery.error} onRetry={() => skillQuery.refetch()} />
      )}

      <div aria-live="polite">
        {received ? (
          <div className="flex flex-col gap-3 rounded-[10px] bg-ok-bg px-4 py-3.5 text-ok">
            <span className="flex items-center gap-3">
              <Dot tone="ok" className="size-2.5" />
              <span>
                {received.device_name
                  ? t("hello.received", { device: received.device_name, time: formatClock(received.time) })
                  : t("hello.receivedNew", { time: formatClock(received.time) })}
              </span>
            </span>
          </div>
        ) : (
          <p role="status" className="flex items-center gap-3 rounded-[10px] bg-soft px-4 py-3.5 text-muted">
            <LoaderCircle aria-hidden="true" className="size-4 animate-spin" />
            {t("hello.waiting")}
          </p>
        )}
      </div>

      {received && !received.device_name && received.device_id ? (
        <Field label={t("hello.echoName.label")} hint={t("hello.echoName.hint")} error={nameError}>
          <Input value={name} onChange={(e) => setName(e.target.value)} autoComplete="off" />
        </Field>
      ) : null}

      {invocation ? <p className="text-[13px] text-muted">{t("hello.alexaPlus", { invocation })}</p> : null}

      <StepFooter
        nav={{ ...nav, canSkip: false }}
        primary={
          <Button type="submit" variant="primary" pending={update.isPending}>
            {t("hello.finish")}
          </Button>
        }
      />
    </form>
  );
}
