import type { ComponentProps, ReactNode } from "react";

import { useTranslation } from "react-i18next";

import { cn } from "@components/ui/cn";

export type Tone = "ok" | "info" | "neutral" | "warn" | "bad" | "ask" | "outline" | "link";

const tones: Record<Tone, string> = {
  ask: "bg-ask-bg text-ask",
  bad: "bg-bad-bg text-bad",
  info: "bg-info-bg text-info",
  link: "border border-line text-link",
  neutral: "bg-neutral-bg text-neutral",
  ok: "bg-ok-bg text-ok",
  outline: "border border-line text-muted",
  warn: "bg-warn-bg text-warn",
};

/** Rounded status pill. */
export function Chip({ tone = "neutral", className, ...props }: ComponentProps<"span"> & { tone?: Tone }) {
  return (
    <span
      className={cn(
        "inline-flex items-center whitespace-nowrap rounded-full px-2.5 py-0.5 font-medium text-[12.5px]",
        tones[tone],
        className,
      )}
      {...props}
    />
  );
}

/** The "Managed by config" badge shown next to read-only, config-controlled fields. */
export function ManagedBadge() {
  const { t } = useTranslation("common");
  return (
    <span className="whitespace-nowrap rounded-full bg-neutral-bg px-2 py-px font-medium text-[11.5px] text-neutral">
      {t("field.managed")}
    </span>
  );
}

/** A toggle chip with `aria-pressed` (locale chips, example pills). */
export function ToggleChip({
  pressed,
  className,
  children,
  ...props
}: Omit<ComponentProps<"button">, "type"> & { pressed: boolean; children: ReactNode }) {
  return (
    <button
      type="button"
      aria-pressed={pressed}
      className={cn(
        // 36px visually; the pseudo-element extends the hit area to 44px.
        "relative inline-flex min-h-9 cursor-pointer after:absolute after:inset-x-0 after:-inset-y-1 after:content-[''] items-center rounded-full border px-3 font-medium text-[13px] outline-none transition-colors focus-visible:outline-2 focus-visible:outline-focus focus-visible:outline-offset-2 disabled:cursor-not-allowed disabled:opacity-60",
        pressed ? "border-transparent bg-ok-bg text-ok" : "border-line bg-transparent text-muted hover:text-ink",
        className,
      )}
      {...props}
    >
      {children}
    </button>
  );
}
