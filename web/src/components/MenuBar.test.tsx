import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it } from "vitest";

import { MENU_BAR_STORAGE_KEY, setMenuBar } from "@components/menuBar";
import { getDb } from "@mocks/handlers";
import { server } from "@test/server";
import { renderApp } from "@test/utils";

function menuBar() {
  return screen.findByRole("navigation", { name: "Menu bar" });
}

/** The bar behind an open sheet, which hides it, name and all, from the accessibility tree. */
function barBehindSheet() {
  return document.querySelector<HTMLElement>('nav[aria-label="Menu bar"]') as HTMLElement;
}

/** The labels along the bar, More included. */
function barLabels(bar: HTMLElement) {
  return within(bar)
    .getAllByRole("listitem", { hidden: true })
    .map((item) => item.textContent);
}

async function openMore() {
  const user = userEvent.setup();
  await user.click(within(await menuBar()).getByRole("button", { name: "More" }));
  return { sheet: await screen.findByRole("dialog", { name: "More" }), user };
}

async function openArrange() {
  const { sheet, user } = await openMore();
  await user.click(within(sheet).getByRole("button", { name: "Arrange menu" }));
  return { sheet: await screen.findByRole("dialog", { name: "Arrange menu" }), user };
}

/** The rows of the menu bar list in Arrange menu, by section name. */
function arranged(sheet: HTMLElement) {
  return within(within(sheet).getByRole("list", { name: "Menu bar · up to 4" }))
    .getAllByRole("listitem")
    .map((item) => item.textContent);
}

describe("MenuBar", () => {
  it("puts four sections and More along the bottom, marking the current one", async () => {
    renderApp("/kodi");
    const bar = await menuBar();

    expect(barLabels(bar)).toEqual(["Overview", "Kodi", "Echos", "Requests", "More"]);
    expect(within(bar).getByRole("link", { name: "Kodi" })).toHaveAttribute("aria-current", "page");
    expect(within(bar).getByRole("button", { name: "More" })).not.toHaveAttribute("aria-current");
  });

  it("marks More as current on a page it holds", async () => {
    renderApp("/settings");
    const bar = await menuBar();
    await screen.findByRole("heading", { level: 1, name: "Settings" });
    expect(within(bar).getByRole("button", { name: "More" })).toHaveAttribute("aria-current", "page");
  });

  it("opens the other sections from More and closes it on the way", async () => {
    const { router } = renderApp("/");
    const { sheet, user } = await openMore();

    expect(
      within(sheet)
        .getAllByRole("link")
        .map((link) => link.textContent),
    ).toEqual(["Alexa skill", "Library", "Settings"]);

    await user.click(within(sheet).getByRole("link", { name: "Library" }));

    await waitFor(() => expect(screen.queryByRole("dialog", { name: "More" })).not.toBeInTheDocument());
    expect(router.state.location.pathname).toBe("/library");
  });

  it("closes More with Escape", async () => {
    renderApp("/");
    const { user } = await openMore();
    await user.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "More" })).not.toBeInTheDocument());
  });

  it("switches the theme from More and saves it", async () => {
    renderApp("/");
    const { sheet, user } = await openMore();
    const theme = within(sheet).getByRole("group", { name: "Theme" });
    expect(within(theme).getByRole("button", { name: "Match system" })).toHaveAttribute("aria-pressed", "true");

    await user.click(within(theme).getByRole("button", { name: "Dark" }));

    expect(within(theme).getByRole("button", { name: "Dark" })).toHaveAttribute("aria-pressed", "true");
    expect(document.documentElement.dataset.theme).toBe("dark");
    expect(window.localStorage.getItem("encore.theme")).toBe("dark");
  });

  it("names who is signed in and the release under More, and signs out", async () => {
    const { router } = renderApp("/");
    const { sheet, user } = await openMore();

    expect(await within(sheet).findByText("v0.1.0")).toBeInTheDocument();
    expect(within(sheet).getByText("admin")).toHaveAttribute("data-sensitive", "admin");

    await user.click(within(sheet).getByRole("button", { name: "Sign out" }));

    await waitFor(() => expect(router.state.location.pathname).toBe("/login"));
  });

  it("offers no sign out under More when a proxy signs people in", async () => {
    getDb().authMode = "header";
    renderApp("/");
    const { sheet } = await openMore();
    expect(await within(sheet).findByText("v0.1.0")).toBeInTheDocument();
    expect(within(sheet).queryByRole("button", { name: "Sign out" })).not.toBeInTheDocument();
  });

  it("leaves the release out under More when the server reports none", async () => {
    let checked = false;
    server.use(
      http.get("*/api/v1/healthz", () => {
        checked = true;
        return HttpResponse.json({ status: "ok" });
      }),
    );
    renderApp("/");
    const { sheet } = await openMore();
    await waitFor(() => expect(checked).toBe(true));
    expect(within(sheet).queryByText(/^v\d/)).not.toBeInTheDocument();
  });
});

