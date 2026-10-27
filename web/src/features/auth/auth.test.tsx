import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import { getDb, resetDb } from "@mocks/handlers";
import { failWith, recordRequests, renderApp } from "@test/utils";

describe("auth gating", () => {
  it("sends a signed-out visitor to the login page and back after signing in", async () => {
    getDb().authenticated = false;
    const user = userEvent.setup();
    const { router } = renderApp("/kodi");

    await screen.findByRole("heading", { level: 1, name: "Sign in" });
    expect(router.state.location.pathname).toBe("/login");
    expect(router.state.location.search).toMatchObject({ redirect: "/kodi" });

    await user.type(screen.getByLabelText("Username"), "admin");
    await user.type(screen.getByLabelText("Password"), "wrong");
    await user.click(screen.getByRole("button", { name: "Sign in" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("the username or password is incorrect");

    await user.clear(screen.getByLabelText("Password"));
    await user.type(screen.getByLabelText("Password"), "correct-horse-battery");
    await user.click(screen.getByRole("button", { name: "Sign in" }));

    await screen.findByRole("heading", { level: 1, name: "Kodi instances" });
    expect(router.state.location.pathname).toBe("/kodi");
  });

  it("validates the login form before calling the server", async () => {
    getDb().authenticated = false;
    const user = userEvent.setup();
    const requests = recordRequests();
    renderApp("/login");
    await screen.findByRole("heading", { level: 1, name: "Sign in" });
    await user.click(screen.getByRole("button", { name: "Sign in" }));
    expect(await screen.findByText("Enter your username.")).toBeInTheDocument();
    expect(screen.getByText("Enter your password.")).toBeInTheDocument();
    expect(requests.some((r) => r.path === "/auth/login")).toBe(false);
  });

  it("explains a lockout", async () => {
    getDb().authenticated = false;
    failWith("post", "/auth/login", 429, "Too many attempts.");
    const user = userEvent.setup();
    renderApp("/login");
    await screen.findByRole("heading", { level: 1, name: "Sign in" });
    await user.type(screen.getByLabelText("Username"), "admin");
    await user.type(screen.getByLabelText("Password"), "whatever");
    await user.click(screen.getByRole("button", { name: "Sign in" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Too many failed attempts. Wait a few minutes and try again.",
    );
  });

  it("offers single sign-on in OIDC mode", async () => {
    const db = getDb();
    db.authenticated = false;
    db.authMode = "oidc";
    renderApp("/login");
    const button = await screen.findByRole("link", { name: "Sign in with single sign-on" });
    expect(button).toHaveAttribute("href", "/api/v1/auth/oidc/login");
    expect(screen.queryByLabelText("Password")).not.toBeInTheDocument();
  });

  it("sends first runs to the setup wizard, even from the login page", async () => {
    resetDb("fresh");
    const { router } = renderApp("/login");
    await screen.findByRole("heading", { level: 1, name: "Create the admin account" });
    expect(router.state.location.pathname).toBe("/setup");
  });

  it("leaves the login page when already signed in", async () => {
    const { router } = renderApp("/login");
    await screen.findByRole("heading", { level: 1, name: "Overview" });
    expect(router.state.location.pathname).toBe("/");
  });

  it("signs out from the rail", async () => {
    const user = userEvent.setup();
    const requests = recordRequests();
    const { router } = renderApp("/");
    expect(await screen.findByText(/Signed in as/)).toHaveTextContent("Signed in as admin");
    await user.click(screen.getByRole("button", { name: "Sign out" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"));
    const logout = requests.find((r) => r.path === "/auth/logout");
    expect(logout?.csrf).toBe("mock-csrf-token");
  });
});
