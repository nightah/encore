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

const pluralSuffix = /^(.+)_(zero|one|two|few|many|other)$/;

/**
 * What is wrong with a translation's keys against English's: a key English lacks, or placeholders that differ. i18next picks a plural key by the language's own CLDR categories, so a translation may give a plural English has (with _one and _other) in any category its language uses, such as Arabic's _zero, _two, _few and _many. Those may leave {{count}} out, as a form for one or two often spells the number in words.
 */
function keyProblems(code: string, en: Map<string, string[]>, translated: Map<string, string[]>): string[] {
  const categories = new Intl.PluralRules(code).resolvedOptions().pluralCategories as string[];
  const problems: string[] = [];
  const withoutCount = (ph: string[]) => ph.filter((p) => p !== "count");

  for (const [path, placeholders] of translated) {
    const own = en.get(path);
    if (own) {
      if (placeholders.join() !== own.join())
        problems.push(`${path} has placeholders [${placeholders}], expected [${own}]`);
      continue;
    }

    const [, base = "", category = ""] = pluralSuffix.exec(path) ?? [];
    const other = en.get(`${base}_other`) ?? en.get(`${base}_one`);
    if (!other || !categories.includes(category)) {
      problems.push(`${path} isn't in en`);
    } else if (withoutCount(placeholders).join() !== withoutCount(other).join()) {
      problems.push(`${path} has placeholders [${placeholders}], expected [${other}]`);
    }
  }

  return problems;
}

describe("locales", () => {
  it("has a file for every language and namespace", () => {
    for (const { code } of LANGUAGES) {
      for (const ns of NAMESPACES) expect(files[`./locales/${code}/${ns}.json`], `${code}/${ns}.json`).toBeDefined();
    }
  });

  // Crowdin writes translations under English's keys, plural keys in the language's own categories, so another key would be lost at the next sync; a missing one falls back to English.
  for (const { code } of LANGUAGES.filter((l) => l.code !== "en")) {
    it(`${code} uses only English keys, with the same placeholders`, () => {
      for (const ns of NAMESPACES) {
        const en = flatten(files[`./locales/en/${ns}.json`]);
        expect(keyProblems(code, en, flatten(files[`./locales/${code}/${ns}.json`])), `${code}/${ns}.json`).toEqual([]);
      }
    });
  }
});

describe("keyProblems", () => {
  const en = new Map([
    ["title", []],
    ["items_one", ["count"]],
    ["items_other", ["count"]],
    ["named_other", ["count", "name"]],
  ]);

  it.each([
    [
      "ar",
      [
        ["items_zero", []],
        ["items_two", []],
        ["items_few", ["count"]],
        ["items_many", ["count"]],
      ],
    ],
    ["ja", [["items_other", ["count"]]]],
    [
      "pt",
      [
        ["items_one", ["count"]],
        ["items_many", ["count"]],
        ["named_other", ["count", "name"]],
      ],
    ],
    ["ar", [["named_two", ["name"]]]],
  ] as const)("accepts %s plurals in its own categories", (code, keys) => {
    expect(keyProblems(code, en, new Map(keys.map(([k, ph]) => [k, [...ph]])))).toEqual([]);
  });

  it.each([
    ["de", "items_few", ["count"], "items_few isn't in en"],
    ["ja", "items_two", ["count"], "items_two isn't in en"],
    ["ar", "title_few", [], "title_few isn't in en"],
    ["ar", "other_few", [], "other_few isn't in en"],
    ["fr", "subtitle", [], "subtitle isn't in en"],
    ["ar", "named_few", ["count"], "named_few has placeholders [count], expected [count,name]"],
    ["de", "items_one", [], "items_one has placeholders [], expected [count]"],
  ] as const)("rejects %s %s", (code, key, placeholders, problem) => {
    expect(keyProblems(code, en, new Map([[key, [...placeholders]]]))).toEqual([problem]);
  });
});
