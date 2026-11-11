import type { LogErrorOptions, Logger } from "vite";
import { describe, expect, it } from "vitest";

import { quietProxyRefusals, quietRefusals } from "@src/dev/proxyLog.ts";

function fakeLogger() {
  const calls: { level: string; msg: string }[] = [];
  const logger = {
    clearScreen: () => {},
    error: (msg: string, _options?: LogErrorOptions) => calls.push({ level: "error", msg }),
    hasErrorLogged: () => false,
    hasWarned: false,
    info: (msg: string) => calls.push({ level: "info", msg }),
    warn: (msg: string) => calls.push({ level: "warn", msg }),
    warnOnce: (msg: string) => calls.push({ level: "warn", msg }),
  } satisfies Logger;

  return { calls, logger };
}

function refused(): LogErrorOptions {
  return { error: Object.assign(new Error("connect ECONNREFUSED 127.0.0.1:8080"), { code: "ECONNREFUSED" }) };
}

// While air restarts the Go server, every request the dev server proxies is refused, and Vite logged each with a stack trace: one line per outage says what is happening instead.
describe("quietRefusals", () => {
  it("says once that the Go server isn't answering, until it has been quiet a while", () => {
    let now = 0;
    const { calls, logger } = fakeLogger();
    const log = quietRefusals(logger, "http://127.0.0.1:8080", () => now);

    log.error("http proxy error: /api/v1/skill", refused());
    now = 1_000;
    log.error("http proxy error: /api/v1/auth/state", refused());
    now = 9_000;
    log.error("http proxy error: /api/v1/healthz", refused());

    expect(calls).toHaveLength(1);
    expect(calls[0]?.level).toBe("warn");
    expect(calls[0]?.msg).toContain("http://127.0.0.1:8080");

    now = 30_000;
    log.error("http proxy error: /api/v1/healthz", refused());
    expect(calls).toHaveLength(2);
  });

  it("passes every other error through", () => {
    const { calls, logger } = fakeLogger();
    const log = quietRefusals(logger, "http://127.0.0.1:8080", () => 0);

    log.error("http proxy error: /api/v1/skill", { error: new Error("socket hang up") });
    log.error("something else");

    expect(calls.map((c) => c.msg)).toEqual(["http proxy error: /api/v1/skill", "something else"]);
  });

  // The plugin wraps the logger Vite built from its own options (logLevel, clearScreen) in place, so those options still apply.
  it("wraps the dev server's own logger in place", () => {
    let now = 0;
    const { calls, logger } = fakeLogger();
    const plugin = quietProxyRefusals("http://127.0.0.1:8080", () => now);
    const configResolved = plugin.configResolved as (config: { logger: Logger }) => void;

    configResolved({ logger });

    logger.error("http proxy error: /api/v1/skill", refused());
    now = 1_000;
    logger.error("http proxy error: /api/v1/skill", refused());
    logger.error("something else");

    expect(calls.map((c) => c.level)).toEqual(["warn", "error"]);
    expect(plugin.apply).toBe("serve");
  });
});
