import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { OIDC_PRESETS } from "@features/settings/oidcProviders";
import { getDb } from "@mocks/handlers";
import { server } from "@mocks/node";
import { failWith, recordRequests, renderApp } from "@test/utils";
import { THEME_STORAGE_KEY, initTheme } from "@theme/store";

function badgeNextTo(label: string) {
  const field = screen.getByLabelText(new RegExp(`^${label}`));
  const labelEl = document.querySelector(`label[for="${field.id}"]`);
  return { field, label: labelEl };
}

describe("Settings page", () => {
  beforeEach(() => {
    initTheme();
  });

  it("opens on General", async () => {
    renderApp("/settings");
    expect(await screen.findByRole("tab", { name: "General" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByLabelText("Timezone")).not.toHaveAttribute("readonly");
    expect(screen.getByRole("button", { name: "Save changes" })).toBeDisabled();
  });

  it("previews the theme and saves it with Save changes", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/settings");
    const save = await screen.findByRole("button", { name: "Save changes" });
    await user.click(await screen.findByRole("radio", { name: /Dark/ }));
    expect(document.documentElement.dataset.theme).toBe("dark");
    expect(window.localStorage.getItem(THEME_STORAGE_KEY)).toBeNull();
    await waitFor(() => expect(save).toBeEnabled());

    await user.click(save);
    expect(window.localStorage.getItem(THEME_STORAGE_KEY)).toBe("dark");
    await waitFor(() => expect(save).toBeDisabled());
    expect(log.some((r) => r.method === "PATCH")).toBe(false);
  });

  it("puts the saved theme back when leaving without saving", async () => {
    const user = userEvent.setup();
    const { router } = renderApp("/settings");
    await user.click(await screen.findByRole("radio", { name: /Dark/ }));
    expect(document.documentElement.dataset.theme).toBe("dark");
    await router.navigate({ to: "/" });
    await waitFor(() => expect(document.documentElement.dataset.theme).toBe("light"));
    expect(window.localStorage.getItem(THEME_STORAGE_KEY)).toBeNull();
  });

  it("switches the interface's language and remembers it in this browser", async () => {
    const user = userEvent.setup();
    renderApp("/settings");
    await user.selectOptions(await screen.findByRole("combobox", { name: "Language" }), "de");
    const save = screen.getByRole("button", { name: "Save changes" });
    await waitFor(() => expect(save).toBeEnabled());
    await user.click(save);
    expect(await screen.findByRole("heading", { level: 2, name: "Allgemein" })).toBeInTheDocument();
    expect(window.localStorage.getItem("encore.language")).toBe("de");
    expect(document.documentElement.lang).toBe("de");
  });

  it("names the browser's language, as it calls itself, in the option that follows it", async () => {
    const languages = vi.spyOn(navigator, "languages", "get").mockReturnValue(["it-IT", "en"]);
    try {
      renderApp("/settings");
      const picker = await screen.findByRole("combobox", { name: "Language" });
      expect(within(picker).getByRole("option", { name: "Match browser (Italiano)" })).toHaveValue("auto");
    } finally {
      languages.mockRestore();
    }
  });

  it("saves the log level", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/settings");
    await user.selectOptions(await screen.findByRole("combobox", { name: "Log level" }), "debug");
    const save = screen.getByRole("button", { name: "Save changes" });
    await waitFor(() => expect(save).toBeEnabled());
    await user.click(save);
    await waitFor(() => expect(getDb().settings.logging.level).toBe("debug"));
    expect(log.find((r) => r.method === "PATCH" && r.path === "/settings")?.body).toEqual({
      logging: { level: "debug" },
    });
  });

  it("saves the base path", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/settings");
    const field = await screen.findByRole("textbox", { name: "Base path" });
    expect(field).toHaveValue("/");
    await user.clear(field);
    await user.type(field, "/encore");
    const save = screen.getByRole("button", { name: "Save changes" });
    await waitFor(() => expect(save).toBeEnabled());
    await user.click(save);
    await waitFor(() => expect(getDb().settings.server.base_path).toBe("/encore"));
    expect(log.find((r) => r.method === "PATCH" && r.path === "/settings")?.body).toEqual({
      server: { base_path: "/encore" },
    });
  });

  it("offers only the skill's enabled locales as the default", async () => {
    getDb().skill.locales_enabled = ["en-AU"];
    renderApp("/settings?tab=general");
    const select = await screen.findByRole("combobox", { name: "Default locale" });
    await waitFor(() =>
      expect(
        within(select)
          .getAllByRole("option")
          .map((o) => o.textContent),
      ).toEqual(["en-AU"]),
    );
  });

  it("saves only the changed fields with the CSRF header", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    getDb().skill.locales_enabled = ["en-AU", "en-GB"];
    renderApp("/settings?tab=general");
    await screen.findByRole("option", { name: "en-GB" });
    await user.selectOptions(await screen.findByRole("combobox", { name: "Default locale" }), "en-GB");
    const save = screen.getByRole("button", { name: "Save changes" });
    await waitFor(() => expect(save).toBeEnabled());
    await user.click(save);
    await waitFor(() => expect(getDb().settings.locale.default_locale).toBe("en-GB"));
    const patch = log.find((r) => r.method === "PATCH" && r.path === "/settings");
    expect(patch?.csrf).toBe("mock-csrf-token");
    expect(patch?.body).toEqual({ locale: { default_locale: "en-GB" } });
    await waitFor(() => expect(save).toBeDisabled());
  });

  it("shows the fields for each sign-in method and saves the mode", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/settings?tab=access");

    expect(await screen.findByRole("button", { name: "Change password…" })).toBeInTheDocument();
    expect(screen.getByLabelText("Session length")).toBeInTheDocument();
    expect(screen.queryByLabelText("Issuer URL")).not.toBeInTheDocument();

    await user.click(screen.getByRole("radio", { name: /OpenID Connect/ }));
    expect(screen.getByLabelText("Issuer URL")).toHaveValue("https://auth.example.net");
    expect(screen.getByLabelText("Redirect URI to register with your provider")).toHaveAttribute("readonly");
    expect(screen.queryByRole("button", { name: "Change password…" })).not.toBeInTheDocument();

    await user.click(screen.getByRole("radio", { name: /Trusted proxy headers/ }));
    const { field, label } = badgeNextTo("Trusted proxy addresses");
    expect(field).toHaveAttribute("readonly");
    expect(label).toHaveTextContent("Managed by config");
    // The visible badge sits under the field, so the field lines up with its neighbours.
    expect(field.nextElementSibling).toHaveTextContent("Managed by config");
    expect(screen.getByLabelText("User header")).toHaveValue("Remote-User");
    expect(screen.getByText("Your proxy must strip these headers from incoming requests.")).toBeInTheDocument();

    await user.click(screen.getByRole("radio", { name: /OpenID Connect/ }));
    await user.click(screen.getByRole("button", { name: "Save changes" }));
    await waitFor(() => expect(getDb().settings.auth.mode).toBe("oidc"));
    expect(log.find((r) => r.method === "PATCH")?.body).toEqual({ auth: { mode: "oidc" } });
  });

  it("turns the break-glass login off and on", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/settings?tab=access");
    await user.click(await screen.findByRole("radio", { name: /OpenID Connect/ }));
    const breakGlass = screen.getByRole("checkbox", { name: "Keep local password as a break-glass login" });
    expect(breakGlass).toBeChecked();
    expect(breakGlass).not.toHaveAttribute("aria-disabled", "true");
    await user.click(breakGlass);
    expect(breakGlass).not.toBeChecked();
    await user.click(screen.getByRole("button", { name: "Save changes" }));
    await waitFor(() => expect(getDb().settings.auth.oidc.break_glass).toBe(false));
    expect(log.find((r) => r.method === "PATCH")?.body).toEqual({
      auth: { mode: "oidc", oidc: { break_glass: false } },
    });
  });

  it("starts the one-time code dialog empty each time it opens", async () => {
    const user = userEvent.setup();
    getDb().settings.auth.local.totp_enabled = false;
    renderApp("/settings?tab=access");

    await user.click(await screen.findByRole("checkbox", { name: /Require a one-time code/ }));
    let dialog = await screen.findByRole("dialog", { name: "Turn on one-time codes" });
    await user.type(within(dialog).getByRole("textbox", { name: "Code" }), "123");
    await user.click(within(dialog).getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());

    await user.click(screen.getByRole("checkbox", { name: /Require a one-time code/ }));
    dialog = await screen.findByRole("dialog", { name: "Turn on one-time codes" });
    expect(within(dialog).getByRole("textbox", { name: "Code" })).toHaveValue("");
  });

  it("validates OIDC fields before saving", async () => {
    const user = userEvent.setup();
    renderApp("/settings?tab=access");
    await user.click(await screen.findByRole("radio", { name: /OpenID Connect/ }));
    await user.clear(screen.getByLabelText("Issuer URL"));
    await user.click(screen.getByRole("button", { name: "Save changes" }));
    expect(await screen.findByText(/Enter the issuer URL/)).toBeInTheDocument();
    expect(getDb().settings.auth.mode).toBe("local");
  });

  it("fills in a provider's settings and shows its side of the setup", async () => {
    const user = userEvent.setup();
    getDb().settings.auth.oidc = {
      ...getDb().settings.auth.oidc,
      client_id: "",
      groups_claim: "",
      scopes: [],
    };
    renderApp("/settings?tab=access");
    await user.click(await screen.findByRole("radio", { name: /OpenID Connect/ }));
    await user.selectOptions(screen.getByRole("combobox", { name: "Provider" }), "authelia");

    expect(screen.getByLabelText("Scopes")).toHaveValue("openid profile email groups");
    expect(screen.getByLabelText("Groups claim")).toHaveValue("groups");
    expect(screen.getByLabelText("Client ID")).toHaveValue("encore");
    const snippet = screen.getByRole("figure", { name: /identity_providers.oidc/ });
    expect(snippet).toHaveTextContent("client_id: 'encore'");
    expect(snippet).toHaveTextContent("https://encore.example.net/api/v1/auth/oidc/callback");

    await user.selectOptions(screen.getByRole("combobox", { name: "Provider" }), "other");
    expect(screen.queryByRole("figure")).not.toBeInTheDocument();
  });

  it("shows a 409 from the server inline", async () => {
    const user = userEvent.setup();
    failWith("patch", "/settings", 409, "auth.session_hours is managed by config");
    renderApp("/settings?tab=access");
    await user.selectOptions(await screen.findByLabelText("Session length"), "24");
    await user.click(screen.getByRole("button", { name: "Save changes" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("auth.session_hours is managed by config");
  });

  it("puts 422 field errors next to the field", async () => {
    const user = userEvent.setup();
    server.use(
      http.patch("*/api/v1/settings", () =>
        HttpResponse.json(
          {
            detail: "Check the highlighted fields.",
            errors: [{ location: "body.auth.local.session_hours", message: "must be at most 8760" }],
            status: 422,
            title: "Unprocessable Entity",
          },
          { headers: { "Content-Type": "application/problem+json" }, status: 422 },
        ),
      ),
    );
    renderApp("/settings?tab=access");
    await user.selectOptions(await screen.findByLabelText("Session length"), "720");
    await user.click(screen.getByRole("button", { name: "Save changes" }));
    expect(await screen.findByText("must be at most 8760")).toBeInTheDocument();
  });

  it("tests the storage connection and shows the local path as managed", async () => {
    const user = userEvent.setup();
    renderApp("/settings?tab=storage");
    await user.click(await screen.findByRole("button", { name: "Test connection" }));
    expect(await screen.findByText("Wrote, read and deleted a test object in 64 ms")).toBeInTheDocument();

    await user.click(screen.getByRole("radio", { name: /Local disk/ }));
    const { field, label } = badgeNextTo("Data directory");
    expect(field).toHaveAttribute("readonly");
    expect(label).toHaveTextContent("Managed by config");
    // Unsaved changes can't be tested.
    expect(screen.getByRole("button", { name: "Test connection" })).toBeDisabled();
    expect(screen.getByText("Save changes to test them.")).toBeInTheDocument();
  });

  it("saves the backup folder", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/settings?tab=storage");
    const folder = await screen.findByRole("textbox", { name: "Backup folder" });
    expect(folder).toHaveValue("Encore");
    await user.clear(folder);
    await user.type(folder, "Kodi backups");
    const save = screen.getByRole("button", { name: "Save changes" });
    await waitFor(() => expect(save).toBeEnabled());
    await user.click(save);
    await waitFor(() => expect(getDb().settings.storage.backups.prefix).toBe("Kodi backups"));
    const patch = log.find((r) => r.method === "PATCH" && r.path === "/settings");
    expect(patch?.body).toEqual({ storage: { backups: { prefix: "Kodi backups" } } });
  });

  it("saves an empty backup folder for the top of the storage location", async () => {
    const user = userEvent.setup();
    renderApp("/settings?tab=storage");
    await user.clear(await screen.findByRole("textbox", { name: "Backup folder" }));
    const save = screen.getByRole("button", { name: "Save changes" });
    await waitFor(() => expect(save).toBeEnabled());
    await user.click(save);
    await waitFor(() => expect(getDb().settings.storage.backups.prefix).toBe(""));
  });

  it("shows a failed storage test", async () => {
    const user = userEvent.setup();
    getDb().settings.storage.backend = "webdav";
    renderApp("/settings?tab=storage");
    await user.click(await screen.findByRole("button", { name: "Test connection" }));
    expect(await screen.findByText(/401 Unauthorized from the WebDAV server/)).toBeInTheDocument();
  });

  it("shows the key source as managed and rotates the key after confirming", async () => {
    const user = userEvent.setup();
    renderApp("/settings?tab=secrets");
    await screen.findByRole("heading", { name: "Secrets" });
    const { field, label } = badgeNextTo("Encryption key source");
    expect(field).toHaveAttribute("readonly");
    expect(label).toHaveTextContent("Managed by config");

    await user.click(screen.getByRole("button", { name: "Rotate encryption key…" }));
    const dialog = await screen.findByRole("dialog", { name: "Rotate the encryption key?" });
    await user.click(within(dialog).getByRole("button", { name: "Rotate key" }));
    await waitFor(() => expect(getDb().settings.secrets.key_id).toBe("k3"));
    await waitFor(() => expect(dialog).not.toBeInTheDocument());
    expect(await screen.findByLabelText("Active key ID")).toHaveDisplayValue(/^k3 \(rotated /);
  });

  it("offers the confirmation sound, on by default", async () => {
    renderApp("/settings?tab=features");
    expect(await screen.findByRole("switch", { name: /Confirm commands with a sound/ })).toBeChecked();
    expect(screen.getAllByRole("switch")[0]).toHaveAccessibleName(/Confirm commands with a sound/);
  });

  it("saves a feature switch straight away", async () => {
    const user = userEvent.setup();
    renderApp("/settings?tab=features");
    const before = getDb().settings.features.shutdown_quits;
    await user.click(await screen.findByRole("switch", { name: "“Shutdown” quits Kodi" }));
    await waitFor(() => expect(getDb().settings.features.shutdown_quits).toBe(!before));
  });

  it("saves the queue limit when the field is left, and ignores numbers out of range", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/settings?tab=features");
    const field = await screen.findByRole("spinbutton", { name: /Queue limit/ });
    expect(field).toHaveValue(100);

    await user.clear(field);
    await user.type(field, "0");
    await user.tab();
    expect(field).toHaveValue(100);
    expect(log.some((r) => r.method === "PATCH")).toBe(false);

    await user.clear(field);
    await user.type(field, "25{Enter}");
    await waitFor(() => expect(getDb().settings.features.queue_limit).toBe(25));
    expect(log.find((r) => r.method === "PATCH")?.body).toEqual({ features: { queue_limit: 25 } });
  });

  it("puts the password fields in a form that the dialog's button submits, so Enter submits too", async () => {
    const user = userEvent.setup();
    renderApp("/settings?tab=access");

    await user.click(await screen.findByRole("button", { name: "Change password…" }));
    const dialog = await screen.findByRole("dialog", { name: "Change password" });
    const current = within(dialog).getByLabelText("Current password") as HTMLInputElement;
    const submit = within(dialog).getByRole("button", { name: "Change password" }) as HTMLButtonElement;
    expect(current.form).not.toBeNull();
    expect(submit.form).toBe(current.form);

    await user.type(current, "correct-horse-battery");
    await user.type(within(dialog).getByLabelText("New password"), "a-much-longer-password");
    await user.type(within(dialog).getByLabelText("Confirm new password"), "a-much-longer-password");
    await user.click(submit);
    await waitFor(() => expect(getDb().admin?.password).toBe("a-much-longer-password"));
    await waitFor(() => expect(dialog).not.toBeInTheDocument());
  });
});

