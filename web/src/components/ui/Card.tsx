import type { ComponentProps, ReactNode } from "react";

import { cn } from "@components/ui/cn";

export function Card({ className, ...props }: ComponentProps<"div">) {
  return <div className={cn("rounded-xl border border-line bg-surface", className)} {...props} />;
}

/** A labelled card section: `<section aria-labelledby>` with an h2. */
export function Section({
  id,
  title,
  className,
  children,
  titleClassName,
  as: Tag = "section",
}: {
  id: string;
  title: ReactNode;
  className?: string;
  titleClassName?: string;
  children?: ReactNode;
  as?: "section" | "aside" | "article";
}) {
  return (
    <Tag
      aria-labelledby={id}
      className={cn("flex min-w-0 flex-col gap-3 rounded-xl border border-line bg-surface p-4 sm:px-5", className)}
    >
      <div className="flex flex-wrap items-center justify-between gap-2.5">
        <h2 id={id} className={cn("font-display font-semibold text-lg", titleClassName)}>
          {title}
        </h2>
      </div>
      {children}
    </Tag>
  );
}

/** Small uppercase mono label above a value. */
export function Label({ className, ...props }: ComponentProps<"span">) {
  return <span className={cn("font-mono text-[11px] text-muted uppercase tracking-[0.06em]", className)} {...props} />;
}

/** Definition list in an auto-fit grid. */
export function KeyValues({ items }: { items: Array<{ label: string; value: ReactNode; mono?: boolean }> }) {
  return (
    <dl className="m-0 grid grid-cols-[repeat(auto-fit,minmax(150px,1fr))] gap-x-4.5 gap-y-3">
      {items.map((item) => (
        <div key={item.label} className="flex min-w-0 flex-col gap-0.5">
          <dt className="font-mono text-[11px] text-muted uppercase tracking-[0.06em]">{item.label}</dt>
          <dd className={cn("m-0 wrap-anywhere text-sm", item.mono && "font-mono text-[13px]")}>{item.value}</dd>
        </div>
      ))}
    </dl>
  );
}

/** Stat tile used on the overview and library pages. */
export function StatTile({
  label,
  value,
  detail,
  dot,
}: {
  label: string;
  value: ReactNode;
  detail?: ReactNode;
  dot?: "ok" | "warn" | "bad" | "muted";
}) {
  return (
    <Card className="flex flex-col gap-1.5 px-4.5 py-4">
      <Label>{label}</Label>
      <span className="flex items-center gap-2 font-semibold text-lg">
        {dot ? <Dot tone={dot} /> : null}
        {value}
      </span>
      {detail ? <span className="text-[13px] text-muted">{detail}</span> : null}
    </Card>
  );
}

const dotTones = {
  accent: "bg-accent",
  bad: "bg-bad",
  muted: "bg-muted",
  ok: "bg-dot-ok",
  warn: "bg-dot-warn",
} as const;

export function Dot({ tone, className }: { tone: keyof typeof dotTones; className?: string }) {
  return (
    <span
      aria-hidden="true"
      className={cn("inline-block size-2.25 shrink-0 rounded-full", dotTones[tone], className)}
    />
  );
}
