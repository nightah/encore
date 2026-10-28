import { useEffect, useEffectEvent, useSyncExternalStore } from "react";

import { type InfiniteData, type QueryClient, useQueryClient } from "@tanstack/react-query";

import { buildUrl } from "@api/client";
import type {
  Attention,
  Deploy,
  DeployEvent,
  Device,
  Instance,
  Library,
  Overview,
  RequestPage,
  RequestSummary,
} from "@api/generated/model";
import { keys } from "@api/keys";
import type { RequestFilters } from "@api/requests";

/** How many recent requests the overview shows; the server sends the same number. */
export const RECENT_REQUESTS = 10;

/** The `deploy` event: one step of a deploy, with the deploy it belongs to. */
export interface DeployStreamEvent extends DeployEvent {
  deploy_id: string;
}

/** Event names and payloads of `GET /api/v1/events` (documented on the streamEvents operation; SSE payloads are not in the OpenAPI spec). */
export interface EventPayloads {
  request: RequestSummary;
  instance: Instance;
  library: Library;
  device: Device;
  deploy: DeployStreamEvent;
  attention: Attention[];
}

export type EventName = keyof EventPayloads;
export type EventPayload<E extends EventName> = EventPayloads[E];
const EVENT_NAMES: EventName[] = ["request", "instance", "library", "device", "deploy", "attention"];
export type StreamStatus = "connecting" | "open" | "error";

type Listener<E extends EventName> = (payload: EventPayload<E>) => void;

const FAILED_OUTCOMES = new Set(["error", "not_found"]);

function matchesFilters(item: RequestSummary, filters: RequestFilters): boolean {
  if (filters.device !== undefined && item.device_id !== filters.device) return false;
  if (filters.outcome && item.outcome !== filters.outcome) return false;
  if (filters.q && !item.heard.toLowerCase().includes(filters.q.toLowerCase())) return false;
  return true;
}

function upsert<T extends { id: number }>(list: T[], item: T): T[];
function upsert<T extends { id: number }>(list: T[] | undefined, item: T): T[] | undefined;
function upsert<T extends { id: number }>(list: T[] | undefined, item: T) {
  if (!list) return list;
  const index = list.findIndex((entry) => entry.id === item.id);
  if (index === -1) return [...list, item];
  const next = list.slice();
  next[index] = item;
  return next;
}

/** Applies one server event to the TanStack Query cache. */
function applyEvent<E extends EventName>(queryClient: QueryClient, name: E, payload: EventPayload<E>): void {
  switch (name) {
    case "request": {
      const item = payload as RequestSummary;
      for (const query of queryClient.getQueryCache().findAll({ queryKey: keys.requests })) {
        const filters = query.queryKey[2] as RequestFilters;
        if (!matchesFilters(item, filters)) continue;
        queryClient.setQueryData<InfiniteData<RequestPage>>(query.queryKey, (data) => {
          if (!data?.pages[0] || data.pages[0].items.some((r) => r.id === item.id)) return data;
          const [first, ...rest] = data.pages;
          return { ...data, pages: [{ ...first, items: [item, ...first.items] }, ...rest] };
        });
      }
      queryClient.setQueryData<Overview>(keys.overview, (overview) => {
        // The overview, like the server, leaves out the simulator's requests during deploy smoke tests.
        if (!overview || item.simulated || overview.recent_requests.some((r) => r.id === item.id)) return overview;
        return {
          ...overview,
          recent_requests: [item, ...overview.recent_requests].slice(0, RECENT_REQUESTS),
          requests_today: {
            ...overview.requests_today,
            failed: overview.requests_today.failed + (FAILED_OUTCOMES.has(item.outcome) ? 1 : 0),
            total: overview.requests_today.total + 1,
          },
        };
      });
      if (item.device_id !== null) {
        const known = queryClient.getQueryData<Device[]>(keys.devices)?.some((d) => d.id === item.device_id);
        if (!known) void queryClient.invalidateQueries({ queryKey: keys.devices });
      }
      break;
    }
    case "instance": {
      const item = payload as Instance;
      queryClient.setQueryData<Instance[]>(keys.instances, (list) => upsert(list, item));
      queryClient.setQueryData<Overview>(keys.overview, (overview) =>
        overview ? { ...overview, instances: upsert(overview.instances, item) } : overview,
      );
      break;
    }
    case "library": {
      const item = payload as Library;
      queryClient.setQueryData<Library[]>(keys.libraries, (list) => upsert(list, item));
      void queryClient.invalidateQueries({ queryKey: ["libraries", item.id] });
      break;
    }
    case "device": {
      const item = payload as Device;
      queryClient.setQueryData<Device[]>(keys.devices, (list) => upsert(list, item));
      break;
    }
    case "deploy": {
      const { deploy_id, ...event } = payload as DeployStreamEvent;
      // A load of this deploy still on its way was answered before this step, and would put the deploy back as it was; the next poll catches up instead.
      void queryClient.cancelQueries({ queryKey: keys.deploy(deploy_id) });
      queryClient.setQueryData<Deploy>(keys.deploy(deploy_id), (deploy) => {
        const base: Deploy = deploy ?? {
          error: "",
          events: [],
          id: deploy_id,
          state: "in_progress",
        };
        const finished =
          event.state === "failed"
            ? "failed"
            : event.step === "simulate" && event.state !== "started"
              ? "succeeded"
              : base.state;
        return { ...base, events: [...base.events, event], state: finished };
      });
      if (event.state === "failed" || (event.step === "simulate" && event.state !== "started")) {
        void queryClient.invalidateQueries({ queryKey: keys.skill });
        void queryClient.invalidateQueries({ queryKey: keys.overview });
      }
      break;
    }
    case "attention": {
      const list = payload as Attention[];
      queryClient.setQueryData<Overview>(keys.overview, (overview) =>
        overview ? { ...overview, attention: list } : overview,
      );
      break;
    }
  }
}

