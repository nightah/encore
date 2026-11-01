import { readFileSync } from "node:fs";
import { mkdir } from "node:fs/promises";
import path from "node:path";
import { type Page, test } from "@playwright/test";

import { anonymise } from "@e2e/screenshots/anonymise";

/**
 * Screenshots of every page, light and dark, with sensitive details replaced by examples (see anonymise.ts).
 *
 * - SCREENSHOT_URL: the Encore to capture, e.g. https://encore.example.net; without it, the mock data is captured.
 * - SCREENSHOT_SESSION_FILE: a file holding the value of a signed-in browser's encore_session cookie, for an Encore that signs in with OpenID Connect or proxy headers. The session is used, never signed out.
 * - ENCORE_USERNAME (default admin) and ENCORE_PASSWORD or ENCORE_PASSWORD_FILE: otherwise, the admin to sign in as with the password. Prefer the file, so the password stays out of shell history.
 * - SCREENSHOT_REPLACE: more text to replace, comma-separated original=example pairs, such as MisatoNUC=Lounge. Without =example, the text is blurred.
 * - SCREENSHOT_DIR: where the images go; default docs/src/assets/screenshots.
 * - SCREENSHOT_ONLY: capture only these pages, comma-separated names (a phone page by its name without -phone).
 *
 * The phone project captures PHONE_SHOTS at iPhone size, named with -phone before the theme.
 */
const OUT = path.resolve(
  process.env.SCREENSHOT_DIR ?? path.join(import.meta.dirname, "../../../docs/src/assets/screenshots"),
);
const REPLACE = (process.env.SCREENSHOT_REPLACE ?? "")
  .split(",")
  .filter((pair) => pair.trim())
  .map((pair): [string, string] => {
    const [from = "", to = ""] = pair.split("=");
    return [from.trim(), to.trim()];
  });
const ONLY = (process.env.SCREENSHOT_ONLY ?? "").split(",").filter(Boolean);

interface Shot {
  name: string;
  path: string;
  heading: string | RegExp;
  /** Runs after the page loads, e.g. to open a panel. */
  prepare?: (page: Page) => Promise<void>;
}

const SHOTS: Shot[] = [
  { heading: "Overview", name: "overview", path: "/" },
  {
    heading: "Kodi",
    name: "kodi",
    path: "/kodi",
    // Let the network search finish, so the panel shows what it found.
    prepare: (page) => page.getByText(/^Searching for Kodi/).waitFor({ state: "hidden", timeout: 20_000 }),
  },
  { heading: "Echo devices", name: "echo-devices", path: "/echo-devices" },
  { heading: "Alexa skill", name: "alexa-skill", path: "/alexa-skill" },
  { heading: "Library", name: "library", path: "/library" },
  { heading: "Request log", name: "request-log", path: "/request-log" },
  { heading: "Settings", name: "settings", path: "/settings" },
  { heading: "Settings", name: "settings-access", path: "/settings?tab=access" },
  { heading: "Settings", name: "settings-storage", path: "/settings?tab=storage" },
];

/** The phone layouts: the menu bar and its sheets, tables as cards and a request opened in the list. */
const PHONE_SHOTS: Shot[] = [
  { heading: "Overview", name: "overview", path: "/" },
  {
    heading: "Overview",
    name: "more",
    path: "/",
    prepare: async (page) => {
      await page.getByRole("navigation", { name: "Menu bar" }).getByRole("button", { name: "More" }).click();
      await page.getByRole("dialog", { name: "More" }).waitFor();
      await page.waitForTimeout(500);
    },
  },
  {
    heading: "Overview",
    name: "arrange-menu",
    path: "/",
    prepare: async (page) => {
      await page.getByRole("navigation", { name: "Menu bar" }).getByRole("button", { name: "More" }).click();
      await page.getByRole("dialog", { name: "More" }).getByRole("button", { name: "Arrange menu" }).click();
      await page.getByRole("dialog", { name: "Arrange menu" }).waitFor();
      await page.waitForTimeout(500);
    },
  },
  { heading: "Echo devices", name: "echo-devices", path: "/echo-devices" },
  {
    heading: "Request log",
    name: "request-log",
    path: "/request-log",
    // Open a request that matched, so its candidates and Kodi calls show under its row.
    prepare: async (page) => {
      const list = page.getByRole("region", { name: "Requests" });
      await list.getByRole("button", { expanded: false }).filter({ hasText: "Played" }).nth(1).click();
      await list.getByRole("button", { name: "Replay (dry run)" }).waitFor();
      await page.waitForTimeout(500);
    },
  },
];

function password(): string {
  const file = process.env.ENCORE_PASSWORD_FILE;
  if (file) return readFileSync(file, "utf8").trim();
  return process.env.ENCORE_PASSWORD ?? "correct-horse-battery";
}

const MOCK = !process.env.SCREENSHOT_URL;

async function signIn(page: Page) {
  const sessionFile = process.env.SCREENSHOT_SESSION_FILE;
  if (sessionFile) {
    await page.context().addCookies([
      {
        name: "encore_session",
        url: test.info().project.use.baseURL ?? "",
        value: readFileSync(sessionFile, "utf8").trim(),
      },
    ]);
  }
  await page.goto("./");
  await page.waitForURL(/\/(login|setup)?(\?.*)?$/);
  if (!new URL(page.url()).pathname.endsWith("/login")) return;
  await page.getByLabel("Username").fill(process.env.ENCORE_USERNAME ?? "admin");
  await page.getByLabel("Password").fill(password());
  await page.getByRole("button", { exact: true, name: "Sign in" }).click();
  await page.waitForURL((url) => !url.pathname.endsWith("/login"));
}

/** Waits for the page's data: its heading, no loading placeholders, and a moment for live status to arrive. */
async function settle(page: Page, heading: string | RegExp) {
  await page.getByRole("heading", { level: 1, name: heading }).waitFor();
  await page.waitForFunction(() => document.querySelectorAll(".animate-pulse").length === 0, undefined, {
    timeout: 30_000,
  });
  await page.waitForTimeout(1500);
}

async function capture(page: Page, name: string) {
  await anonymise(page, { host: new URL(page.url()).hostname, replace: REPLACE });
  await page.screenshot({ animations: "disabled", path: path.join(OUT, `${name}.png`) });
}

for (const theme of ["light", "dark"] as const) {
  test.describe(theme, () => {
    test.use({ colorScheme: theme });

    test.beforeEach(async ({ page }) => {
      await mkdir(OUT, { recursive: true });
      await page.addInitScript((t) => window.localStorage.setItem("encore.theme", t), theme);
    });

    test("sign-in page", async ({ page }) => {
      test.skip(ONLY.length > 0 && !ONLY.includes("login"));
      test.skip(test.info().project.name === "phone", "the sign-in page looks the same on a phone");
      if (MOCK) {
        // The mock keeps its session in the browser; signing out ends only this test's.
        await signIn(page);
        await page.getByRole("button", { name: "Sign out" }).click();
      } else {
        await page.goto("./login");
      }
      await settle(page, "Sign in");
      await capture(page, `login-${theme}`);
    });

    test("pages", async ({ page }) => {
      const phone = test.info().project.name === "phone";
      await signIn(page);
      for (const shot of phone ? PHONE_SHOTS : SHOTS) {
        if (ONLY.length && !ONLY.includes(shot.name)) continue;
        await page.goto(`.${shot.path}`);
        await settle(page, shot.heading);
        await shot.prepare?.(page);
        await capture(page, phone ? `${shot.name}-phone-${theme}` : `${shot.name}-${theme}`);
      }
    });
  });
}
