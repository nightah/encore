import { type ComponentProps, type ReactNode, useState } from "react";

import { CircleAlert, Copy, CopyCheck } from "lucide-react";
import { useTranslation } from "react-i18next";

import { errorMessage } from "@api/client";
import { Button } from "@components/ui/Button";
import { cn, focusRing } from "@components/ui/cn";

export function Skeleton({ className }: { className?: string }) {
  return <div aria-hidden="true" className={cn("animate-pulse rounded-lg bg-soft", className)} />;
}

/** Placeholder while a page's first query loads. */
export function LoadingBlock({ label, className }: { label?: string; className?: string }) {
  const { t } = useTranslation("common");
  return (
    <div role="status" aria-live="polite" className={cn("flex flex-col gap-3", className)}>
      <span className="sr-only">{label ?? t("loading")}</span>
      <Skeleton className="h-24" />
      <Skeleton className="h-40" />
    </div>
  );
}

export function EmptyState({
  title,
  children,
  action,
  className,
}: {
  title: string;
  children?: ReactNode;
  action?: ReactNode;
  className?: string;
}) {
  return (
    <div
      className={cn(
        "flex flex-col items-center gap-2 rounded-xl border border-line border-dashed px-6 py-8 text-center",
        className,
      )}
    >
      <p className="font-display font-semibold text-ink">{title}</p>
      {children ? <div className="max-w-prose text-muted text-sm">{children}</div> : null}
      {action ? <div className="mt-2">{action}</div> : null}
    </div>
  );
}

export function ErrorState({ error, onRetry }: { error: unknown; onRetry?: () => void }) {
  const { t } = useTranslation("common");
  return (
    <div role="alert" className="flex flex-wrap items-center gap-3 rounded-xl bg-bad-bg px-4 py-3.5 text-bad">
      <CircleAlert aria-hidden="true" className="size-5 shrink-0" />
      <span className="min-w-0 flex-1">{errorMessage(error)}</span>
      {onRetry ? (
        <Button onClick={onRetry} className="min-h-9">
          {t("actions.tryAgain")}
        </Button>
      ) : null}
    </div>
  );
}

/** A tinted box for a notice inside a card or form. */
export function Callout({
  tone = "warn",
  className,
  children,
  ...props
}: ComponentProps<"div"> & { tone?: "warn" | "soft" | "ok" | "bad" | "warnStrong" }) {
  const tones = {
    bad: "bg-bad-bg text-bad",
    ok: "bg-ok-bg text-ok",
    soft: "bg-soft",
    warn: "bg-warn-soft",
    warnStrong: "bg-warn-bg text-warn",
  };
  return (
    <div className={cn("flex gap-3 rounded-[10px] px-3.5 py-3", tones[tone], className)} {...props}>
      {children}
    </div>
  );
}

/** Copies text to the clipboard and confirms inline. */
export function CopyButton({
  text,
  label,
  speakText = true,
}: {
  text: string;
  label?: string;
  /** Read the text out in the button's name; off for long text such as a code block. */
  speakText?: boolean;
}) {
  const { t } = useTranslation("common");
  const [copied, setCopied] = useState(false);
  const name = label ?? t("actions.copy");
  return (
    <button
      type="button"
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(text);
          setCopied(true);
          setTimeout(() => setCopied(false), 2000);
        } catch {
          setCopied(false);
        }
      }}
      aria-label={copied ? t("actions.copied") : speakText ? t("actions.copyWithText", { label: name, text }) : name}
      className={cn(
        "inline-flex size-11 shrink-0 items-center justify-center rounded-lg text-muted hover:bg-btn-hover hover:text-ink",
        focusRing,
      )}
    >
      {copied ? (
        <CopyCheck aria-hidden="true" className="size-4 text-ok" />
      ) : (
        <Copy aria-hidden="true" className="size-4" />
      )}
    </button>
  );
}
