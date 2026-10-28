import { QueryClientProvider, useMutation } from "@tanstack/react-query";
import { createMemoryHistory } from "@tanstack/react-router";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it } from "vitest";

import { ApiError } from "@api/client";
import { ToastProvider } from "@components/ui/Toast";
import { getDb } from "@mocks/handlers";
import { App, readCspNonce } from "@src/App";
import { basePath } from "@src/basePath";
import { type MutationMeta, createAppRouter, createQueryClient } from "@src/router";

function setBase(href: string | undefined) {
  for (const base of document.head.querySelectorAll("base")) base.remove();
  if (href === undefined) return;
  const base = document.createElement("base");
  base.href = href;
  document.head.append(base);
}

afterEach(() => {
  setBase(undefined);
  for (const meta of document.head.querySelectorAll("meta[property='csp-nonce']")) meta.remove();
});

describe("basePath", () => {
  it.each([
    [undefined, "/"],
    ["/", "/"],
    ["/encore/", "/encore/"],
    ["/encore/index.html", "/encore/"],
    ["/media/encore", "/media/"],
    ["http://localhost:3000/a/b/", "/a/b/"],
  ])("is %s's directory: %s", (href, expected) => {
    setBase(href);
    expect(basePath()).toBe(expected);
  });
});

describe("readCspNonce", () => {
  function setNonce(content: string) {
    const meta = document.createElement("meta");
    meta.setAttribute("property", "csp-nonce");
    meta.setAttribute("content", content);
    document.head.append(meta);
  }

  it("reads the nonce the server injected", () => {
    setNonce("r4nd0m==");
    expect(readCspNonce()).toBe("r4nd0m==");
  });

  it("has none without the tag, with an empty one or with the unrendered template", () => {
    expect(readCspNonce()).toBeUndefined();
    setNonce("");
    expect(readCspNonce()).toBeUndefined();
    document.head.querySelector("meta[property='csp-nonce']")?.remove();
    setNonce("{{ .CSPNonce }}");
    expect(readCspNonce()).toBeUndefined();
  });
});

describe("createQueryClient", () => {
  const retry = createQueryClient().getDefaultOptions().queries?.retry as (count: number, error: unknown) => boolean;

  it("retries server errors and lost connections twice", () => {
    for (const error of [new ApiError("down", 503), new ApiError("offline", 0), new TypeError("Failed to fetch")]) {
      expect(retry(0, error)).toBe(true);
      expect(retry(1, error)).toBe(true);
      expect(retry(2, error)).toBe(false);
    }
  });

  it("never retries a client error, which would fail the same way", () => {
    for (const status of [400, 401, 403, 404, 409, 422, 429, 499]) {
      expect(retry(0, new ApiError("no", status))).toBe(false);
    }
    expect(retry(0, new ApiError("server", 500))).toBe(true);
  });

  it("doesn't refetch on window focus", () => {
    expect(createQueryClient().getDefaultOptions().queries?.refetchOnWindowFocus).toBe(false);
  });
});

function Trigger({ meta, fail }: { meta?: MutationMeta; fail?: unknown }) {
  const mutation = useMutation({
    ...(meta ? { meta } : {}),
    mutationFn: async () => {
      if (fail) throw fail;
      return "ok";
    },
  });
  return (
    <button type="button" onClick={() => mutation.mutate()}>
      Run
    </button>
  );
}

describe("mutation toasts", () => {
  function renderTrigger(props: { meta?: MutationMeta; fail?: unknown }) {
    render(
      <QueryClientProvider client={createQueryClient()}>
        <ToastProvider>
          <Trigger {...props} />
        </ToastProvider>
      </QueryClientProvider>,
    );
    return userEvent.setup();
  }

  it("shows the success message a mutation names", async () => {
    const user = renderTrigger({ meta: { success: "Kodi instance saved." } });
    await user.click(screen.getByRole("button", { name: "Run" }));
    expect(await screen.findByText("Kodi instance saved.")).toBeInTheDocument();
  });

  it("shows nothing for a success without a message", async () => {
    const user = renderTrigger({});
    await user.click(screen.getByRole("button", { name: "Run" }));
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(screen.queryByText("That didn't work")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Dismiss" })).not.toBeInTheDocument();
  });

  it("shows a failure with the server's explanation", async () => {
    const user = renderTrigger({ fail: new ApiError("x", 409, { detail: "That room already exists." }) });
    await user.click(screen.getByRole("button", { name: "Run" }));
    // Base UI announces an urgent toast as an alert.
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("That didn't work");
    expect(alert).toHaveTextContent("That room already exists.");
  });

  it("leaves failures of inline forms to the form", async () => {
    const user = renderTrigger({
      fail: new Error("Shown next to the field."),
      meta: { inline: true, success: "Saved." },
    });
    await user.click(screen.getByRole("button", { name: "Run" }));
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(screen.queryByText("Shown next to the field.")).not.toBeInTheDocument();
    expect(screen.queryByText("Saved.")).not.toBeInTheDocument();
  });
});

describe("createAppRouter", () => {
  it("serves the app under the base path the server injected", async () => {
    setBase("/encore/");
    getDb().authenticated = false;
    const queryClient = createQueryClient();
    const router = createAppRouter({
      history: createMemoryHistory({ initialEntries: ["/encore/login"] }),
      queryClient,
    });
    render(<App queryClient={queryClient} router={router} nonce="abc" />);

    expect(await screen.findByRole("heading", { level: 1, name: "Sign in" })).toBeInTheDocument();
    expect(router.basepath).toBe("/encore/");
    expect(router.state.location.pathname).toBe("/login");
  });
});
