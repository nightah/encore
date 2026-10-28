import type { QueryClient } from "@tanstack/react-query";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { DEPLOY_STEPS } from "@features/skill/timeline";
import { getDb } from "@mocks/handlers";
import { server } from "@mocks/node";
import { FakeEventSource } from "@test/eventSource";
import { type RecordedRequest, failWith, findEnabledButton, recordRequests, renderApp } from "@test/utils";

/** Waits until the page has loaded the deploy it just started, and returns its ID. The mock API doesn't record the steps a test streams, so a load landing after them would put the deploy back as the API has it. */
async function followedDeploy(log: RecordedRequest[], queryClient: QueryClient) {
  await waitFor(() => expect(log.some((r) => r.method === "GET" && r.path.startsWith("/skill/deploy/"))).toBe(true));
  await waitFor(() => expect(queryClient.isFetching({ queryKey: ["deploy"] })).toBe(0));
  return Object.keys(getDb().deploys)[0] ?? "";
}

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

  it("measures each locale's model against the limit Amazon sets", async () => {
    const size = getDb().skill.model_size;
    if (!size) throw new Error("no model size");
    size.limit_bytes = 2_000_000;
    renderApp("/alexa-skill");

    const table = await screen.findByRole("table", { name: "Interaction model by locale" });
    expect(within(table).getByRole("meter", { name: /^en-AU: .* of 2 MB used$/ })).toBeInTheDocument();
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

    await user.click(await findEnabledButton({ name: "Music" }));
    await waitFor(() => expect(getDb().skill.content).toEqual(["movies", "tv"]));
    const put = log.find((r) => r.method === "PUT" && r.path === "/skill/settings");
    expect(put?.body).toMatchObject({ content: ["movies", "tv"] });

    await user.click(await findEnabledButton({ name: "TV shows", pressed: true }));
    await waitFor(() => expect(getDb().skill.content).toEqual(["movies"]));
    await user.click(await findEnabledButton({ name: "Movies", pressed: true }));
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
    const { queryClient } = renderApp("/alexa-skill");

    await user.click(await findEnabledButton({ name: "Redeploy" }));

    const deployId = await followedDeploy(log, queryClient);
    const post = log.find((r) => r.method === "POST" && r.path === "/skill/deploy");
    expect(post?.csrf).toBe("mock-csrf-token");
    expect(screen.getByRole("button", { name: /Deploying/ })).toBeDisabled();
    expect(screen.getByText(/^Deploying: /)).toBeInTheDocument();
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

  it("keeps Amazon connected when disconnecting is cancelled", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/alexa-skill");

    await user.click(await screen.findByRole("button", { name: "Disconnect" }));
    const dialog = await screen.findByRole("dialog", { name: "Disconnect from Amazon?" });
    await user.click(within(dialog).getByRole("button", { name: "Cancel" }));

    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(log.some((r) => r.method === "DELETE")).toBe(false);
    expect(getDb().skill.amazon.connected).toBe(true);
  });

  it("keeps Amazon connected when the disconnect dialog is closed with Escape", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/alexa-skill");

    await user.click(await screen.findByRole("button", { name: "Disconnect" }));
    await screen.findByRole("dialog", { name: "Disconnect from Amazon?" });
    await user.keyboard("{Escape}");

    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(log.some((r) => r.method === "DELETE")).toBe(false);
    expect(getDb().skill.amazon.connected).toBe(true);
  });

  it("can't deploy before Amazon is connected", async () => {
    getDb().skill.amazon = { ...getDb().skill.amazon, connected: false };
    renderApp("/alexa-skill");
    await screen.findByRole("region", { name: "Deployment status" });
    expect(screen.getByRole("button", { name: "Redeploy" })).toBeDisabled();
    expect(screen.getByRole("link", { name: "Connect in the setup wizard" })).toHaveAttribute("href", "/setup");
  });

  it.each([
    [false, { force: true }],
    [true, {}],
  ])("forces a redeploy only when nothing changed (changes waiting: %s)", async (needsDeploy, body) => {
    getDb().skill.needs_deploy = needsDeploy;
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/alexa-skill");

    await user.click(await findEnabledButton({ name: "Redeploy" }));

    await waitFor(() => expect(log.find((r) => r.method === "POST")?.body).toEqual(body));
  });

  it("points to the timeline for a deploy that fails without saying why", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    const { queryClient } = renderApp("/alexa-skill");
    await user.click(await findEnabledButton({ name: "Redeploy" }));
    const deployId = await followedDeploy(log, queryClient);

    FakeEventSource.emit("deploy", {
      at: new Date().toISOString(),
      deploy_id: deployId,
      message: "",
      state: "failed",
      step: "build",
    });

    expect(await screen.findByText("Deploy failed: see the timeline for the failed step")).toBeInTheDocument();
  });

  it("shows a dash for Amazon details it doesn't know", async () => {
    const db = getDb();
    db.skill.amazon = { ...db.skill.amazon, connected: true, profile_client_id: "", vendor_id: "" };
    renderApp("/alexa-skill");
    const section = await screen.findByRole("region", { name: /Amazon/ });
    const value = (label: string) => within(section).getByText(label).nextElementSibling;
    expect(value("Vendor")).toHaveTextContent(/^-$/);
    expect(value("Security profile")).toHaveTextContent(/^-$/);
  });

  it("explains a deploy that fails", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    const { queryClient } = renderApp("/alexa-skill");
    await user.click(await findEnabledButton({ name: "Redeploy" }));
    const deployId = await followedDeploy(log, queryClient);

    FakeEventSource.emit("deploy", {
      at: new Date().toISOString(),
      deploy_id: deployId,
      message: "Amazon rejected the interaction model.",
      state: "failed",
      step: "build",
    });

    expect(await screen.findByText("Deploy failed: Amazon rejected the interaction model.")).toBeInTheDocument();
    const status = screen.getByRole("region", { name: "Deployment status" });
    expect(within(status).getByText("The last build failed")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Redeploy" })).toBeEnabled();
  });

  it("shows each locale's build, its errors and an unknown status as it is", async () => {
    const db = getDb();
    if (!db.skill.last_deploy) throw new Error("no sample deploy");
    db.skill.last_deploy = {
      ...db.skill.last_deploy,
      locales: [
        ...db.skill.last_deploy.locales,
        {
          built_at: null,
          errors: ["Slot type MEDIA has too many values."],
          locale: "de-DE",
          size_bytes: 0,
          status: "failed",
          titles: 0,
        },
        { built_at: null, errors: [], locale: "fr-FR", size_bytes: 0, status: "queued", titles: 0 },
      ] as typeof db.skill.last_deploy.locales,
      warnings: ["en-AU: 12 titles were too long to include."],
    };
    renderApp("/alexa-skill");

    const table = await screen.findByRole("table", { name: "Interaction model by locale" });
    const german = within(table).getByText("de-DE").closest("tr") as HTMLElement;
    expect(german).toHaveTextContent("Failed");
    expect(german).toHaveTextContent("Slot type MEDIA has too many values.");
    expect(within(german).getAllByText("-")).toHaveLength(3);
    expect(within(table).getByText("fr-FR").closest("tr")).toHaveTextContent("queued");
    expect(screen.getByRole("note")).toHaveTextContent("en-AU: 12 titles were too long to include.");
  });

  it("explains what appears after the first deploy", async () => {
    getDb().skill = { ...getDb().skill, last_deploy: null, model_size: null };
    renderApp("/alexa-skill");
    expect(await screen.findByText(/Nothing built yet/)).toBeInTheDocument();
    expect(screen.getByText("Shown after the first deploy.")).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    expect(screen.queryByRole("note")).not.toBeInTheDocument();
  });

  it("shows why saving locales failed", async () => {
    const user = userEvent.setup();
    failWith("put", "/skill/settings", 502, "Amazon is unavailable.");
    renderApp("/alexa-skill");
    await user.click(await screen.findByRole("button", { name: "en-GB" }));
    expect(await screen.findByText("Amazon is unavailable.")).toBeInTheDocument();
  });

  it("sends Amazon re-authorisation through Login with Amazon", async () => {
    const user = userEvent.setup();
    const assign = vi.fn();
    vi.stubGlobal("location", { ...window.location, assign });
    try {
      getDb().skill.amazon = { ...getDb().skill.amazon, needs_reauthorise: true };
      renderApp("/alexa-skill");
      expect(await screen.findByText(/Amazon stopped accepting Encore's token/)).toBeInTheDocument();

      await user.click(screen.getByRole("button", { name: "Re-authorise" }));

      await waitFor(() => expect(assign).toHaveBeenCalledWith(expect.stringContaining("mock_lwa=1")));
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it("says why re-authorising can't start", async () => {
    const user = userEvent.setup();
    getDb().skill.amazon = { ...getDb().skill.amazon, has_client_secret: false };
    renderApp("/alexa-skill");

    await user.click(await screen.findByRole("button", { name: "Re-authorise" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Save the Login with Amazon client ID and secret first.",
    );
    expect(screen.getByRole("button", { name: "Re-authorise" })).toBeEnabled();
  });

  it("offers to retry when the skill can't be loaded", async () => {
    const user = userEvent.setup();
    failWith("get", "/skill", 500, "Encore's database is locked.");
    renderApp("/alexa-skill");
    expect(await screen.findByRole("alert", {}, { timeout: 4000 })).toHaveTextContent("Encore's database is locked.");

    server.resetHandlers();
    await user.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByRole("region", { name: "Deployment status" })).toBeInTheDocument();
  });
});