async function saveChanges(user: ReturnType<typeof userEvent.setup>) {
  const save = screen.getByRole("button", { name: "Save changes" });
  await waitFor(() => expect(save).toBeEnabled());
  await user.click(save);
}

describe("Access settings", () => {
  it("asks for the client ID before saving OpenID Connect", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/settings?tab=access");
    await user.click(await screen.findByRole("radio", { name: /OpenID Connect/ }));
    await user.clear(screen.getByLabelText("Client ID"));
    await saveChanges(user);

    expect(await screen.findByText("Enter the client ID.")).toBeInTheDocument();
    expect(screen.getByLabelText("Client ID")).toHaveAttribute("aria-invalid", "true");
    expect(log.some((r) => r.method === "PATCH")).toBe(false);
  });

  it("asks for the user header before saving trusted proxy headers", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/settings?tab=access");
    await user.click(await screen.findByRole("radio", { name: /Trusted proxy headers/ }));
    await user.clear(screen.getByLabelText("User header"));
    await saveChanges(user);

    expect(await screen.findByText("Enter the header that carries the user name.")).toBeInTheDocument();
    expect(log.some((r) => r.method === "PATCH")).toBe(false);
  });

  it("saves trusted proxies as a list, whatever separates them", async () => {
    getDb().settings.managed = [];
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/settings?tab=access");
    await user.click(await screen.findByRole("radio", { name: /Trusted proxy headers/ }));
    const proxies = screen.getByLabelText("Trusted proxy addresses");
    await user.clear(proxies);
    await user.type(proxies, " 10.0.0.1,10.0.0.2   10.0.0.3, ,");
    await saveChanges(user);

    await waitFor(() =>
      expect(log.find((r) => r.method === "PATCH")?.body).toEqual({
        auth: { header: { trusted_proxies: ["10.0.0.1", "10.0.0.2", "10.0.0.3"] }, mode: "header" },
      }),
    );
  });

  it("saves the session length as hours and the lockout as attempts and minutes", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/settings?tab=access");
    await user.selectOptions(await screen.findByLabelText("Session length"), "30 days");
    await user.selectOptions(screen.getByLabelText("Lock out after"), "10 failed attempts for 1 hour");
    await saveChanges(user);

    await waitFor(() =>
      expect(log.find((r) => r.method === "PATCH")?.body).toEqual({
        auth: { local: { lockout_attempts: 10, lockout_minutes: 60, session_hours: 720 } },
      }),
    );
    expect(getDb().settings.auth.local).toMatchObject({ lockout_attempts: 10, lockout_minutes: 60 });
  });

  it("keeps a saved session length and lockout that aren't among the choices", async () => {
    Object.assign(getDb().settings.auth.local, { lockout_attempts: 3, lockout_minutes: 5, session_hours: 12 });
    renderApp("/settings?tab=access");

    const session = await screen.findByLabelText("Session length");
    expect(session).toHaveDisplayValue("12 hours");
    expect(
      within(session)
        .getAllByRole("option")
        .map((o) => o.textContent),
    ).toEqual(["12 hours", "7 days", "1 day", "30 days"]);
    expect(screen.getByLabelText("Lock out after")).toHaveDisplayValue("3 failed attempts for 5 minutes");
  });

  it("splits scopes on spaces and sends a new client secret", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/settings?tab=access");
    await user.click(await screen.findByRole("radio", { name: /OpenID Connect/ }));
    expect(screen.getByText("A secret is stored. Type a new one to replace it.")).toBeInTheDocument();
    const scopes = screen.getByLabelText("Scopes");
    await user.clear(scopes);
    await user.type(scopes, "  openid   email ");
    await user.type(screen.getByLabelText("Client secret"), "s3cret");
    await saveChanges(user);

    await waitFor(() =>
      expect(log.find((r) => r.method === "PATCH")?.body).toEqual({
        auth: { mode: "oidc", oidc: { client_secret: "s3cret", scopes: ["openid", "email"] } },
      }),
    );
  });

  it("says a public client needs no secret when none is stored", async () => {
    getDb().settings.auth.oidc.has_client_secret = false;
    const user = userEvent.setup();
    renderApp("/settings?tab=access");
    await user.click(await screen.findByRole("radio", { name: /OpenID Connect/ }));
    expect(screen.getByText("Leave empty for a public client with PKCE only.")).toBeInTheDocument();
    expect(screen.getByLabelText("Client secret")).toHaveAttribute("placeholder", "");
  });

  it("keeps a client ID already entered and fields managed by config when picking a provider", async () => {
    const db = getDb();
    db.settings.auth.oidc.client_id = "my-encore";
    db.settings.auth.oidc.scopes = ["openid"];
    db.settings.auth.oidc.groups_claim = "roles";
    db.settings.managed = ["auth.oidc.scopes"];
    const user = userEvent.setup();
    renderApp("/settings?tab=access");
    await user.click(await screen.findByRole("radio", { name: /OpenID Connect/ }));
    await user.selectOptions(screen.getByRole("combobox", { name: "Provider" }), "keycloak");

    expect(screen.getByLabelText(/^Client ID/)).toHaveValue("my-encore");
    expect(screen.getByLabelText(/^Scopes/)).toHaveValue("openid");
    expect(screen.getByLabelText(/^Groups claim/)).toHaveValue(OIDC_PRESETS.keycloak.groupsClaim);
    expect(screen.getByRole("figure")).toHaveTextContent("my-encore");
    expect(screen.getByText(/The realm's address/)).toBeInTheDocument();
  });

  it("keeps a groups claim managed by config when picking a provider", async () => {
    const db = getDb();
    db.settings.auth.oidc.scopes = ["openid"];
    db.settings.auth.oidc.groups_claim = "roles";
    db.settings.managed = ["auth.oidc.groups_claim"];
    const user = userEvent.setup();
    renderApp("/settings?tab=access");
    await user.click(await screen.findByRole("radio", { name: /OpenID Connect/ }));
    await user.selectOptions(screen.getByRole("combobox", { name: "Provider" }), "keycloak");

    expect(screen.getByLabelText(/^Groups claim/)).toHaveValue("roles");
    expect(screen.getByLabelText(/^Scopes/)).toHaveValue(OIDC_PRESETS.keycloak.scopes);
  });

  it("locks the break-glass login when config sets it", async () => {
    getDb().settings.managed = ["auth.oidc.break_glass"];
    const user = userEvent.setup();
    renderApp("/settings?tab=access");
    await user.click(await screen.findByRole("radio", { name: /OpenID Connect/ }));

    const breakGlass = screen.getByRole("checkbox", { name: "Keep local password as a break-glass login" });
    expect(breakGlass).toHaveAttribute("aria-disabled", "true");
    expect(screen.getByText(/Set by ENCORE_BREAK_GLASS or encore.yml/)).toBeInTheDocument();
  });

  it("offers a test sign-in only once OpenID Connect is saved", async () => {
    const user = userEvent.setup();
    renderApp("/settings?tab=access");
    await user.click(await screen.findByRole("radio", { name: /OpenID Connect/ }));
    expect(screen.getByRole("button", { name: "Test sign-in" })).toBeDisabled();
    expect(screen.getByText("Save with OpenID Connect selected to test it.")).toBeInTheDocument();

    await saveChanges(user);
    const link = await screen.findByRole("link", { name: "Test sign-in" });
    expect(link).toHaveAttribute("href", "/api/v1/auth/oidc/login");
    expect(link).toHaveAttribute("target", "_blank");
  });

  it("shows a validation error for a setting the form doesn't show above the form", async () => {
    const user = userEvent.setup();
    server.use(
      http.patch("*/api/v1/settings", () =>
        HttpResponse.json(
          {
            detail: "auth.local.username can't be changed.",
            errors: [{ location: "body.auth.local.username", message: "read-only" }],
            status: 422,
            title: "Unprocessable Entity",
          },
          { headers: { "Content-Type": "application/problem+json" }, status: 422 },
        ),
      ),
    );
    renderApp("/settings?tab=access");
    await user.selectOptions(await screen.findByLabelText("Session length"), "24");
    await saveChanges(user);

    expect(await screen.findByRole("alert")).toHaveTextContent("auth.local.username can't be changed.");
  });

  it("shows a network failure above the form", async () => {
    const user = userEvent.setup();
    server.use(http.patch("*/api/v1/settings", () => HttpResponse.error()));
    renderApp("/settings?tab=access");
    await user.selectOptions(await screen.findByLabelText("Session length"), "24");
    await saveChanges(user);

    expect(await screen.findByRole("alert")).toHaveTextContent("The Encore server could not be reached.");
    expect(screen.getByRole("button", { name: "Save changes" })).toBeEnabled();
  });
});

