import { setupWorker } from "msw/browser";

import type { MockDb, Scenario } from "@mocks/data";
import { completeMockLwa, createEventsHandler, handlers, onPersist, resetDb } from "@mocks/handlers";
import { basePath } from "@src/basePath";

const STORAGE_KEY = "encore.mock.db";

/**
 * Starts the MSW service worker for `pnpm dev:mock`.
 *
 * - `?mock=fresh` starts from a first-run state (setup required), `?mock=configured` from the sample home setup.
 * - The database is kept in sessionStorage, so reloads and the Login with Amazon round trip keep their state.
 */
export async function startMockWorker() {
  const url = new URL(window.location.href);
  const requested = url.searchParams.get("mock");
  let stored: MockDb | undefined;
  try {
    const raw = window.sessionStorage.getItem(STORAGE_KEY);
    stored = raw ? (JSON.parse(raw) as MockDb) : undefined;
  } catch {
    stored = undefined;
  }

  if (requested === "fresh" || requested === "configured") {
    resetDb(requested as Scenario);
    url.searchParams.delete("mock");
    window.history.replaceState(null, "", url);
  } else if (stored) {
    resetDb(stored.scenario, stored);
  }

  onPersist((db) => {
    try {
      window.sessionStorage.setItem(STORAGE_KEY, JSON.stringify(db));
    } catch {
      // Storage full or blocked: the mock just won't survive a reload.
    }
  });

  if (url.searchParams.has("mock_lwa")) {
    completeMockLwa();
    url.searchParams.delete("mock_lwa");
    window.history.replaceState(null, "", url);
  }

  const worker = setupWorker(createEventsHandler(), ...handlers);
  await worker.start({
    onUnhandledFrame: "bypass",
    quiet: true,
    serviceWorker: { url: `${basePath()}mockServiceWorker.js` },
  });
}
