import { expect, test } from "@playwright/test";

const h1 = (page: import("@playwright/test").Page, name: string) => page.getByRole("heading", { level: 1, name });

test("setup wizard happy path", async ({ page }) => {
  // ?mock=fresh starts the mock API from a first run.
  await page.goto("./?mock=fresh");
  await expect(h1(page, "Create the admin account")).toBeVisible();
  await expect(page).toHaveURL(/\/setup$/);

  await page.getByLabel("Setup token").fill("K7QF-2M9X-PL4D");
  await page.getByLabel("Password").fill("correct-horse-battery");
  await page.getByRole("button", { name: "Continue" }).click();

  await expect(h1(page, "Tell us your public address")).toBeVisible();
  await page.getByRole("button", { name: "Check address" }).click();
  await expect(page.getByRole("list", { name: "Reachability check results" })).toContainText("answers with HTTP 400");
  await page.getByRole("button", { name: "Continue" }).click();

  await expect(h1(page, "Add your first Kodi")).toBeVisible();
  await expect(page.getByRole("radio", { name: /Lounge/ })).toBeChecked();
  await page.getByLabel("Kodi password").fill("hunter2hunter2");
  await page.getByRole("button", { name: "Add and test" }).click();
  await expect(page.getByRole("list", { name: "Connection test results" })).toBeVisible();
  await page.getByRole("button", { name: "Continue" }).click();

  // The mock Login with Amazon comes back to the wizard, already authorised.
  await expect(h1(page, "Connect your Amazon developer account")).toBeVisible();
  await page.getByLabel("Client ID").fill("amzn1.application-oa2-client.4f1c8d2b7a6e4e92a");
  await page.getByLabel("Client secret").fill("s3cret-from-amazon");
  await page.getByRole("button", { name: "Authorise with Amazon" }).click();
  await expect(page).toHaveURL(/\/setup$/);
  await expect(page.getByText(/Connected to Amazon \(vendor/)).toBeVisible();
  await page.getByRole("button", { name: "Continue" }).click();

  await expect(h1(page, "Deploy the skill")).toBeVisible();
  await expect(page.getByRole("checkbox", { name: "en-AU" })).toBeChecked();
  await expect(page.getByRole("button", { name: "Deploy skill" })).toBeDisabled();
  for (const [question, answer] of [
    ["The skill collects personal information", "no"],
    ["The skill is directed at children under 13", "no"],
    ["The skill is export compliant", "yes"],
    ["The skill contains advertising", "no"],
    ["The skill allows purchases", "no"],
  ]) {
    await page.getByRole("radio", { name: `${question}: ${answer}` }).check();
  }
  await page.getByRole("button", { name: "Deploy skill" }).click();
  await expect(page.getByText(/Deployed. The skill is enabled for testing/)).toBeVisible({
    timeout: 15_000,
  });
  await page.getByRole("button", { name: "Continue" }).click();

  // The mock sends the first request a moment later.
  await expect(h1(page, "Say hello")).toBeVisible();
  await expect(page.getByText(/Request received from a new Echo/)).toBeVisible({ timeout: 15_000 });
  await page.getByLabel("Echo name").fill("Lounge Echo Show");
  await page.getByRole("button", { name: "Finish" }).click();

  await expect(h1(page, "Overview")).toBeVisible();
  await expect(page.getByText("Setup isn't finished yet")).toHaveCount(0);
  await expect(page.getByRole("table", { name: "Recent requests" })).toContainText("Lounge Echo Show");
});
