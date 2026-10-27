import { StrictMode } from "react";

import { createRoot } from "react-dom/client";

import "@i18n/index";
import { installErrorReporting } from "@lib/reportError";
import { installStaleModuleReload } from "@lib/staleReload";
import { App, readCspNonce } from "@src/App";
import { createAppRouter, createQueryClient } from "@src/router";
import { initTheme } from "@theme/store";
import "@styles/index.css";

async function main() {
  initTheme();
  installErrorReporting();
  installStaleModuleReload();

  // `vite --mode mock` serves the API from MSW in the browser. The condition is static, so production builds drop the mocks entirely.
  if (import.meta.env.MODE === "mock") {
    const { startMockWorker } = await import("@mocks/browser");
    await startMockWorker();
  }

  const queryClient = createQueryClient();
  const router = createAppRouter({ queryClient });

  const container = document.getElementById("root");
  if (!container) throw new Error("encore: #root element missing from index.html");

  createRoot(container).render(
    <StrictMode>
      <App queryClient={queryClient} router={router} nonce={readCspNonce()} />
    </StrictMode>,
  );
}

void main();
