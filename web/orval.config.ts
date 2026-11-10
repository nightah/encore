import { readFile, readdir, writeFile } from "node:fs/promises";
import { dirname, join, posix, relative, resolve, sep } from "node:path";
import { defineConfig } from "orval";

const API_DIR = resolve(import.meta.dirname, "src/api");
const GENERATED_DIR = join(API_DIR, "generated");
const RELATIVE_SPECIFIER = /(\bfrom\s*)(["'])(\.{1,2}\/[^"']*)\2/g;

/**
 * Rewrites the relative specifiers orval still writes (between model files, and in the barrels) to `@api/…`, so generated code follows the same alias rule as the rest of `src/`. Orval has no option for these: `schemas.importPath` and an aliased mutator path only cover the endpoint files.
 */
async function aliasRelativeImports() {
  const files = (await readdir(GENERATED_DIR, { recursive: true })).filter((file) => file.endsWith(".ts"));
  await Promise.all(
    files.map(async (file) => {
      const path = join(GENERATED_DIR, file);
      const source = await readFile(path, "utf8");
      const aliased = source.replace(RELATIVE_SPECIFIER, (_, keyword, quote, specifier) => {
        const target = relative(API_DIR, resolve(dirname(path), specifier))
          .split(sep)
          .join(posix.sep);
        return `${keyword}${quote}@api/${target}${quote}`;
      });
      if (aliased !== source) await writeFile(path, aliased);
    }),
  );
}

/**
 * Generates the API client from the server's OpenAPI 3.1 spec (`internal/api/openapi.json`, written by Huma): fetch functions in `src/api/generated/endpoints/` and models in `src/api/generated/model/`. Run `pnpm generate:api` after the spec changes and commit the result; see "API layer" in README.md.
 */
export default defineConfig({
  encore: {
    hooks: { afterAllFilesWrite: [aliasRelativeImports, "biome check --write src/api/generated"] },
    input: { target: "../internal/api/openapi.json" },
    output: {
      clean: true,
      client: "fetch",
      mode: "tags-split",
      override: {
        fetch: { includeHttpResponseReturnType: false },
        // Base path, CSRF header and problem+json errors; resolves to the response body. The alias is written into the generated imports as is.
        mutator: { name: "orvalFetch", path: "@api/orvalFetch.ts" },
      },
      schemas: { importPath: "@api/generated/model", path: "src/api/generated/model" },
      target: "src/api/generated/endpoints/index.ts",
    },
  },
});
