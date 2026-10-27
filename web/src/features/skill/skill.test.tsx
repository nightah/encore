import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import { DEPLOY_STEPS } from "@features/skill/timeline";
import { getDb } from "@mocks/handlers";
import { FakeEventSource } from "@test/eventSource";
import { recordRequests, renderApp } from "@test/utils";

describe("Alexa skill page", () => {
  it("shows the deployment timeline, locale builds and model size", async () => {
    renderApp("/alexa-skill");

    expect(await screen.findByRole("heading", { level: 1, name: "Alexa skill" })).toBeInTheDocument();
    const status = await screen.findByRole("region", { name: "Deployment status" });
    expect(within(status).getByText(/Smoke test passed/)).toBeInTheDocument();
    expect(within(status).getByText("en-AU built")).toBeInTheDocument();

    const table = screen.getByRole("table", { name: "Interaction model by locale" });
    expect(within(table).getByText("en-AU")).toBeInTheDocument();
    expect(within(table).getByText("Succeeded")).toBeInTheDocument();
    expect(within(table).getByText("3,480")).toBeInTheDocument();

    expect(screen.getByText("1,206 titles")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "en-AU" })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("button", { name: "de-DE" })).toHaveAttribute("aria-pressed", "false");
    expect(screen.getByRole("heading", { name: "Titles in the model" })).toBeInTheDocument();
    expect(within(table).getByRole("meter", { name: /^en-AU: .* of 1.5 MB used$/ })).toBeInTheDocument();
  });

  it("saves locale changes with the CSRF token", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/alexa-skill");

    await user.click(await screen.findByRole("button", { name: "en-GB" }));

    await waitFor(() => expect(log.some((r) => r.method === "PUT")).toBe(true));
    const put = log.find((r) => r.method === "PUT");
    expect(put?.path).toBe("/skill/settings");
    expect(put?.csrf).toBe("mock-csrf-token");
    expect(put?.body).toMatchObject({ invocation: "encore", locales_enabled: ["en-AU", "en-GB"] });
    await waitFor(() => expect(getDb().skill.locales_enabled).toEqual(["en-AU", "en-GB"]));
  });

  it("saves what the skill can play and keeps at least one", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/alexa-skill");

    await user.click(await screen.findByRole("button", { name: "Music" }));
    await waitFor(() => expect(getDb().skill.content).toEqual(["movies", "tv"]));
    const put = log.find((r) => r.method === "PUT" && r.path === "/skill/settings");
    expect(put?.body).toMatchObject({ content: ["movies", "tv"] });

    await user.click(await screen.findByRole("button", { name: "TV shows", pressed: true }));
    await waitFor(() => expect(getDb().skill.content).toEqual(["movies"]));
    await user.click(await screen.findByRole("button", { name: "Movies", pressed: true }));
    expect(await screen.findByText("Keep at least one of movies, TV shows or music.")).toBeInTheDocument();
    expect(getDb().skill.content).toEqual(["movies"]);
  });

  it("says when only new library titles are waiting for a deploy", async () => {
    getDb().skill = { ...getDb().skill, needs_deploy: true, update_reason: "library" };
    renderApp("/alexa-skill");
    expect(await screen.findByText(/New library titles since the last deploy/)).toBeInTheDocument();
    expect(screen.queryByText(/Settings changed since the last deploy/)).not.toBeInTheDocument();
  });

  it("refuses to disable the last enabled locale", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/alexa-skill");

    await user.click(await screen.findByRole("button", { name: "en-AU" }));

    expect(await screen.findByText("Keep at least one locale enabled.")).toBeInTheDocument();
    expect(log.some((r) => r.method === "PUT")).toBe(false);
  });

  it("follows a deploy already in progress when the page opens", async () => {
    const db = getDb();
    if (db.skill.last_deploy) db.skill.last_deploy = { ...db.skill.last_deploy, id: "d-running", state: "in_progress" };
    db.deploys["d-running"] = { error: "", events: [], id: "d-running", state: "in_progress" };
    const log = recordRequests();
    renderApp("/alexa-skill");

    await waitFor(() => expect(log.some((r) => r.method === "GET" && r.path === "/skill/deploy/d-running")).toBe(true));
    expect(await screen.findByRole("button", { name: /Deploying/ })).toBeDisabled();
  });

  it("redeploys and follows progress from the event stream", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/alexa-skill");

    await user.click(await screen.findByRole("button", { name: "Redeploy" }));

    await waitFor(() => expect(log.some((r) => r.method === "GET" && r.path.startsWith("/skill/deploy/"))).toBe(true));
    const post = log.find((r) => r.method === "POST" && r.path === "/skill/deploy");
    expect(post?.csrf).toBe("mock-csrf-token");
    expect(screen.getByRole("button", { name: /Deploying/ })).toBeDisabled();

    const deployId = Object.keys(getDb().deploys)[0] ?? "";
    // Let the initial GET settle before streaming events on top of it.
    await screen.findByText(/^Deploying: /);
    for (const step of DEPLOY_STEPS) {
      FakeEventSource.emit("deploy", {
        at: new Date().toISOString(),
        deploy_id: deployId,
        message: `Finished ${step}`,
        state: "succeeded",
        step,
      });
    }

    expect(await screen.findByText(/Deploy finished/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Redeploy" })).toBeEnabled();
  });

  it("asks before disconnecting from Amazon", async () => {
    const user = userEvent.setup();
    renderApp("/alexa-skill");

    await user.click(await screen.findByRole("button", { name: "Disconnect" }));
    const dialog = await screen.findByRole("dialog", { name: "Disconnect from Amazon?" });
    await user.click(within(dialog).getByRole("button", { name: "Disconnect" }));

    await waitFor(() => expect(getDb().skill.amazon.connected).toBe(false));
    expect(await screen.findByRole("link", { name: "Connect in the setup wizard" })).toBeInTheDocument();
  });
});
