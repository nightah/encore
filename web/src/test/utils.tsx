import { createMemoryHistory } from "@tanstack/react-router";
import { type ByRoleOptions, render, screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { expect } from "vitest";

import { server } from "@mocks/node";
import { App } from "@src/App";
import { createAppRouter, createQueryClient } from "@src/router";

/** Renders the whole app (router, query client, toasts) at `path`, against the MSW mock API. */
export function renderApp(path: string) {
  const queryClient = createQueryClient();
  queryClient.setDefaultOptions({
    queries: { ...queryClient.getDefaultOptions().queries, retry: false },
  });
  const router = createAppRouter({
    history: createMemoryHistory({ initialEntries: [path] }),
    queryClient,
  });
  const result = render(<App queryClient={queryClient} router={router} />);
  return { ...result, queryClient, router };
}

/** Finds a button and waits until it's enabled: buttons stay disabled while their data loads or a save is in flight, and a click on a disabled button does nothing. */
export async function findEnabledButton(options: ByRoleOptions) {
  const button = await screen.findByRole("button", options);
  await waitFor(() => expect(button).toBeEnabled());
  return button;
}

export interface RecordedRequest {
  method: string;
  path: string;
  csrf: string | null;
  body: unknown;
}

/** Records every request to the mock API (method, path, CSRF header and JSON body) without changing the response. */
export function recordRequests(): RecordedRequest[] {
  const log: RecordedRequest[] = [];
  server.events.on("request:start", async ({ request }) => {
    const url = new URL(request.url);
    let body: unknown;
    try {
      body = await request.clone().json();
    } catch {
      body = undefined;
    }
    log.push({
      body,
      csrf: request.headers.get("X-CSRF-Token"),
      method: request.method,
      path: url.pathname.replace(/^.*\/api\/v1/, "") + url.search,
    });
  });
  return log;
}

/** Makes one endpoint fail with a problem+json response. */
export function failWith(
  method: "get" | "post" | "put" | "patch" | "delete",
  path: string,
  status: number,
  detail: string,
) {
  server.use(
    http[method](`*/api/v1${path}`, () =>
      HttpResponse.json(
        { detail, status, title: "Error", type: "about:blank" },
        { headers: { "Content-Type": "application/problem+json" }, status },
      ),
    ),
  );
}
