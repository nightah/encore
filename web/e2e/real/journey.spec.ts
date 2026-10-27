import { type Page, expect, test } from "@playwright/test";

import { ADMIN, AUTH_STATE, realServer } from "@e2e/real/env";

/**
 * First run against the real Encore binary and a fake Kodi: setup wizard, sign-in, then every page. Alexa requests can't reach a test server, so the request log stays empty, and Amazon steps are skipped.
 */

const h1 = (page: Page, name: string) => page.getByRole("heading", { level: 1, name });
const nav = (page: Page, name: string) =>
  page.getByRole("navigation", { name: "Primary" }).getByRole("link", { exact: true, name });

test.describe.configure({ mode: "serial" });

test("first run: setup wizard to every page", async ({ page }) => {
  test.setTimeout(180_000);
  // Anything the UI asks for that the server refuses is a contract mismatch, except the deliberate wrong password.
  const failures: string[] = [];
  page.on("response", (response) => {
    const url = new URL(response.url());
    if (!url.pathname.startsWith("/api/") || response.status() < 400) return;
    if (url.pathname === "/api/v1/auth/login" && response.status() === 401) return;
    failures.push(`${response.request().method()} ${url.pathname} → ${response.status()}`);
  });
  page.on("pageerror", (error) => failures.push(`page error: ${error.message}`));
  page.on("console", (message) => {
    // Failed requests are checked above, with the expected one left out.
    if (message.text().startsWith("Failed to load resource")) return;
    if (message.type() === "error" || message.type() === "warning") failures.push(`console: ${message.text()}`);
  });
  const token = process.env.E2E_SETUP_TOKEN;
  expect(token, "global setup reads the setup token from Encore's log").toBeTruthy();

  await test.step("setup wizard", async () => {
    await page.goto("./");
    await expect(page).toHaveURL(/\/setup$/);
    await expect(h1(page, "Create the admin account")).toBeVisible();

    await page.getByLabel("Setup token").fill(token ?? "");
    await page.getByLabel("Username").fill(ADMIN.username);
    await page.getByLabel("Password").fill(ADMIN.password);
    await page.getByRole("button", { name: "Continue" }).click();

    // A test server has no public HTTPS address.
    await expect(h1(page, "Tell us your public address")).toBeVisible();
    await page.getByRole("button", { name: "Skip for now" }).click();

    await expect(h1(page, "Add your first Kodi")).toBeVisible();
    await page.getByRole("radio", { name: /Enter an address manually/ }).click();
    await page.getByLabel("Name", { exact: true }).fill("Lounge");
    await page.getByLabel("JSON-RPC address").fill(realServer.kodiURL);
    await page.getByRole("button", { name: "Add and test" }).click();
    await expect(page.getByText("Lounge was added.")).toBeVisible();
    const checks = page.getByRole("list", { name: "Connection test results" });
    await expect(checks).toContainText("JSON-RPC over HTTP");
    await expect(checks).toContainText("7 movies");
    await page.getByRole("button", { name: "Continue" }).click();

    await expect(h1(page, "Connect your Amazon developer account")).toBeVisible();
    await expect(page.getByText("/api/v1/oauth/lwa/callback")).toBeVisible();
    await page.getByRole("button", { name: "Skip for now" }).click();

    await expect(h1(page, "Deploy the skill")).toBeVisible();
    await page.getByRole("button", { name: "Skip for now" }).click();

    await expect(h1(page, "Say hello")).toBeVisible();
    await expect(page.getByText("Waiting for the first request…")).toBeVisible();
    await page.getByRole("button", { name: "Finish" }).click();
    await expect(h1(page, "Overview")).toBeVisible();
  });

  await test.step("sign out and back in", async () => {
    await page.getByRole("button", { name: "Sign out" }).click();
    await expect(h1(page, "Sign in")).toBeVisible();

    await page.getByLabel("Username").fill(ADMIN.username);
    await page.getByLabel("Password").fill("not-the-password");
    await page.getByRole("button", { name: "Sign in" }).click();
    await expect(page.getByRole("alert")).toContainText("the username or password is incorrect");

    await page.getByLabel("Password").fill(ADMIN.password);
    await page.getByRole("button", { name: "Sign in" }).click();
    await expect(h1(page, "Overview")).toBeVisible();
    await page.context().storageState({ path: AUTH_STATE });
  });

  await test.step("overview", async () => {
    await expect(page.getByRole("region", { name: "Needs attention" })).toContainText("Connect to Amazon");
    await expect(page.getByText("Lounge").first()).toBeVisible();
  });

  await test.step("Kodi instances", async () => {
    await nav(page, "Kodi").click();
    await expect(h1(page, "Kodi instances")).toBeVisible();
    const card = page.getByRole("article", { name: "Lounge" });
    await expect(card).toContainText("Kodi 21.3 · API 13.5");
    // The wizard assumed Kodi's usual WebSocket port; the fake Kodi serves it next to HTTP.
    await expect(card).toContainText("Connected · HTTP only");
    await card.getByRole("button", { name: "Edit Lounge" }).click();
    const dialog = page.getByRole("dialog", { name: "Edit Lounge" });
    await expect(dialog.getByLabel("WebSocket URL")).toHaveValue(/:9090\/jsonrpc$/);
    await dialog.getByLabel("WebSocket URL").fill(realServer.kodiURL.replace(/^http/, "ws"));
    await dialog.getByRole("button", { name: "Save" }).click();
    await expect(page.getByText("Kodi instance saved.")).toBeVisible();
    await expect(card).toContainText("Connected · WebSocket", { timeout: 30_000 });
    await expect(card).toContainText("7 movies · 4 shows");
    await card.getByRole("button", { name: "Test connection" }).click();
    await expect(card.getByRole("list", { name: "Connection test results" })).toContainText("7 movies");
  });

  await test.step("Echo devices", async () => {
    // A second tab only learns about the change from the event stream.
    const other = await page.context().newPage();
    await other.goto("./request-log");
    await expect(other.getByText(/^Live ·/)).toBeVisible();
    await other.goto("./echo-devices");
    await expect(other.getByRole("region", { name: /not linked to a Kodi yet/ })).toBeVisible();

    await nav(page, "Echo devices").click();
    await expect(h1(page, "Echo devices")).toBeVisible();
    const banner = page.getByRole("region", { name: /not linked to a Kodi yet/ });
    await expect(banner).toBeVisible();
    await banner.getByLabel("Device name").fill("Kitchen Dot");
    await banner.getByLabel("Controls Kodi").selectOption({ label: "Lounge" });
    await banner.getByRole("button", { name: "Link device" }).click();
    await expect(banner).toHaveCount(0);
    await expect(page.getByRole("row", { name: /Kitchen Dot/ })).toContainText("Lounge");
    await expect(other.getByRole("row", { name: /Kitchen Dot/ })).toContainText("Lounge");
    await other.close();
  });

  await test.step("library and match tester", async () => {
    await nav(page, "Library").click();
    await expect(h1(page, "Library")).toBeVisible();
    await page.getByLabel("What Alexa heard").fill("dune part two");
    await page.getByRole("button", { exact: true, name: "Test" }).click();
    const table = page.getByRole("table", { name: "Match candidates" });
    await expect(table.getByRole("row").nth(1)).toContainText("Dune: Part Two (2024)");

    await page.getByRole("button", { name: /as an alias for the top result/ }).click();
    await expect(page.getByText(/Alias saved/)).toBeVisible();
    await expect(page.getByText("“dune part two” → Dune: Part Two")).toBeVisible();
  });

  await test.step("Alexa skill", async () => {
    await nav(page, "Alexa skill").click();
    await expect(h1(page, "Alexa skill")).toBeVisible();
    await expect(page.getByText("Connect in the setup wizard")).toBeVisible();
    await expect(page.getByRole("region", { name: "Titles in the model" })).toContainText("titles");
  });

  await test.step("request log", async () => {
    await nav(page, "Request log").click();
    await expect(h1(page, "Request log")).toBeVisible();
    await expect(page.getByText("No requests yet")).toBeVisible();
  });

  await test.step("settings", async () => {
    await nav(page, "Settings").click();
    await page.getByRole("tab", { name: "Access" }).click();
    await expect(page.getByLabel("Username")).toHaveValue(ADMIN.username);
    await page.getByLabel("Session length").selectOption("24");
    await page.getByRole("button", { name: "Save changes" }).click();
    await expect(page.getByText("Settings saved.")).toBeVisible();
    await page.reload();
    await expect(page.getByLabel("Session length")).toHaveValue("24");

    await page.getByRole("tab", { name: "Features" }).click();
    const quits = page.getByRole("switch", { name: "“Shutdown” quits Kodi" });
    await expect(quits).not.toBeChecked();
    await quits.click();
    await expect(quits).toBeChecked();
    await expect(page.getByText("Settings saved.")).toBeVisible();
    await page.reload();
    await expect(page.getByRole("switch", { name: "“Shutdown” quits Kodi" })).toBeChecked();
  });

  expect(failures).toEqual([]);
});
