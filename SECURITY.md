# Security policy

## Supported versions

Encore is pre-1.0. Only the **latest release** gets security fixes.

## Reporting a vulnerability

**Please do not report security vulnerabilities through public GitHub issues, discussions or pull requests.**

Report them privately through [GitHub Security Advisories](https://github.com/nightah/encore/security/advisories/new) for `nightah/encore` (**Security** tab, then **Report a vulnerability**).

Please include:

- the affected version (`encore version`) and how you deploy it
- a description of the issue and its impact
- steps to reproduce, or a proof of concept
- any suggested fix or mitigation

Redact any real tokens, skill IDs or personal data.

You should get an acknowledgement within a few days. We will keep you updated while we investigate, agree a disclosure timeline with you, and credit you in the advisory unless you prefer otherwise.

## Verifying releases

Archives and container images carry [SLSA Build Level 3](https://slsa.dev) provenance, signed with Sigstore through [GitHub artifact attestations](https://docs.github.com/en/actions/concepts/security/artifact-attestations), which proves they were built from this repository by its release workflow. Check them with the [GitHub CLI](https://cli.github.com), using `encore.intoto.jsonl` from the release for an archive:

```sh
gh attestation verify encore_<version>_linux_amd64.tar.gz --bundle encore.intoto.jsonl --repo nightah/encore
gh attestation verify oci://ghcr.io/nightah/encore:<version> --repo nightah/encore
```

The image check fetches the attestation from GitHub, so it needs the CLI signed in (`gh auth login`). For a stricter check, add `--signer-workflow nightah/encore/.github/workflows/release-slsa.yml` to require the release workflow itself as the signer, and `--source-ref refs/tags/v<version>` to require that release's tag.

0.1.0 and 0.1.1 were attested with slsa-github-generator instead. Their archives need its repository as the signer and its older provenance version, and their images need [cosign](https://github.com/sigstore/cosign), as the GitHub CLI can't find those attestations in the registry:

```sh
gh attestation verify encore_0.1.1_linux_amd64.tar.gz --bundle encore.intoto.jsonl --repo nightah/encore --signer-repo slsa-framework/slsa-github-generator --predicate-type https://slsa.dev/provenance/v0.2
cosign verify-attestation --type slsaprovenance --certificate-oidc-issuer https://token.actions.githubusercontent.com --certificate-identity-regexp '^https://github.com/slsa-framework/slsa-github-generator/' --certificate-github-workflow-repository nightah/encore ghcr.io/nightah/encore:0.1.1 > /dev/null
```
