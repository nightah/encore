---
title: Matching and aliases
description: How Encore finds the title you asked for, how to test it, and how to teach it other names.
---

Alexa hands Encore what it heard, such as "rocky two" or "the avengers end game". Encore matches that against an in-memory index of your library rather than relying on Alexa alone, so it copes with spelling, numbering and titles Alexa has never heard of.

## How a title is matched

1. **Normalise.** Both the request and every title are folded the same way: case and accents removed, punctuation and leading articles dropped, and initials joined ("E.T." becomes "et"). Numbers are made consistent: roman numerals after a word, number words, ordinals and spoken years become digits ("rocky second" is "rocky 2", "nineteen eighty four" is "1984", "the 6th sense" is "6 sense"), and "part" or "chapter" before a number is dropped. English, German, French, Spanish, Italian, Dutch, Portuguese, Arabic, Hindi and Japanese each have their own articles and number words. German, Italian and Dutch numbers written as one word are read too ("neunzehnhundertvierundachtzig", "millenovecentottantaquattro" and "negentienhonderdvierentachtig" are all "1984"), and ½ reads as the language says "and a half", so *8½* is "8 e mezzo" in Italian. Spanish reads acronyms spelt by letter name ("de ve de" is "dvd"), Portuguese reads feminine and plural numbers (duas, duzentas, milhões) and spells ç as ss, and Dutch titles also read English numbers and ordinals, as Dutch libraries keep English titles as published.
   - **Other scripts.** Arabic, Hindi and Japanese requests and titles are compared in Latin letters, so a title in one script meets a request in another. Arabic folds the letters written several ways for one sound (hamza seats, ة and ه, ى and ي), strips the article ال and its fused forms (وال, بال, لل), and is written as a consonant skeleton, so an English title asked for in Arabic letters ("تايتانك") meets *Titanic*. Hindi arrives in Devanagari while most Indian libraries have romanised titles, so it is written the way film titles are (schwa deletion, nukta letters, nasals by the next consonant) and "दिलवाले दुल्हनिया ले जाएंगे" meets *Dilwale Dulhania Le Jayenge*; Hindi numbers read lakh and crore. Japanese is split into words with a port of TinySegmenter and its kana written in Hepburn romaji without long vowels, so hiragana, katakana and Latin spellings of a word meet; kanji numerals become digits and kanji stay as written, since reading them takes a dictionary. Every Unicode digit (٣, ३) counts as its ASCII digit.
2. **Aliases.** Every title also answers to variants: roman numerals converted ("Rocky V" is "rocky 5"), German umlauts spelt out, and shortenings that drop or keep only a subtitle ("Dune: Part Two" is also "Dune", "Avengers: Endgame" also "Endgame"). Derived variants score a little lower than the real title. Your own aliases (below) count as the title.
3. **Find candidates** with fuzzy word matches, sound-alike matches and, when those find little, character trigrams. Each language has its own sound-alike rules: Double Metaphone for English, Italian and Dutch, and for Hindi and Japanese in Latin letters; Kölner Phonetik for German (which also treats a final "-er" like "-a", so "lieda" finds *Lieder*); and Encore's own rules for French, Spanish, Brazilian Portuguese and Arabic. The Spanish rules also hear the Mexican and Caribbean jota in an h or w ("hosé" finds *José*) and the French ones the Québec "quoi" ("kwè"); the Portuguese ones hear Brazilian d and t before i, a final l said as u and the nasal endings; the Arabic ones merge the consonants Arabic dialects interchange (ق, ج and g, the interdentals and sibilants, خ and ح). [Dependencies and benchmarks](/encore/contributing/dependencies/#phonetic-matching) explains why and how well each does.
4. **Score** each candidate by how well the words, characters and sounds agree. An exact match scores 1. A year at the end of the request ("dune 2021") is matched against the release year, and numbers must match exactly, so "rocky 2" never plays "Rocky 3". Asking for a sequel your library doesn't have ("crocodile dundee 2" when it only has "Crocodile Dundee") makes Alexa ask rather than play the original.
5. **Decide.** Encore plays the best match when it's exact and unique, or clearly ahead of the rest. When two or more are close, Alexa asks which one you meant (see [answering a question](/encore/voice-commands/#answering-a-question)). When nothing scores well enough, Alexa says it couldn't find it.

The rules for a library follow its Kodi instance's `language` (English by default), set in [config](/encore/reference/configuration/#kodilanguage).

[`alexa.deep_search`](/encore/reference/configuration/#alexadeep_search) (on by default) makes "play X" search music as well as films and TV when the request doesn't say what kind of title it is.

A "play X" request whose words are one of the skill's film, TV or music commands, such as "play the next episode of Bluey", "play season 2 episode 4 of Bluey", "play the movie Frozen" or "put on music by Pink Floyd", isn't searched as a title first: Encore runs that command and matches only the name in it ("Bluey", "Frozen", "Pink Floyd"). Alexa sends a request this way when it doesn't recognise the name, so without it the whole phrase would be searched and found nothing. When the command finds nothing, the whole phrase is searched after all, so a film called *The Sound of Music* still plays rather than a genre called "the sound of". Numbers only count as numbers here when Alexa writes them as digits.

Alexa can also leave a word for a kind of title in the name: "play Frozen movie", "play Innuendo album", "千と千尋の神隠しの映画を再生して". When the name as heard finds nothing, Encore takes the word off and searches only that kind ("Frozen" among films), using the words for films, shows, albums, songs, artists and music videos each language lists for recommendations. Words that name no title at all play what they mean: "play music by Queen" or "play something by Queen" heard as a song request plays Queen, and "play some songs" on its own shuffles all your music.

## The match tester

On the **Library** page's match tester, type what Alexa would hear, choose a library and optionally the kinds of title, and see the normalised query, the decision (play, ask or not found), the margin between the top two, and every candidate with a breakdown of its score. It runs against the live index without touching Kodi.

The same page shows each library's newest titles and whether each one is findable by voice and included in the skill model.

## Aliases

When a title is hard to say or Alexa keeps hearing it differently, give it an alias: another name it should answer to. In the match tester, type the phrase, check that the top result is the title you mean, and choose **Add "…" as an alias for the top result**; for example "lotr two" for "The Lord of the Rings: The Two Towers". The library's aliases are listed on the same page, where you can remove them.

- Aliases work for voice requests straight away.
- On the next [deploy](/encore/guides/deploy-skill/#redeploying) they are added to the skill model as synonyms of the title, which helps Alexa recognise them.
- An alias belongs to a library, so instances that [share a library](/encore/guides/shared-libraries/) share their aliases.

Rooms have aliases too; see [Echo devices and rooms](/encore/guides/echo-devices/#rooms).

## Titles in the skill model

When Encore deploys the skill, your library's titles, rooms, playlists, genres and music videos become slot values, each title with its aliases as synonyms. Amazon limits each interaction model to 1.5 MB, so Encore includes the most recently added or played titles of each kind up to a limit. Titles that don't fit can still be found by voice, because Encore's matcher always searches the whole library.
