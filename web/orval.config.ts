import { defineConfig } from "orval";

/**
 * Generates the API client from the server's OpenAPI 3.1 spec (`internal/api/openapi.json`, written by Huma): fetch functions in `src/api/generated/endpoints/` and models in `src/api/generated/model/`. Run `pnpm generate:api` after the spec changes and commit the result; see "API layer" in README.md.
 */
export default defineConfig({
  encore: {
    hooks: { afterAllFilesWrite: "biome check --write src/api/generated" },
    input: { target: "../internal/api/openapi.json" },
    output: {
      clean: true,
      client: "fetch",
      mode: "tags-split",
      override: {
        fetch: { includeHttpResponseReturnType: false },
        // Base path, CSRF header and problem+json errors; resolves to the response body.
        mutator: { name: "orvalFetch", path: "src/api/orvalFetch.ts" },
      },
      schemas: "src/api/generated/model",
      target: "src/api/generated/endpoints/index.ts",
    },
  },
});
