# Pinned SMAPI spec

`spec.json` is the Swagger 2.0 description of the Alexa Skill Management API (SMAPI) that Amazon publishes with its SDK models, stored byte-for-byte.

| Field | Value |
|-------|-------|
| Repository | `alexa/alexa-apis-for-nodejs` |
| Path | `ask-smapi-model/spec.json` |
| Commit | `56a90b1eb721970bf28e7c3d25232ce92ff69485` ("chore: release model", 2023-06-13, the latest commit touching the file) |
| URL | `https://raw.githubusercontent.com/alexa/alexa-apis-for-nodejs/56a90b1eb721970bf28e7c3d25232ce92ff69485/ask-smapi-model/spec.json` |
| SHA-256 | `b3ed0a3ccd7f2301c54efb73c4395b3f1bbf7aba8e14082600920f63142f03f9` |

`spec.json.sha256` holds the same checksum in `sha256sum` format (`sha256sum -c spec.json.sha256`); smapigen refuses to run when it does not match. To bump the spec, download the new file, update both this table and `spec.json.sha256`, then run `go generate ./internal/smapi/smapiclient`.
