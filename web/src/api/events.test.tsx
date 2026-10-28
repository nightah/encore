import type { ReactNode } from "react";

import { type InfiniteData, QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { RECENT_REQUESTS, eventStream, useEventStream, useServerEvent } from "@api/events";
import type { Deploy, Device, Instance, Library, Overview, RequestPage, RequestSummary } from "@api/generated/model";
import { keys } from "@api/keys";
import { FakeEventSource } from "@test/eventSource";

const request = (id: number, patch: Partial<RequestSummary> = {}): RequestSummary => ({
  device_id: 1,
  device_name: "Kitchen",
  duration_ms: 120,
  heard: "The Matrix",
  id,
  intent: "PlayMovie",
  locale: "en-AU",
  outcome: "played",
  simulated: false,
  target: "Living room",
  time: "2026-10-28T09:00:00+11:00",
  ...patch,
});

const page = (items: RequestSummary[], nextBefore: number | null = null): RequestPage => ({
  items,
  next_before: nextBefore,
});

const infinite = (...pages: RequestPage[]): InfiniteData<RequestPage> => ({
  pageParams: pages.map((_, i) => (i === 0 ? undefined : i)),
  pages,
});

const overview = (patch: Partial<Overview> = {}) =>
  ({
    attention: [],
    instances: [],
    recent_requests: [],
    requests_today: { failed: 0, median_ms: 0, total: 0 },
    ...patch,
  }) as unknown as Overview;

let queryClient: QueryClient;
let release: () => void;

function emit(event: string, data: unknown) {
  act(() => FakeEventSource.emit(event, data));
}

function itemIds(filters: object) {
  return queryClient
    .getQueryData<InfiniteData<RequestPage>>(keys.requestList(filters))
    ?.pages.map((p) => p.items.map((r) => r.id));
}

beforeEach(() => {
  queryClient = new QueryClient();
  release = eventStream.acquire(queryClient);
});

afterEach(() => {
  release();
  queryClient.clear();
  vi.restoreAllMocks();
});

describe("request events", () => {
  it("prepends a request to the first page of every list it matches", () => {
    queryClient.setQueryData(keys.requestList({}), infinite(page([request(2)], 2), page([request(1)])));
    queryClient.setQueryData(keys.requestList({ device: 1 }), infinite(page([])));
    queryClient.setQueryData(keys.requestList({ device: 9 }), infinite(page([])));
    queryClient.setQueryData(keys.requestList({ outcome: "played" }), infinite(page([])));
    queryClient.setQueryData(keys.requestList({ outcome: "error" }), infinite(page([])));
    queryClient.setQueryData(keys.requestList({ q: "matrix" }), infinite(page([])));
    queryClient.setQueryData(keys.requestList({ q: "dune" }), infinite(page([])));

    emit("request", request(3, { heard: "The MATRIX Reloaded" }));

    expect(itemIds({})).toEqual([[3, 2], [1]]);
    expect(itemIds({ device: 1 })).toEqual([[3]]);
    expect(itemIds({ device: 9 })).toEqual([[]]);
    expect(itemIds({ outcome: "played" })).toEqual([[3]]);
    expect(itemIds({ outcome: "error" })).toEqual([[]]);
    expect(itemIds({ q: "matrix" })).toEqual([[3]]);
    expect(itemIds({ q: "dune" })).toEqual([[]]);
  });

  it("doesn't add a request a list already has, as after a reconnect replays it", () => {
    queryClient.setQueryData(keys.requestList({}), infinite(page([request(3), request(2)])));
    emit("request", request(3));
    expect(itemIds({})).toEqual([[3, 2]]);
  });

  it("keeps requests from an unknown Echo out of a list filtered by Echo", () => {
    queryClient.setQueryData(keys.requestList({ device: 1 }), infinite(page([])));
    emit("request", request(4, { device_id: null }));
    expect(itemIds({ device: 1 })).toEqual([[]]);
  });

  it("adds the request to the overview and counts it, failures separately", () => {
    queryClient.setQueryData(keys.overview, overview({ requests_today: { failed: 1, median_ms: 90, total: 5 } }));

    emit("request", request(1, { outcome: "played" }));
    emit("request", request(2, { outcome: "error" }));
    emit("request", request(3, { outcome: "not_found" }));
    emit("request", request(4, { outcome: "asked" }));

    const data = queryClient.getQueryData<Overview>(keys.overview);
    expect(data?.recent_requests.map((r) => r.id)).toEqual([4, 3, 2, 1]);
    expect(data?.requests_today).toEqual({ failed: 3, median_ms: 90, total: 9 });
  });

  it("keeps the overview to the most recent requests", () => {
    const recent = Array.from({ length: RECENT_REQUESTS }, (_, i) => request(RECENT_REQUESTS - i));
    queryClient.setQueryData(keys.overview, overview({ recent_requests: recent }));
    emit("request", request(100));
    const ids = queryClient.getQueryData<Overview>(keys.overview)?.recent_requests.map((r) => r.id);
    expect(ids).toHaveLength(RECENT_REQUESTS);
    expect(ids?.[0]).toBe(100);
    expect(ids).not.toContain(1);
  });

  it("leaves the simulator's requests and repeats out of the overview", () => {
    queryClient.setQueryData(keys.overview, overview({ recent_requests: [request(1)] }));
    emit("request", request(2, { simulated: true }));
    emit("request", request(1));
    const data = queryClient.getQueryData<Overview>(keys.overview);
    expect(data?.recent_requests.map((r) => r.id)).toEqual([1]);
    expect(data?.requests_today.total).toBe(0);
  });

  it("refreshes the Echo list when a request comes from an Echo it doesn't know", () => {
    queryClient.setQueryData<Device[]>(keys.devices, [{ id: 1 } as Device]);
    emit("request", request(1, { device_id: 1 }));
    expect(queryClient.getQueryState(keys.devices)?.isInvalidated).toBe(false);

    emit("request", request(2, { device_id: null }));
    expect(queryClient.getQueryState(keys.devices)?.isInvalidated).toBe(false);

    emit("request", request(3, { device_id: 7 }));
    expect(queryClient.getQueryState(keys.devices)?.isInvalidated).toBe(true);
  });

  it("creates nothing in the cache for lists nobody has loaded", () => {
    emit("request", request(1));
    expect(queryClient.getQueryData(keys.overview)).toBeUndefined();
    expect(queryClient.getQueryData(keys.requestList({}))).toBeUndefined();
  });
});

describe("instance, library and device events", () => {
  it("replaces a known Kodi instance in place and appends a new one, in the list and the overview", () => {
    const a = { id: 1, name: "Lounge" } as Instance;
    const b = { id: 2, name: "Bedroom" } as Instance;
    queryClient.setQueryData<Instance[]>(keys.instances, [a, b]);
    queryClient.setQueryData(keys.overview, overview({ instances: [a, b] }));

    emit("instance", { ...a, name: "Lounge room" });
    emit("instance", { id: 3, name: "Study" });

    const names = ["Lounge room", "Bedroom", "Study"];
    expect(queryClient.getQueryData<Instance[]>(keys.instances)?.map((i) => i.name)).toEqual(names);
    expect(queryClient.getQueryData<Overview>(keys.overview)?.instances.map((i) => i.name)).toEqual(names);
  });

  it("doesn't create an instance list that wasn't loaded", () => {
    emit("instance", { id: 1, name: "Lounge" });
    expect(queryClient.getQueryData(keys.instances)).toBeUndefined();
    expect(queryClient.getQueryData(keys.overview)).toBeUndefined();
  });

  it("updates a library and refreshes what is cached under it", () => {
    queryClient.setQueryData<Library[]>(keys.libraries, [{ id: 4, movies: 10 } as unknown as Library]);
    queryClient.setQueryData(keys.recent(4, 20), []);
    queryClient.setQueryData(keys.recent(5, 20), []);

    emit("library", { id: 4, movies: 11 });

    expect(queryClient.getQueryData<Library[]>(keys.libraries)).toEqual([{ id: 4, movies: 11 }]);
    expect(queryClient.getQueryState(keys.recent(4, 20))?.isInvalidated).toBe(true);
    expect(queryClient.getQueryState(keys.recent(5, 20))?.isInvalidated).toBe(false);
  });

  it("updates an Echo", () => {
    queryClient.setQueryData<Device[]>(keys.devices, [{ id: 1, name: "Kitchen" } as Device]);
    emit("device", { id: 1, name: "Kitchen Echo" });
    emit("device", { id: 2, name: "Bedroom" });
    expect(queryClient.getQueryData<Device[]>(keys.devices)?.map((d) => d.name)).toEqual(["Kitchen Echo", "Bedroom"]);
  });

  it("replaces the overview's attention list", () => {
    queryClient.setQueryData(
      keys.overview,
      overview({ attention: [{ kind: "old" }] as unknown as Overview["attention"] }),
    );
    emit("attention", [{ kind: "lwa" }]);
    expect(queryClient.getQueryData<Overview>(keys.overview)?.attention).toEqual([{ kind: "lwa" }]);
    emit("attention", []);
    expect(queryClient.getQueryData<Overview>(keys.overview)?.attention).toEqual([]);
  });
});

describe("deploy events", () => {
  const step = (s: string, state: string) => ({
    at: "2026-10-28T09:00:00+11:00",
    deploy_id: "d-1",
    locales: {},
    message: "",
    state,
    step: s,
  });

  const deploy = () => queryClient.getQueryData<Deploy>(keys.deploy("d-1"));

  it("follows a deploy it hasn't fetched yet, step by step", () => {
    emit("deploy", step("build", "started"));
    expect(deploy()).toMatchObject({ error: "", id: "d-1", state: "in_progress" });
    expect(deploy()?.events).toHaveLength(1);
    expect(deploy()?.events[0]).not.toHaveProperty("deploy_id");

    emit("deploy", step("build", "succeeded"));
    emit("deploy", step("simulate", "started"));
    expect(deploy()?.state).toBe("in_progress");
    expect(deploy()?.events.map((e) => `${e.step}:${e.state}`)).toEqual([
      "build:started",
      "build:succeeded",
      "simulate:started",
    ]);
  });

  it.each([
    ["simulate", "succeeded", "succeeded"],
    ["simulate", "skipped", "succeeded"],
    ["simulate", "failed", "failed"],
    ["upload", "failed", "failed"],
  ])("ends the deploy when %s is %s, and refreshes the skill and overview", (s, state, final) => {
    queryClient.setQueryData(keys.skill, {});
    queryClient.setQueryData(keys.overview, overview());
    queryClient.setQueryData<Deploy>(keys.deploy("d-1"), { error: "", events: [], id: "d-1", state: "in_progress" });

    emit("deploy", step(s, state));

    expect(deploy()?.state).toBe(final);
    expect(queryClient.getQueryState(keys.skill)?.isInvalidated).toBe(true);
    expect(queryClient.getQueryState(keys.overview)?.isInvalidated).toBe(true);
  });

  it("doesn't refresh the skill for steps that don't end the deploy", () => {
    queryClient.setQueryData(keys.skill, {});
    emit("deploy", step("upload", "succeeded"));
    expect(queryClient.getQueryState(keys.skill)?.isInvalidated).toBe(false);
  });
});

describe("event stream", () => {
  it("ignores malformed and non-object events", () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    const listener = vi.fn();
    const off = eventStream.on("device", listener);
    queryClient.setQueryData<Device[]>(keys.devices, []);

    act(() => {
      for (const source of FakeEventSource.instances) {
        source.dispatchEvent(new MessageEvent("device", { data: "{not json" }));
        source.dispatchEvent(new MessageEvent("device", { data: "42" }));
        source.dispatchEvent(new MessageEvent("device", { data: "null" }));
      }
    });

    expect(listener).not.toHaveBeenCalled();
    expect(queryClient.getQueryData(keys.devices)).toEqual([]);
    expect(warn).toHaveBeenCalledWith("encore: ignoring malformed event", "device");
    off();
  });

  it("calls listeners after the cache is updated, until they unsubscribe", () => {
    queryClient.setQueryData<Device[]>(keys.devices, []);
    const seen: unknown[] = [];
    const off = eventStream.on("device", (payload) => {
      seen.push([payload.id, queryClient.getQueryData<Device[]>(keys.devices)?.length]);
    });
    emit("device", { id: 1 });
    off();
    emit("device", { id: 2 });
    expect(seen).toEqual([[1, 1]]);
  });

  it("shares one connection and closes it when the last user leaves", () => {
    expect(FakeEventSource.open).toBe(1);
    const second = eventStream.acquire(queryClient);
    expect(FakeEventSource.open).toBe(1);

    second();
    expect(FakeEventSource.open).toBe(1);
    release();
    expect(FakeEventSource.open).toBe(0);

    release = eventStream.acquire(queryClient);
    expect(FakeEventSource.open).toBe(1);
  });
});