/** One shared EventSource for the whole app. The browser resumes with `Last-Event-ID` on reconnect; the server replays missed events. */
class EventStream {
  private source: EventSource | undefined;
  private refs = 0;
  // Set by acquire, the only way a connection (and so an event) starts.
  private queryClient!: QueryClient;
  private listeners = new Map<EventName, Set<Listener<EventName>>>();
  private statusListeners = new Set<(status: StreamStatus) => void>();
  status: StreamStatus = "connecting";

  acquire(queryClient: QueryClient): () => void {
    this.queryClient = queryClient;
    this.refs += 1;
    if (!this.source) this.open();
    return () => {
      this.refs -= 1;
      if (this.refs === 0) this.close();
    };
  }

  on<E extends EventName>(name: E, listener: Listener<E>): () => void {
    let set = this.listeners.get(name);
    if (!set) {
      set = new Set();
      this.listeners.set(name, set);
    }
    set.add(listener as Listener<EventName>);
    return () => set.delete(listener as Listener<EventName>);
  }

  onStatus(listener: (status: StreamStatus) => void): () => void {
    this.statusListeners.add(listener);
    return () => this.statusListeners.delete(listener);
  }

  private setStatus(status: StreamStatus) {
    this.status = status;
    for (const listener of this.statusListeners) listener(status);
  }

  private open() {
    if (typeof EventSource === "undefined") return;
    const source = new EventSource(buildUrl("/events"), { withCredentials: true });
    this.source = source;
    this.setStatus("connecting");
    source.onopen = () => this.setStatus("open");
    source.onerror = () => this.setStatus(source.readyState === 2 ? "error" : "connecting");
    for (const name of EVENT_NAMES) {
      source.addEventListener(name, (event) => this.dispatch(name, (event as MessageEvent).data));
    }
  }

  private dispatch(name: EventName, raw: unknown) {
    let data: EventPayload<EventName>;
    try {
      data = JSON.parse(String(raw));
    } catch {
      console.warn("encore: ignoring malformed event", name);
      return;
    }
    if (typeof data !== "object" || data === null) return;
    applyEvent(this.queryClient, name, data);
    for (const listener of this.listeners.get(name) ?? []) listener(data);
  }

  private close() {
    this.source?.close();
    this.source = undefined;
  }

  /** Test helper: drop the connection and listeners. */
  reset() {
    this.close();
    this.refs = 0;
    this.listeners.clear();
    this.statusListeners.clear();
  }
}

export const eventStream = new EventStream();

const subscribeStatus = (onChange: () => void) => eventStream.onStatus(onChange);
const getStatus = () => eventStream.status;

/** Keeps the event stream open while mounted and returns its connection status. */
export function useEventStream(): StreamStatus {
  const queryClient = useQueryClient();
  useEffect(() => eventStream.acquire(queryClient), [queryClient]);
  return useSyncExternalStore(subscribeStatus, getStatus, getStatus);
}

/** Calls `listener` for each event of one type (the cache is already updated by then). */
export function useServerEvent<E extends EventName>(name: E, listener: Listener<E>): void {
  const queryClient = useQueryClient();
  const onEvent = useEffectEvent(listener);
  useEffect(() => {
    const release = eventStream.acquire(queryClient);
    const off = eventStream.on(name, (payload) => onEvent(payload));
    return () => {
      off();
      release();
    };
  }, [name, queryClient]);
}
