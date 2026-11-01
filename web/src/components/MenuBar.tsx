import { useState } from "react";

import { Dialog as BaseDialog } from "@base-ui/react/dialog";
import { Link, useMatchRoute } from "@tanstack/react-router";
import { ArrowDownUp, Ellipsis, LogOut } from "lucide-react";
import { useTranslation } from "react-i18next";

import { SignedInAs, useAccount } from "@components/Account";
import { ArrangeMenu } from "@components/ArrangeMenu";
import { useMenuBar } from "@components/menuBar";
import { NAV_ITEMS, type NavItem } from "@components/nav";
import { cn, focusRing } from "@components/ui/cn";
import { type ThemePreference, useTheme } from "@theme/store";

const THEMES: readonly ThemePreference[] = ["light", "dark", "system"];

/** Phones only: the sections chosen for the bar along the bottom, and More for the rest, the theme and the account. */
export function MenuBar() {
  const { t } = useTranslation("common");
  const bar = useMenuBar();
  const matchRoute = useMatchRoute();
  const [sheet, setSheet] = useState<"more" | "arrange" | null>(null);

  const inBar = bar.map((to) => NAV_ITEMS.find((item) => item.to === to)).filter((item) => item !== undefined);
  const inMore = NAV_ITEMS.filter((item) => !bar.includes(item.to));
  // More is the current section while you're on a page it holds.
  const moreCurrent = inMore.some((item) => matchRoute({ fuzzy: item.to !== "/", to: item.to }));

  return (
    <>
      <nav
        aria-label={t("menu.label")}
        className="fixed inset-x-0 bottom-0 z-30 border-rail-line border-t bg-rail pr-[env(safe-area-inset-right)] pb-[env(safe-area-inset-bottom)] pl-[env(safe-area-inset-left)] min-[800px]:hidden"
      >
        <ul className="flex px-1">
          {inBar.map(({ to, shortLabel, icon: Icon }) => (
            <li key={to} className="min-w-0 flex-1">
              <Link to={to} activeOptions={{ exact: to === "/" }} className={barItem}>
                <Icon aria-hidden="true" className="size-5.5 shrink-0" />
                <span className="max-w-full truncate">{shortLabel}</span>
              </Link>
            </li>
          ))}
          <li className="min-w-0 flex-1">
            <button
              type="button"
              aria-haspopup="dialog"
              aria-current={moreCurrent ? "page" : undefined}
              data-status={moreCurrent ? "active" : undefined}
              onClick={() => setSheet("more")}
              className={cn(barItem, "w-full")}
            >
              <Ellipsis aria-hidden="true" className="size-5.5 shrink-0" />
              <span className="max-w-full truncate">{t("menu.more")}</span>
            </button>
          </li>
        </ul>
      </nav>

      {/* Without a trigger, Base UI only ever asks to close. */}
      <BaseDialog.Root open={sheet !== null} onOpenChange={() => setSheet(null)}>
        <BaseDialog.Portal>
          <BaseDialog.Backdrop className="fixed inset-0 z-40 bg-black/50 transition-opacity data-ending-style:opacity-0 data-starting-style:opacity-0" />
          <BaseDialog.Popup className="fixed inset-x-0 bottom-0 z-50 flex max-h-[calc(100dvh-2rem)] flex-col overflow-y-auto rounded-t-2xl border-line border-t bg-surface pr-[env(safe-area-inset-right)] pb-[calc(1rem+env(safe-area-inset-bottom))] pl-[env(safe-area-inset-left)] text-ink shadow-xl outline-none transition-transform data-ending-style:translate-y-full data-starting-style:translate-y-full">
            <span
              aria-hidden="true"
              className="mt-2 mb-1 h-1.25 w-10 shrink-0 self-center rounded-full bg-btn-border"
            />
            {sheet === "arrange" ? (
              <ArrangeMenu onDone={() => setSheet("more")} />
            ) : (
              <More items={inMore} onNavigate={() => setSheet(null)} onArrange={() => setSheet("arrange")} />
            )}
          </BaseDialog.Popup>
        </BaseDialog.Portal>
      </BaseDialog.Root>
    </>
  );
}

const barItem = cn(
  "flex min-h-14 flex-col items-center justify-center gap-0.5 rounded-lg px-1 py-1.5 text-[11px] text-rail-muted no-underline outline-none transition-colors",
  "hover:text-white focus-visible:outline-2 focus-visible:outline-rail-accent focus-visible:-outline-offset-2 data-[status=active]:text-rail-accent",
);

function More({
  items,
  onNavigate,
  onArrange,
}: {
  items: readonly NavItem[];
  onNavigate: () => void;
  onArrange: () => void;
}) {
  const { t } = useTranslation(["common", "settings"]);
  const { preference, setPreference } = useTheme();
  const { name, version, canSignOut, signOut } = useAccount();
  return (
    <>
      <BaseDialog.Title className="sr-only">{t("menu.more")}</BaseDialog.Title>
      <ul className="flex flex-col px-2">
        {items.map(({ to, label, icon: Icon }) => (
          <li key={to}>
            <Link
              to={to}
              activeOptions={{ exact: to === "/" }}
              onClick={onNavigate}
              className={cn(
                "flex min-h-13 items-center gap-3.5 rounded-lg px-3.5 text-base text-ink no-underline hover:bg-btn-hover data-[status=active]:bg-selected data-[status=active]:shadow-[inset_3px_0_0_var(--color-accent)]",
                focusRing,
              )}
            >
              <Icon aria-hidden="true" className="size-5.5 shrink-0 text-muted" />
              {label}
            </Link>
          </li>
        ))}
        <li>
          <button
            type="button"
            onClick={onArrange}
            className={cn(
              "flex min-h-13 w-full items-center gap-3.5 rounded-lg px-3.5 text-[15px] text-link hover:bg-btn-hover",
              focusRing,
            )}
          >
            <ArrowDownUp aria-hidden="true" className="size-5.5 shrink-0" />
            {t("menu.arrange")}
          </button>
        </li>
      </ul>

      <fieldset className="mx-5 mt-2 flex flex-col gap-2.5 border-line border-t pt-4">
        <legend className="float-left mb-2.5 font-mono text-[11.5px] text-muted uppercase tracking-[0.06em]">
          {t("menu.theme")}
        </legend>
        <div className="clear-left flex gap-1 rounded-[10px] border border-line bg-bg p-1">
          {THEMES.map((theme) => (
            <button
              key={theme}
              type="button"
              aria-pressed={preference === theme}
              onClick={() => setPreference(theme)}
              className={cn(
                "min-h-11 flex-1 rounded-lg px-2 text-muted text-sm aria-pressed:bg-soft aria-pressed:font-medium aria-pressed:text-ink",
                focusRing,
              )}
            >
              {t(`settings:general.theme.${theme}.label`)}
            </button>
          ))}
        </div>
      </fieldset>

      <div className="mx-5 mt-4 flex flex-wrap items-center justify-between gap-3 text-[13px] text-muted">
        <span className="flex flex-col">
          {name ? (
            <span>
              <SignedInAs name={name} />
            </span>
          ) : null}
          {version ? <span className="font-mono">{version}</span> : null}
        </span>
        {canSignOut ? (
          <button
            type="button"
            onClick={signOut}
            className={cn(
              "inline-flex min-h-11 items-center gap-2 rounded-lg border border-btn-border px-3.5 text-ink text-sm hover:bg-btn-hover",
              focusRing,
            )}
          >
            <LogOut aria-hidden="true" className="size-4" />
            {t("shell.signOut")}
          </button>
        ) : null}
      </div>
    </>
  );
}
