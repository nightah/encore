import type { ReactNode } from "react";

import { Link } from "@tanstack/react-router";
import { ArrowRight, LogOut } from "lucide-react";
import { useTranslation } from "react-i18next";

import { SignedInAs, useAccount } from "@components/Account";
import { LogoMark } from "@components/Logo";
import { MenuBar } from "@components/MenuBar";
import { NAV_ITEMS } from "@components/nav";
import { PAGE_WIDTH } from "@components/Page";
import { cn } from "@components/ui/cn";
import { useSetupProgress } from "@lib/setupProgress";

export function AppShell({ children }: { children: ReactNode }) {
  // Also re-renders the nav when the language changes, as its labels are read from NAV_ITEMS.
  const { t } = useTranslation("common");
  return (
    // viewport-fit=cover lets the page reach under a landscape iPhone's notch; the insets keep content clear of it.
    <div className="flex min-h-dvh flex-row pr-[env(safe-area-inset-right)] pl-[env(safe-area-inset-left)]">
      <a
        href="#main"
        className="sr-only z-50 rounded-md bg-surface px-3 py-2 text-ink focus:not-sr-only focus:fixed focus:start-2 focus:top-2"
      >
        {t("shell.skipToContent")}
      </a>

      {/* Wide screens: the rail. Phones get the menu bar at the bottom instead. */}
      <aside className="sticky top-0 hidden h-dvh w-58 shrink-0 flex-col bg-rail text-rail-ink min-[800px]:flex">
        <div className="flex items-center gap-2.5 px-6.5 pt-6.5 pb-5.5">
          <LogoMark className="size-8 rounded-lg" />
          <span className="font-bold font-display text-[22px] text-white tracking-tight">Encore</span>
        </div>

        <nav aria-label={t("nav.label")} className="flex-1 overflow-y-auto px-3.5 pb-4">
          <ul className="flex flex-col gap-1">
            {NAV_ITEMS.map(({ to, label, icon: Icon }) => (
              <li key={to} className="shrink-0">
                <Link
                  to={to}
                  activeOptions={{ exact: to === "/" }}
                  className="flex min-h-11 items-center gap-2.5 whitespace-nowrap rounded-lg px-3 py-2 text-[#c3ccd6] text-sm no-underline outline-none transition-colors hover:bg-[#212a35] hover:text-white focus-visible:outline-2 focus-visible:outline-rail-accent focus-visible:-outline-offset-2 data-[status=active]:bg-[#2a3542] data-[status=active]:text-white data-[status=active]:shadow-[inset_3px_0_0_var(--color-rail-accent)]"
                >
                  <Icon aria-hidden="true" className="size-4.5 shrink-0" />
                  <span>{label}</span>
                </Link>
              </li>
            ))}
          </ul>
        </nav>

        <RailFooter />
      </aside>

      {/* Below 800px, room for the menu bar and the home indicator under it. */}
      <main
        id="main"
        tabIndex={-1}
        className="min-w-0 flex-1 pb-[calc(4.5rem+env(safe-area-inset-bottom))] focus:outline-none min-[800px]:pb-0"
      >
        <SetupBanner />
        {children}
      </main>

      <MenuBar />
    </div>
  );
}

function RailFooter() {
  const { name, version, canSignOut, signOut } = useAccount();
  const { t } = useTranslation("common");
  return (
    <div className="flex flex-col gap-1 border-[#2a3542] border-t px-6 pt-4 pb-5 text-[#8e9aa8] text-[12.5px]">
      {name ? (
        <span className="text-[#c3ccd6]">
          <SignedInAs name={name} />
        </span>
      ) : null}
      {version ? <span className="font-mono">{version}</span> : null}
      {canSignOut ? (
        <button
          type="button"
          onClick={signOut}
          className="-ms-2 mt-1 inline-flex min-h-11 w-fit items-center gap-2 rounded-lg px-2 text-[#c3ccd6] outline-none hover:bg-[#212a35] hover:text-white focus-visible:outline-2 focus-visible:outline-rail-accent"
        >
          <LogOut aria-hidden="true" className="size-4 rtl:-scale-x-100" />
          {t("shell.signOut")}
        </button>
      ) : null}
    </div>
  );
}

/** Shown while the setup wizard was left unfinished. */
function SetupBanner() {
  const progress = useSetupProgress();
  const { t } = useTranslation("common");
  if (!progress) return null;
  return (
    <div className="border-line border-b bg-warn-soft">
      <div
        className={cn(
          "mx-auto flex flex-wrap items-center justify-between gap-3 px-4 py-2.5 sm:px-10",
          PAGE_WIDTH.wide,
        )}
      >
        <span>{t("setupBanner.message", { step: progress.step })}</span>
        <Link
          to="/setup"
          className="inline-flex min-h-11 items-center gap-1.5 font-medium text-link underline-offset-2 hover:underline"
        >
          {t("setupBanner.continue")}
          <ArrowRight aria-hidden="true" className="size-4 rtl:-scale-x-100" />
        </Link>
      </div>
    </div>
  );
}
