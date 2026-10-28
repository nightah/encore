import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it } from "vitest";

import { ThemeToggle } from "@components/ThemeToggle";
import { THEME_STORAGE_KEY, initTheme } from "@theme/store";

describe("ThemeToggle", () => {
  beforeEach(() => {
    window.localStorage.clear();
    initTheme();
  });

  it("flips between light and dark and persists the choice", async () => {
    const user = userEvent.setup();
    render(<ThemeToggle />);
    // jsdom has no matchMedia, so "system" resolves to light.
    const button = screen.getByRole("button", { name: "Switch to dark mode" });

    await user.click(button);
    expect(button).toHaveAccessibleName("Switch to light mode");
    expect(document.documentElement.dataset.theme).toBe("dark");
    expect(window.localStorage.getItem(THEME_STORAGE_KEY)).toBe("dark");

    await user.click(button);
    expect(button).toHaveAccessibleName("Switch to dark mode");
    expect(document.documentElement.dataset.theme).toBe("light");
    expect(window.localStorage.getItem(THEME_STORAGE_KEY)).toBe("light");
  });

  it("restores a persisted preference on init", () => {
    window.localStorage.setItem(THEME_STORAGE_KEY, "dark");
    initTheme();
    render(<ThemeToggle />);

    expect(screen.getByRole("button", { name: "Switch to light mode" })).toBeInTheDocument();
    expect(document.documentElement.dataset.theme).toBe("dark");
  });

  it("ignores garbage in storage", () => {
    window.localStorage.setItem(THEME_STORAGE_KEY, "neon");
    initTheme();
    render(<ThemeToggle />);

    expect(screen.getByRole("button", { name: "Switch to dark mode" })).toBeInTheDocument();
  });
});
