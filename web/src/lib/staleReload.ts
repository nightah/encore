import { pageLocation } from "@lib/pageLocation";

/** sessionStorage key marking that the page already reloaded once for a stale module. */
const RELOADED_KEY = "encore.staleReload";

/** How long a reload counts as recent: a second failure soon after means reloading won't help. */
const RECENT_MS = 30_000;

/** Reloads the page, once, when it fails to load part of the app because the app changed underneath it: a new Encore version, or the dev server re-bundling. Returns false when it already reloaded recently, so the caller shows the error instead of looping. */
export function reloadForStaleModule(): boolean {
  try {
    const last = Number(sessionStorage.getItem(RELOADED_KEY) ?? 0);
    if (Date.now() - last < RECENT_MS) return false;
    sessionStorage.setItem(RELOADED_KEY, String(Date.now()));
  } catch {
    // Without storage there's no loop guard; show the error rather than risk reloading forever.
    return false;
  }

  pageLocation.reload();
  return true;
}

/** Whether an error is a failed load of part of the app (a stale or missing chunk) rather than a bug. */
export function isStaleModuleError(error: unknown): boolean {
  const message = error instanceof Error ? error.message : String(error);
  return /dynamically imported module|Importing a module script failed|Failed to fetch dynamically imported module/i.test(
    message,
  );
}

/** Vite reports failed chunk preloads with this event; reloading picks up the current app. */
export function installStaleModuleReload(): void {
  window.addEventListener("vite:preloadError", (event) => {
    if (reloadForStaleModule()) event.preventDefault();
  });
}
