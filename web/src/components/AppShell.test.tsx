import { createMemoryHistory } from "@tanstack/react-router";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import { NAV_ITEMS } from "@components/nav";
import { App } from "@src/App";
import { createAppRouter, createQueryClient } from "@src/router";

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
});
