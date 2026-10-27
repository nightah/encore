import { useEffect } from "react";

import type { QueryClient } from "@tanstack/react-query";
import { type ErrorComponentProps, Link, Outlet, createRootRouteWithContext } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import { useAuthState } from "@api/auth";
import { ApiError, errorMessage } from "@api/client";
import { useEventStream } from "@api/events";
import { Button } from "@components/ui/Button";
import { reportError } from "@lib/reportError";
import { isStaleModuleError, reloadForStaleModule } from "@lib/staleReload";

export interface RouterContext {
  queryClient: QueryClient;
}

export const Route = createRootRouteWithContext<RouterContext>()({
  component: Root,
  errorComponent: RootError,
  notFoundComponent: NotFound,
});

function Root() {
  const { data: auth } = useAuthState();
  return (
    <>
      {auth?.authenticated ? <EventStream /> : null}
      <Outlet />
    </>
  );
}

/** One live event stream for the whole signed-in app, including the setup wizard. */
function EventStream() {
  useEventStream();
  return null;
}

function NotFound() {
  const { t } = useTranslation("common");
  return (
    <div className="mx-auto max-w-md px-4 py-16 text-center">
      <h1 className="font-semibold text-2xl text-ink">{t("notFound.title")}</h1>
      <p className="mt-2 text-muted text-sm">
        <Link to="/" className="text-link underline underline-offset-2">
          {t("notFound.back")}
        </Link>
      </p>
    </div>
  );
}

/** Whether an error means the server didn't answer (or answered with a server error), as opposed to the page itself failing. */
export function isServerUnreachable(error: unknown): boolean {
  if (error instanceof ApiError) return error.status === 0 || error.status >= 500;
  // fetch rejects with a TypeError when the server can't be reached at all.
  return error instanceof TypeError && /fetch|network/i.test(error.message);
}

function RootError({ error, reset }: ErrorComponentProps) {
  const { t } = useTranslation("common");
  const unreachable = isServerUnreachable(error);

  // A part of the app that failed to load because the app changed underneath the page (a new version, or the dev server re-bundling) is fixed by reloading, once. Anything else is a bug worth a line in the server log; an unreachable server can't take the report anyway.
  useEffect(() => {
    if (isStaleModuleError(error) && reloadForStaleModule()) return;
    if (!unreachable) void reportError("render", error);
  }, [error, unreachable]);

  return (
    <div role="alert" className="mx-auto flex max-w-md flex-col items-center gap-3 px-4 py-16 text-center">
      <h1 className="font-semibold text-2xl text-ink">
        {unreachable ? t("error.unreachable") : t("error.pageFailed")}
      </h1>
      <p className="text-muted">{errorMessage(error)}</p>
      <Button
        variant="primary"
        onClick={() => {
          reset();
          window.location.reload();
        }}
      >
        {t("actions.tryAgain")}
      </Button>
    </div>
  );
}
