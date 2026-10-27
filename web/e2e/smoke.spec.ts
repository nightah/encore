import { expect, test } from "@playwright/test";

// Runs in both modes: against the mock API, and against the real server signed in by the journey.
test("shell renders and navigates", async ({ page }) => {
  await page.goto("./");
  await expect(page.getByRole("navigation", { name: "Primary" })).toBeVisible();
  await expect(page.getByRole("heading", { level: 1, name: "Overview" })).toBeVisible();

  await page.getByRole("link", { exact: true, name: "Kodi" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Kodi instances" })).toBeVisible();
  await expect(page.getByRole("link", { exact: true, name: "Kodi" })).toHaveAttribute("aria-current", "page");
});
