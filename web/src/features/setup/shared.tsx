import type { ReactNode } from "react";

import { Info, LoaderCircle } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@components/ui/Button";
import { Dot } from "@components/ui/Card";
import { ErrorState, LoadingBlock } from "@components/ui/States";

/** The wizard's steps in order; each names its title and subtitle under steps.* in the setup namespace. */
export const STEPS = ["admin", "address", "kodi", "amazon", "deploy", "hello"] as const;

export interface StepNav {
  /** Go to the next step, marking this one done. */
  done: () => void;
  back: () => void;
  skip: () => void;
  goTo: (step: number) => void;
  canBack: boolean;
  canSkip: boolean;
}

export interface StepProps {
  nav: StepNav;
}

/** Back at the start; "Skip for now" and the step's primary action at the end (the right, or the left in a right-to-left language). */
export function StepFooter({ nav, primary }: { nav: StepNav; primary: ReactNode }) {
  const { t } = useTranslation("setup");
  return (
    <div className="flex flex-wrap justify-between gap-3 border-line-soft border-t pt-4">
      <Button onClick={nav.back} disabled={!nav.canBack}>
        {t("nav.back")}
      </Button>
      <div className="flex flex-wrap gap-2.5">
        {nav.canSkip ? <Button onClick={nav.skip}>{t("nav.skip")}</Button> : null}
        {primary}
      </div>
    </div>
  );
}

/** What a step shows while the query it starts from loads: the loading state, or the error with a retry. */
export function StepPending({ error, onRetry }: { error: unknown; onRetry: () => void }) {
  return error ? <ErrorState error={error} onRetry={onRetry} /> : <LoadingBlock />;
}

export function StepHeading({ children, intro }: { children: ReactNode; intro?: ReactNode }) {
  return (
    <>
      <h1
        tabIndex={-1}
        data-step-heading
        className="font-display font-semibold text-[28px] leading-[1.15] outline-none"
      >
        {children}
      </h1>
      {intro ? <p className="text-muted">{intro}</p> : null}
    </>
  );
}

/** "pending" is a check still running. */
export type CheckLevel = "ok" | "warn" | "fail" | "pending";

const DOT_TONES = { fail: "bad", ok: "ok", warn: "warn" } as const;

/** A bordered list of pass/warn/fail checks (reachability, Kodi connection test). */
export function CheckList({
  title,
  label,
  checks,
}: {
  title?: string;
  label: string;
  checks: Array<{
    key: string;
    level: CheckLevel;
    detail: ReactNode;
    /** Extra information shown beneath the result, e.g. about a check that passed. */
    note?: ReactNode;
    meta?: ReactNode;
  }>;
}) {
  const { t } = useTranslation("setup");
  return (
    <div className="flex flex-col gap-2 rounded-[10px] border border-line px-4 py-3.5">
      {title ? <span className="font-semibold">{title}</span> : null}
      <ul aria-label={label} className="flex flex-col gap-2">
        {checks.map((check) => (
          <li key={check.key} className="flex items-start gap-2.5">
            {check.level === "pending" ? (
              <LoaderCircle aria-hidden className="mt-1 size-3.5 shrink-0 animate-spin text-muted" />
            ) : (
              <Dot tone={DOT_TONES[check.level]} className="mt-1.5" />
            )}
            <span className="sr-only">{t(`check.${check.level}`)}</span>
            <span className="flex min-w-0 flex-1 flex-col gap-0.5">
              <span>{check.detail}</span>
              {check.note ? (
                <span className="flex items-start gap-1.5 text-[13px] text-muted">
                  <Info aria-hidden className="mt-0.5 size-3.5 shrink-0" />
                  <span className="whitespace-pre-line">{check.note}</span>
                </span>
              ) : null}
            </span>
            {check.meta ? <span className="font-mono text-[13px] text-muted">{check.meta}</span> : null}
          </li>
        ))}
      </ul>
    </div>
  );
}
