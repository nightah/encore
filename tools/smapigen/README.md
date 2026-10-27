# smapigen

Generates `internal/smapi/smapiclient/smapi.gen.go`, the Alexa Skill Management API (SMAPI) client Encore uses to create and update the skill in the user's Amazon developer account.

```sh
go generate ./internal/smapi/smapiclient/         # from the repo root
# which runs, from tools/:
go run ./smapigen -spec smapigen/spec/spec.json -sum smapigen/spec/spec.json.sha256 \
    -out ../internal/smapi/smapiclient
```

## Pipeline

1. **Verify** `spec/spec.json` against `spec/spec.json.sha256` (see [`spec/SOURCES.md`](spec/SOURCES.md) for provenance).
2. **operationId**: the spec has none, only `x-operation-name`; it is copied into `operationId`. Names are unique today; duplicates would get a numeric suffix in sorted path order.
3. **Convert** Swagger 2.0 to OpenAPI 3 with `github.com/getkin/kin-openapi/openapi2conv`, then reload the result through JSON with `openapi3.Loader` (the in-memory conversion leaves empty `allOf` slices that crash oapi-codegen; reloading also resolves `$ref`s exactly as the oapi-codegen CLI would).
4. **Request bodies**: operations without `consumes` convert to `*/*`; these are rewritten to `application/json` so typed JSON body helpers exist.
5. **Generate** with oapi-codegen used as a library (`github.com/oapi-codegen/oapi-codegen/v2/pkg/codegen`): models + client (including `ClientWithResponses`), restricted to the allow-list in [`operations.go`](operations.go) and the schemas those operations reach. Enum constants are always prefixed with their type name because SMAPI reuses values such as `SUCCEEDED` across many enums. The oapi-codegen header is replaced by a deterministic one (it otherwise embeds the VCS version of the tools module).

Every allow-listed operation must exist in the spec, otherwise generation fails. To expose another endpoint, add its `x-operation-name` to `operations.go` and regenerate.

## Runtime dependency

The generated code imports `github.com/oapi-codegen/runtime` (which pulls in `github.com/google/uuid` and `github.com/apapsch/go-jsonmerge/v2`). The version is pinned in `tools/go.mod` through a blank import in `pinned_test.go`; the main module must require the same version.

## Caveats

- **Polymorphism is lost.** Swagger 2.0 models subtypes with `discriminator` on a base definition, and OpenAPI 3 conversion keeps only the base type, so structs such as the manifest's interfaces or the interaction model's slot validations would drop every subtype field. Encore never sends or reads a manifest or interaction model through generated types: it uploads a skill package it builds itself (`internal/smapi/skillpkg`), so those operations aren't in the allow-list, and it decodes responses into `internal/smapi`'s own types.
- The spec has no `host`; `internal/smapi` passes the SMAPI base URL to `NewClient`, and its HTTP client adds the LWA bearer token and retries.
- The spec was last updated by Amazon in June 2023; endpoints added later are not available until a newer spec is published.

## Tests

`go test ./smapigen` (from `tools/`) covers checksum verification, operationId assignment, the body rewrite and the allow-list check, fails if the checked-in client is stale, and builds and vets the generated client in a scratch module requiring the pinned runtime.
