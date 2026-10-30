---
title: Deploying the skill
description: Create your own Login with Amazon security profile, authorise Encore, choose locales and deploy the private skill.
---

Encore deploys a private custom skill into **your own** Amazon developer account through the Skill Management API (SMAPI). The skill stays in the development stage: it is never published, and it works on the Echo devices registered to the same Amazon account.

You need:

- An [Amazon developer account](https://developer.amazon.com/) on **the same Amazon account your Echo devices are registered to**. Signing up is free.
- Encore reachable from the internet at an HTTPS address; see [exposing Encore to Alexa](/encore/guides/reverse-proxy/).

## Connect your Amazon account

Encore talks to SMAPI with a Login with Amazon (LWA) security profile that you create. Encore ships no client ID of its own, and you shouldn't reuse another tool's (such as the ASK CLI's built-in client): the authorisation would be tied to software you don't control, and Encore couldn't refresh its tokens.

### 1. Create a security profile

1. Open the [Login with Amazon console](https://developer.amazon.com/loginwithamazon/console/site/lwa/overview.html), signed in with your developer account.
2. Choose **Create a New Security Profile**.
3. Fill in the form:
   - **Security Profile Name:** `encore`
   - **Security Profile Description:** anything, for example "My Encore server".
   - **Consent Privacy Notice URL:** required by Amazon. Only you see the consent screen, so any page you control will do, such as your Encore's public address.
4. Save, then open the profile's **Web Settings** and choose **Edit**.
5. Under **Allowed Return URLs**, add the one URL Encore shows on the **Amazon account** step. It depends on how you reach the web interface:
   - Over HTTPS: `https://<your encore address>/api/v1/oauth/lwa/callback`. Amazon returns there and Encore finishes connecting by itself.
   - Over plain HTTP: `http://127.0.0.1:9090/cb`, for the paste fallback below (Amazon only allows plain HTTP for loopback addresses). Nothing listens there; your browser only needs to land on it.
6. Save. Leave **Allowed Origins** empty.
7. Copy the profile's **Client ID** (`amzn1.application-oa2-client.…`) and **Client Secret**.

### 2. Authorise Encore

1. Paste the client ID and secret into Encore and choose **Authorise with Amazon**. The secret is encrypted at rest and used only to refresh the access token.
2. Amazon's consent page asks you to allow the profile to manage your skills and interaction models and to test skills (the scopes `alexa::ask:skills:readwrite`, `alexa::ask:models:readwrite` and `alexa::ask:skills:test`). Allow it.
3. Amazon returns you to Encore's Alexa skill page, now connected to your developer account.

The authorisation lasts until you revoke it. If Amazon stops accepting it (for example after you remove the security profile), Encore asks you to authorise again.

### No HTTPS callback? Paste instead

If you open the web interface over plain HTTP, the consent page returns to `http://127.0.0.1:9090/cb?code=…&state=…` and your browser shows a connection error. That's expected: copy the **whole address** from the address bar and paste it under **No HTTPS callback?** → **Paste the redirect URL**. The code is single-use and expires within minutes, and the pending authorisation within 15 minutes.

### Or paste a refresh token

If you use the [ASK CLI](https://developer.amazon.com/en-US/docs/alexa/smapi/quick-start-alexa-skills-kit-command-line-interface.html), it can produce a refresh token for **your** security profile:

```sh
ask util generate-lwa-tokens \
  --client-id <your profile's client ID> \
  --client-confirmation <your profile's client secret> \
  --scopes "alexa::ask:skills:readwrite alexa::ask:models:readwrite alexa::ask:skills:test"
```

Paste the `refresh_token` value under **Paste a refresh token**. It must have been issued to the same client ID you saved in Encore. Run it without `--client-id` and the token belongs to the ASK CLI's own client, which Encore can't use.

## Choose what to deploy

On the **Deploy skill** step or the Alexa skill page:

- **Locales.** Pick the locales to build. See [locales](#locales) below.
- **What it can play.** Movies, TV shows and music, all on by default. Turning one off leaves its voice commands out of the skill (so Alexa won't route "play the album …" to Encore without music), and "play X" stops searching those titles. Music includes music videos. Keep at least one; set it as code with [`alexa.content`](/encore/reference/configuration/#alexacontent), e.g. `content: [movies, tv]`.
- **Invocation name.** Choose **encore** or **kodi** ("Alexa, ask kodi to…"). French and Brazilian Portuguese say "kodi" for both, and Arabic and Japanese spell each in their own script; see [locales](#locales) for every locale's name.
- **Skill name.** Empty uses each locale's default.
- **Endpoint.** The public `https://` URL of `/api/v1/alexa`, served with a certificate from a public authority (see [what Alexa requires](/encore/guides/reverse-proxy/#what-alexa-requires)).
- **Privacy and compliance.** Amazon asks every skill whether it collects personal information, is directed at children under 13, is export compliant, contains advertising and allows purchases. These are your own declarations to Amazon, so read and answer each question yourself. Encore refuses to deploy until all of them are answered.

## Deploy

Choose **Deploy**. Progress is shown step by step:

| Step | What happens |
| --- | --- |
| package | Encore builds the skill package: the manifest and one interaction model per locale, with your rooms, playlists, genres and library titles as slot values. |
| vendor | Finds your developer account (vendor ID). |
| skill | Finds the existing Encore skill, or prepares to create one. |
| upload | Uploads the package to Amazon. |
| import | Amazon imports it, creating or updating the skill. |
| build | Amazon builds each locale's interaction model. Per-locale results and errors appear here. |
| enable | Enables the skill for testing on the devices registered to your Amazon account. |
| simulate | A smoke test through Amazon's simulation API: Encore opens the skill in each locale the API supports. A failed smoke test is reported as a warning; the skill is already live by then. |

When it finishes, say "Alexa, open encore" to an Echo. The skill also appears in the Alexa app under **Skills & Games** → **Your Skills** → **Dev**.

## Locales

An Echo can use the skill only when **its language matches a deployed locale**. An Echo set to English (United Kingdom) needs `en-GB`; one set to German needs `de-DE`. Check each device's language in the Alexa app (**Devices** → the Echo → **Language**). Encore ships these locales:

| Language | Locales | Invocation name with encore chosen | With kodi chosen |
| --- | --- | --- | --- |
| English | `en-AU`, `en-CA`, `en-GB`, `en-IN`, `en-US` | encore | kodi |
| German | `de-DE` | encore | kodi |
| French | `fr-CA`, `fr-FR` | kodi | kodi |
| Spanish | `es-ES`, `es-MX`, `es-US` | encore | kodi |
| Italian | `it-IT` | encore | kodi |
| Dutch | `nl-NL` | encore | kodi |
| Portuguese | `pt-BR` | kodi | kodi |
| Arabic | `ar-SA` | انكور | كودي |
| Hindi | `hi-IN` | encore | kodi |
| Japanese | `ja-JP` | アンコール | コディ |

French and Brazilian Portuguese say kodi whichever you choose: "encore" means "again" in French, and Portuguese reads it as "en-có-ri", which the recogniser hears as "âncora". Arabic and Japanese invocation names must be written in their own script, so each choice is spelt the way people there say it; the deploy step shows what each enabled locale will say.

Deploy only what you need: every locale is a separate interaction model for Amazon to build, so fewer locales deploy faster. Set them on the Alexa skill page or as code with [`alexa.locales`](/encore/reference/configuration/#alexalocales):

```yaml title="encore.yml"
alexa:
  locales: [en-AU, en-GB]
```

`ENCORE_ALEXA_LOCALES=en-AU,en-GB` does the same. When set in config, the locale choice is read-only in the web interface. Empty means every locale Encore ships. The [voice commands](/encore/voice-commands/) pages list the phrases for each language.

## Redeploying

- **Deploy again after changing locales, the invocation or skill name, or the endpoint.** The overview shows "Skill update pending" when what Encore would deploy differs from what was last deployed.
- **Rooms and title [aliases](/encore/guides/matching/#aliases) work straight away** through Encore's own matching. A redeploy adds them to the skill model as slot values, which helps Alexa recognise them.
- **New library titles don't need a redeploy.** Encore finds them as soon as Kodi reports them; a redeploy adds them to the skill model too, which helps with unusual names.
- **Unchanged packages are skipped.** Packages are built deterministically; if nothing changed since the last successful deploy, Encore stops after the package step. Use **Force** to redeploy anyway.
- **The same skill is updated.** Encore remembers the skill ID. If it's lost, for example after reinstalling, it finds the skill by name instead of creating a duplicate. If you deleted the skill in the developer console, the next deploy creates a new one.
- **Some locales failing doesn't hold the others back.** When Amazon won't build some locales' models for a reason it names, Encore deploys once more straight away. A locale whose build errors quote some of its titles or aliases is built without those titles; otherwise, if other locales did build, it keeps every other library title, and all its rooms. The deploy then succeeds with those locales marked **Fewer titles** on the Alexa skill page, which lists the titles left out or how many were kept, with Amazon's message. Titles left out are still found by Encore's matcher. The package still counts as deployed, so Encore doesn't keep redeploying it; the next change, or **Redeploy**, tries every title again. If the second attempt fails too, the deploy fails as before.
- **Expired access tokens are renewed.** If Amazon rejects Encore's access token before it expires, Encore gets a new one from Login with Amazon and repeats the request once.
- **Model size.** Amazon limits each interaction model to 1.5 MB. Encore includes the most recently added or played titles up to a per-kind limit that keeps the largest locale well under it; titles left out are still found by Encore's matcher. The Alexa skill page shows each locale's size against the limit.

### Automatic redeploys

With **Deploy skill changes automatically** on under **Settings › Features**, as it is by default, Encore redeploys the skill by itself whenever what it would deploy changes: new library titles, rooms or aliases, changed skill settings, or an Encore upgrade that changes the interaction model. To deploy only by hand, turn it off there or set [`alexa.auto_deploy`](/encore/reference/configuration/#alexaauto_deploy) to `false` (`ENCORE_ALEXA_AUTO_DEPLOY=false`).

- **Encore checks every 5 minutes** and deploys a change once it has stayed the same for a whole check, so a library sync or a run of settings changes is deployed once, after it settles. A change goes live within two checks, about 10 minutes. Change how often it checks with **Check for changes every** beneath the switch, from 1 minute to 1440 (a day) and fractions such as 2.5 allowed, or with [`alexa.auto_deploy_minutes`](/encore/reference/configuration/#alexaauto_deploy_minutes) (`ENCORE_ALEXA_AUTO_DEPLOY_MINUTES`).
- **Only a skill you've already deployed is redeployed.** The first deploy is always yours, as is reconnecting to Amazon when it asks.
- **A failure that may pass is tried again.** When an automatic deploy fails because Amazon is unavailable or throttling (even after Encore's own retries of each request), another build of the skill is still running, the network failed, an import or build took too long, or a locale failed to build without Amazon saying why, Encore tries again at the first check 5 minutes later, then after 10 and 20 more. The Alexa skill page shows the failed attempt, how many have failed and when the next one runs. A retry doesn't wait for the package to settle again.
- **Any other failure pauses automatic deploys**, as does a fourth failure in a row: Amazon no longer accepting Encore's authorisation, or a package or model Amazon rejected for a reason it named. The overview shows "Automatic deploys are paused" with Amazon's reason, and the Alexa skill page shows it too. Fix the cause, then deploy by hand: once a deploy succeeds, automatic deploys resume.
- **Without a configured endpoint**, an automatic deploy reuses the one the skill was last deployed with, since there's no browser request to work it out from. If Encore has no record of it, for example for a skill deployed by an older version, it reads it from the skill at Amazon.

## Without SMAPI: export the package

The Alexa skill page can download the skill package as a zip (`skill-package/` plus `ask-resources.json`), for importing in the [Alexa developer console](https://developer.amazon.com/alexa/console/ask) or deploying with `ask deploy`. When you deploy by hand, set [`alexa.skill_id`](/encore/reference/configuration/#alexaskill_id) to the new skill's ID so Encore rejects requests for any other skill; until a skill ID is known, Encore accepts requests for any skill and logs a warning.

## Troubleshooting

**"client ID or secret rejected" when authorising.** Check you copied the client ID and secret of the same security profile, with no spaces.

**Amazon's page says the redirect URI is not allowed.** The return URL Encore used isn't in the profile's **Allowed Return URLs**. Add exactly the URLs Encore shows, including the scheme and path.

**"authorise Encore with Amazon again".** Amazon rejected the stored refresh token: the profile was deleted or its secret changed, or you revoked access. Authorise again. Saving a different client ID also forgets the stored authorisation.

**A locale fails to build.** Open the deploy's **build** step for Amazon's message per locale. Problems with a title or alias usually name the slot value, and Encore leaves a named title out by itself; fix or remove an alias Amazon names and redeploy to bring the title back.

**A locale shows "Fewer titles".** Amazon wouldn't build that locale's model with every title, so Encore deployed it with fewer; the warnings on the Alexa skill page say which titles were left out or how many were kept, with Amazon's message. Voice commands still work, and Encore's matcher still finds every title. Choose **Redeploy** to try every title again, for example after removing an alias Amazon named.

**"Deploy" is refused.** Encore answers why: privacy questions unanswered, the endpoint isn't `https://`, Amazon isn't connected, or another deploy is running.

**Alexa says the skill didn't respond.** Alexa can't reach Encore, doesn't accept the certificate, or Encore refused the request. Run the public address check on the Alexa skill page, and look at the [request log](/encore/guides/request-log/). If nothing arrives, look at the overview's **Needs attention** first: requests refused for a stale timestamp (the host's clock is off by more than 150 seconds), a bad signature or another skill's ID are counted there, since they never reach the request log. If the overview counts nothing either, the problem is between Alexa and your proxy.

**Every voice request fails at once, but the web interface works.** Something in front of Encore is refusing Amazon's servers, usually with a `403`: Cloudflare's Bot Fight Mode, a WAF rule or a country block. Your browser gets through, Amazon's servers don't. See [bot protection, WAF and country blocks](/encore/guides/reverse-proxy/#bot-protection-waf-and-country-blocks) for how to let Alexa through.

**Alexa says a Kodi instance isn't answering.** Encore gives each voice request seven seconds, which leaves time for the answer before Alexa gives up at about eight, and says this when Kodi hasn't answered by then. Check that the Kodi instance is running and responsive; the request log shows the request as an error after seven seconds, with the Kodi calls that had finished by then.

**The Echo answers with something unrelated or "I don't know that".** The Echo's language may not match a deployed locale, or it heard a different invocation name. Check the device language and the locales you deployed.

**"This Echo isn't linked to a Kodi yet."** Link the device on the **Echo devices** page, or make one Kodi instance the default for unlinked Echos; see [Echo devices and rooms](/encore/guides/echo-devices/).
