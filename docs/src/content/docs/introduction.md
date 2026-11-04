---
title: Why Encore
description: What Encore is, why it exists, and how it differs from Kanzi.
---

Encore is a self-hosted Alexa skill server for Kodi. You run it on your own network, it talks to your Kodi instances over JSON-RPC, and it deploys a private custom Alexa skill into your own Amazon developer account. When you say "Alexa, ask encore to play the next episode of Severance", Alexa sends the request to Encore, which finds the episode in your library and tells the right Kodi to play it.

:::caution[Early releases]
Encore is in its first releases (0.x). Voice control, the web interface and skill deploys work end to end, tested with a real Echo in English (Australia), but expect rough edges, and breaking changes are possible before 1.0. The other locales haven't been tried on real devices yet; if you use one, [reports](https://github.com/nightah/encore/issues) are welcome.
:::

## Kanzi is archived

[Kanzi](https://github.com/m0ngr31/kanzi) by m0ngr31 was the popular way to control Kodi with Alexa. It has been archived and no longer receives fixes. Setting it up meant creating a skill by hand in the Alexa developer console, pasting interaction models, and deploying a Python app to a cloud function or your own server. Encore keeps the voice commands people relied on and replaces the setup with a web interface.

## What Encore does differently

- **Setup in a web interface.** A first-run wizard walks through the admin account, the public address, Kodi, your Amazon developer account and the first deploy. Kodi instances, Echo devices, rooms, storage and sign-in are managed there afterwards.
- **One-click skill deploys.** Encore builds the skill package (manifest and an interaction model per locale) and creates or updates the skill in your developer account through the Skill Management API, using a Login with Amazon security profile you create yourself. No copying JSON into the developer console.
- **Your library in the skill model.** Titles, rooms and playlists from your library become slot values at deploy time, so Alexa recognises the names you actually have. Titles that don't fit in the model are still found by Encore's own matcher.
- **New titles are findable straight away.** Encore holds an in-memory index of each library, updated by Kodi's WebSocket notifications, so something added a minute ago can be played by voice without a redeploy.
- **Rooms and several Kodi instances.** Each Echo can be linked to a Kodi instance, and requests can name a room: "play Dune in the bedroom". Instances that share a MySQL/MariaDB library are detected and share one index.
- **Wake-on-LAN.** A sleeping Kodi is woken and the request retried.
- **A request log and match tester.** See what Alexa heard, how Encore matched it, which Kodi calls it made and how long each step took, and try queries against your library.
- **Every Alexa locale: seventeen locales, ten languages.** English (Australia, Canada, India, United Kingdom, United States), German, French (France, Canada), Spanish (Spain, Mexico, United States), Italian, Dutch, Brazilian Portuguese, Arabic (Saudi Arabia), Hindi and Japanese.
- **Runs anywhere.** A single binary and a multi-arch container image, with local, S3-compatible or WebDAV storage and an optional stateless mode for hosts without persistent volumes.

## How it fits together

Encore listens on one HTTP port (8080 by default) and serves three things:

- `POST /api/v1/alexa`: the skill endpoint Alexa calls. This is the only route that must be reachable from the internet, over HTTPS.
- `/api/v1/…`: the [admin API](/encore/reference/api/) used by the web interface. Apart from a few public routes, such as the health check and sign-in, it needs an admin sign-in.
- `/`: the web interface itself.

A reverse proxy in front of Encore terminates TLS and exposes the Alexa endpoint, or all of Encore behind its sign-in; see [exposing Encore to Alexa](/encore/guides/reverse-proxy/).

## Next steps

- [Run Encore with Docker](/encore/getting-started/docker/) or [as a binary](/encore/getting-started/binary/).
- Walk through the [first run](/encore/getting-started/first-run/).