describe("Arrange menu", () => {
  it("reorders the bar with the buttons, keeping focus on the button pressed", async () => {
    renderApp("/");
    const { sheet, user } = await openArrange();
    expect(arranged(sheet)).toEqual(["Overview", "Kodi", "Echo devices", "Request log"]);
    expect(within(sheet).getByRole("button", { name: "Move Overview up" })).toHaveAttribute("aria-disabled", "true");

    const down = within(sheet).getByRole("button", { name: "Move Kodi down" });
    await user.click(down);

    expect(arranged(sheet)).toEqual(["Overview", "Echo devices", "Kodi", "Request log"]);
    expect(down).toHaveFocus();
    expect(screen.getByText("Kodi is now 3 of 4 in the menu bar.")).toBeInTheDocument();

    await user.click(down);
    expect(arranged(sheet)).toEqual(["Overview", "Echo devices", "Request log", "Kodi"]);
    expect(down).toHaveAttribute("aria-disabled", "true");
    expect(down).toHaveFocus();

    // At the end of the list, down does nothing.
    await user.click(down);
    expect(arranged(sheet)).toEqual(["Overview", "Echo devices", "Request log", "Kodi"]);

    await user.click(within(sheet).getByRole("button", { name: "Move Request log up" }));
    expect(arranged(sheet)).toEqual(["Overview", "Request log", "Echo devices", "Kodi"]);

    expect(barLabels(barBehindSheet())).toEqual(["Overview", "Requests", "Echos", "Kodi", "More"]);
    expect(JSON.parse(window.localStorage.getItem(MENU_BAR_STORAGE_KEY) ?? "null")).toEqual([
      "/",
      "/request-log",
      "/echo-devices",
      "/kodi",
    ]);
  });

  it("swaps a section between the bar and More, with focus following it", async () => {
    renderApp("/");
    const { sheet, user } = await openArrange();
    const add = within(sheet).getByRole("button", { name: "Add Library to the menu bar" });
    expect(add).toHaveAttribute("aria-disabled", "true");
    expect(within(sheet).getByText(/The menu bar is full/)).toBeInTheDocument();

    // While the bar is full, adding does nothing.
    await user.click(add);
    expect(arranged(sheet)).toEqual(["Overview", "Kodi", "Echo devices", "Request log"]);

    await user.click(within(sheet).getByRole("button", { name: "Move Kodi to More" }));

    expect(arranged(sheet)).toEqual(["Overview", "Echo devices", "Request log"]);
    expect(within(sheet).getByRole("button", { name: "Add Kodi to the menu bar" })).toHaveFocus();
    expect(screen.getByText("Kodi moved to More.")).toBeInTheDocument();
    expect(within(sheet).queryByText(/The menu bar is full/)).not.toBeInTheDocument();

    await user.click(within(sheet).getByRole("button", { name: "Add Library to the menu bar" }));

    expect(arranged(sheet)).toEqual(["Overview", "Echo devices", "Request log", "Library"]);
    expect(within(sheet).getByRole("button", { name: "Move Library to More" })).toHaveFocus();
    expect(screen.getByText("Library is now 4 of 4 in the menu bar.")).toBeInTheDocument();
    expect(barLabels(barBehindSheet())).toEqual(["Overview", "Echos", "Requests", "Library", "More"]);
  });

  it("says when the bar is empty", async () => {
    setMenuBar([]);
    renderApp("/");
    const { sheet, user } = await openArrange();
    expect(within(sheet).queryByRole("list", { name: "Menu bar · up to 4" })).not.toBeInTheDocument();
    expect(within(sheet).getByText("Nothing here yet.")).toBeInTheDocument();
    expect(barLabels(barBehindSheet())).toEqual(["More"]);

    await user.click(within(sheet).getByRole("button", { name: "Add Overview to the menu bar" }));
    expect(arranged(sheet)).toEqual(["Overview"]);
    expect(within(sheet).queryByText("Nothing here yet.")).not.toBeInTheDocument();
  });

  it("resets to the default arrangement, then hides the reset", async () => {
    setMenuBar(["/settings"]);
    renderApp("/");
    const { sheet, user } = await openArrange();
    expect(arranged(sheet)).toEqual(["Settings"]);

    await user.click(within(sheet).getByRole("button", { name: "Reset to default" }));

    expect(arranged(sheet)).toEqual(["Overview", "Kodi", "Echo devices", "Request log"]);
    expect(within(sheet).queryByRole("button", { name: "Reset to default" })).not.toBeInTheDocument();
    expect(window.localStorage.getItem(MENU_BAR_STORAGE_KEY)).toBeNull();
  });

  it("goes back to More with Done", async () => {
    renderApp("/");
    const { sheet, user } = await openArrange();
    await user.click(within(sheet).getByRole("button", { name: "Done" }));
    expect(await screen.findByRole("dialog", { name: "More" })).toBeInTheDocument();
  });

  it("reorders the bar by dragging a row's handle", async () => {
    renderApp("/");
    const { sheet } = await openArrange();
    const rowOf = (name: string) => within(sheet).getByText(name).closest("li") as HTMLElement;
    const handle = rowOf("Overview").querySelector("span[aria-hidden]") as HTMLElement;
    const pointer = (type: string, clientY: number) =>
      act(() => {
        handle.dispatchEvent(new PointerEvent(type, { bubbles: true, clientY, pointerId: 7 }));
      });

    const target = rowOf("Request log").getBoundingClientRect();
    await pointer("pointerdown", rowOf("Overview").getBoundingClientRect().top + 1);
    // Overview goes after every other row whose middle is above the pointer: Kodi and Echo devices.
    await pointer("pointermove", target.top + target.height / 2);
    expect(arranged(sheet)).toEqual(["Kodi", "Echo devices", "Overview", "Request log"]);
    // Moving within the same slot changes nothing.
    await pointer("pointermove", target.top + target.height / 2 - 1);
    await pointer("pointerup", target.top + target.height / 2);

    expect(arranged(sheet)).toEqual(["Kodi", "Echo devices", "Overview", "Request log"]);
    expect(screen.getByText("Overview is now 3 of 4 in the menu bar.")).toBeInTheDocument();

    // Once let go, moving the pointer drags nothing, and letting go again says nothing new.
    await pointer("pointermove", 0);
    await pointer("pointerup", 0);
    expect(arranged(sheet)).toEqual(["Kodi", "Echo devices", "Overview", "Request log"]);
    expect(screen.getByText("Overview is now 3 of 4 in the menu bar.")).toBeInTheDocument();
  });
});
