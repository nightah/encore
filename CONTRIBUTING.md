# Contributing

Contributions are welcome: code, documentation, translations of the voice commands, testing with other Kodi versions and locales, and bug reports. If Encore is useful to you, you can also support it through [GitHub Sponsors](https://github.com/sponsors/nightah) or [Ko-fi](https://ko-fi.com/nightah).

Everyone taking part is expected to follow the [code of conduct](CODE_OF_CONDUCT.md).

## Bugs and feature requests

Open a [bug report](https://github.com/nightah/encore/issues/new?template=bug_report.yml) or a [feature request](https://github.com/nightah/encore/issues/new?template=feature_request.yml), after checking the existing issues. Report security vulnerabilities privately, as [SECURITY.md](SECURITY.md) explains, never in a public issue.

## Code

Fork the repository and open a pull request. [Development setup](https://nightah.github.io/encore/contributing/development/) covers running Encore locally, the Git hooks (installed by `pnpm install` in `web/`), the make targets and the tests, and [Contributing](https://nightah.github.io/encore/contributing/) covers the conventions.

For anything larger than a small fix, open an issue first so we can agree on what to change and how. Add or update tests with each change; pull requests are merged once every check passes (`make lint`, `make test` and generated code with no diff).

Pull requests are squash-merged and the title becomes the commit message, so it follows [Conventional Commits](https://www.conventionalcommits.org/), with a header of at most 72 characters; commitlint checks it. Commit as often as you like on your branch, but please don't force-push once a review has started, except to rebase or fix the commit message. To ask for another review, use **Re-request review** rather than mentioning the reviewer.

## Licence

By contributing, you agree that your contributions are licensed under the [Apache License 2.0](LICENSE).
