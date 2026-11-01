import type { ReactNode } from "react";

import { ThemeToggle } from "@components/ThemeToggle";
import { cn } from "@components/ui/cn";

/** How wide a page's content may grow: pages of tables and cards use large screens, pages of forms and prose stay at a readable width. */
export const PAGE_WIDTH = {
  text: "max-w-[1200px]",
  wide: "max-w-[1200px] min-[1800px]:max-w-[1600px] min-[2400px]:max-w-[2160px] min-[3200px]:max-w-[2880px]",
} as const;

interface PageProps {
  title: string;
  description?: ReactNode;
  /** Header buttons, shown after the theme toggle. */
  actions?: ReactNode;
  children?: ReactNode;
  width?: keyof typeof PAGE_WIDTH;
}

/** Page frame inside the shell: h1, description, theme toggle and actions, then content. */
export function Page({ title, description, actions, children, width = "wide" }: PageProps) {
  return (
    <div className={cn("mx-auto flex w-full flex-col gap-6 px-4 py-6 sm:px-10 sm:pt-8 sm:pb-12", PAGE_WIDTH[width])}>
      <header className="flex flex-wrap items-end justify-between gap-4">
        <div className="flex min-w-0 flex-[1_1_28rem] flex-col gap-1">
          <h1 className="font-bold font-display text-[28px] text-ink leading-[1.1] sm:text-[32px]">{title}</h1>
          {description ? <p className="text-muted">{description}</p> : null}
        </div>
        <div className="flex flex-wrap items-center gap-2.5">
          {/* Phones switch the theme under More in the menu bar. */}
          <ThemeToggle className="max-[800px]:hidden" />
          {actions}
        </div>
      </header>
      {children}
    </div>
  );
}
