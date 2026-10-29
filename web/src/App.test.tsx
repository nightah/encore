import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import { setLanguage } from "@i18n/index";
import { renderApp } from "@test/utils";

describe("App", () => {
  it.each([
    ["en", "{ArrowRight}", "{ArrowLeft}"],
    ["ar", "{ArrowLeft}", "{ArrowRight}"],
  ] as const)("moves to the next tab with the arrow key that points forward in %s", async (lng, forward, back) => {
    await setLanguage(lng);
    const user = userEvent.setup();
    renderApp("/settings");

    const tabs = within(await screen.findByRole("tablist")).getAllByRole("tab");
    tabs[0]?.focus();

    await user.keyboard(forward);
    expect(tabs[1]).toHaveFocus();

    await user.keyboard(back);
    expect(tabs[0]).toHaveFocus();
  });
});
