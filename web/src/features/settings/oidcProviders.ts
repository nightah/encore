import i18n from "@i18n/index";

/** OpenID Connect providers with known settings: picking one fills in Encore's side and shows the provider's side to copy. Each one's issuer hint and snippet title are settings:access.oidc.providers.<provider>.issuerHint and .snippetTitle. */
export type OidcProvider = "authelia" | "authentik" | "keycloak" | "other";

export interface OidcPreset {
  label: string;
  scopes: string;
  groupsClaim: string;
  snippet: (v: { redirectUri: string; clientId: string }) => string;
}

export const OIDC_PRESETS: Record<Exclude<OidcProvider, "other">, OidcPreset> = {
  authelia: {
    groupsClaim: "groups",
    label: "Authelia",
    scopes: "openid profile email groups",
    snippet: ({ redirectUri, clientId }) => `identity_providers:
  oidc:
    clients:
      - client_id: '${clientId}'
        client_name: 'Encore'
        # ${i18n.t("settings:access.oidc.providers.authelia.generate")}
        #   authelia crypto hash generate pbkdf2 --variant sha512 --random --random.length 72 --random.charset rfc3986
        # ${i18n.t("settings:access.oidc.providers.authelia.digest")}
        client_secret: '$pbkdf2-sha512$310000$...'
        public: false
        authorization_policy: 'two_factor'
        require_pkce: true
        pkce_challenge_method: 'S256'
        redirect_uris:
          - '${redirectUri}'
        scopes:
          - 'openid'
          - 'profile'
          - 'email'
          - 'groups'
        response_types:
          - 'code'
        grant_types:
          - 'authorization_code'
        token_endpoint_auth_method: 'client_secret_post'`,
  },
  authentik: {
    groupsClaim: "groups",
    label: "Authentik",
    scopes: "openid profile email",
    snippet: ({ redirectUri, clientId }) => `Application name:  Encore
Application slug:  encore
Provider type:     OAuth2/OpenID Provider
Client type:       Confidential
Client ID:         ${clientId}
Redirect URIs:     Strict  ${redirectUri}
Scopes:            openid, email, profile ${i18n.t("settings:access.oidc.providers.authentik.scopesNote")}

${i18n.t("settings:access.oidc.providers.authentik.copySecret", { issuer: "https://<authentik host>/application/o/encore/" })}`,
  },
  keycloak: {
    groupsClaim: "groups",
    label: "Keycloak",
    scopes: "openid profile email",
    snippet: ({ redirectUri, clientId }) =>
      JSON.stringify(
        {
          attributes: { "pkce.code.challenge.method": "S256" },
          clientId,
          directAccessGrantsEnabled: false,
          name: "encore",
          protocol: "openid-connect",
          protocolMappers: [
            {
              config: {
                "access.token.claim": "true",
                "claim.name": "groups",
                "full.path": "false",
                "id.token.claim": "true",
                "userinfo.token.claim": "true",
              },
              name: "groups",
              protocol: "openid-connect",
              protocolMapper: "oidc-group-membership-mapper",
            },
          ],
          publicClient: false,
          redirectUris: [redirectUri],
          standardFlowEnabled: true,
          webOrigins: ["+"],
        },
        null,
        2,
      ),
  },
};

/** Guesses the provider from a saved issuer URL; Authelia's has no telltale path. */
export function detectProvider(issuer: string): OidcProvider {
  if (issuer.includes("/application/o/")) return "authentik";
  if (issuer.includes("/realms/")) return "keycloak";
  return "other";
}
