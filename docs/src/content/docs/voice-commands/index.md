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

In French the invocation name is always "kodi" ("Alexa, demande à kodi de …"), because "encore" on its own means "again". The invocation name can be changed on the Alexa skill page.

## Rooms

Commands that start playback accept a room at the end: "play Dune in the bedroom" plays on the bedroom's Kodi instance, whichever Echo you're talking to. See [Echo devices and rooms](/encore/guides/echo-devices/).

## Finding titles

Encore matches what Alexa heard against your own library, so it copes with numbering ("rocky two"), subtitles ("endgame") and titles Alexa doesn't know. When two titles are close, it asks which one you meant. See [matching and aliases](/encore/guides/matching/).

## Every phrase, by language

The pages below are generated from the interaction model Encore deploys, so they always match what your skill understands. Each lists the locales and invocation names, every command with its sample patterns, and the words custom slots accept.

- [English](/encore/voice-commands/en/): `en-AU`, `en-CA`, `en-GB`, `en-IN`, `en-US`
- [German](/encore/voice-commands/de/): `de-DE`
- [French](/encore/voice-commands/fr/): `fr-FR`
- [Spanish](/encore/voice-commands/es/): `es-ES`
- [Italian](/encore/voice-commands/it/): `it-IT`

Your Echo has to use one of these languages, and that locale has to be deployed; see [locales](/encore/guides/deploy-skill/#locales).
