import { Moon, Sun } from "lucide-react";
import { useTranslation } from "react-i18next";

import { cn, focusRing } from "@components/ui/cn";
import { useTheme } from "@theme/store";

/** Sun/moon button in every page header: flips between light and dark. Settings › General offers "Match system" too. */
export function ThemeToggle({ onDark = false, className }: { onDark?: boolean; className?: string }) {
  const { t } = useTranslation("common");
  const { resolved, setPreference } = useTheme();
  const dark = resolved === "dark";
  const label = dark ? t("theme.toLight") : t("theme.toDark");
  return (
    <button
      type="button"
      onClick={() => setPreference(dark ? "light" : "dark")}
      aria-label={label}
      title={label}
      data-theme-resolved={resolved}
      className={cn(
        "inline-flex size-11 shrink-0 items-center justify-center rounded-lg border transition-colors",
        onDark
          ? "border-[#334050] bg-transparent text-[#c3ccd6] hover:bg-white/5 hover:text-white"
          : "border-btn-border bg-surface text-ink hover:bg-btn-hover",
        focusRing,
        className,
      )}
    >
      {dark ? <Sun aria-hidden="true" className="size-4.5" /> : <Moon aria-hidden="true" className="size-4.5" />}
    </button>
  );
}
