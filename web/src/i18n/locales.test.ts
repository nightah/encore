import { describe, expect, it } from "vitest";

import { LANGUAGES, NAMESPACES } from "@i18n/index";

const files = import.meta.glob<Record<string, unknown>>("./locales/*/*.json", { eager: true, import: "default" });

/** Every key path to a string, with its {{placeholders}}. */
function flatten(value: unknown, prefix = ""): Map<string, string[]> {
  const out = new Map<string, string[]>();
  if (typeof value === "string") {
    out.set(prefix, [...value.matchAll(/\{\{\s*(\w+)/g)].map((m) => m[1] ?? "").sort());
  } else if (value && typeof value === "object") {
    for (const [k, v] of Object.entries(value)) {
      for (const [path, ph] of flatten(v, prefix ? `${prefix}.${k}` : k)) out.set(path, ph);
    }
  }
  return out;
}

describe("locales", () => {
  it("has a file for every language and namespace", () => {
    for (const { code } of LANGUAGES) {
      for (const ns of NAMESPACES) expect(files[`./locales/${code}/${ns}.json`], `${code}/${ns}.json`).toBeDefined();
    }
  });

  // Crowdin writes translations under English's keys, so another key would be lost at the next sync; a missing one falls back to English.
  for (const { code } of LANGUAGES.filter((l) => l.code !== "en")) {
    it(`${code} uses only English keys, with the same placeholders`, () => {
      for (const ns of NAMESPACES) {
        const en = flatten(files[`./locales/en/${ns}.json`]);
        for (const [path, placeholders] of flatten(files[`./locales/${code}/${ns}.json`])) {
          expect(en.has(path), `${code}/${ns}.json: ${path} isn't in en`).toBe(true);
          expect(placeholders, `${code}/${ns}.json: ${path}`).toEqual(en.get(path));
        }
      }
    });
  }
});
