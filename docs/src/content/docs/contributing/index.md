---
title: Contributing
description: How to report bugs, suggest features and send pull requests, and the conventions Encore follows.
---

Contributions are welcome: code, documentation, translations of the voice commands, testing with other Kodi versions and locales, and bug reports.

Everyone taking part is expected to follow the [code of conduct](https://github.com/nightah/encore/blob/main/CODE_OF_CONDUCT.md).

## How Encore is built

Encore is an AI-assisted project. Its maintainer designs it, sets its architecture and conventions, and understands and owns every line of it; AI coding agents (Claude Code) speed up writing the code, tests and documentation. Every change is reviewed and committed by the maintainer, who signs off and GPG-signs every commit, and is held to the same standard as a contribution: understood well enough to explain in review.

Every change goes through the same checks: linting, race-enabled tests, coverage thresholds, generated code with no diff and a docs build, run in CI on every pull request. Releases are built in CI with [SLSA Build Level 3](https://slsa.dev/spec/v1.0/levels#build-l3) provenance, so a binary or image can be traced back to the commit it was built from. The maintainer is responsible for what ships, bugs included.

## Bugs and feature requests

Open a [bug report](https://github.com/nightah/encore/issues/new?template=bug_report.yml) or a [feature request](https://github.com/nightah/encore/issues/new?template=feature_request.yml), after checking the [existing issues](https://github.com/nightah/encore/issues). For a bug, include the request log entry (**Copy JSON**) and the log lines around it.

**Don't report security vulnerabilities in public issues.** Use [GitHub Security Advisories](https://github.com/nightah/encore/security/advisories/new) instead, as the [security policy](https://github.com/nightah/encore/blob/main/SECURITY.md) explains.

## Translations

Translations are managed on [Crowdin](https://crowdin.com/project/encore), where anyone can sign in and translate; it opens a pull request with new translations (`.crowdin.yml`). For the Alexa skill, three files in `i18n/lang/en/` are translated:

- `responses.yaml`: what Alexa says. A message not translated yet is spoken in English.
- `utterances.yaml`: how people ask for each thing, one entry per way of asking (`WatchMovie.play_movie`). A translation can hold several patterns, one per line; `(a|b)` picks one, `[a]` is optional and `{Slot}` is a slot.
- `types.yaml`: words for the fixed slot values, such as media types and navigation actions: the value on the first line and its synonyms below it.

Example rooms, playlists and genres (`examples.yaml`) aren't translated: each language picks its own, and deploys replace them with what's set up in Encore.

The translated files (`i18n/lang/<language>/` and `web/src/i18n/locales/<language>/`) are written by Crowdin, in its own layout and with the English file's comments, so change translations on Crowdin rather than in the repository: its next pull request would undo an edit made there. The English skill files are written the same way (double-quoted values, no blank lines, a one-line comment at the top), which yamllint and a test in `i18n/` check, so a sync only changes what was translated.

### Writing for each language

Responses are filled in by the handler: every language uses the same `{placeholders}` (the linter checks), and the wording around a placeholder avoids agreeing with it, since the handler can't inflect. `{remaining}` is a spoken duration ("1 hour 12 minutes"), `{end}` a time ("15:04"), `{language}` a language name and `{titles}` a list joined with the language's "and". Where a response suggests what to say, it uses a phrasing `utterances.yaml` accepts. An optional room at the end of a request, such as `[(in|on) the {Room}]` in English, lets it name the room whose Kodi should play it.

- **German:** Alexa says "du". `{remaining}` reads like "1 Stunde 12 Minuten", `{language}` like "Französisch", and lists end in "und". People speak with du-imperatives ("spiel", "spiele", "zeig mir"), and the main playback intents also take the infinitive for one-shot requests ("Alexa, sag encore, den Film Dune abzuspielen"). Rooms are `[(im|in der) {Room}]`, so room names carry no article.
- **Spanish (Spain):** Alexa uses tú, and `{remaining}` reads like "1 hora 12 minutos". People speak with tú imperatives ("pon", "reproduce"); one-shot requests ("Alexa, pide a encore que ponga Dune") take the subjunctive, so the main playback verbs list both. Rooms are `[en (el|la) {Room}]`.
- **French:** Alexa says "vous". Placeholders that may start with a vowel stay away from "de" and "le" behind a noun that carries them ("la série {show}", "le film {title}"), since the handler can't elide. People speak with tu-imperatives ("mets", "lance", "joue"); one-shot requests go through "demande à kodi de …", which takes an infinitive, so the main verbs come in both forms ("mets|mettre"). Rooms are `[(dans le|dans la|au|à la) {Room}]`. Some slot types hold the verb itself (`SEEK_DIRECTION` "avance"/"recule", `SUBTITLE_ACTION` "active"/"désactive", `POWER_ACTION` "éteins"), because French puts the action first ("avance de dix minutes"); "arrête" and a bare "quitte" are left out, since said alone they would take over the built-in stop.
- **Regional Spanish and French (es-MX, es-US, fr-CA):** these locales use the `es` and `fr` language files; their overlays in `i18n/locales/` add utterances and slot-type synonyms and override responses. Mexico and the US share one overlay, so change both files together. Alexa uses tú there, as in Spain, so responses use the preterite where Spain uses the perfect ("no encontré"), "está bien" for "vale", "configuración", "video" and "cuarto". Times are on the twelve-hour clock ("3:04 p. m."), so a response puts `{end}` where it needs no article ("Hora de término: {end}"), since "a las" would have to be "a la" at one o'clock. Utterances add "pon", "ponme", "la peli", "el capítulo", "la rola", "dale play" and "en mi {Room}". Canadian French uses vous, as in France, and kodi for the encore choice; it adds "fais jouer", "fais-moi entendre", "l'émission", "le téléroman", "la toune" and "pèse sur", and responses say "liste de lecture", "vidéoclip" and "compétence".
- **Italian:** Alexa uses "tu". `{remaining}` reads like "1 ora 12 minuti", `{language}` is a lowercase Italian language name and lists are joined with "e". Requests are tu-imperatives ("metti il film Dune"); one-shot requests ("Alexa, chiedi a encore di riprodurre Dune") take the infinitive, so the main verbs come in both forms. Rooms are `[(in|nel|nella) {Room}]`.
- **Dutch:** Alexa uses "je". `{remaining}` reads like "1 uur 12 minuten", `{language}` is a capitalised Dutch language name ("in het Duits") and lists are joined with "en". Requests are imperatives ("speel de film Dune", "zet Flodder op", "laat me de film Dune zien") or "ik wil … kijken"; one-shot requests ("Alexa, vraag encore om de film Dune af te spelen") take "om … af te spelen". Rooms are `[(in de|in het|op de|op) {Room}]`, the bare "op" for "op zolder"; samples ending in the separable "op" ("zet … op") leave it out. `nl-NL` has no `AMAZON.SearchQuery`, so its title, favourite, add-on and text slots take the fallback custom types, whose examples are in `lang/nl/examples.yaml`.
- **Brazilian Portuguese:** Alexa says "você". Every verb-first sample has the colloquial imperative (toca, coloca, mostra) and the infinitive (tocar, colocar, mostrar), since a one-shot "Alexa, peça ao kodi para mostrar os filmes" hands the skill an infinitive; playback and browsing also take the formal imperative (toque, coloque, ponha, reproduza, mostre), which "peça ao kodi que toque …" needs. Keep the "quero assistir/ver/ouvir" forms and write accents and tildes. Adjectives such as "anterior" follow the noun, so slots like `{Step}` need both orders ("áudio anterior", "próximo áudio"). The invocation is "kodi": Portuguese reads "encore" as "en-có-ri", which is heard as "âncora", and "bis" is a common word.
- **Arabic:** samples address Alexa in the feminine imperative her Saudi voice uses (شغلي, افتحي, وقفي) and also accept the masculine and Modern Standard forms people say (شغل, اعرض, ابدأ). They are everyday Gulf and Saudi Arabic with MSA variants and dialect question words (وش, ايش), undiacritised, with numbers spelt out; adjectives follow their noun, and slot values take the feminine forms that agree with feminine nouns ("الترجمة التالية"). Responses are polite MSA that never inflects for the listener's gender or a placeholder ("هل المقصود …", "يمكن مثلا قول: …"). `{remaining}` follows Arabic number agreement ("ساعتان", "3 ساعات", "11 ساعة"), and lists end in a "و" joined to the last item. The invocation name is in Arabic letters (`انكور`, with `kodi_invocation: كودي`).
- **Hindi:** Alexa speaks as a woman in the first person ("खोल रही हूँ") and addresses the listener as आप. `{remaining}` reads like "1 घंटा 12 मिनट", `{end}` like "शाम 7:05 बजे", and lists are joined with "और". English technical words stay where Indians say them (ऐड-ऑन, सेटिंग्स), and placeholders sit behind "नाम का" so nothing has to agree with them. Requests put the verb last, with familiar imperatives (चलाओ, लगाओ, दिखाओ, बजाओ, play करो), polite ones (चलाइए) and the subjunctive for one-shot requests ("Alexa, encore से कहो कि Dune चलाए"). English words come in both scripts ("play करो" and "प्ले करो"), as Amazon advises for `hi-IN`. Rooms come first as `[{Room} में]`, so room names carry no postposition.
- **Japanese:** samples have no spaces: the slot comes before its particle (「{Movie}を再生して」), the room comes first with で or just before the verb (「{Room}で映画{Movie}を再生して」, 「映画{Movie}を{Room}で再生して」), each request has a plain して form and a ください form, and numbers are written in kana or kanji, never digits. Slot values used inside a pattern must be bare nouns or verbs with no particle or object of their own, or the expanded samples double up. Responses are polite です/ます Japanese. The invocation is アンコール, with `kodi_invocation: コディ`.

A locale whose script can't write the Latin "kodi" gives its own spelling as `kodi_invocation`. A language written without spaces goes in the Alexa adapter's `unspaced` map, so utterances and queries are compared character by character, and one that puts the verb last goes in `verbFinal`, so carrier phrases may follow a search query.

The web interface's text is in `web/src/i18n/locales/en/`, one JSON file per area of the interface; a string not translated yet shows in English. In code, English text goes in those files and components look it up with `useTranslation` (an unknown key is a type error); every other language arrives from Crowdin. Plural keys follow each language's own categories (`_one` and `_other` in most, only `_other` in Japanese, `_zero` to `_many` in Arabic). Arabic is laid out right to left, so spacing and alignment use logical classes (`ms-`, `pe-`, `start-`, `text-end`) rather than left and right, and icons that point forward or back are mirrored with `rtl:`.

## Pull requests

Fork the repository and open a pull request. [Development setup](/encore/contributing/development/) covers running Encore locally and the checks.

- For anything larger than a small fix, open an issue first, so we can agree on what to change and how before you spend time on it. Say when you start on an issue, so nobody duplicates the work.
- Keep a pull request to one change, and add or update tests with it. A fix comes with a test that fails without it.
- It can be merged once every check passes: `make lint`, `make test`, and generated code with no diff. The [Git hooks](/encore/contributing/development/#git-hooks) run the quick ones before each commit.
- Pull requests are squash-merged, so commit as often as you like on your branch. Please don't force-push once a review has started, except to rebase or to fix the commit message.
- To ask for another review, use **Re-request review** rather than mentioning the reviewer.

### AI-assisted contributions

Pull requests written with AI tools are welcome, on the same terms as any other. You must understand every line you submit and be able to explain it in review, and you're responsible for it as its author: that it works, that it's tested, and that you have the right to contribute it. A pull request its author can't explain will be closed.

## Commit messages

The pull request title becomes the commit message, so it follows [Conventional Commits](https://www.conventionalcommits.org/): `feat(kodi): add queue intent`, `fix(alexa): handle an empty slot`. The header is lower case throughout, names and acronyms included (`fix(kodi): retry when kodi is asleep`), at most 72 characters, where GitHub truncates it, and the scope is one of those listed in `web/commitlint.config.mjs` (a package or area, such as `kodi`, `alexa`, `web`, `setup` or `deps`). Use a single scope; a change across several areas leaves the scope out instead, as in `refactor: share the retry helper`. The body explains why, one line per paragraph. The `commit-msg` hook and the pull request checks enforce this with [commitlint](https://commitlint.js.org/).

The type is one of `build`, `ci`, `docs`, `feat`, `fix`, `i18n` (internationalisation: the tooling and code that make Encore translatable), `l10n` (translations, from Crowdin), `perf`, `refactor`, `release` (the commit that stamps a new version), `revert` or `test`. It labels the pull request, which groups it in the release notes (each type has its own section, dependency updates have theirs, and `release` is left out) and puts it in the milestone of the release it will ship in:

- `feat:` goes in the next minor release; any other type in the next patch release.
- `feat!:` (any type with `!`) or a `BREAKING CHANGE:` footer is a breaking change, for the next major release.

### Scopes

The scope becomes the pull request's `area/<scope>` label.

| Scope | Covers |
| --- | --- |
| `actions` | GitHub Actions workflows (`.github/`) |
| `alexa` | The Alexa request handling (`internal/adapter/alexa`) |
| `api` | The admin API (`internal/api`) |
| `app` | The application wiring: Kodi instances, library groups, targets (`internal/app`) |
| `auth` | Sign-in, sessions and admin accounts (`internal/auth`) |
| `backups` | Scheduled backups and `encore restore` |
| `basepath` | The request base path (`internal/basepath`) |
| `benchmarks` | The comparisons with replaced libraries (`benchmarks/`) |
| `blobstore` | The local, S3 and WebDAV storage backends (`internal/blobstore`) |
| `buildinfo` | Version and build details (`internal/buildinfo`) |
| `cli` | The commands (`cmd/encore`, `internal/cli`) |
| `config` | Configuration loading and validation (`internal/config`) |
| `core` | The channel-agnostic action core (`internal/core`) |
| `deploy` | Deploying the skill to Amazon |
| `deps` | Dependency updates |
| `docker` | The container image (`Dockerfile`, `docker/`) |
| `docs` | This site (`docs/`) |
| `docsgen` | The generated reference pages (`internal/docsgen`) |
| `e2e` | The Playwright tests (`web/e2e`) |
| `fakekodi` | The fake Kodi used in tests (`internal/backend/kodi/fakekodi`) |
| `fuzzy` | The fuzzy matching library: phonetic encoders per language (`internal/fuzzy`) |
| `i18n` | The interaction model and translations (`i18n/`, the web interface's locales) |
| `kodi` | The Kodi backend (`internal/backend/kodi`) |
| `layout` | The package layout test (`internal/layout`) |
| `lefthook` | The Git hooks (`.lefthook.yml`) |
| `library` | Library sync and the title index (`internal/core/library`) |
| `logs` | Log messages and logging |
| `match` | Title matching (`internal/core/match`) |
| `media` | The media backend contract (`internal/core/media`) |
| `overview` | The Overview page |
| `requests` | The request log |
| `secrets` | Secrets at rest and the secrets key (`internal/secrets`) |
| `server` | The HTTP listener and the embedded web interface (`internal/server`) |
| `settings` | The Settings page and settings API |
| `setup` | The setup wizard |
| `skill` | The Alexa skill page and skill settings |
| `skillmodel` | Building and linting the interaction model (`internal/skillmodel`) |
| `smapi` | Amazon's skill management API and Login with Amazon (`internal/smapi`) |
| `statestore` | Saving state for stateless deployments (`internal/statestore`) |
| `storage` | Storage settings and backup locations |
| `store` | The SQLite database and migrations (`internal/store`) |
| `testkit` | Shared test helpers (`internal/testkit`) |
| `tools` | The code generators and developer tools (`tools/`) |
| `web` | The web interface (`web/`) |

Signing off your commits with `git commit -s` ([Developer Certificate of Origin](https://developercertificate.org/)) is encouraged but not enforced.

## Conventions

**Writing.** Australian English (licence, authorise). "Encore" is capitalised in prose; the command, binary, module, `encore.yml`, `ENCORE_*` variables and the Alexa invocation stay lower case. Say "Kodi instances", not boxes or servers. Markdown, comments and YAML keep one line per paragraph, with no hard wrapping.

**Go.** `golangci-lint` must pass. Each package keeps its constants in `consts.go`, its types in `types.go`, its sentinel errors and error types in `errors.go`, and its package comment in `doc.go`; `go test ./internal/layout/` enforces this. Exported identifiers have doc comments; unexported ones get one only when the name can't say what it's for, and comments inside functions explain why rather than what. Log lines use the keys `instance=` and `backend=` for a Kodi instance, `host=` for an address and one `component=`; quote human text with “”, include `error` only when there is one, and never log secrets or tokens.

**Web.** Biome and `tsc` must pass. Import through the per-folder aliases (`@api/…`, `@components/…`, `@features/…` and so on), never relative paths.

**Generated code** (the Kodi and SMAPI clients, the OpenAPI document and the web client) is committed: run `make generate`, and `pnpm generate:api` in `web/` after an API change, and commit the result. The docs' reference pages are generated at build time instead.

## Database migrations

The schema lives in `internal/store/migrations/`. The files are embedded in the binary, and [goose](https://github.com/pressly/goose) applies any that haven't run each time Encore opens its database, so upgrading Encore upgrades the database in place.

| Migration | What it does |
| --- | --- |
| `00001_init.sql` | The initial schema. |
| `00002_room_alias_kind.sql` | Marks each spoken name for a room as another name for the room or a device in it. |

:::note
Until 1.0.0, migrations are added for every schema change so development databases upgrade in place. When 1.0.0 is released they'll all be reset into a single migration for the 1.0.0 schema, and databases from earlier development builds won't upgrade.
:::

- **Never edit a migration that has been released.** Installations have already run it. Add a new file with the next number and a short description, such as `00002_room_icons.sql`.
- **Write both directions.** `-- +goose Up` makes the change; `-- +goose Down` undoes it. Up must work on any database the previous release could have written, with data in it.
- **Follow the existing tables.** `STRICT` tables, timestamps as Unix milliseconds in an `INTEGER`, booleans as `INTEGER` 0/1 with a `CHECK`, and `NOT NULL` with a default where a value always exists. Foreign keys are enforced.
- **Mind what SQLite can alter.** Adding a column, or dropping one nothing depends on, works directly. Changing a column's type or constraints means rebuilding the table: create the new one, copy the rows across, drop the old one and rename the new one (see [SQLite's ALTER TABLE](https://www.sqlite.org/lang_altertable.html#otheralter)).
- **Change the code with it.** Update the repository in `internal/store` (its column list and scanning) and its tests in the same commit, and test the upgrade against rows in the old shape.
- **Migrations only go forward.** A newer Encore restores an older backup and migrates it, but an older Encore refuses a backup made by a newer one, so a release can't rely on users going back.

```sql title="internal/store/migrations/00002_room_icons.sql"
-- An icon for each room, shown in the web interface.

-- +goose Up
ALTER TABLE rooms ADD COLUMN icon TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE rooms DROP COLUMN icon;
```

## Licence

By contributing, you agree that your contributions are licensed under the [Apache License 2.0](https://github.com/nightah/encore/blob/main/LICENSE).
