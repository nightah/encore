---
title: Request log
description: See every voice request, what Encore matched and which Kodi calls it made.
---

The **Request log** page lists every request Alexa sends to Encore, newest first, and updates live as new ones arrive. It is the first place to look when a command doesn't do what you expected.

Each entry shows the time, the Echo, the locale, the intent, what Alexa heard, the Kodi instance it went to, how long it took, and the outcome:

| Outcome | Meaning |
| --- | --- |
| played | Something started playing. |
| ok | A command that doesn't play anything (volume, navigation, …) succeeded. |
| asked | Encore asked which title you meant, or asked a follow-up. |
| not_found | Nothing in the library matched well enough. |
| unlinked | The Echo isn't linked to a Kodi instance and there is no default. |
| error | Something went wrong, such as Kodi being unreachable. |

Filter by Echo, by outcome or by text.

## Request details

Opening an entry shows:

- the **slots** Alexa filled (title, room, numbers, …),
- the **match**: the normalised query, the decision and its margin, and the candidates with their score breakdown (see [matching](/encore/guides/matching/)),
- the **Kodi calls** Encore made, in order, with each one's duration and result,
- the **timing**: how long verifying Alexa's signature, matching and talking to Kodi took,
- the **speech** Alexa was given to say.

**Replay** runs the request again against the current library index without touching Kodi, and shows what Encore would say and which Kodi calls it would make. Use it after adding an [alias](/encore/guides/matching/#aliases) to check the fix.

A new Echo's Alexa device ID (`amzn1.ask.device.…`) is shown here too, for when you [declare devices in config](/encore/guides/echo-devices/#declare-rooms-and-devices-in-config).

## Retention

Encore keeps the most recent 500 requests and nothing older than seven days, whichever limit is reached first. The log lives in Encore's database, so it is included in [backups](/encore/guides/storage/#backups).

## Nothing arrives?

If no entry appears when you speak to the skill, Alexa isn't reaching Encore: check the public address on the Alexa skill page and your [reverse proxy](/encore/guides/reverse-proxy/#check-it). Requests that fail Encore's signature or skill ID checks are refused before they are logged; look at Encore's own log (`ENCORE_LOG_LEVEL=debug` for more detail).
