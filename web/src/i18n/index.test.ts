import { afterEach, describe, expect, it, vi } from "vitest";

import i18n, { LANGUAGES, NAMESPACES, browserLanguage, setLanguage, storedLanguage } from "@i18n/index";

const STORAGE_KEY = "encore.language";

function browserPrefers(languages: readonly string[] | undefined, language = "en-US") {
  vi.spyOn(navigator, "languages", "get").mockReturnValue(languages as readonly string[]);
  vi.spyOn(navigator, "language", "get").mockReturnValue(language);
}

afterEach(() => vi.restoreAllMocks());

describe("browserLanguage", () => {
  it.each([
    [["de-AT", "en"], "de"],
    [["pt-BR", "fr-CA", "de"], "fr"],
    [["IT"], "it"],
    [["es-419"], "es"],
    [["ja", "zh-Hant"], "en"],
    [[], "en"],
  ])("picks the first supported language of %j", (languages, expected) => {
    browserPrefers(languages);
    expect(browserLanguage()).toBe(expected);
  });

  it("falls back to navigator.language without a language list", () => {
    browserPrefers(undefined, "fr-FR");
    expect(browserLanguage()).toBe("fr");
  });
});

describe("storedLanguage", () => {
  it("returns a supported language saved in this browser", () => {
    window.localStorage.setItem(STORAGE_KEY, "it");
    expect(storedLanguage()).toBe("it");
  });

  it.each(["", "klingon", "DE", "de-AT"])("ignores %j", (value) => {
    window.localStorage.setItem(STORAGE_KEY, value);
    expect(storedLanguage()).toBeUndefined();
  });

  it("is undefined when storage is blocked", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new DOMException("blocked", "SecurityError");
    });
    expect(storedLanguage()).toBeUndefined();
  });
});

describe("setLanguage", () => {
  it("saves the choice, switches to it and labels the document", async () => {
    await setLanguage("fr");
    expect(window.localStorage.getItem(STORAGE_KEY)).toBe("fr");
    expect(i18n.language).toBe("fr");
    expect(document.documentElement.lang).toBe("fr");
    expect(i18n.t("actions.tryAgain")).toBe(i18n.getResource("fr", "common", "actions.tryAgain"));
    expect(i18n.t("actions.tryAgain")).not.toBe("Try again");
  });

  it("sets the document's direction with its language", async () => {
    await i18n.changeLanguage("ar");
    expect(document.documentElement.lang).toBe("ar");
    expect(document.documentElement.dir).toBe("rtl");
    await setLanguage("fr");
    expect(document.documentElement.dir).toBe("ltr");
  });

  it("forgets the choice and follows the browser for undefined", async () => {
    window.localStorage.setItem(STORAGE_KEY, "fr");
    browserPrefers(["de-DE"]);
    await setLanguage(undefined);
    expect(window.localStorage.getItem(STORAGE_KEY)).toBeNull();
    expect(i18n.language).toBe("de");
  });

  it("still switches when storage is blocked", async () => {
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new DOMException("blocked", "SecurityError");
    });
    await setLanguage("es");
    expect(i18n.language).toBe("es");
  });
});

describe("resources", () => {
  it("bundles every namespace for every language", () => {
    for (const { code } of LANGUAGES) {
      for (const ns of NAMESPACES) {
        expect(i18n.hasResourceBundle(code, ns), `${code}/${ns}`).toBe(true);
      }
    }
  });

  it("falls back to English for a key a translation lacks", async () => {
    i18n.addResource("en", "common", "test.onlyEnglish", "English only");
    await i18n.changeLanguage("de");
    expect(i18n.t("test.onlyEnglish" as "actions.tryAgain")).toBe("English only");
  });
});
