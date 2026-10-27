# kodigen

Generates `internal/backend/kodi/kodirpc` from Kodi's JSON-RPC introspection schema (`methods.json`, `types.json`, `notifications.json`, `version.txt`). The output is stdlib-only, gofmt'd and deterministic.

```sh
go generate ./internal/backend/kodi/kodirpc/      # from the repo root
# which runs, from tools/:
go run ./kodigen -schema kodigen/schema/piers -compat kodigen/schema/omega \
    -out ../internal/backend/kodi/kodirpc
```

`tools` is a separate Go module so generator dependencies never reach the main module. The pinned inputs and their checksums are in [`schema/SOURCES.md`](schema/SOURCES.md).

## Output

| File | Contents |
|------|----------|
| `types.gen.go` | One declaration per `types.json` id (`Video.Fields.Movie` → `VideoFieldsMovie`) plus the inline types they need |
| `methods.gen.go` | `<Method>Params` / `<Method>Result` per method, `Method*` name constants, the `Methods` table, `SchemaVersion`, `BaselineVersion` |
| `client.gen.go` | `func (c Client) <Method>(ctx, p) (<Method>Result, error)` wrappers over `Call` |
| `notifications.gen.go` | `<Notification>Notification` payload structs, `NotificationName` constants, `Notifications`, `NewNotificationPayload` |

Hand-written files in the same package: `caller.go` (`Caller`, `Call`, `Client`), `version.go` (`APIVersion`, `MethodInfo`), `union.go` (the union helper) and `generate.go`. The generator only ever writes or deletes `*.gen.go`.

## Schema handling

The schema is a JSON Schema draft-03 dialect with Kodi-specific quirks:

- **Comments**: `//` and `/* */` are stripped outside string literals.
- **`required`**: `true` on a property means no `omitempty`. A few entries use the strings `"true"`/`"false"`; both forms are accepted.
- **Property order** is preserved from the files, so struct fields follow the schema. Everything else (types, methods, constants) is sorted.
- **`extends`**: a single object parent is *embedded*, so `VideoDetailsMovie` embeds `VideoDetailsFile` and fields are promoted. Multiple parents (`List.Item.Base` extends both `Video.Details.File` and `Audio.Details.Media`) are *flattened*, because the two chains share `Item.Details.Base` and `encoding/json` silently drops fields that two embedded structs provide at the same depth. Non-object schemas (for example the field-name arrays extending `Item.Fields.Base`) are merged keyword by keyword with the child winning.
- **Unions** (`"type": [...]`):
  - `null` plus one alternative becomes that type, wrapped in a pointer unless it is already nilable (`Optional.String` → `type OptionalString = *string`).
  - Two or more alternatives become a struct embedding the hand-written `union` helper, with `New<Union>From<Variant>(v)` constructors and `As<Variant>() (T, error)` accessors. Decoding keeps the raw JSON; the accessors decode on demand and do **not** validate which alternative was sent, since Kodi's alternatives often overlap structurally (objects without `additionalProperties: false`). Callers choose the alternative from context, or inspect `Raw()`. Variant names are the referenced type, the primitive (`Bool`, `String`, `Int`, ...) or, for inline objects, the required property names (`PlayerOpenItemPlaylistID`).
  - Unions implement `IsZero`, so optional union fields use `omitzero`.
- **Enums** become `type X string` (or `int`) with one constant per value, named `<Type><Value>`. Inline enums are named after their context (`VideoFieldsMovie` is `[]VideoFieldsMovieItem`).
- **Inline types** are named `<Parent><Field>`, `<Parent>Item` (array elements) and `<Parent>Value` (map values); a numeric suffix resolves the rare clash with a schema id. Collisions between schema ids are an error.
- **Optional fields**: structs are pointers; slices, maps and scalars use `omitempty`; a scalar whose schema `default` is non-zero (and whose zero value is legal) becomes a pointer so the zero value can still be sent (`JSONRPC.Introspect`'s `getdescriptions` is `*bool`). Only the default written at the use site counts, not one on the referenced type (`Library.Id`'s `-1` is a "no id" sentinel). Note that zero values in responses are indistinguishable from absent ones for non-pointer fields.
- **Type misuse**: `"type": "Integer"` is treated as `integer`, and a type id used as `"type"` (`"type": "Video.Ratings"`) as a `$ref`.
- **Inline ids**: schemas with an `id` outside `types.json` (`Notifications.Library.Audio.Type`) are hoisted to named types.
- **Runtime enums**: `Addon.Types`, `GUI.Window`, `Input.Action`, `List.Filter.Operators` and `List.Filter.Fields.*` are registered by Kodi at startup from C++ tables and never appear in the files. They are generated as plain `string` types; list their values with `JSONRPC.Introspect`.
- Validation keywords (`minimum`, `minLength`, `uniqueItems`, ...) are ignored. `any`, `null` and schema-less objects map to `any` / `map[string]any`.

## Versions and availability

The package is generated from the newest pinned schema (Piers, API 13.1000.0). The `-compat` schema (Omega, API 13.5.0) only feeds availability data into the `Methods` table:

- `MethodInfo.Since` is `BaselineVersion` (13.5.0) for methods present in Omega and `SchemaVersion` for Piers-only methods (`Player.GetChapters`, `GUI.TakeScreenshot`, `VideoLibrary.SetSourceContent`, ...). "Since" is the oldest *pinned* version; the method may be older.
- `MethodInfo.ParamsSince` lists parameters added after the method existed (for example `trailer` on `VideoLibrary.SetTVShowDetails`).
- `MethodInfo.AvailableIn(v)` / `ParamAvailableIn(p, v)` gate calls at runtime against the version reported by `JSONRPC.Version`.

Types are not versioned: new fields and enum values simply never appear when talking to an older Kodi. Methods removed in Piers would not be generated (there are none between these two pins).

## Tests

`go test ./kodigen` (from `tools/`) covers the preprocessor (comments, `required`, `extends`, unions, naming), fails if the checked-in output is stale, and builds and vets the generated package together with the hand-written files in a scratch module. Round-trip tests for real payloads live in `internal/backend/kodi/kodirpc/kodirpc_test.go`.
