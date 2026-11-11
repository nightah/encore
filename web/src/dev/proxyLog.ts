import type { LogErrorOptions, Logger, Plugin } from "vite";

// quietFor is how long without a refused request ends an outage, so the next refusal is reported again.
const quietFor = 10_000;

function isRefused(options?: LogErrorOptions): boolean {
  return (options?.error as NodeJS.ErrnoException | undefined)?.code === "ECONNREFUSED";
}

// quietRefusals wraps the dev server's logger so that requests the Go server refuses, as it does while air restarts it, are reported once per outage instead of once each with a stack trace.
// It keeps the logger's own error and warn, so the result's error can replace the logger's in place.
export function quietRefusals(logger: Logger, target: string, now: () => number = Date.now): Logger {
  const error = logger.error.bind(logger);
  const warn = logger.warn.bind(logger);
  let last = Number.NEGATIVE_INFINITY;

  return {
    ...logger,
    error(msg, options) {
      if (!isRefused(options)) {
        error(msg, options);

        return;
      }

      const t = now();
      if (t - last > quietFor) {
        warn(
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

// quietProxyRefusals quiets refused proxy requests in the dev server's own logger, which Vite builds from logLevel and clearScreen, from the command line or the config, and its proxy logs to when a request fails.
export function quietProxyRefusals(target: string, now: () => number = Date.now): Plugin {
  return {
    apply: "serve",
    configResolved(config) {
      config.logger.error = quietRefusals(config.logger, target, now).error;
    },
    name: "encore:quiet-proxy-refusals",
  };
}
