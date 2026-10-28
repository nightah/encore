import { FetchInterceptor } from "@mswjs/interceptors/fetch";
import { InterceptorSource, defineNetwork } from "msw/experimental";

import { handlers } from "@mocks/handlers";

/**
 * The mock API for the tests: MSW answering the test page's own `fetch` calls, started in `src/test/setup.ts`.
 *
 * Not MSW's service worker, as `pnpm dev:mock` uses: the worker serves every test page in the browser, and passes the module loads of a page that hasn't started MSW yet through another test page. When that page closes first, the loads fail and the starting file can't import its tests.
 */
export const server = defineNetwork({
  handlers,
  onUnhandledFrame: "error",
  sources: [new InterceptorSource({ interceptors: [new FetchInterceptor()] })],
});
