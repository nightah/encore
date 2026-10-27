---
title: Admin sign-in
description: Local accounts with TOTP, OpenID Connect, trusted reverse-proxy headers, and break-glass recovery.
---

Everything under `/api/v1/` except the Alexa endpoint, health check and a few sign-in routes requires a signed-in admin, whether or not it is reachable from outside. Encore supports three ways to sign in, chosen under **Settings** → **Access** or with [`auth.mode`](/encore/reference/configuration/#authmode).

## Local (default)

Encore keeps one admin account, created with the setup token on [first run](/encore/getting-started/first-run/).

- Passwords must be at least 10 characters and are stored as argon2id hashes (19 MiB, two passes).
- **Two-factor sign-in (TOTP):** under **Settings** → **Access**, enrol an authenticator app by scanning the QR code or entering the secret, then confirm with a code. From then on the sign-in form asks for a code. Disabling TOTP needs a current code.
- **Lockout:** 5 failed sign-ins within 15 minutes lock the account and the client address for 15 minutes ([`auth.lockout_attempts`](/encore/reference/configuration/#authlockout_attempts), [`auth.lockout_minutes`](/encore/reference/configuration/#authlockout_minutes)).
- **Sessions** last 168 hours (a week) of inactivity, extended as you use the web interface ([`auth.session_hours`](/encore/reference/configuration/#authsession_hours)). The session cookie is `HttpOnly`, `SameSite=Lax`, and `Secure` when Encore is reached over HTTPS. Changing your password signs out every other session.

## OpenID Connect

Sign in through your identity provider, such as Authelia, Authentik, Keycloak or Pocket ID. Encore uses the authorisation code flow with PKCE, state and nonce. It sends the client secret in the token request body (`client_secret_post`), falling back to an HTTP Basic header (`client_secret_basic`) if the provider rejects that, so a client configured for either works.

At the provider, register a confidential client for Encore with the redirect URI `https://<your admin address>/api/v1/auth/oidc/callback`, the `openid`, `profile`, `email` and `groups` scopes, and a client secret. Then:

```yaml title="encore.yml"
auth:
  mode: oidc
  oidc:
    issuer: https://auth.example.net
    client_id: encore
    admin_group: encore-admins # optional
```

```sh
ENCORE_AUTH_OIDC_CLIENT_SECRET_FILE=/run/secrets/encore_oidc_secret
```

- `issuer` must serve `/.well-known/openid-configuration` reachable from Encore.
- With `admin_group` set, only users whose ID token or userinfo response lists that group in the `groups` claim ([`auth.oidc.groups_claim`](/encore/reference/configuration/#authoidcgroups_claim)) can sign in. Without it, anyone your provider lets through is an admin.
- The callback URL is derived from the request. Behind a proxy, list it in [`server.trusted_proxies`](/encore/guides/reverse-proxy/#servertrusted_proxies) so Encore sees the original scheme and host, or set [`auth.oidc.redirect_uri`](/encore/reference/configuration/#authoidcredirect_uri).

You can also fill in these fields under **Settings** → **Access**. Picking **Authelia**, **Authentik** or **Keycloak** as the provider fills in the usual scopes and groups claim and shows the provider's side of the setup with your redirect URI in it: an Authelia client to add to its configuration, the values for Authentik's application wizard, or a Keycloak client to import (including the group membership mapper that adds the `groups` claim).

## Trusted headers

If a reverse proxy already authenticates you (Authelia, Authentik or similar with forward auth), Encore can trust the user it reports in `Remote-User`, and the groups in `Remote-Groups`.

```yaml title="encore.yml"
server:
  trusted_proxies: [172.18.0.0/16]
auth:
  mode: header
  header:
    admin_group: encore-admins # optional
```

- The headers are honoured **only** on connections from [`server.trusted_proxies`](/encore/reference/configuration/#servertrusted_proxies); Encore refuses `auth.mode: header` without it, and strips the headers from every other request.
- Header names can be changed with [`auth.header.user_header`](/encore/reference/configuration/#authheaderuser_header) and [`auth.header.groups_header`](/encore/reference/configuration/#authheadergroups_header).
- With `admin_group`, the user must have that group in the comma-separated groups header.
- Switching to header mode from the web interface is refused unless your own connection comes through a trusted proxy, so you can't lock yourself out that way.

Put forward auth in front of the web interface but never in front of `/api/v1/alexa`: Alexa can't sign in, and Encore verifies Alexa's requests itself. Use a separate admin hostname, or a rule that skips that path when everything shares one hostname.

## Break glass

To keep the local admin password working under OIDC or header mode, as a way back in if the identity provider or proxy breaks, tick **Keep local password as a break-glass login** under **Settings** → **Access**.

If it's off and you're already locked out, start Encore with:

```sh
ENCORE_BREAK_GLASS=true
```

The password form comes back so you can sign in with the local admin account and fix the settings; remove the variable afterwards. In a config file the same setting is [`auth.break_glass`](/encore/reference/configuration/#authbreak_glass); set either way, it can't be changed in the web interface.

## Changing the mode

Changing `auth.mode` signs out every other session. Settings made in config are read-only in the web interface until you remove them from config.
