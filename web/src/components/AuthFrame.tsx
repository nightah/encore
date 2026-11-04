import type { ReactNode } from "react";

import { LogoMark } from "@components/Logo";
import { ThemeToggle } from "@components/ThemeToggle";

/** Navy full-page frame for the login page and the setup wizard. */
export function AuthFrame({ subtitle, children }: { subtitle: string; children: ReactNode }) {
  return (
    <div className="flex min-h-dvh flex-col items-center gap-7 bg-rail px-4 py-10 sm:py-12">
      <div className="flex flex-wrap items-center justify-center gap-2.5">
        <LogoMark size={56} className="size-14" />
        <span className="font-bold font-display text-2xl text-white">Encore</span>
        <span className="ms-1.5 text-[#8e9aa8]">{subtitle}</span>
        <span className="ms-3">
          <ThemeToggle onDark />
        </span>
      </div>
      {children}
    </div>
  );
}
