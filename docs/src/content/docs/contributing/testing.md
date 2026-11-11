---
title: Testing a language
description: How to try Encore's skill on an Echo in your language, what to report, and the phrases Amazon's language model still misreads.
---

Encore's skill speaks 17 Alexa locales in 10 languages, but few of them have been tried by someone who speaks the language at home. Every phrase is checked by typing it into Amazon's language model (the [NLU checks](/encore/contributing/development/) in `testdata/nlu/`), which shows what Amazon makes of the words but not whether anyone would say them that way, or what happens when they're spoken to an Echo. A native speaker can tell us both.

## What you need

- An Echo, or the Alexa app, set to your language. Each language has its own Alexa locales (Spanish has `es-ES`, `es-MX` and `es-US`), and the skill can sound different in each one.
- Encore running with its skill deployed to your Amazon account and linked to Kodi: see [Deploying the skill](/encore/guides/deploy-skill/) and [Kodi instances](/encore/guides/kodi/).
- A few films, shows and songs in Kodi's library, so requests for them have something to find.

## How to test

1. Open your language's page under **Voice commands**. It lists every phrase the skill knows in that language, generated from the interaction model, and the skill's invocation name in each locale.
2. Open the skill in your language and ask for things the way you'd ask a friend, before you read the listed phrases. What you say without looking is the best test of whether the skill understands you.
3. Then try the listed phrases, and the ones in [Phrases Amazon still misreads](#phrases-amazon-still-misreads) for your locale.
4. For each problem, note what you said, what Alexa or Kodi did, and how you'd naturally say it. The [Request log](/encore/guides/request-log/) shows what Alexa sent Encore for each request: the intent it chose and the words it put in each slot.
5. Listen to the replies too: one that is wrong, awkward, too formal or too casual, or that agrees wrongly with a title, is worth reporting.

## What to report

- Phrases nobody would say, with what you'd say instead.
- Requests that do the wrong thing or nothing, with what the Request log shows for them.
- Replies that sound wrong or unnatural.
- Which locale your Echo is set to, such as `es-MX` or `fr-CA`.

Send them on Discord (`@nightah`), on Matrix (`@nightah:nerv.com.au`) or as a [GitHub issue](https://github.com/nightah/encore/issues). To fix a translation yourself, see [Contributing](/encore/contributing/#translations).

## Phrases Amazon still misreads

Each phrase below is one of the skill's own samples, and Amazon's language model still reads it wrongly when it is typed in. Spoken, a phrase can work better or worse, and some may be phrases nobody says. Telling us which is the most useful test of all. Each one is a pending row of the NLU checks, and the list changes as they pass.

### Arabic (`ar-SA`)

| Say | Meaning | Amazon reads it as | It should |
| --- | --- | --- | --- |
| لورا خمس دقائق | Back five minutes | a skip of five minutes, with no direction | skip back five minutes |
| رجعي لورا خمس دقائق | Go back five minutes | a skip, without the time | rewind five minutes |
| ابغى اقتراح فيلم | I want a film suggestion | nothing, after some changes to the model | recommend a film |
| حولي على ام بي سي | Switch to MBC | nothing, when the skill knows only a few channels | switch to the channel MBC |

### German (`de-DE`)

| Say | Meaning | Amazon reads it as | It should |
| --- | --- | --- | --- |
| geh zur startseite | Go to the home screen | nothing | go to Kodi's home screen |
| öffne das tv programm | Open the TV guide | shuffling a show called "das TV programm" | open the TV guide |

### English (`en-US`)

| Say | Meaning | Amazon reads it as | It should |
| --- | --- | --- | --- |
| go home | Go home | nothing, after some changes to the model | go to Kodi's home screen |

### Spanish (`es-MX`)

| Say | Meaning | Amazon reads it as | It should |
| --- | --- | --- | --- |
| dale play a la casa de papel | Play Money Heist | nothing | play La Casa de Papel |
| dame una recomendación de película | Give me a film recommendation | nothing, after some changes to the model | recommend a film |

### Spanish (`es-US`)

| Say | Meaning | Amazon reads it as | It should |
| --- | --- | --- | --- |
| ve al inicio | Go to the home screen | nothing | go to Kodi's home screen |
| dame una recomendación de película | Give me a film recommendation | a random film, after some changes to the model | recommend a film |
| me refiero a dune de 2021 | I mean Dune from 2021 | nothing, though es-ES and es-MX understand it | answer the question with Dune from 2021 |
| dale play a la casa de papel | Play Money Heist | the next episode of a show called "play a la casa de papel" | play La Casa de Papel |
| quiero ver el capítulo 3 de la temporada 2 de la casa de papel | I want to watch season 2 episode 3 of Money Heist | the next episode, with the season and episode in the show's name | play that episode |

### French (`fr-CA`)

| Say | Meaning | Amazon reads it as | It should |
| --- | --- | --- | --- |
| qu'est-ce qu'on écoute ce soir | What shall we listen to tonight? | what's new in a show called "ce soir" | recommend something |

### Hindi (`hi-IN`)

| Say | Meaning | Amazon reads it as | It should |
| --- | --- | --- | --- |
| पांच मिनट पीछे करो | Go back five minutes | a rewind, without the time | rewind five minutes |
| मुझे फ़िल्म शोले देखनी है | I want to watch the film Sholay | nothing it understands | play the film शोले |
| पांच मिनट पीछे | Five minutes back | nothing, though it understands it with करो | skip back five minutes |
| म्यूट | Mute | nothing | mute |
| फ़ेवरेट आज तक खोलो | Open the favourite Aaj Tak | nothing | open the favourite आज तक |
| मैं फ़िल्म शोले देखना चाहता हूँ | I want to watch the film Sholay | an album | play the film शोले |

### Italian (`it-IT`)

| Say | Meaning | Amazon reads it as | It should |
| --- | --- | --- | --- |
| riprendi la tv | Resume the TV | nothing | resume |
| mostra i canali tv | Show the TV channels | the TV guide, without saying which | show the channel list |

### Japanese (`ja-JP`)

| Say | Meaning | Amazon reads it as | It should |
| --- | --- | --- | --- |
| 2001年の千と千尋の神隠しです | Spirited Away from 2001, please | the answer, without the year | answer the question with Spirited Away from 2001 |
| 1984年の方 | The one from 1984 | an answer, without the year | answer the question with the title from 1984 |

### Dutch (`nl-NL`)

| Say | Meaning | Amazon reads it as | It should |
| --- | --- | --- | --- |
| vorige | Previous | nothing | go to the previous item |
| ik bedoel dune uit 2021 | I mean Dune from 2021 | watching a film called "2021" | answer the question with Dune from 2021 |
| wat is er nu op npo een | What's on NPO 1 now | nothing, when the skill knows only a few channels | say what's on NPO 1 |
| zet het volume op 30 | Set the volume to 30 | volume up | set the volume to 30 |
| speel wat muziek van marco borsato | Play some Marco Borsato | a search for "whatmuziek van marco borsato" | play the artist |
| speel wat muziek van marco borsato in de slaapkamer | Play some Marco Borsato in the bedroom | a search for "whatmuziek van marco borsato in de slaapkamer" | play the artist in the bedroom |
| zet de luizenmoeder seizoen 2 aflevering 3 op in de slaapkamer | Put on De Luizenmoeder season 2 episode 3 in the bedroom | binge a show called "de luizenmoeder seizoen 2 aflevering 3" | play that episode in the bedroom |
