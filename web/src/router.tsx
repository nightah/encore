import { MutationCache, QueryClient } from "@tanstack/react-query";
import { type RouterHistory, createRouter } from "@tanstack/react-router";

import { ApiError, errorMessage } from "@api/client";
import { notifyError, notifySuccess } from "@components/ui/Toast";
import i18n from "@i18n/index";
import { basePath } from "@src/basePath";
import { routeTree } from "@src/routeTree.gen";

/**
 * Mutation `meta`:
 * - `success`: toast shown when the mutation succeeds.
 * - `inline`: the caller shows errors next to the form, so no error toast.
 */
export interface MutationMeta extends Record<string, unknown> {
  success?: string;
  inline?: boolean;
}

export function createQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        refetchOnWindowFocus: false,
        retry: (count, error) => count < 2 && !(error instanceof ApiError && error.status >= 400 && error.status < 500),
        staleTime: 10_000,
      },
    },
    mutationCache: new MutationCache({
      onError: (error, _vars, _ctx, mutation) => {
        if (!mutation.meta?.inline) notifyError(i18n.t("toast.failed"), errorMessage(error));
      },
      onSuccess: (_data, _vars, _ctx, mutation) => {
        const message = mutation.meta?.success;
        if (message) notifySuccess(message);
      },
    }),
  });
}

export function createAppRouter(options: { queryClient: QueryClient; history?: RouterHistory }) {
  return createRouter({
    basepath: basePath(),
    context: { queryClient: options.queryClient },
    defaultPreload: "intent",
    defaultPreloadStaleTime: 0,
    routeTree,
    scrollRestoration: true,
    ...(options.history ? { history: options.history } : {}),
  });
}

declare module "@tanstack/react-router" {
  interface Register {
    router: ReturnType<typeof createAppRouter>;
  }
}

declare module "@tanstack/react-query" {
  interface Register {
    mutationMeta: MutationMeta;
  }
}
