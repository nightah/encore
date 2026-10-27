import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

// The UI writes plain hyphens: em and en dashes read oddly and show up as unset markers that are hard to tell apart.
describe("web UI text", () => {
  it("uses no em or en dashes", () => {
    const root = join(__dirname, "..");
    // Built from char codes so this file passes its own check.
    const dashes = new RegExp(`[${String.fromCharCode(0x2013, 0x2014)}]`);
    const offenders = readdirSync(root, { encoding: "utf8", recursive: true })
      .filter((f) => /\.(ts|tsx|css|html)$/.test(f) && !f.includes("generated"))
      .filter((f) => dashes.test(readFileSync(join(root, f), "utf8")));
    expect(offenders).toEqual([]);
  });
});
