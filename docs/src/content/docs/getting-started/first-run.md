---
title: First run
description: Create the admin account with the setup token and walk through the setup wizard.
---

## Find the setup token

Until an admin account exists, Encore logs a one-time setup token when it starts:

```text
level=WARN msg="no admin account exists yet; open the web interface and create one with this one-time setup token" setup_token=<token>
```

```sh
docker compose logs encore | grep setup_token
```

## Open the web interface

Browse to Encore on your network, for example `http://192.168.1.10:8080/`. This opens the six-step setup wizard. Every step after the first can be skipped and resumed later from the "Continue setup" banner.

1. **Admin account.** Paste the setup token and choose a username and a password of at least 10 characters. You can later switch to [OIDC or trusted-header sign-in](/encore/guides/sign-in/).
2. **Public address.** Enter the public HTTPS address Alexa will use, such as `https://encore.example.net`. Encore calls its own Alexa endpoint at that address and reports whether it answered, whether Alexa accepts the certificate, and whether the web interface is also reachable there (fine either way, since it needs a sign-in). See [exposing Encore to Alexa](/encore/guides/reverse-proxy/).
3. **Kodi.** Choose what the skill plays (movies, TV, music), then pick a Kodi instance found on your network or enter its address. Encore syncs only that content from Kodi. See [Kodi instances](/encore/guides/kodi/) for the Kodi settings Encore needs.
4. **Amazon account.** Create a Login with Amazon security profile and authorise Encore. See [deploying the skill](/encore/guides/deploy-skill/).
5. **Deploy skill.** Choose locales, answer Amazon's privacy questions and deploy.
6. **Say hello.** Say "Alexa, open encore" to an Echo and watch the request arrive.

## After the wizard

- Each Echo that talks to Encore shows up under **Echo devices**. Link it to a Kodi instance and a room, or mark one Kodi instance as the default for unlinked Echos. See [Echo devices and rooms](/encore/guides/echo-devices/).
- The **Request log** shows what Alexa heard and what Encore did; see [request log](/encore/guides/request-log/).
- Set up [storage and backups](/encore/guides/storage/).

## Install as an app

The web interface can be added to a phone's home screen or installed on a computer, and then opens in its own window without the browser's address bar.

- **iPhone and iPad:** open Encore in Safari, tap Share, then **Add to Home Screen**.
- **Android:** open Encore in Chrome, open the menu, then **Add to Home screen** and **Install**.
- **Desktop Chrome and Edge:** use the install icon at the end of the address bar.

Chrome and Edge install Encore as an app only over HTTPS, so reach it through your [reverse proxy](/encore/guides/reverse-proxy/) rather than `http://<host>:8080/`. Over plain HTTP they add a shortcut that opens a normal browser tab. Safari installs it either way.
