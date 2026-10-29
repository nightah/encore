import { readFileSync } from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { describe, expect, it } from "vitest";

const root = path.resolve(import.meta.dirname, "../..");

describe("commit scopes", () => {
  // The contributing guide's table explains each scope commitlint allows; a scope added to one has to be added to the other.
  it("are all described in the contributing guide", async () => {
    const config = (await import(pathToFileURL(path.join(root, "web/commitlint.config.mjs")).href)).default;
    const allowed: string[] = config.rules["scope-enum"][2];
    const guide = readFileSync(path.join(root, "docs/src/content/docs/contributing/index.md"), "utf8");
    const documented = [...guide.matchAll(/^\| `([a-z0-9-]+)` \|/gm)].map((m) => m[1]);
    expect(documented).toEqual(allowed);
  });
});
