/**
 * In-memory EventSource for tests. `FakeEventSource.emit("request", payload)` delivers an event to every open source, like the server's SSE stream would.
 */
export class FakeEventSource extends EventTarget {
  static instances: FakeEventSource[] = [];
  static lastId = 0;

  readonly url: string;
  readonly withCredentials: boolean;
  readyState = 0;
  onopen: ((event: Event) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;
  onmessage: ((event: MessageEvent) => void) | null = null;

  constructor(url: string | URL, init?: EventSourceInit) {
    super();
    this.url = String(url);
    this.withCredentials = !!init?.withCredentials;
    FakeEventSource.instances.push(this);
    queueMicrotask(() => {
      if (this.readyState === 2) return;
      this.readyState = 1;
      this.onopen?.(new Event("open"));
    });
  }

  close() {
    this.readyState = 2;
    FakeEventSource.instances = FakeEventSource.instances.filter((s) => s !== this);
  }

  static emit(event: string, data: unknown) {
    FakeEventSource.lastId += 1;
    for (const source of FakeEventSource.instances) {
      source.dispatchEvent(
        new MessageEvent(event, {
          data: JSON.stringify(data),
          lastEventId: String(FakeEventSource.lastId),
        }),
      );
    }
  }

  static get open(): number {
    return FakeEventSource.instances.length;
  }

  static reset() {
    FakeEventSource.instances = [];
  }
}
