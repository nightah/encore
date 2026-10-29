import { createMemoryHistory } from "@tanstack/react-router";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it } from "vitest";

import { NAV_ITEMS } from "@components/nav";
import { saveSetupProgress } from "@lib/setupProgress";
import { getDb } from "@mocks/handlers";
import { App } from "@src/App";
import { createAppRouter, createQueryClient } from "@src/router";
import { server } from "@test/server";

function renderAt(path: string) {
  const queryClient = createQueryClient();
  const router = createAppRouter({
    history: createMemoryHistory({ initialEntries: [path] }),
    queryClient,
  });
  render(<App queryClient={queryClient} router={router} />);
  return router;
}

describe("AppShell", () => {
  it("renders every nav item and marks the current page", async () => {
    renderAt("/kodi");

    expect(await screen.findByRole("heading", { level: 1, name: "Kodi instances" })).toBeInTheDocument();
    const nav = screen.getByRole("navigation", { name: "Primary" });
    const links = within(nav).getAllByRole("link");
    expect(links.map((link) => link.textContent)).toEqual(NAV_ITEMS.map((item) => item.label));

    expect(within(nav).getByRole("link", { name: "Kodi" })).toHaveAttribute("aria-current", "page");
    expect(within(nav).getByRole("link", { name: "Overview" })).not.toHaveAttribute("aria-current");
  });

  it("moves aria-current when navigating", async () => {
    const user = userEvent.setup();
    renderAt("/kodi");
    const nav = await screen.findByRole("navigation", { name: "Primary" });

    await user.click(within(nav).getByRole("link", { name: "Settings" }));

    expect(await screen.findByRole("heading", { level: 1, name: "Settings" })).toBeInTheDocument();
    expect(within(nav).getByRole("link", { name: "Settings" })).toHaveAttribute("aria-current", "page");
    expect(within(nav).getByRole("link", { name: "Kodi" })).not.toHaveAttribute("aria-current");
  });

  it("renders /setup outside the shell", async () => {
    renderAt("/setup");

    // The configured sample already has an admin, so the wizard resumes at step 2.
    expect(await screen.findByRole("heading", { level: 1, name: "Tell us your public address" })).toBeInTheDocument();
    expect(screen.queryByRole("navigation", { name: "Primary" })).not.toBeInTheDocument();
  });

  it("names who is signed in and the release version", async () => {
    renderAt("/");
    expect(await screen.findByText("v0.1.0")).toBeInTheDocument();
    expect(screen.getByText("admin")).toHaveAttribute("data-sensitive", "admin");
  });

  it("shows a development build's version as it is", async () => {
    server.use(http.get("*/api/v1/healthz", () => HttpResponse.json({ status: "ok", version: "dev" })));
    renderAt("/");
    expect(await screen.findByText("dev")).toBeInTheDocument();
    expect(screen.queryByText("vdev")).not.toBeInTheDocument();
  });

  it("leaves out the version line, gap and all, when the server reports none", async () => {
    let checked = false;
    server.use(
      http.get("*/api/v1/healthz", () => {
        checked = true;
        return HttpResponse.json({ status: "ok" });
      }),
    );
    renderAt("/");
    const footer = (await screen.findByRole("button", { name: "Sign out" })).parentElement as HTMLElement;
    await waitFor(() => expect(checked).toBe(true));
    expect(footer).toHaveTextContent(/^Signed in as admin\s*Sign out$/);
    // An empty line would still take a gap in the column.
    expect(footer.querySelectorAll(":scope > :empty")).toHaveLength(0);
  });

  it("offers no sign-out when the reverse proxy signs people in", async () => {
    getDb().authMode = "header";
    renderAt("/");
    await screen.findByRole("heading", { level: 1, name: "Overview" });
    expect(screen.queryByRole("button", { name: "Sign out" })).not.toBeInTheDocument();
  });

  it("offers to continue an unfinished setup from any page", async () => {
    const user = userEvent.setup();
    saveSetupProgress({ done: [1, 2], skipped: [], step: 3 });
    const router = renderAt("/kodi");
    const banner = await screen.findByText(/step 3/);
    expect(banner).toBeInTheDocument();

    await user.click(screen.getByRole("link", { name: /Continue setup/ }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/setup"));
  });
});
