import type { ReactNode } from "react";

import { Popover } from "@base-ui/react/popover";
import { Info } from "lucide-react";
import { useTranslation } from "react-i18next";

import { cn, focusRing } from "@components/ui/cn";

/** An info button that explains something on hover, focus or tap. `label` names what it explains, for screen readers. */
export function InfoTip({ label, children }: { label: string; children: ReactNode }) {
  const { t } = useTranslation("common");
  return (
    <Popover.Root>
      <Popover.Trigger
        openOnHover
        delay={150}
        aria-label={t("infoTip.label", { label })}
        className={cn(
          "inline-flex size-5 shrink-0 cursor-help items-center justify-center rounded-full text-muted hover:text-ink",
          focusRing,
        )}
      >
        <Info aria-hidden className="size-3.5" />
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Positioner sideOffset={6} className="z-50">
          <Popover.Popup className="max-w-xs rounded-[10px] border border-line bg-surface px-3.5 py-2.5 text-[13px] text-ink leading-snug shadow-lg transition-opacity data-ending-style:opacity-0 data-starting-style:opacity-0">
            {children}
          </Popover.Popup>
        </Popover.Positioner>
      </Popover.Portal>
    </Popover.Root>
  );
}
