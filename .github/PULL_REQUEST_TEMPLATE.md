<!--
Title: a Conventional Commit, lower case, at most 72 characters, with one scope or none for a change across several areas, e.g. `feat(kodi): add queue intent` or `fix(alexa): handle a missing slot`. It becomes the squash-merged commit, and its type and scope label the pull request and place it in the release notes (`feat` is a minor release, anything else a patch, `!` a breaking change).
-->

## What and why

<!-- What changes, and why. Link the issue it closes with "Fixes #123". -->

## How it was tested

<!-- What you ran or tried by hand: Kodi version, Echo model and locale, browser. -->

- [ ] `make lint` and `make test` pass
- [ ] `make generate` leaves no diff (after changing a schema, a generator or the admin API; run `pnpm generate:api` in `web/` too)
- [ ] `pnpm test:e2e:mock` passes in `web/` (after changing the web interface)
- [ ] Tried with a real Kodi or Echo, or not applicable

## Screenshots

<!-- For changes to the web interface: before and after, light and dark if colours changed. Delete this section otherwise. -->

## Breaking changes

<!-- Config keys, API fields or behaviour that existing setups rely on, and how to move over. Delete this section if there are none. -->

## Checklist

- [ ] Tests added or updated; a fix comes with a test that fails without it
- [ ] Docs updated where behaviour, settings or commands changed
- [ ] New interface or skill strings added in English only (Crowdin handles the other languages)
- [ ] No secrets, tokens or personal data in code, tests, fixtures or logs
