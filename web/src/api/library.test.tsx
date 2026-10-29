import type { ReactNode } from "react";

import { QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";

import { setCsrfToken } from "@api/client";
import { keys } from "@api/keys";
import { useAliases, useCreateAlias, useDeleteAlias, useMatch } from "@api/library";
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

const libraryId = () => getDb().libraries[0]?.id ?? 0;

describe("title aliases", () => {
  it("loads nothing until a library is chosen, then that library's aliases", async () => {
    const requests = recordRequests();
    const { result, rerender } = renderHook(({ id }: { id: number | undefined }) => useAliases(id), {
      initialProps: { id: undefined as number | undefined },
      wrapper,
    });
    expect(result.current.fetchStatus).toBe("idle");
    expect(requests).toHaveLength(0);

    rerender({ id: libraryId() });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(requests.map((r) => r.path)).toEqual([`/aliases?library_id=${libraryId()}`]);
    expect(result.current.data?.every((a) => a.library_id === libraryId())).toBe(true);
  });

  it("refreshes the aliases of the library an alias was added to, and only those", async () => {
    const other = libraryId() + 1000;
    queryClient.setQueryData(keys.aliases(libraryId()), []);
    queryClient.setQueryData(keys.aliases(other), []);
    const { result } = renderHook(() => useCreateAlias(), { wrapper });

    const alias = await act(() =>
      result.current.mutateAsync({ alias: "The Office", kind: "tvshow", library_id: libraryId(), media_id: 7 }),
    );

    expect(alias).toMatchObject({ alias: "the office", library_id: libraryId() });
    expect(queryClient.getQueryState(keys.aliases(libraryId()))?.isInvalidated).toBe(true);
    expect(queryClient.getQueryState(keys.aliases(other))?.isInvalidated).toBe(false);
  });

  it("refreshes every library's aliases after removing one", async () => {
    const alias = getDb().aliases[0];
    queryClient.setQueryData(keys.aliases(1), []);
    queryClient.setQueryData(keys.aliases(2), []);
    const { result } = renderHook(() => useDeleteAlias(), { wrapper });

    await act(() => result.current.mutateAsync(alias?.id ?? 0));

    expect(queryClient.getQueryState(keys.aliases(1))?.isInvalidated).toBe(true);
    expect(queryClient.getQueryState(keys.aliases(2))?.isInvalidated).toBe(true);
  });
});

describe("useMatch", () => {
  it("probes the matcher and reports invalid input by field", async () => {
    const { result } = renderHook(() => useMatch(), { wrapper });

    const match = await act(() => result.current.mutateAsync({ library_id: libraryId(), query: "the matrix" }));
    expect(match).toMatchObject({ candidates: expect.any(Array), decision: expect.any(String) });

    await act(async () => {
      await result.current.mutateAsync({ library_id: libraryId(), query: " " }).catch(() => undefined);
    });
    await waitFor(() =>
      expect(result.current.error).toMatchObject({ fieldErrors: { query: "Type what Alexa heard." } }),
    );
  });
});
