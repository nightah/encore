import { useCallback, useEffect, useRef, useState } from "react";

import { useNavigate } from "@tanstack/react-router";
import { Check } from "lucide-react";
import { Trans, useTranslation } from "react-i18next";

import { useAuthState } from "@api/auth";
import { AuthFrame } from "@components/AuthFrame";
import { cn, focusRing } from "@components/ui/cn";
import { STEPS, type StepNav } from "@features/setup/shared";
import { AddressStep } from "@features/setup/steps/AddressStep";
import { AdminStep } from "@features/setup/steps/AdminStep";
import { AmazonStep } from "@features/setup/steps/AmazonStep";
import { DeployStep } from "@features/setup/steps/DeployStep";
import { HelloStep } from "@features/setup/steps/HelloStep";
import { KodiStep } from "@features/setup/steps/KodiStep";
import { clearSetupProgress, getSetupProgress, saveSetupProgress } from "@lib/setupProgress";

const STEP_COMPONENTS = [AdminStep, AddressStep, KodiStep, AmazonStep, DeployStep, HelloStep];

/** The six-step first-run wizard. Every step after the admin account can be skipped; progress is saved per browser so you can leave and resume (the shell shows a "Continue setup" banner). */
export function SetupWizard() {
  const { t } = useTranslation("setup");
  const { data: auth } = useAuthState();
  const navigate = useNavigate();
  const setupRequired = auth?.setup_required ?? true;

  const [state, setState] = useState(() => {
    const saved = getSetupProgress();
    if (setupRequired) return { done: [] as number[], skipped: [] as number[], step: 1 };
    return {
      done: saved?.done ?? [1],
      skipped: saved?.skipped ?? [],
      step: saved?.step ?? 2,
    };
  });

  const headingFocusRef = useRef(false);
  const update = useCallback((next: typeof state) => {
    setState(next);
    saveSetupProgress({ ...getSetupProgress(), ...next });
    headingFocusRef.current = true;
  }, []);

  useEffect(() => {
    if (!headingFocusRef.current) return;
    headingFocusRef.current = false;
    document.querySelector<HTMLElement>("[data-step-heading]")?.focus();
  });

  const { step, done, skipped } = state;
  const finish = useCallback(() => {
    clearSetupProgress();
    void navigate({ to: "/" });
  }, [navigate]);

  const nav: StepNav = {
    back: () => update({ ...state, step: Math.max(setupRequired ? 1 : 2, step - 1) }),
    canBack: step > (setupRequired ? 1 : 2),
    // The admin account can't be skipped: everything else needs a signed-in admin.
    canSkip: step > 1,
    done: () => {
      const nextDone = [...new Set([...done, step])];
      const nextSkipped = skipped.filter((s) => s !== step);
      if (step === STEPS.length) {
        finish();
        return;
      }
      update({ done: nextDone, skipped: nextSkipped, step: step + 1 });
    },
    goTo: (target) => update({ ...state, step: target }),
    skip: () => {
      if (step === STEPS.length) {
        finish();
        return;
      }
      update({ done, skipped: [...new Set([...skipped, step])], step: step + 1 });
    },
  };

  const Step = STEP_COMPONENTS[step - 1] ?? AdminStep;
  const reached = Math.max(step, ...done, ...skipped);
  const stepKey = STEPS[step - 1];
  const title = stepKey ? t(`steps.${stepKey}.title`) : "";

  return (
    <AuthFrame subtitle={t("wizard.subtitle")}>
      <div className="flex w-full max-w-[1040px] flex-wrap overflow-hidden rounded-2xl bg-surface">
        <ol
          aria-label={t("wizard.steps")}
          className="flex flex-[1_1_260px] flex-col gap-1.5 bg-soft px-4 py-5 sm:px-6 sm:py-7"
        >
          {STEPS.map((key, index) => {
            const n = index + 1;
            const isDone = done.includes(n);
            const isSkipped = skipped.includes(n) && !isDone;
            const current = n === step;
            const reachable = n <= reached && n !== step && !(setupRequired && n > 1) && n >= (setupRequired ? 1 : 2);
            const content = (
              <>
                <span
                  aria-hidden="true"
                  className={cn(
                    "flex size-7 flex-none items-center justify-center rounded-full font-semibold text-[13px]",
                    isDone ? "bg-[#166534] text-white" : current ? "bg-primary text-primary-ink" : "bg-line text-ink",
                  )}
                >
                  {isDone ? <Check className="size-4" strokeWidth={3} /> : n}
                </span>
                <span className="flex flex-col text-left">
                  <span className="font-semibold">{t(`steps.${key}.title`)}</span>
                  <span className="text-[13px] text-muted">
                    {isSkipped ? t("wizard.skipped") : t(`steps.${key}.sub`)}
                  </span>
                </span>
                <span className="sr-only">
                  {current
                    ? t("wizard.state.current")
                    : isDone
                      ? t("wizard.state.done")
                      : isSkipped
                        ? t("wizard.state.skipped")
                        : ""}
                </span>
              </>
            );
            return (
              <li key={key} aria-current={current ? "step" : undefined}>
                {reachable ? (
                  <button
                    type="button"
                    onClick={() => nav.goTo(n)}
                    className={cn(
                      "flex min-h-11 w-full items-center gap-3 rounded-[10px] px-3 py-2.5 hover:bg-surface/60",
                      focusRing,
                    )}
                  >
                    {content}
                  </button>
                ) : (
                  <div
                    className={cn(
                      "flex items-center gap-3 rounded-[10px] px-3 py-2.5",
                      current && "bg-surface shadow-[0_0_0_1px_var(--color-line)]",
                    )}
                  >
                    {content}
                  </div>
                )}
              </li>
            );
          })}
        </ol>

        <section
          aria-label={t("wizard.label", { step, title })}
          className="flex min-w-0 flex-[999_1_520px] flex-col gap-5.5 px-5 py-7 sm:px-10 sm:py-9"
        >
          <p aria-live="polite" className="font-mono text-[13px] text-link">
            <Trans
              t={t}
              i18nKey="wizard.progress"
              values={{ step, title, total: STEPS.length }}
              components={{ sr: <span className="sr-only" /> }}
            />
          </p>
          <Step key={step} nav={nav} />
        </section>
      </div>
    </AuthFrame>
  );
}
