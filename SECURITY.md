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

Archives and container images carry [SLSA Build Level 3](https://slsa.dev) provenance, signed with Sigstore, which proves they were built from this repository by its release workflow. Check them with [slsa-verifier](https://github.com/slsa-framework/slsa-verifier), using `encore.intoto.jsonl` from the release:

```sh
slsa-verifier verify-artifact --provenance-path encore.intoto.jsonl --source-uri github.com/nightah/encore encore_<version>_linux_amd64.tar.gz
slsa-verifier verify-image ghcr.io/nightah/encore@<digest> --source-uri github.com/nightah/encore
```