describe("Change password dialog", () => {
  async function open(user: ReturnType<typeof userEvent.setup>) {
    renderApp("/settings?tab=access");
    await user.click(await screen.findByRole("button", { name: "Change password…" }));
    return within(await screen.findByRole("dialog", { name: "Change password" }));
  }

  it("checks the passwords before sending them", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    const dialog = await open(user);

    await user.type(dialog.getByLabelText("New password"), "short");
    await user.type(dialog.getByLabelText("Confirm new password"), "different");
    await user.click(dialog.getByRole("button", { name: "Change password" }));

    expect(await dialog.findByText("Enter your current password.")).toBeInTheDocument();
    expect(dialog.getByText("Use at least 12 characters.")).toBeInTheDocument();
    expect(dialog.getByText("The passwords don't match.")).toBeInTheDocument();
    expect(log.some((r) => r.method === "PUT")).toBe(false);
  });

  it("needs a new password of at least 12 characters", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    const dialog = await open(user);
    const password = dialog.getByLabelText("New password");
    const confirm = dialog.getByLabelText("Confirm new password");

    await user.type(dialog.getByLabelText("Current password"), "correct-horse-battery");
    await user.type(password, "elevenchars");
    await user.type(confirm, "elevenchars");
    await user.click(dialog.getByRole("button", { name: "Change password" }));
    expect(await dialog.findByText("Use at least 12 characters.")).toBeInTheDocument();
    expect(log.some((r) => r.method === "PUT")).toBe(false);

    await user.type(password, "!");
    await user.type(confirm, "!");
    await user.click(dialog.getByRole("button", { name: "Change password" }));
    await waitFor(() => expect(getDb().admin?.password).toBe("elevenchars!"));
  });

  it("shows a wrong current password, and starts empty after cancelling", async () => {
    const user = userEvent.setup();
    let dialog = await open(user);

    await user.type(dialog.getByLabelText("Current password"), "not-it");
    await user.type(dialog.getByLabelText("New password"), "a-much-longer-password");
    await user.type(dialog.getByLabelText("Confirm new password"), "a-much-longer-password");
    await user.click(dialog.getByRole("button", { name: "Change password" }));
    expect(await dialog.findByText("the username or password is incorrect")).toBeInTheDocument();
    expect(getDb().admin?.password).toBe("correct-horse-battery");

    await user.click(dialog.getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Change password" })).not.toBeInTheDocument());
    await user.click(screen.getByRole("button", { name: "Change password…" }));
    dialog = within(await screen.findByRole("dialog", { name: "Change password" }));
    expect(dialog.getByLabelText("Current password")).toHaveValue("");
    expect(dialog.queryByText("the username or password is incorrect")).not.toBeInTheDocument();
  });
});

