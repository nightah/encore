import type { ReactNode } from "react";

import { Link, useNavigate } from "@tanstack/react-router";
import { ArrowRight, LogOut } from "lucide-react";
import { Trans, useTranslation } from "react-i18next";

import { useAuthState, useLogout } from "@api/auth";
import { useHealth } from "@api/health";
import { LogoMark } from "@components/Logo";
import { NAV_ITEMS } from "@components/nav";
import { PAGE_WIDTH } from "@components/Page";
import { cn } from "@components/ui/cn";
import { useSetupProgress } from "@lib/setupProgress";

export function AppShell({ children }: { children: ReactNode }) {
  // Also re-renders the nav when the language changes, as its labels are read from NAV_ITEMS.
  const { t } = useTranslation("common");
  return (
    <div className="flex min-h-dvh flex-col min-[800px]:flex-row">
      <a
        href="#main"
        className="sr-only z-50 rounded-md bg-surface px-3 py-2 text-ink focus:not-sr-only focus:fixed focus:top-2 focus:left-2"
      >
        {t("shell.skipToContent")}
      </a>

      <aside className="shrink-0 bg-rail text-rail-ink min-[800px]:sticky min-[800px]:top-0 min-[800px]:flex min-[800px]:h-dvh min-[800px]:w-58 min-[800px]:flex-col">
        <div className="flex items-center gap-2.5 px-4 pt-3 pb-2 min-[800px]:px-6.5 min-[800px]:pt-6.5 min-[800px]:pb-5.5">
          <LogoMark className="size-8 rounded-lg" />
          <span className="font-bold font-display text-[22px] text-white tracking-tight">Encore</span>
        </div>

        <nav
          aria-label={t("nav.label")}
          className="overflow-x-auto px-2 pb-2 min-[800px]:flex-1 min-[800px]:px-3.5 min-[800px]:pb-4"
        >
          <ul className="flex gap-1 min-[800px]:flex-col">
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

      <main id="main" tabIndex={-1} className="min-w-0 flex-1 focus:outline-none">
        <SetupBanner />
        {children}
      </main>
    </div>
  );
}

function RailFooter() {
  const { data: auth } = useAuthState();
  const { data: health } = useHealth();
  const logout = useLogout();
  const navigate = useNavigate();
  const { t } = useTranslation("common");
  return (
    <div className="hidden flex-col gap-1 border-[#2a3542] border-t px-6 pt-4 pb-5 text-[#8e9aa8] text-[12.5px] min-[800px]:flex">
      {auth?.user ? (
        <span className="text-[#c3ccd6]">
          {/* data-sensitive: screenshots show "admin" instead of the real name. */}
          <Trans
            t={t}
            i18nKey="shell.signedInAs"
            values={{ name: auth.user.name }}
            components={{ name: <span data-sensitive="admin" /> }}
          />
        </span>
      ) : null}
      {health?.version ? (
        // Release builds report a semantic version; development builds report "dev".
        <span className="font-mono">{/^\d/.test(health.version) ? `v${health.version}` : health.version}</span>
      ) : null}
      {auth?.mode !== "header" ? (
        <button
          type="button"
          onClick={() => logout.mutate(undefined, { onSuccess: () => navigate({ to: "/login" }) })}
          className="-ml-2 mt-1 inline-flex min-h-11 w-fit items-center gap-2 rounded-lg px-2 text-[#c3ccd6] outline-none hover:bg-[#212a35] hover:text-white focus-visible:outline-2 focus-visible:outline-rail-accent"
        >
          <LogOut aria-hidden="true" className="size-4" />
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
          <ArrowRight aria-hidden="true" className="size-4" />
        </Link>
      </div>
    </div>
  );
}
