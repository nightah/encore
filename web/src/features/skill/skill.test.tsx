import type { QueryClient } from "@tanstack/react-query";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it } from "vitest";

import { keys } from "@api/keys";
import { DEPLOY_STEPS } from "@features/skill/timeline";
import { pageLocation } from "@lib/pageLocation";
import { getDb } from "@mocks/handlers";
import { FakeEventSource } from "@test/eventSource";
import { server } from "@test/server";
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
    getDb().skill = { ...getDb().skill, auto_deploy: false, needs_deploy: true, update_reason: "library" };
    renderApp("/alexa-skill");
    expect(await screen.findByText(/New library titles since the last deploy/)).toBeInTheDocument();
    expect(screen.queryByText(/Settings changed since the last deploy/)).not.toBeInTheDocument();
  });

  it("says to redeploy when settings changed and automatic deploys are off", async () => {
    getDb().skill = { ...getDb().skill, auto_deploy: false, needs_deploy: true, update_reason: "settings" };
    renderApp("/alexa-skill");
    expect(
      await screen.findByText("Settings changed since the last deploy. Redeploy to apply them."),
    ).toBeInTheDocument();
  });

  it("says a pending change will deploy by itself when automatic deploys are on", async () => {
    getDb().skill = { ...getDb().skill, auto_deploy: true, needs_deploy: true, update_reason: "library" };
    renderApp("/alexa-skill");
    expect(
      await screen.findByText("Changes since the last deploy. Encore deploys them automatically once they settle."),
    ).toBeInTheDocument();
    expect(screen.queryByText(/New library titles since the last deploy/)).not.toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("explains why automatic deploys are paused and how to resume them", async () => {
    getDb().skill = {
      ...getDb().skill,
      auto_deploy: true,
      auto_deploy_paused: "deploy: build failed: en-AU: slot value too long",
      needs_deploy: true,
      update_reason: "library",
    };
    renderApp("/alexa-skill");
    const alert = await screen.findByRole("alert");
    expect(within(alert).getByText("Automatic deploys are paused")).toBeInTheDocument();
    expect(within(alert).getByText(/slot value too long/)).toBeInTheDocument();
    expect(within(alert).getByText(/Deploy the skill by hand/)).toBeInTheDocument();
    expect(screen.getByText(/New library titles since the last deploy/)).toBeInTheDocument();
  });

  it("says a failed automatic deploy will be tried again", async () => {
    getDb().skill = {
      ...getDb().skill,
      auto_deploy: true,
      auto_deploy_retry: {
        attempts: 1,
        error: "smapi: create upload URL: HTTP 503: Service Unavailable",
        max_attempts: 4,
        next_at: new Date().toISOString(),
      },
      needs_deploy: true,
    };
    renderApp("/alexa-skill");
    const status = await screen.findByText("An automatic deploy failed and will be tried again");
    const card = status.closest("[role=status]") as HTMLElement;
    expect(within(card).getByText(/HTTP 503/)).toBeInTheDocument();
    expect(within(card).getByText(/^Attempt 1 of 4 failed\. Encore tries again after /)).toBeInTheDocument();
    expect(screen.queryByText("Automatic deploys are paused")).not.toBeInTheDocument();
  });

  it("shows only the pause once automatic deploys gave up", async () => {
    getDb().skill = {
      ...getDb().skill,
      auto_deploy: true,
      auto_deploy_paused: "smapi: create upload URL: HTTP 503: Service Unavailable",
      auto_deploy_retry: { attempts: 3, error: "HTTP 503", max_attempts: 4, next_at: new Date().toISOString() },
    };
    renderApp("/alexa-skill");
    expect(await screen.findByText("Automatic deploys are paused")).toBeInTheDocument();
    expect(screen.queryByText("An automatic deploy failed and will be tried again")).not.toBeInTheDocument();
  });

  it("shows the locales built with fewer library titles", async () => {
    const db = getDb();
    const last = db.skill.last_deploy;
    if (!last) throw new Error("the mock skill has been deployed");
    const row = last.locales[0];
    if (!row) throw new Error("the mock deploy built a locale");
    db.skill = {
      ...db.skill,
      last_deploy: {
        ...last,
        locales: [
          { ...row, locale: "en-AU" },
          {
            ...row,
            degraded: { dropped: [], reasons: ["INVALID_INTERACTION_MODEL: too large"], titles_asked_for: 3480 },
            locale: "en-GB",
            titles: 1740,
          },
          {
            ...row,
            degraded: { dropped: ["Dune", "Heat"], reasons: ["Slot value 'Dune' is invalid"], titles_asked_for: 3480 },
            locale: "en-US",
            titles: 3478,
          },
          {
            ...row,
            degraded: { dropped: ["Arrival"], reasons: [], titles_asked_for: 3480 },
            locale: "en-CA",
            titles: 3479,
          },
          {
            ...row,
            degraded: { dropped: ["Alien"], reasons: [], titles_asked_for: 3480 },
            errors: ["it failed anyway"],
            locale: "en-IN",
            status: "failed",
          },
        ],
        warnings: ["slot type AMAZON.Movie: dropped 1 values or synonyms longer than 140 characters"],
      },
    };
    renderApp("/alexa-skill");

    const note = await screen.findByRole("note");
    expect(
      within(note).getByText(
        "en-GB: Amazon would not build the model with every library title, so it was built with 1,740 of 3,480.",
      ),
    ).toBeInTheDocument();
    expect(within(note).getByText("INVALID_INTERACTION_MODEL: too large")).toBeInTheDocument();
    expect(
      within(note).getByText("en-US: Amazon rejected these titles, so they were left out: Dune and Heat"),
    ).toBeInTheDocument();
    const canada = within(note).getByText("en-CA: Amazon rejected these titles, so they were left out: Arrival");
    expect(within(canada).queryByRole("list")).not.toBeInTheDocument();
    expect(within(note).queryByText(/Alien/)).not.toBeInTheDocument();
    expect(within(note).getByText(/dropped 1 values/)).toBeInTheDocument();

    const table = screen.getByRole("table");
    expect(within(table).getAllByText("Fewer titles")).toHaveLength(4);
    expect(within(table).getByText("1,740 of 3,480")).toBeInTheDocument();
    expect(within(table).getByText("3,478 of 3,480")).toBeInTheDocument();
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

  it("keeps a streamed step that arrives while the deploy is still loading", async () => {
    const db = getDb();
    if (db.skill.last_deploy) db.skill.last_deploy = { ...db.skill.last_deploy, id: "d-running", state: "in_progress" };
    db.deploys["d-running"] = { error: "", events: [], id: "d-running", state: "in_progress" };
    let answer: (() => void) | undefined;
    server.use(
      http.get(
        "*/api/v1/skill/deploy/d-running",
        () =>
          new Promise<Response>((resolve) => {
            // The server's answer from before the step happened.
            answer = () => resolve(HttpResponse.json({ error: "", events: [], id: "d-running", state: "in_progress" }));
          }),
        { once: true },
      ),
    );
    const { queryClient } = renderApp("/alexa-skill");
    await waitFor(() => expect(answer).toBeDefined());

    FakeEventSource.emit("deploy", {
      at: new Date().toISOString(),
      deploy_id: "d-running",
      message: "Amazon rejected the interaction model.",
      state: "failed",
      step: "build",
    });
    expect(await screen.findByText("Deploy failed: Amazon rejected the interaction model.")).toBeInTheDocument();

    answer?.();
    await waitFor(() => expect(queryClient.isFetching({ queryKey: keys.deploy("d-running") })).toBe(0));
    expect(screen.getByText("Deploy failed: Amazon rejected the interaction model.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Redeploy" })).toBeEnabled();
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
    getDb().skill.amazon = { ...getDb().skill.amazon, needs_reauthorise: true };
    renderApp("/alexa-skill");
    expect(await screen.findByText(/Amazon stopped accepting Encore's token/)).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Re-authorise" }));

    await waitFor(() => expect(pageLocation.assign).toHaveBeenCalledWith(expect.stringContaining("mock_lwa=1")));
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
