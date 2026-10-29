import type { ReactNode } from "react";

import { QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";

import { setCsrfToken } from "@api/client";
import type { Instance, Library } from "@api/generated/model";
import { keys } from "@api/keys";
import {
  syncInstance,
  useCreateInstance,
  useDeleteInstance,
  useDiscovered,
  useGroupLibrary,
  useInstances,
  useLibraries,
  useMakeDefaultInstance,
  useRecentTitles,
  useSetLibrarySource,
  useSplitLibrary,
  useSyncInstance,
  useTestInstance,
  useUpdateInstance,
  useWakeInstance,
} from "@api/kodi";
import { getDb } from "@mocks/handlers";
import { createQueryClient } from "@src/router";
import { recordRequests } from "@test/utils";

let queryClient: ReturnType<typeof createQueryClient>;
const wrapper = ({ children }: { children: ReactNode }) => (
  <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
);

const KODI_KEYS = [keys.instances, keys.discovered, keys.libraries, keys.overview];

/** Caches every query a Kodi change affects, unobserved, so invalidation marks them without refetching. */
function cacheKodiQueries() {
  for (const key of KODI_KEYS) queryClient.setQueryData(key, []);
}

const invalidated = (key: readonly unknown[]) => queryClient.getQueryState(key)?.isInvalidated;

beforeEach(() => {
  queryClient = createQueryClient();
  queryClient.setDefaultOptions({ queries: { retry: false } });
  setCsrfToken(getDb().csrf);
});

describe("Kodi instances", () => {
  it("loads the instances and the ones found on the network", async () => {
    const { result } = renderHook(() => ({ discovered: useDiscovered(), instances: useInstances() }), { wrapper });
    await waitFor(() => expect(result.current.instances.isSuccess && result.current.discovered.isSuccess).toBe(true));
    expect(result.current.instances.data?.map((i) => i.id)).toEqual(getDb().instances.map((i) => i.id));
    expect(result.current.discovered.data).toEqual(getDb().discovered);
  });

  it.each([
    ["adding", () => useCreateInstance(), { name: "Study", url: "http://192.168.1.40:8080/jsonrpc" }],
    ["saving", () => useUpdateInstance(), { body: { name: "Lounge room" }, id: 1 }],
    ["removing", () => useDeleteInstance(), 2],
  ] as const)(
    "refreshes instances, discoveries, libraries and the overview after %s an instance",
    async (_name, hook, vars) => {
      cacheKodiQueries();
      const { result } = renderHook(hook as () => { mutateAsync: (v: unknown) => Promise<unknown> }, { wrapper });
      await act(() => result.current.mutateAsync(vars));
      for (const key of KODI_KEYS) expect(invalidated(key), String(key)).toBe(true);
    },
  );

  it("refreshes nothing when adding an instance fails", async () => {
    cacheKodiQueries();
    const { result } = renderHook(() => useCreateInstance(), { wrapper });
    await act(async () => {
      await result.current.mutateAsync({ name: "", url: "" }).catch(() => undefined);
    });
    await waitFor(() => expect(result.current.isError).toBe(true));
    for (const key of KODI_KEYS) expect(invalidated(key)).toBe(false);
  });

  it("makes one instance the default for unlinked Echos, and no other", async () => {
    const { result } = renderHook(() => ({ default: useMakeDefaultInstance(), instances: useInstances() }), {
      wrapper,
    });
    await waitFor(() => expect(result.current.instances.isSuccess).toBe(true));

    await act(() => result.current.default.mutateAsync(2));

    const defaults = queryClient.getQueryData<Instance[]>(keys.instances)?.map((i) => [i.id, i.default_for_unlinked]);
    expect(defaults).toEqual(getDb().instances.map((i) => [i.id, i.id === 2]));
  });

  it("moves the default in the cached list at once, without waiting for the refetch", async () => {
    queryClient.setQueryData<Instance[]>(keys.instances, [
      { default_for_unlinked: true, id: 1 } as Instance,
      { default_for_unlinked: false, id: 2 } as Instance,
      { default_for_unlinked: false, id: 3 } as Instance,
    ]);
    const { result } = renderHook(() => useMakeDefaultInstance(), { wrapper });

    await act(() => result.current.mutateAsync(2));

    const defaults = queryClient.getQueryData<Instance[]>(keys.instances)?.map((i) => [i.id, i.default_for_unlinked]);
    expect(defaults).toEqual([
      [1, false],
      [2, true],
      [3, false],
    ]);
    expect(invalidated(keys.instances)).toBe(true);
  });

  it("tests, syncs and wakes an instance by id", async () => {
    const requests = recordRequests();
    const { result } = renderHook(
      () => ({ sync: useSyncInstance(), test: useTestInstance(), wake: useWakeInstance() }),
      {
        wrapper,
      },
    );
    await act(() => result.current.test.mutateAsync(1));
    await act(() => result.current.sync.mutateAsync(1));
    await act(() => result.current.wake.mutateAsync(3));
    await act(() => syncInstance(2));
    expect(requests.map((r) => `${r.method} ${r.path}`)).toEqual([
      "POST /kodi/instances/1/test",
      "POST /kodi/instances/1/sync",
      "POST /kodi/instances/3/wake",
      "POST /kodi/instances/2/sync",
    ]);
  });
});

describe("libraries", () => {
  it("doesn't ask for recent titles until a library is chosen", async () => {
    const requests = recordRequests();
    const { result, rerender } = renderHook(({ id }: { id: number | undefined }) => useRecentTitles(id, 5), {
      initialProps: { id: undefined as number | undefined },
      wrapper,
    });
    expect(result.current.fetchStatus).toBe("idle");
    expect(requests).toHaveLength(0);

    const id = getDb().libraries[0]?.id ?? 0;
    rerender({ id });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(requests.map((r) => r.path)).toEqual([`/libraries/${id}/recent?limit=5`]);
    expect(result.current.data?.length).toBeLessThanOrEqual(5);
  });

  it("changes a library's source in place and refreshes the instances", async () => {
    const shared = getDb().libraries.find((l) => l.members.length > 1) as Library;
    const other = shared.members.find((m) => m.instance_id !== shared.source_instance_id)?.instance_id ?? 0;
    queryClient.setQueryData(keys.instances, []);
    const { result } = renderHook(() => ({ libraries: useLibraries(), source: useSetLibrarySource() }), { wrapper });
    await waitFor(() => expect(result.current.libraries.isSuccess).toBe(true));

    await act(() => result.current.source.mutateAsync({ id: shared.id, sourceInstanceId: other }));

    const cached = queryClient.getQueryData<Library[]>(keys.libraries);
    expect(cached?.find((l) => l.id === shared.id)?.source_instance_id).toBe(other);
    expect(cached).toHaveLength(getDb().libraries.length);
    expect(invalidated(keys.instances)).toBe(true);
  });

  it("groups instances into one library and splits one out again", async () => {
    cacheKodiQueries();
    const requests = recordRequests();
    const { result } = renderHook(() => ({ group: useGroupLibrary(), split: useSplitLibrary() }), { wrapper });

    const library = await act(() => result.current.group.mutateAsync([1, 3]));
    expect(requests[0]).toMatchObject({ body: { instance_ids: [1, 3] }, path: "/libraries/group" });
    for (const key of KODI_KEYS) expect(invalidated(key)).toBe(true);

    cacheKodiQueries();
    await act(() => result.current.split.mutateAsync({ id: library.id, instanceId: 3 }));
    expect(requests[1]).toMatchObject({ body: { instance_id: 3 }, path: `/libraries/${library.id}/split` });
    for (const key of KODI_KEYS) expect(invalidated(key)).toBe(true);
  });
});
