/**
 * Commit messages and pull request titles (which become the squash-merged commit): Conventional Commits, whose type and scope label the pull request (.github/workflows/pr-labels.yml) and group the release notes. The header stops at 72 characters, where GitHub truncates it.
 */
export default {
  defaultIgnores: true,
  extends: ["@commitlint/config-conventional"],
  helpUrl: "https://nightah.github.io/encore/contributing/#commit-messages",
  // Renovate writes its own titles (.renovaterc.json), which can run past 72 characters with long package names.
  ignores: [(message) => /^(build|ci|fix)\(deps\): /.test(message)],
  plugins: [
    {
      rules: {
        // A change across several areas leaves the scope out rather than listing them.
        "scope-single": ({ scope }) => [
          !scope || !/[,/\\]/.test(scope),
          "use one scope, or leave it out for a change across several areas",
        ],
      },
    },
  ],
  rules: {
    // Bodies keep one line per paragraph, including paragraphs commitlint reads as footers.
    "body-max-line-length": [2, "always", Number.POSITIVE_INFINITY],
    "footer-max-line-length": [2, "always", Number.POSITIVE_INFINITY],
    // Headers are lower case throughout, names and acronyms included (encore, kodi, oidc).
    "header-case": [2, "always", "lower-case"],
    "header-max-length": [2, "always", 72],
    "scope-enum": [
      2,
      "always",
      [
        "actions",
        "alexa",
        "api",
        "app",
        "auth",
        "backups",
        "basepath",
        "blobstore",
        "buildinfo",
        "cli",
        "config",
        "core",
        "deploy",
        "deps",
        "docker",
        "docs",
        "docsgen",
        "e2e",
        "fakekodi",
        "i18n",
        "kodi",
        "layout",
        "lefthook",
        "library",
        "logs",
        "match",
        "media",
        "overview",
        "requests",
        "secrets",
        "server",
        "settings",
        "setup",
        "skill",
        "skillmodel",
        "smapi",
        "statestore",
        "storage",
        "store",
        "testkit",
        "tools",
        "web",
      ],
    ],
    "scope-single": [2, "always"],
    // No chore; i18n is Crowdin's translation pull requests (.crowdin.yml); release is the commit that stamps a new version (make version).
    "type-enum": [
      2,
      "always",
      ["build", "ci", "docs", "feat", "fix", "i18n", "perf", "refactor", "release", "revert", "test"],
    ],
  },
};
