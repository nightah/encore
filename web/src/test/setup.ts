import "@testing-library/jest-dom/vitest";

import { cleanup } from "@testing-library/react";
import { afterAll, afterEach, beforeAll, beforeEach } from "vitest";

import { setCsrfToken } from "@api/client";
import { eventStream } from "@api/events";
import i18n from "@i18n/index";
import { onEmit, resetDb } from "@mocks/handlers";
import { server } from "@mocks/node";
import { FakeEventSource } from "@test/eventSource";

// Every test talks to the MSW implementation of the admin API (docs/src/content/docs/reference/api/); unknown requests fail loudly.
beforeAll(() => {
  server.listen({ onUnhandledFrame: "error" });
  globalThis.EventSource = FakeEventSource as unknown as typeof EventSource;
  // Simulated server events (deploy progress, wake, sync…) reach the app through the fake EventSource.
  onEmit(({ event, data }) => FakeEventSource.emit(event, data));
});

beforeEach(async () => {
  // Tests read the English strings; a test that switches language doesn't leak into the next.
  await i18n.changeLanguage("en");
  resetDb("configured");
  setCsrfToken(undefined);
});

afterEach(() => {
  cleanup();
  server.resetHandlers();
  server.events.removeAllListeners();
  eventStream.reset();
  FakeEventSource.reset();
  try {
    window.localStorage.clear();
  } catch {
    // Storage blocked: nothing to clear.
  }
  delete document.documentElement.dataset.theme;
});

afterAll(() => server.close());