describe("One-time code dialog", () => {
  it("turns one-time codes on with the code from the authenticator app", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/settings?tab=access");

    await user.click(await screen.findByRole("checkbox", { name: /Require a one-time code/ }));
    const dialog = within(await screen.findByRole("dialog", { name: "Turn on one-time codes" }));
    expect(await dialog.findByLabelText("Secret")).toHaveValue("JBSWY3DPEHPK3PXP");
    expect(dialog.getByRole("link", { name: "Open in an authenticator app" })).toHaveAttribute(
      "href",
      "otpauth://totp/Encore:admin?secret=JBSWY3DPEHPK3PXP&issuer=Encore",
    );

    await user.type(dialog.getByRole("textbox", { name: "Code" }), "000000");
    await user.click(dialog.getByRole("button", { name: "Turn on" }));
    expect(await dialog.findByText("the one-time code is incorrect")).toBeInTheDocument();

    await user.clear(dialog.getByRole("textbox", { name: "Code" }));
    await user.type(dialog.getByRole("textbox", { name: "Code" }), " 123456 ");
    await user.click(dialog.getByRole("button", { name: "Turn on" }));
    await waitFor(() =>
      expect(screen.queryByRole("dialog", { name: "Turn on one-time codes" })).not.toBeInTheDocument(),
    );
    expect(log.filter((r) => r.path === "/auth/totp/confirm").map((r) => r.body)).toEqual([
      { code: "000000" },
      { code: "123456" },
    ]);
    await waitFor(() => expect(screen.getByRole("checkbox", { name: /Require a one-time code/ })).toBeChecked());
    expect((await screen.findAllByText("One-time codes turned on.")).length).toBeGreaterThan(0);
  });

  it("lets Enter in the code field confirm, as the dialog's button submits the code's form", async () => {
    const user = userEvent.setup();
    renderApp("/settings?tab=access");

    await user.click(await screen.findByRole("checkbox", { name: /Require a one-time code/ }));
    const dialog = within(await screen.findByRole("dialog", { name: "Turn on one-time codes" }));
    await dialog.findByLabelText("Secret");
    const code = dialog.getByRole("textbox", { name: "Code" }) as HTMLInputElement;
    const submit = dialog.getByRole("button", { name: "Turn on" }) as HTMLButtonElement;
    // With the secret field shown the form has two text fields, so a browser submits on Enter only through a submit button.
    expect(code.form).not.toBeNull();
    expect(submit.form).toBe(code.form);

    await user.type(code, "123456");
    await user.click(submit);
    await waitFor(() => expect(getDb().settings.auth.local.totp_enabled).toBe(true));
  });

  it("can't be turned on until the secret has been created", async () => {
    failWith("post", "/auth/totp/enroll", 500, "Couldn't create a secret.");
    const user = userEvent.setup();
    renderApp("/settings?tab=access");

    await user.click(await screen.findByRole("checkbox", { name: /Require a one-time code/ }));
    const dialog = within(await screen.findByRole("dialog", { name: "Turn on one-time codes" }));
    expect(await dialog.findByText("Couldn't create a secret.")).toBeInTheDocument();
    expect(dialog.getByRole("button", { name: "Turn on" })).toBeDisabled();
    expect(dialog.queryByLabelText("Secret")).not.toBeInTheDocument();
  });

  it("turns one-time codes off after confirming a code, without enrolling again", async () => {
    getDb().settings.auth.local.totp_enabled = true;
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/settings?tab=access");

    const totp = await screen.findByRole("checkbox", { name: /Require a one-time code/ });
    expect(totp).toBeChecked();
    await user.click(totp);
    const dialog = within(await screen.findByRole("dialog", { name: "Turn off one-time codes" }));
    expect(dialog.getByText("Enter a code from your authenticator app to confirm.")).toBeInTheDocument();
    expect(dialog.queryByLabelText("Secret")).not.toBeInTheDocument();
    await user.type(dialog.getByRole("textbox", { name: "Code" }), "123456");
    await user.click(dialog.getByRole("button", { name: "Turn off" }));

    await waitFor(() => expect(getDb().settings.auth.local.totp_enabled).toBe(false));
    expect(log.some((r) => r.path === "/auth/totp/enroll")).toBe(false);
    expect(log.find((r) => r.path === "/auth/totp/disable")?.body).toEqual({ code: "123456" });
    await waitFor(() => expect(screen.getByRole("checkbox", { name: /Require a one-time code/ })).not.toBeChecked());
  });
});

