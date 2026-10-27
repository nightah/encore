import { createMemoryHistory } from "@tanstack/react-router";
import { render } from "@testing-library/react";
import { HttpResponse, http } from "msw";

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
