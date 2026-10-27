import { describe, expect, it } from "vitest";

import { OIDC_PRESETS, detectProvider } from "@features/settings/oidcProviders";

const v = {
  clientId: "encore",
  redirectUri: "https://encore.example.net/api/v1/auth/oidc/callback",
};

describe("OpenID Connect presets", () => {
  it("gives Keycloak JSON it can import", () => {
    const client = JSON.parse(OIDC_PRESETS.keycloak.snippet(v));
    expect(client.clientId).toBe("encore");
    expect(client.redirectUris).toEqual([v.redirectUri]);
    expect(client.protocolMappers[0].config["claim.name"]).toBe("groups");
  });

  it("puts the redirect URI and client ID in every snippet", () => {
    for (const preset of Object.values(OIDC_PRESETS)) {
      expect(preset.snippet(v)).toContain(v.redirectUri);
      expect(preset.snippet({ ...v, clientId: "kodi-admin" })).toContain("kodi-admin");
    }
  });

  it("guesses the provider from the issuer", () => {
    expect(detectProvider("https://authentik.example.com/application/o/encore/")).toBe("authentik");
    expect(detectProvider("https://keycloak.example.com/realms/home")).toBe("keycloak");
    expect(detectProvider("https://auth.example.com")).toBe("other");
  });
});