describe("Storage settings", () => {
  it("saves the S3 switches as the values the server expects", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/settings?tab=storage");

    await user.click(await screen.findByRole("checkbox", { name: "Path-style addressing" }));
    await user.click(screen.getByRole("checkbox", { name: "Server-side encryption" }));
    await user.click(screen.getByRole("checkbox", { name: "Custom CA certificate" }));
    await user.type(screen.getByLabelText("CA certificate (PEM)"), "-----BEGIN CERTIFICATE-----");
    await user.type(screen.getByLabelText("Secret key"), "minio-secret");
    await saveChanges(user);

    await waitFor(() =>
      expect(log.find((r) => r.method === "PATCH")?.body).toEqual({
        storage: {
          s3: {
            addressing: "auto",
            ca_bundle: "-----BEGIN CERTIFICATE-----",
            secret_key: "minio-secret",
            sse: "AES256",
          },
        },
      }),
    );
  });

  it("clears the CA bundle when the custom certificate is unticked", async () => {
    getDb().settings.storage.s3.ca_bundle = "-----BEGIN CERTIFICATE-----";
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/settings?tab=storage");

    expect(await screen.findByLabelText("CA certificate (PEM)")).toHaveValue("-----BEGIN CERTIFICATE-----");
    await user.click(screen.getByRole("checkbox", { name: "Custom CA certificate" }));
    expect(screen.queryByLabelText("CA certificate (PEM)")).not.toBeInTheDocument();
    await saveChanges(user);

    await waitFor(() =>
      expect(log.find((r) => r.method === "PATCH")?.body).toEqual({ storage: { s3: { ca_bundle: "" } } }),
    );
  });

  it("sends nothing when the only change has nothing to save", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/settings?tab=storage");

    await user.click(await screen.findByRole("checkbox", { name: "Custom CA certificate" }));
    await saveChanges(user);

    await waitFor(() => expect(screen.getByRole("button", { name: "Save changes" })).toBeDisabled());
    expect(log.some((r) => r.method === "PATCH")).toBe(false);
  });

  it("saves path-style addressing on and server-side encryption off", async () => {
    Object.assign(getDb().settings.storage.s3, { addressing: "auto", has_secret_key: false, sse: "AES256" });
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/settings?tab=storage");

    expect(await screen.findByText("Encrypted at rest.")).toBeInTheDocument();
    expect(screen.getByLabelText("Secret key")).toHaveAttribute("placeholder", "");
    await user.click(screen.getByRole("checkbox", { name: "Path-style addressing" }));
    await user.click(screen.getByRole("checkbox", { name: "Server-side encryption" }));
    await saveChanges(user);

    await waitFor(() =>
      expect(log.find((r) => r.method === "PATCH")?.body).toEqual({ storage: { s3: { addressing: "path", sse: "" } } }),
    );
  });

  it("says which key encrypts the stored S3 secret", async () => {
    renderApp("/settings?tab=storage");
    expect(await screen.findByText("Stored, encrypted with key k2. Type a new one to replace it.")).toBeInTheDocument();
  });

  it("saves WebDAV credentials", async () => {
    getDb().settings.storage.backend = "webdav";
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/settings?tab=storage");

    const url = await screen.findByLabelText("WebDAV URL");
    await user.clear(url);
    await user.type(url, "https://dav.example.net/encore/");
    await user.type(screen.getByLabelText("App password"), "app-pass");
    await saveChanges(user);

    await waitFor(() =>
      expect(log.find((r) => r.method === "PATCH")?.body).toEqual({
        storage: { webdav: { password: "app-pass", url: "https://dav.example.net/encore/" } },
      }),
    );
    await waitFor(() => expect(getDb().settings.storage.webdav.has_password).toBe(true));
  });

  it("explains a failed test that gave no reason", async () => {
    server.use(http.post("*/api/v1/settings/storage/test", () => HttpResponse.json({ error: "", ms: 0, ok: false })));
    const user = userEvent.setup();
    renderApp("/settings?tab=storage");
    await user.click(await screen.findByRole("button", { name: "Test connection" }));
    expect(await screen.findByText("The test object couldn't be written.")).toBeInTheDocument();
  });

  it("shows why the storage test couldn't run", async () => {
    failWith("post", "/settings/storage/test", 500, "Storage is unavailable.");
    const user = userEvent.setup();
    renderApp("/settings?tab=storage");
    await user.click(await screen.findByRole("button", { name: "Test connection" }));
    expect(await screen.findByText("Storage is unavailable.")).toBeInTheDocument();
  });

  it("forgets the last test result when the backend changes", async () => {
    const user = userEvent.setup();
    renderApp("/settings?tab=storage");
    await user.click(await screen.findByRole("button", { name: "Test connection" }));
    expect(await screen.findByText(/Wrote, read and deleted/)).toBeInTheDocument();

    await user.click(screen.getByRole("radio", { name: /WebDAV/ }));
    await user.click(screen.getByRole("radio", { name: /S3-compatible/ }));
    expect(screen.queryByText(/Wrote, read and deleted/)).not.toBeInTheDocument();
  });

  it("keeps a saved schedule and number of backups that aren't among the choices", async () => {
    Object.assign(getDb().settings.storage.backups, { keep: 21, last_backup_at: null, schedule: "monthly@1 03:00" });
    renderApp("/settings?tab=storage");

    const schedule = await screen.findByLabelText("Backups");
    expect(schedule).toHaveValue("monthly@1 03:00");
    expect(within(schedule).getAllByRole("option")[0]).toHaveTextContent("monthly@1 03:00");
    const keep = screen.getByLabelText("Keep");
    expect(keep).toHaveDisplayValue("21 backups");
    expect(
      within(keep)
        .getAllByRole("option")
        .map((o) => o.textContent),
    ).toEqual(["7 backups", "14 backups", "21 backups", "30 backups"]);
    expect(screen.getByText("Never")).toBeInTheDocument();
  });

  it("saves the number of backups to keep as a number", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/settings?tab=storage");
    await user.selectOptions(await screen.findByLabelText("Keep"), "30 backups");
    await user.selectOptions(screen.getByLabelText("Backups"), "Off");
    await saveChanges(user);

    await waitFor(() =>
      expect(log.find((r) => r.method === "PATCH")?.body).toEqual({
        storage: { backups: { keep: 30, schedule: "off" } },
      }),
    );
  });

  it("runs a backup now", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/settings?tab=storage");
    await user.click(await screen.findByRole("button", { name: "Run backup now" }));

    expect((await screen.findAllByText("Backup started.")).length).toBeGreaterThan(0);
    expect(log.find((r) => r.method === "POST")?.path).toBe("/settings/backups/run");
  });

  it("locks the backend choice when config sets it", async () => {
    getDb().settings.managed = ["storage.backend"];
    renderApp("/settings?tab=storage");
    expect(await screen.findByRole("radio", { name: /Local disk/ })).toHaveAttribute("aria-disabled", "true");
  });
});

