import type { ReactNode } from "react";

import { QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";

import { setCsrfToken } from "@api/client";
import { type RequestFilters, useReplayRequest, useRequest, useRequests } from "@api/requests";
import { getDb } from "@mocks/handlers";
import { createQueryClient } from "@src/router";
import { recordRequests } from "@test/utils";

let queryClient: ReturnType<typeof createQueryClient>;
const wrapper = ({ children }: { children: ReactNode }) => (
  <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
);

beforeEach(() => {
  queryClient = createQueryClient();
  queryClient.setDefaultOptions({ queries: { retry: false } });
  setCsrfToken(getDb().csrf);
});

/** Adds requests to the mock log until it holds more than one page. */
function fillLog(total: number) {
  const db = getDb();
  const template = db.requests[0];
  if (!template) throw new Error("no sample request");
  db.requests = Array.from({ length: total }, (_, i) => ({ ...template, id: total - i }));
}

describe("useRequests", () => {
  it("asks for 50 requests at a time and pages back with the last id", async () => {
    fillLog(120);
    const requests = recordRequests();
    const { result } = renderHook(() => useRequests({}), { wrapper });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.hasNextPage).toBe(true);

    await act(() => result.current.fetchNextPage());
    await act(() => result.current.fetchNextPage());

    expect(requests.map((r) => r.path)).toEqual([
      "/requests?limit=50",
      "/requests?limit=50&before=71",
      "/requests?limit=50&before=21",
    ]);
    await waitFor(() => expect(result.current.data?.pages.flatMap((p) => p.items)).toHaveLength(120));
    expect(result.current.hasNextPage).toBe(false);
  });

  it.each<[RequestFilters, string]>([
    [{ device: 0 }, "/requests?limit=50&device=0"],
    [{ device: 3, outcome: "error" }, "/requests?limit=50&device=3&outcome=error"],
    [{ q: "matrix" }, "/requests?limit=50&q=matrix"],
    [{ device: undefined, outcome: undefined, q: "" }, "/requests?limit=50"],
  ])("sends the filters %j", async (filters, path) => {
    const requests = recordRequests();
    const { result } = renderHook(() => useRequests(filters), { wrapper });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(requests.map((r) => r.path)).toEqual([path]);
  });

  it("keeps each filter combination in its own cache entry", async () => {
    const { result, rerender } = renderHook(({ filters }) => useRequests(filters), {
      initialProps: { filters: {} as RequestFilters },
      wrapper,
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    const all = result.current.data?.pages[0]?.items.length ?? 0;

    rerender({ filters: { outcome: "error" } });
    await waitFor(() => expect(result.current.data?.pages[0]?.items.every((r) => r.outcome === "error")).toBe(true));
    expect(result.current.data?.pages[0]?.items.length).toBeLessThan(all);
  });
});

describe("useRequest", () => {
  it("loads one request's detail once there is an id", async () => {
    const requests = recordRequests();
    const id = getDb().requests[0]?.id ?? 0;
    const { result, rerender } = renderHook(({ id }: { id: number | undefined }) => useRequest(id), {
      initialProps: { id: undefined as number | undefined },
      wrapper,
    });
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(requests).toHaveLength(0);

    rerender({ id });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.id).toBe(id);
  });

  it("fails without retrying for a request that doesn't exist", async () => {
    queryClient = createQueryClient();
    const requests = recordRequests();
    const { result } = renderHook(() => useRequest(999_999), { wrapper });
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.error).toMatchObject({ status: 404 });
    expect(requests).toHaveLength(1);
  });
});

describe("useReplayRequest", () => {
  it("replays a request as a dry run", async () => {
    const id = getDb().requests.find((r) => r.match)?.id ?? 0;
    const { result } = renderHook(() => useReplayRequest(), { wrapper });
    const replay = await act(() => result.current.mutateAsync(id));
    expect(replay).toHaveProperty("would_call");
    expect(replay).toHaveProperty("speech");
  });
});
