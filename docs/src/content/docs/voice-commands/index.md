---
title: Voice commands
description: How to talk to Encore, and where to find every phrase it understands.
sidebar:
  label: Overview
  order: -1
---

Encore's skill understands the commands Kanzi did, and a few more: playing films, episodes, music, music videos and playlists by name, random picks and recommendations, playback and volume, subtitles and audio tracks, navigating Kodi like a remote, add-ons, and power and library maintenance.

## Talking to the skill

Open the skill and then speak, or say it all at once:

- "Alexa, open encore" … "play the movie Dune"
- "Alexa, ask encore to play the next episode of Severance"
- "Alexa, tell encore to turn the volume up"

After a remote-style command such as "down" or "select", the skill stays open so you can say the next one without the wake word.

In French the invocation name is always "kodi" ("Alexa, demande à kodi de …"), because "encore" on its own means "again". Brazilian Portuguese uses "kodi" too, since Portuguese reads "encore" as "en-có-ri", which the recogniser hears as "âncora". Arabic and Japanese write the name in their own script: "انكور" ("كودي" when named after Kodi), as in "أليكسا، افتحي انكور", and "アンコール" ("コディ"), as in 「アンコールを開いて」. The invocation name can be changed on the Alexa skill page.

## Rooms

Commands that start playback accept a room at the end: "play Dune in the bedroom" plays on the bedroom's Kodi instance, whichever Echo you're talking to. In Japanese and Hindi the room comes first or before the verb: 「リビングで映画デューンを再生して」, "बेडरूम में शोले चलाओ". See [Echo devices and rooms](/encore/guides/echo-devices/).

## Episodes

Ask for an episode of a show by where it falls, or by its number:

- "play the next episode of Bluey" plays the first one you haven't watched ("play Bluey from where I left off" too)
- "play the first episode of Bluey" or "play Bluey from the start"
- "play the latest episode of Bluey" ("newest", "most recent" and "last" work too)
- "play a random episode of Bluey"
- "play Bluey season 2 episode 4"

The first and latest episodes skip specials (season 0) unless a show has nothing else. Alexa sometimes hears these as a plain "play …" search rather than an episode command, usually when it doesn't recognise the show's name; Encore spots the episode command's wording in the search and runs it anyway, so either way the right episode plays.

## Finding titles

Encore matches what Alexa heard against your own library, so it copes with numbering ("rocky two"), subtitles ("endgame") and titles Alexa doesn't know. When two titles are close, it asks which one you meant. See [matching and aliases](/encore/guides/matching/).

## Answering a question

When two titles are close, Alexa asks which one you meant, naming each with its year ("Did you mean Dune from 1984 or Dune from 2021?"), and waits for the answer:

- "the first one", "the second one" or "the last one" picks by position
- "the newer one" or "the older one" picks by release year
- the title on its own, such as "Dune Part Two", picks it; a title that isn't one of the choices is searched for instead
- "no" offers the next closest matches, two at a time, until there are none left
- "yes" plays the title when Alexa offered only one, and asks again when it offered two

If Alexa doesn't understand the answer, it asks the same question again instead of giving up, and "help" says how to answer. Each language's page lists its ways of answering under "Answering a question".

## Every phrase, by language

The pages below are generated from the interaction model Encore deploys, so they always match what your skill understands. Each lists the locales and invocation names, every command with its sample patterns, and the words custom slots accept.

- [English](/encore/voice-commands/en/): `en-AU`, `en-CA`, `en-GB`, `en-IN`, `en-US`
- [German](/encore/voice-commands/de/): `de-DE`
- [French](/encore/voice-commands/fr/): `fr-CA`, `fr-FR`
- [Spanish](/encore/voice-commands/es/): `es-ES`, `es-MX`, `es-US`
- [Italian](/encore/voice-commands/it/): `it-IT`
- [Dutch](/encore/voice-commands/nl/): `nl-NL`
- [Portuguese](/encore/voice-commands/pt/): `pt-BR`
- [Arabic](/encore/voice-commands/ar/): `ar-SA`
- [Hindi](/encore/voice-commands/hi/): `hi-IN`
- [Japanese](/encore/voice-commands/ja/): `ja-JP`

Your Echo has to use one of these languages, and that locale has to be deployed; see [locales](/encore/guides/deploy-skill/#locales).