describe("useEventStream", () => {
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );

  it("reports the connection's status", async () => {
    release();
    const { result, unmount } = renderHook(() => useEventStream(), { wrapper });
    expect(result.current).toBe("connecting");
    await act(async () => {});
    expect(result.current).toBe("open");

    const source = FakeEventSource.instances[0];
    if (!source) throw new Error("no event source");
    act(() => source.onerror?.(new Event("error")));
    expect(result.current).toBe("connecting");

    source.readyState = 2;
    act(() => source.onerror?.(new Event("error")));
    expect(result.current).toBe("error");

    unmount();
    expect(FakeEventSource.open).toBe(0);
    release = eventStream.acquire(queryClient);
  });
});

describe("useServerEvent", () => {
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );

  it("calls the latest listener for its event only, and stops when unmounted", () => {
    const first = vi.fn();
    const latest = vi.fn();
    const { rerender, unmount } = renderHook(({ listener }) => useServerEvent("instance", listener), {
      initialProps: { listener: first },
      wrapper,
    });
    rerender({ listener: latest });

    emit("instance", { id: 1 });
    emit("device", { id: 2 });
    expect(first).not.toHaveBeenCalled();
    expect(latest).toHaveBeenCalledExactlyOnceWith({ id: 1 });

    unmount();
    emit("instance", { id: 3 });
    expect(latest).toHaveBeenCalledOnce();
  });
});
