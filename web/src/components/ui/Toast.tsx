import type { ReactNode } from "react";

import { Toast } from "@base-ui/react/toast";
import { CircleAlert, CircleCheck, X } from "lucide-react";
import { useTranslation } from "react-i18next";

import { cn, focusRing } from "@components/ui/cn";

/** One manager for the app, so mutation callbacks outside React can show toasts. */
const toastManager = Toast.createToastManager();

export function notifySuccess(title: string, description?: string) {
  toastManager.add({ description, title, type: "success" });
}

export function notifyError(title: string, description?: string) {
  toastManager.add({ description, priority: "high", timeout: 8000, title, type: "error" });
}

export function ToastProvider({ children }: { children: ReactNode }) {
  return (
    <Toast.Provider toastManager={toastManager}>
      {children}
      <Toast.Portal>
        <Toast.Viewport className="fixed end-4 bottom-[calc(5rem+env(safe-area-inset-bottom))] z-60 min-[800px]:bottom-4 flex w-[calc(100vw-2rem)] flex-col gap-2 sm:w-96">
          <ToastList />
        </Toast.Viewport>
      </Toast.Portal>
    </Toast.Provider>
  );
}

function ToastList() {
  const { t } = useTranslation("common");
  const { toasts } = Toast.useToastManager();
  return toasts.map((toast) => {
    const error = toast.type === "error";
    const Icon = error ? CircleAlert : CircleCheck;
    return (
      <Toast.Root
        key={toast.id}
        toast={toast}
        className="rounded-xl border border-line bg-surface text-ink shadow-lg transition-[opacity,translate] data-ending-style:translate-y-2 data-starting-style:translate-y-2 data-ending-style:opacity-0 data-starting-style:opacity-0"
      >
        <Toast.Content className="flex items-start gap-3 p-3.5">
          <Icon aria-hidden="true" className={cn("mt-0.5 size-5 shrink-0", error ? "text-bad" : "text-ok")} />
          <div className="flex min-w-0 flex-1 flex-col gap-0.5">
            <Toast.Title className="font-semibold text-sm" />
            <Toast.Description className="text-muted text-sm" />
          </div>
          <Toast.Close
            aria-label={t("actions.dismiss")}
            className={cn(
              "-m-1.5 inline-flex size-9 shrink-0 items-center justify-center rounded-lg text-muted hover:bg-btn-hover hover:text-ink",
              focusRing,
            )}
          >
            <X aria-hidden="true" className="size-4" />
          </Toast.Close>
        </Toast.Content>
      </Toast.Root>
    );
  });
}
