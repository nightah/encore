import { buildUrl, getCsrfToken, setCsrfToken } from "@api/client";
import type { ClientErrorReportKind } from "@api/generated/model";

/** At most this many reports per page load; the server rate-limits as well. */
const MAX_REPORTS = 10;

const sent = new Set<string>();

/** Sends an error the UI hit to the server's log (POST /client-errors), once per message and page. It never throws: reporting must not cause errors of its own. */
export async function reportError(kind: ClientErrorReportKind, error: unknown): Promise<void> {
  try {
    const message = (error instanceof Error ? error.message : String(error)).slice(0, 1000) || "unknown error";
    const path = window.location.pathname.slice(0, 500);
    const key = `${kind}|${path}|${message}`;

    if (sent.has(key) || sent.size >= MAX_REPORTS) return;
    sent.add(key);

    const stack = error instanceof Error ? (error.stack ?? "").slice(0, 8000) : "";

    // Errors can happen before the app has fetched its CSRF token, e.g. on the first page load.
    if (!getCsrfToken()) {
      const state = await fetch(buildUrl("/auth/state"), { credentials: "same-origin" });
      if (state.ok) setCsrfToken(((await state.json()) as { csrf_token?: string }).csrf_token);
    }

    await fetch(buildUrl("/client-errors"), {
      body: JSON.stringify({ kind, message, path, stack }),
      credentials: "same-origin",
      headers: { "Content-Type": "application/json", "X-CSRF-Token": getCsrfToken() ?? "" },
      keepalive: true,
      method: "POST",
    });
  } catch {
    // The server is unreachable or the report was refused; there is nowhere else to send it.
  }
}

/** Reports uncaught errors and rejected promises that no error screen caught. */
export function installErrorReporting(): void {
  window.addEventListener("error", (event) => void reportError("unhandled", event.error ?? event.message));
  window.addEventListener("unhandledrejection", (event) => void reportError("unhandled", event.reason));
}

/** For tests: forget what was sent. */
export function resetErrorReports(): void {
  sent.clear();
}