describe("Secrets settings", () => {
  it.each([
    ["generated", "Generated by Encore and kept in the data directory"],
    ["config", "Set in encore.yml"],
  ] as const)("describes a key source of %s", async (source, label) => {
    getDb().settings.secrets.key_source = source;
    renderApp("/settings?tab=secrets");
    expect(await screen.findByLabelText(/^Encryption key source/)).toHaveValue(label);
  });

  it("shows only the key ID until the key is first rotated", async () => {
    getDb().settings.secrets.rotated_at = null;
    renderApp("/settings?tab=secrets");
    expect(await screen.findByLabelText("Active key ID")).toHaveValue("k2");
  });

  it("says when the key was last rotated", async () => {
    getDb().settings.secrets.rotated_at = "2026-03-04T05:06:07Z";
    renderApp("/settings?tab=secrets");
    expect(await screen.findByLabelText("Active key ID")).toHaveValue("k2 (rotated 4 Mar 2026)");
  });

  it("keeps the key when the rotation is cancelled", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/settings?tab=secrets");
    await user.click(await screen.findByRole("button", { name: "Rotate encryption key…" }));
    const dialog = await screen.findByRole("dialog", { name: "Rotate the encryption key?" });
    await user.click(within(dialog).getByRole("button", { name: "Cancel" }));

    await waitFor(() => expect(dialog).not.toBeInTheDocument());
    expect(log.some((r) => r.method === "POST")).toBe(false);
    expect(getDb().settings.secrets.key_id).toBe("k2");
  });
});

