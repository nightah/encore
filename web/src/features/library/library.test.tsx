import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it } from "vitest";

import { keys } from "@api/keys";
import { getDb } from "@mocks/handlers";
import { server } from "@mocks/node";
import { failWith, recordRequests, renderApp } from "@test/utils";

describe("Library page", () => {
  it("shows the summary tiles for the shared library", async () => {
    renderApp("/library");
    const tiles = await screen.findByRole("region", { name: "Library summary" });
    expect(within(tiles).getByText("2,184")).toBeInTheDocument();
    expect(within(tiles).getByText("312")).toBeInTheDocument();
    expect(await within(tiles).findByText("18,406 episodes")).toBeInTheDocument();
    expect(within(tiles).getByText("41,907")).toBeInTheDocument();
    expect(await within(tiles).findByText("S3-compatible")).toBeInTheDocument();
    expect(within(tiles).getByText("Backups")).toBeInTheDocument();
    expect(within(tiles).getByText(/last backup/)).toBeInTheDocument();
    expect(screen.getByRole("combobox", { name: "Library" })).toHaveValue("3");
  });

  it("runs an example through the match tester with the CSRF header", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/library");

    await user.click(await screen.findByRole("button", { name: "rocky two" }));

    const table = await screen.findByRole("table", { name: "Match candidates" });
    expect(within(table).getByText("Rocky II (1979)")).toBeInTheDocument();
    expect(screen.getByText("Would play")).toBeInTheDocument();
    expect(screen.getByText("Rocky II scores 0.96, 0.31 ahead of the next.")).toBeInTheDocument();
    expect(screen.getByText("rocky 2", { selector: "span" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "rocky two" })).toHaveAttribute("aria-pressed", "true");

    const post = log.find((r) => r.method === "POST" && r.path === "/match");
    expect(post?.csrf).toBe("mock-csrf-token");
    expect(post?.body).toEqual({ kinds: ["movie"], library_id: 3, query: "rocky two" });
  });

  it("shows the boost and the ask verdict", async () => {
    const user = userEvent.setup();
    renderApp("/library");
    await user.click(await screen.findByRole("button", { name: "the office" }));
    expect(await screen.findByText("Would ask")).toBeInTheDocument();
    expect(screen.getAllByText("+0.02").length).toBeGreaterThan(0);
  });

  it("adds the query as an alias for the top result", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/library");

    await user.type(await screen.findByRole("textbox", { name: "What Alexa heard" }), "  Hot Fuzzy ");
    await user.click(screen.getByRole("button", { name: "Test" }));
    expect(await screen.findByText("Not found")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Add “Hot Fuzzy” as an alias for the top result" }));

    await waitFor(() =>
      expect(log.find((r) => r.method === "POST" && r.path === "/aliases")?.body).toEqual({
        alias: "hot fuzzy",
        kind: "movie",
        library_id: 3,
        media_id: 733,
      }),
    );
    expect(await screen.findByText(/“hot fuzzy” → Hot Fuzz/)).toBeInTheDocument();
  });

  it("shows match errors inline", async () => {
    const user = userEvent.setup();
    failWith("post", "/match", 503, "The library index is still loading.");
    renderApp("/library");
    await user.click(await screen.findByRole("button", { name: "amelie" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("The library index is still loading.");
  });

  it("lists recently added titles with their voice status", async () => {
    renderApp("/library");
    const recent = await screen.findByRole("region", { name: "Recently added" });
    expect(await within(recent).findByText("Conclave (2024)")).toBeInTheDocument();
    expect(within(recent).getByText("Waiting for next update")).toBeInTheDocument();
    expect(within(recent).getByText("In skill model")).toBeInTheDocument();
    expect(within(recent).getByText(/Movie · Lounge · 4 min ago/)).toBeInTheDocument();
  });

  it("syncs every library from its source", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/library");
    await user.click(await screen.findByRole("button", { name: "Sync all now" }));
    await waitFor(() => {
      const syncs = log.filter((r) => r.method === "POST" && r.path.endsWith("/sync")).map((r) => r.path);
      expect(syncs.sort()).toEqual(["/kodi/instances/1/sync", "/kodi/instances/3/sync"]);
    });
  });

  it("waits for the settings before saying there's no backup", async () => {
    let answer: () => void = () => {};
    server.use(
      http.get(
        "*/api/v1/settings",
        () =>
          new Promise<Response>((resolve) => {
            answer = () => resolve(HttpResponse.json(getDb().settings));
          }),
      ),
    );
    renderApp("/library");

    const tiles = await screen.findByRole("region", { name: "Library summary" });
    expect(within(tiles).queryByText(/no backup yet/)).not.toBeInTheDocument();
    expect(within(tiles).getByText("Loading…")).toBeInTheDocument();

    answer();
    expect(await within(tiles).findByText(/last backup/)).toBeInTheDocument();
    expect(within(tiles).getByText("S3-compatible")).toBeInTheDocument();
    expect(within(tiles).queryByText("Loading…")).not.toBeInTheDocument();
  });

  it("doesn't say there's no backup when the settings didn't load", async () => {
    failWith("get", "/settings", 500, "boom");
    const { queryClient } = renderApp("/library");

    const tiles = await screen.findByRole("region", { name: "Library summary" });
    await waitFor(() => expect(queryClient.getQueryState(keys.settings)?.status).toBe("error"));
    expect(within(tiles).queryByText(/no backup yet/)).not.toBeInTheDocument();
    expect(within(tiles).queryByText("Loading…")).not.toBeInTheDocument();
  });

  it("points to adding a Kodi instance when there is no library", async () => {
    getDb().libraries = [];
    renderApp("/library");
    expect(await screen.findByText("No libraries yet")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Add a Kodi instance" })).toHaveAttribute("href", "/kodi");
    expect(screen.getByRole("button", { name: "Sync all now" })).toBeDisabled();
    expect(screen.queryByRole("combobox", { name: "Library" })).not.toBeInTheDocument();
  });

  it("switches the page to another library", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/library");
    const picker = await screen.findByRole("combobox", { name: "Library" });
    const other = getDb().libraries.find((l) => l.id !== 3);
    if (!other) throw new Error("no second library");

    await user.selectOptions(picker, String(other.id));

    expect(picker).toHaveValue(String(other.id));
    const tiles = screen.getByRole("region", { name: "Library summary" });
    expect(within(tiles).getByText(other.counts.movies.toLocaleString("en-GB"))).toBeInTheDocument();
    await waitFor(() => expect(log.some((r) => r.path.startsWith(`/libraries/${other.id}/recent`))).toBe(true));
  });

  it("restricts the match tester to the chosen media type and ignores an empty query", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/library");
    const heard = await screen.findByRole("textbox", { name: "What Alexa heard" });

    await user.click(screen.getByRole("button", { name: "Test" }));
    await user.type(heard, "   ");
    await user.click(screen.getByRole("button", { name: "Test" }));
    expect(log.some((r) => r.path === "/match")).toBe(false);

    await user.clear(heard);
    await user.type(heard, " seinfield ");
    await user.selectOptions(screen.getByRole("combobox", { name: "Media type" }), "shows");
    await user.click(screen.getByRole("button", { name: "Test" }));

    await waitFor(() =>
      expect(log.find((r) => r.path === "/match")?.body).toEqual({
        kinds: ["tvshow", "episode"],
        library_id: 3,
        query: "seinfield",
      }),
    );
  });

  it("says when nothing comes close, without offering an alias", async () => {
    const user = userEvent.setup();
    server.use(
      http.post("*/api/v1/match", () =>
        HttpResponse.json({ candidates: [], decision: "not_found", margin: 0, normalized: "zzz" }),
      ),
    );
    renderApp("/library");
    await user.type(await screen.findByRole("textbox", { name: "What Alexa heard" }), "zzz");
    await user.click(screen.getByRole("button", { name: "Test" }));

    expect(await screen.findByText("Nothing in this library comes close.")).toBeInTheDocument();
    expect(screen.queryByRole("table", { name: "Match candidates" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /as an alias/ })).not.toBeInTheDocument();
  });

  it("won't alias a result the server can't name", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    server.use(
      http.post("*/api/v1/match", () =>
        HttpResponse.json({
          candidates: [
            {
              breakdown: { alias: "", boost: 0, jaro_winkler: 0.9, phonetic: 1, token_set: 0.9 },
              kind: "movie",
              media_id: 0,
              score: 0.93,
              title: "Rocky II",
              year: 1979,
            },
          ],
          decision: "play",
          margin: 1,
          normalized: "rocky two",
        }),
      ),
    );
    renderApp("/library");
    await user.type(await screen.findByRole("textbox", { name: "What Alexa heard" }), "rocky two");
    await user.click(screen.getByRole("button", { name: "Test" }));

    const add = await screen.findByRole("button", { name: "Add “rocky two” as an alias for the top result" });
    expect(add).toBeDisabled();
    expect(
      screen.getByText("The server didn't say which title this is, so it can't be aliased here."),
    ).toBeInTheDocument();
    expect(log.some((r) => r.method === "POST" && r.path === "/aliases")).toBe(false);
  });

  it("removes an alias", async () => {
    const user = userEvent.setup();
    const alias = getDb().aliases.find((a) => a.library_id === 3);
    if (!alias) throw new Error("no sample alias");
    renderApp("/library");

    await user.click(await screen.findByRole("button", { name: `Remove alias ${alias.alias}` }));

    await waitFor(() =>
      expect(screen.queryByRole("button", { name: `Remove alias ${alias.alias}` })).not.toBeInTheDocument(),
    );
    expect(getDb().aliases.some((a) => a.id === alias.id)).toBe(false);
  });

  it("says when nothing was added recently", async () => {
    getDb().recent = {};
    renderApp("/library");
    expect(await screen.findByText("Nothing added recently.")).toBeInTheDocument();
  });

  it("shows an error when recent titles can't load", async () => {
    failWith("get", "/libraries/3/recent", 503, "Kodi is asleep.");
    renderApp("/library");
    const recent = await screen.findByRole("region", { name: "Recently added" });
    expect(await within(recent).findByRole("alert", {}, { timeout: 4000 })).toHaveTextContent("Kodi is asleep.");
  });

  it("offers to retry when the libraries can't be loaded", async () => {
    const user = userEvent.setup();
    failWith("get", "/libraries", 500, "Index is rebuilding.");
    renderApp("/library");
    expect(await screen.findByRole("alert", {}, { timeout: 4000 })).toHaveTextContent("Index is rebuilding.");

    server.resetHandlers();
    await user.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByRole("region", { name: "Library summary" })).toBeInTheDocument();
  });
});
