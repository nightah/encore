import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it } from "vitest";

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