describe("Feature settings", () => {
  it("locks features set by config", async () => {
    getDb().settings.managed = ["features.deep_search", "features.queue_limit"];
    renderApp("/settings?tab=features");

    const deep = await screen.findByRole("switch", { name: "Deep search for “play X”" });
    expect(deep).toHaveAttribute("aria-disabled", "true");
    expect(screen.getByRole("switch", { name: /Confirm commands/ })).not.toHaveAttribute("aria-disabled", "true");
    expect(screen.getByRole("spinbutton", { name: /Queue limit/ })).toBeDisabled();
    expect(within(deep.parentElement as HTMLElement).getByText("Managed by config")).toBeInTheDocument();
    const confirm = screen.getByRole("switch", { name: /Confirm commands/ });
    expect(within(confirm.parentElement as HTMLElement).queryByText("Managed by config")).not.toBeInTheDocument();
  });

  it("shows why a feature couldn't be saved", async () => {
    failWith("patch", "/settings", 500, "Settings are read-only right now.");
    const user = userEvent.setup();
    renderApp("/settings?tab=features");
    await user.click(await screen.findByRole("switch", { name: "Deep search for “play X”" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Settings are read-only right now.");
  });

  it.each([
    ["above the maximum", "1001"],
    ["not a whole number", "2.5"],
    ["unchanged", "100"],
  ])("doesn't save a queue limit that is %s", async (_case, value) => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/settings?tab=features");
    const field = await screen.findByRole("spinbutton", { name: /Queue limit/ });

    await user.clear(field);
    await user.type(field, `${value}{Enter}`);
    await user.tab();

    expect(field).toHaveValue(100);
    expect(log.some((r) => r.method === "PATCH")).toBe(false);
  });

  it("saves the largest queue limit allowed", async () => {
    const user = userEvent.setup();
    renderApp("/settings?tab=features");
    const field = await screen.findByRole("spinbutton", { name: /Queue limit/ });
    await user.clear(field);
    await user.type(field, "1000");
    await user.tab();
    await waitFor(() => expect(getDb().settings.features.queue_limit).toBe(1000));
  });
});

describe("Import and export", () => {
  it("exports the configuration with or without encrypted secrets", async () => {
    const user = userEvent.setup();
    renderApp("/settings?tab=backup");

    const exportLink = await screen.findByRole("link", { name: "Export configuration" });
    expect(exportLink).toHaveAttribute("href", "/api/v1/config/export?include_secrets=false");
    expect(exportLink).toHaveAttribute("download", "encore.yml");
    await user.click(screen.getByRole("checkbox", { name: "Include secrets (encrypted)" }));
    expect(exportLink).toHaveAttribute("href", "/api/v1/config/export?include_secrets=true");
    expect(screen.getByRole("link", { name: "Download skill package" })).toHaveAttribute(
      "download",
      "encore-skill.zip",
    );
  });

  it("imports a file and lists what was applied and skipped", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/settings?tab=backup");
    const input = await screen.findByLabelText("Import configuration…");

    await user.upload(input, new File(["kodi:\nrooms:\nsecrets:\n"], "encore.yml", { type: "application/yaml" }));

    expect(await screen.findByText("Imported kodi, rooms.")).toBeInTheDocument();
    expect(screen.getByText("Skipped: secrets (encrypted with a different key).")).toBeInTheDocument();
    expect(log.find((r) => r.method === "POST")?.path).toBe("/config/import");
    // Cleared, so choosing the same file again imports it again.
    expect(input).toHaveValue("");
  });

  it("lists nothing as skipped when everything was applied", async () => {
    const user = userEvent.setup();
    renderApp("/settings?tab=backup");
    await user.upload(await screen.findByLabelText("Import configuration…"), new File(["kodi:\n"], "encore.yml"));
    expect(await screen.findByText("Imported kodi.")).toBeInTheDocument();
    expect(screen.queryByText(/^Skipped/)).not.toBeInTheDocument();
  });

  it("imports nothing when the file picker is cancelled", async () => {
    const log = recordRequests();
    renderApp("/settings?tab=backup");
    fireEvent.change(await screen.findByLabelText("Import configuration…"), { target: { files: [] } });
    expect(log.some((r) => r.method === "POST")).toBe(false);
    expect(screen.queryByText("Importing…")).not.toBeInTheDocument();
  });

  it("says when a file imported nothing", async () => {
    const user = userEvent.setup();
    renderApp("/settings?tab=backup");
    await user.upload(await screen.findByLabelText("Import configuration…"), new File(["secrets:\n"], "encore.yml"));
    expect(await screen.findByText("Imported nothing.")).toBeInTheDocument();
  });

  it("shows why a file couldn't be imported", async () => {
    const user = userEvent.setup();
    renderApp("/settings?tab=backup");
    await user.upload(await screen.findByLabelText("Import configuration…"), new File(["just some text"], "notes.yml"));
    expect(await screen.findByRole("alert")).toHaveTextContent("That doesn't look like YAML.");
    expect(screen.queryByText(/^Imported/)).not.toBeInTheDocument();
  });
});
