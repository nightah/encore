import type { ReactNode } from "react";

import { Dialog as BaseDialog } from "@base-ui/react/dialog";
import { X } from "lucide-react";
import { useTranslation } from "react-i18next";

import { cn, focusRing } from "@components/ui/cn";

interface DialogProps {
  open: boolean;
  /** Escape, the backdrop or the close button. The caller opens the dialog by setting `open`; it has no trigger of its own. */
  onClose: () => void;
  title: ReactNode;
  description?: ReactNode;
  children?: ReactNode;
  /** Buttons row; usually a Close and a submit button. */
  footer: ReactNode;
  size?: "md" | "lg";
}

/** Modal dialog on Base UI: focus trap, Escape to close, labelled by its title. */
export function Dialog({ open, onClose, title, description, children, footer, size = "md" }: DialogProps) {
  const { t } = useTranslation("common");
  return (
    // Without a trigger, Base UI only ever asks to close.
    <BaseDialog.Root open={open} onOpenChange={() => onClose()}>
      <BaseDialog.Portal>
        <BaseDialog.Backdrop className="fixed inset-0 z-40 bg-black/40 transition-opacity data-ending-style:opacity-0 data-starting-style:opacity-0" />
        <BaseDialog.Popup
          className={cn(
            "fixed top-1/2 left-1/2 z-50 flex max-h-[calc(100dvh-2rem)] w-[calc(100vw-2rem)] -translate-x-1/2 -translate-y-1/2 flex-col gap-4 overflow-y-auto rounded-2xl border border-line bg-surface p-5 text-ink shadow-xl outline-none transition-[opacity,scale] data-ending-style:scale-[0.98] data-starting-style:scale-[0.98] data-ending-style:opacity-0 data-starting-style:opacity-0 sm:p-6",
            size === "lg" ? "max-w-2xl" : "max-w-lg",
          )}
        >
          <div className="flex items-start justify-between gap-3">
            <div className="flex min-w-0 flex-col gap-1">
              <BaseDialog.Title className="font-display font-semibold text-xl">{title}</BaseDialog.Title>
              {description ? (
                <BaseDialog.Description className="text-muted text-sm">{description}</BaseDialog.Description>
              ) : null}
            </div>
            <BaseDialog.Close
              aria-label={t("actions.close")}
              className={cn(
                "-mt-1.5 -me-1.5 inline-flex size-11 shrink-0 items-center justify-center rounded-lg text-muted hover:bg-btn-hover hover:text-ink",
                focusRing,
              )}
            >
              <X aria-hidden="true" className="size-5" />
            </BaseDialog.Close>
          </div>
          {children}
          <div className="flex flex-wrap justify-end gap-2.5 border-line-soft border-t pt-4">{footer}</div>
        </BaseDialog.Popup>
      </BaseDialog.Portal>
    </BaseDialog.Root>
  );
}
