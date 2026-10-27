import { expect, test } from "@playwright/test";

test.beforeEach(async ({ page }) => {
  // Start every test from the configured sample home.
  await page.goto("./?mock=configured");
  await expect(page.getByRole("heading", { level: 1, name: "Overview" })).toBeVisible();
});

test("link an unlinked Echo", async ({ page }) => {
  await page.getByRole("navigation", { name: "Primary" }).getByRole("link", { name: "Echo devices" }).click();
  const banner = page.getByRole("region", { name: /not linked to a Kodi yet/ });
  await expect(banner).toBeVisible();
  await expect(banner).toContainText("“pause”");

  await banner.getByLabel("Device name").fill("Kitchen Dot");
  await banner.getByLabel("Controls Kodi").selectOption({ label: "Lounge" });
  await banner.getByRole("button", { name: "Link device" }).click();

  await expect(banner).toHaveCount(0);
  const row = page.getByRole("row", { name: /Kitchen Dot/ });
  await expect(row).toContainText("Lounge");

  // The overview's attention item goes away too.
  await page.getByRole("navigation", { name: "Primary" }).getByRole("link", { name: "Overview" }).click();
  await expect(page.getByRole("region", { name: "Needs attention" })).not.toContainText("isn't linked");
});

test("match tester", async ({ page }) => {
  await page.getByRole("navigation", { name: "Primary" }).getByRole("link", { name: "Library" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Library" })).toBeVisible();

  await page.getByRole("button", { name: "seinfield" }).click();
  await expect(page.getByText("Would play")).toBeVisible();
  const table = page.getByRole("table", { name: "Match candidates" });
  await expect(table.getByRole("row").nth(1)).toContainText("Seinfeld");

  await page.getByLabel("What Alexa heard").fill("hot fuzzy");
  await page.getByRole("button", { exact: true, name: "Test" }).click();
  await expect(page.getByText("Not found", { exact: true })).toBeVisible();
  await expect(table.getByRole("row").nth(1)).toContainText("Hot Fuzz");

  await page.getByRole("button", { name: /Add “hot fuzzy” as an alias/ }).click();
  await expect(page.getByText(/Alias saved/)).toBeVisible();

  // The alias now wins outright.
  await page.getByRole("button", { exact: true, name: "Test" }).click();
  await expect(page.getByText("Would play")).toBeVisible();
});

test("theme toggle persists", async ({ page }) => {
  const html = page.locator("html");
  await expect(html).toHaveAttribute("data-theme", "light");
  await page.getByRole("button", { name: "Switch to dark mode" }).click();
  await expect(html).toHaveAttribute("data-theme", "dark");

  await page.reload();
  await expect(page.getByRole("heading", { level: 1, name: "Overview" })).toBeVisible();
  await expect(html).toHaveAttribute("data-theme", "dark");
  await expect(page.getByRole("button", { name: "Switch to light mode" })).toBeVisible();

  // Settings › General shows the stored choice and can go back to the system setting.
  await page.getByRole("navigation", { name: "Primary" }).getByRole("link", { name: "Settings" }).click();
  await expect(page.getByRole("radio", { name: /Dark/ })).toBeChecked();
  await page.getByRole("radio", { name: /Match system/ }).click();
  await expect(html).toHaveAttribute("data-theme", "light");
  await page.getByRole("button", { name: "Save changes" }).click();
  await expect(page.getByRole("button", { name: "Save changes" })).toBeDisabled();
  await page.reload();
  await expect(page.getByRole("radio", { name: /Match system/ })).toBeChecked();
});

test("settings shows the right fields for each sign-in method", async ({ page }) => {
  await page.goto("./settings?tab=access");
  await expect(page.getByRole("tab", { name: "Access", selected: true })).toBeVisible();

  await expect(page.getByRole("radio", { name: /Local password/ })).toBeChecked();
  await expect(page.getByLabel("Session length")).toBeVisible();
  await expect(page.getByLabel("Issuer URL")).toHaveCount(0);

  await page.getByRole("radio", { name: /OpenID Connect/ }).click();
  await expect(page.getByLabel("Issuer URL")).toBeVisible();
  await expect(page.getByLabel("Redirect URI to register with your provider")).toHaveAttribute("readonly", "");
  await expect(page.getByLabel("Session length")).toHaveCount(0);

  await page.getByRole("radio", { name: /Trusted proxy headers/ }).click();
  await expect(page.getByLabel("User header")).toHaveValue("Remote-User");
  const proxies = page.getByLabel(/Trusted proxy addresses/);
  await expect(proxies).toHaveAttribute("readonly", "");
  await expect(page.getByText("Managed by config").first()).toBeVisible();
  await expect(page.getByLabel("Issuer URL")).toHaveCount(0);

  // Saving switches the method.
  await page.getByRole("button", { name: "Save changes" }).click();
  await expect(page.getByText("Settings saved.")).toBeVisible();
  await page.reload();
  await expect(page.getByRole("radio", { name: /Trusted proxy headers/ })).toBeChecked();
});
