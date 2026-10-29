import type { ReactNode } from "react";

import { QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";

import { ApiError, setCsrfToken } from "@api/client";
import {
  useAddRoomAlias,
  useCreateRoom,
  useDeleteDevice,
  useDeleteRoom,
  useDeleteRoomAlias,
  useDevices,
  useRooms,
  useUpdateDevice,
  useUpdateRoom,
} from "@api/devices";
import type { Device, Room } from "@api/generated/model";
import { keys } from "@api/keys";
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

const invalidated = (key: readonly unknown[]) => queryClient.getQueryState(key)?.isInvalidated;

/** Runs a mutation that should fail and returns its error. */
async function failure(run: () => Promise<unknown>): Promise<unknown> {
  let error: unknown;
  await act(async () => {
    error = await run().then(
      () => undefined,
      (e: unknown) => e,
    );
  });
  return error;
}

describe("Echo devices", () => {
  it("loads the Echos", async () => {
    const { result } = renderHook(() => useDevices(), { wrapper });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toEqual(getDb().devices);
  });

  it("puts a saved Echo straight into the list and refreshes the list and overview", async () => {
    queryClient.setQueryData(keys.overview, {});
    const { result } = renderHook(() => ({ devices: useDevices(), update: useUpdateDevice() }), { wrapper });
    await waitFor(() => expect(result.current.devices.isSuccess).toBe(true));
    const [first, second] = getDb().devices as [Device, Device];

    const requests = recordRequests();
    await act(() => result.current.update.mutateAsync({ body: { name: "  Hallway  " }, id: first.id }));

    expect(requests[0]).toMatchObject({ body: { name: "  Hallway  " }, method: "PATCH", path: `/devices/${first.id}` });
    const cached = queryClient.getQueryData<Device[]>(keys.devices);
    expect(cached?.find((d) => d.id === first.id)?.name).toBe("Hallway");
    expect(cached?.find((d) => d.id === second.id)).toEqual(second);
    expect(invalidated(keys.overview)).toBe(true);
    await waitFor(() => expect(requests.some((r) => r.method === "GET" && r.path === "/devices")).toBe(true));
  });

  it("names the success message it is given", () => {
    const { result } = renderHook(() => useUpdateDevice("Room changed."), { wrapper });
    // The mutation cache shows the toast from this meta.
    act(() => result.current.mutate({ body: {}, id: 1 }));
    expect(queryClient.getMutationCache().getAll()[0]?.meta).toEqual({ inline: true, success: "Room changed." });
  });

  it("reports validation errors by field", async () => {
    const { result } = renderHook(() => useUpdateDevice(), { wrapper });
    const error = await failure(() => result.current.mutateAsync({ body: { name: " " }, id: 1 }));
    expect(error).toBeInstanceOf(ApiError);
    expect((error as ApiError).fieldErrors).toEqual({ name: "Give the Echo a name." });
  });

  it("forgets an Echo and refreshes the list", async () => {
    const { result } = renderHook(() => ({ devices: useDevices(), remove: useDeleteDevice() }), { wrapper });
    await waitFor(() => expect(result.current.devices.isSuccess).toBe(true));
    const id = getDb().devices[0]?.id ?? 0;

    await act(() => result.current.remove.mutateAsync(id));

    await waitFor(() => expect(result.current.devices.data?.some((d) => d.id === id)).toBe(false));
  });
});

describe("rooms", () => {
  async function renderRooms() {
    const hook = renderHook(
      () => ({
        addAlias: useAddRoomAlias(),
        create: useCreateRoom(),
        remove: useDeleteRoom(),
        removeAlias: useDeleteRoomAlias(),
        rooms: useRooms(),
        update: useUpdateRoom(),
      }),
      { wrapper },
    );
    await waitFor(() => expect(hook.result.current.rooms.isSuccess).toBe(true));
    return hook.result;
  }

  it("adds a room and refetches the list", async () => {
    const result = await renderRooms();
    await act(() => result.current.create.mutateAsync({ name: "Garage" }));
    await waitFor(() => expect(result.current.rooms.data?.map((r) => r.name)).toContain("Garage"));
  });

  it("renames a room and refetches the list", async () => {
    const result = await renderRooms();
    const room = getDb().rooms[0] as Room;
    await act(() => result.current.update.mutateAsync({ body: { name: "Den" }, id: room.id }));
    await waitFor(() => expect(result.current.rooms.data?.find((r) => r.id === room.id)?.name).toBe("Den"));
  });

  it("removes a room and refreshes the Echos that were in it", async () => {
    queryClient.setQueryData(keys.devices, []);
    const result = await renderRooms();
    const room = getDb().rooms[0] as Room;
    await act(() => result.current.remove.mutateAsync(room.id));
    await waitFor(() => expect(result.current.rooms.data?.some((r) => r.id === room.id)).toBe(false));
    expect(invalidated(keys.devices)).toBe(true);
  });

  it("adds a spoken name to its room only, without refetching", async () => {
    const result = await renderRooms();
    const [room, other] = getDb().rooms as [Room, Room];
    const otherAliases = other.aliases.length;
    const requests = recordRequests();

    await act(() => result.current.addAlias.mutateAsync({ alias: "Front Room", kind: "device", roomId: room.id }));

    const cached = queryClient.getQueryData<Room[]>(keys.rooms);
    expect(cached?.find((r) => r.id === room.id)?.aliases.at(-1)).toMatchObject({
      alias: "front room",
      kind: "device",
    });
    expect(requests[0]?.body).toMatchObject({ alias: "Front Room", kind: "device" });
    expect(cached?.find((r) => r.id === other.id)?.aliases).toHaveLength(otherAliases);
    expect(requests.map((r) => r.method)).toEqual(["POST"]);
  });

  it("rejects a spoken name another room uses, leaving the cache alone", async () => {
    const result = await renderRooms();
    const [room, other] = getDb().rooms as [Room, Room];
    const taken = other.aliases[0]?.alias ?? "";
    const before = queryClient.getQueryData<Room[]>(keys.rooms);

    const error = await failure(() =>
      result.current.addAlias.mutateAsync({ alias: taken, kind: "room", roomId: room.id }),
    );

    expect(error).toMatchObject({ status: 409 });
    expect(queryClient.getQueryData<Room[]>(keys.rooms)).toBe(before);
  });

  it("removes a spoken name from its room only", async () => {
    const result = await renderRooms();
    const room = getDb().rooms.find((r) => r.aliases.length > 0) as Room;
    const [gone, ...kept] = room.aliases;

    await act(() => result.current.removeAlias.mutateAsync({ aliasId: gone?.id ?? 0, roomId: room.id }));

    expect(queryClient.getQueryData<Room[]>(keys.rooms)?.find((r) => r.id === room.id)?.aliases).toEqual(kept);
  });
});
