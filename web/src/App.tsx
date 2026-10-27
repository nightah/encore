import { CSPProvider } from "@base-ui/react/csp-provider";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";

import { ToastProvider } from "@components/ui/Toast";
import type { createAppRouter } from "@src/router";

/** Reads the per-request nonce the Go server injects into <meta property="csp-nonce">. */
export function readCspNonce(): string | undefined {
  const content = document.querySelector<HTMLMetaElement>('meta[property="csp-nonce"]')?.getAttribute("content");
  // Unrendered template placeholder (e.g. `vite dev`) means there is no nonce.
  return content && !content.includes("{{") ? content : undefined;
}

interface AppProps {
  queryClient: QueryClient;
  router: ReturnType<typeof createAppRouter>;
  nonce?: string | undefined;
}

export function App({ queryClient, router, nonce }: AppProps) {
  return (
    <CSPProvider nonce={nonce}>
      <QueryClientProvider client={queryClient}>
        <ToastProvider>
          <RouterProvider router={router} />
        </ToastProvider>
      </QueryClientProvider>
    </CSPProvider>
  );
}
