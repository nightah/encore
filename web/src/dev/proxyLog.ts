import type { LogErrorOptions, Logger } from "vite";

// quietFor is how long without a refused request ends an outage, so the next refusal is reported again.
const quietFor = 10_000;

function isRefused(options?: LogErrorOptions): boolean {
  return (options?.error as NodeJS.ErrnoException | undefined)?.code === "ECONNREFUSED";
}

// quietRefusals wraps the dev server's logger so that requests the Go server refuses, as it does while air restarts it, are reported once per outage instead of once each with a stack trace.
export function quietRefusals(logger: Logger, target: string, now: () => number = Date.now): Logger {
  let last = Number.NEGATIVE_INFINITY;

  return {
    ...logger,
    error(msg, options) {
      if (!isRefused(options)) {
        logger.error(msg, options);

        return;
      }

      const t = now();
      if (t - last > quietFor) {
        logger.warn(
          `encore: the Go server at ${target} isn't answering (air may be restarting it); API requests fail until it's back`,
          {
            timestamp: true,
          },
        );
      }

      last = t;
    },
  };
}
