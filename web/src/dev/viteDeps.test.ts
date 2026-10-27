import { readFileSync, readdirSync } from "node:fs";
import { join, resolve } from "node:path";
import { describe, expect, it } from "vitest";

import { baseUIEntries } from "@dev/baseUI";

function sources(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) return entry.name === "generated" ? [] : sources(path);
    return /\.tsx?$/.test(entry.name) ? [path] : [];
  });
}

// The dev server pre-bundles every Base UI component, so using a new one never makes it re-bundle mid-session (which can leave a page with two copies of React).
describe("dev server dependency pre-bundling", () => {
  it("includes every Base UI component the app imports", () => {
    const include = baseUIEntries(process.cwd());

    const imported = new Set(
      sources(resolve(process.cwd(), "src")).flatMap((file) =>
        [...readFileSync(file, "utf8").matchAll(/from "(@base-ui\/react\/[a-z-]+)"/g)].map((m) => m[1] as string),
      ),
    );

    expect(imported.size).toBeGreaterThan(0);
    for (const entry of imported) expect(include).toContain(entry);
  });

  it("leaves out entries that aren't JavaScript", () => {
    const include = baseUIEntries(process.cwd());
    expect(include).not.toContain("@base-ui/react/package.json");
    expect(include.filter((entry) => entry.endsWith(".json"))).toEqual([]);
  });
});
