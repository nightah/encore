/** How long a test's last requests get to finish once it ends; only a deliberately unanswered request should take this long. */
const SETTLE_BUDGET_MS = 1000;

/** Event loop turns with nothing in flight before the app counts as idle: an answer can start a follow-up request a few turns later (a refetch, the next step of a chain). */
const IDLE_TURNS = 3;

const pending = new Set<Promise<unknown>>();
let controller = new AbortController();

const nextTurn = () => new Promise<void>((resolve) => setTimeout(resolve, 0));

/**
 * Tracks every request the app makes, so a test can't end with one still on its way. Its answer would otherwise land in the next test: an error toast on the next test's page, or a follow-up request against the next test's freshly reset mock database. Call once, after the mock API is listening (MSW's Node interceptor replaces `fetch` too).
 */
export function trackRequests() {
  const fetch = globalThis.fetch;
  globalThis.fetch = (input, init) => {
    const signal = init?.signal ? AbortSignal.any([init.signal, controller.signal]) : controller.signal;
    const response = fetch(input, { ...init, signal });
    const settled = response.then(
      () => undefined,
      () => undefined,
    );
    pending.add(settled);
    void settled.then(() => pending.delete(settled));
    return response;
  };
}

/** Waits for the app's requests to finish and the app to go quiet, giving up after a second on a request a test holds back on purpose. */
export async function settleRequests() {
  const deadline = performance.now() + SETTLE_BUDGET_MS;
  let idle = 0;
  while (idle < IDLE_TURNS && performance.now() < deadline) {
    if (pending.size === 0) {
      idle++;
      await nextTurn();
      continue;
    }
    idle = 0;
    let timer: ReturnType<typeof setTimeout> | undefined;
    await Promise.race([
      Promise.all(pending),
      new Promise((resolve) => {
        timer = setTimeout(resolve, Math.max(0, deadline - performance.now()));
      }),
    ]);
    clearTimeout(timer);
  }
}

/** Cancels the requests still unanswered and lets their error handlers run; call once the page is unmounted, so there's nowhere for them to show. */
export async function cancelRequests() {
  controller.abort();
  controller = new AbortController();
  await nextTurn();
}
