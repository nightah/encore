import i18n from "i18next";
import { initReactI18next } from "react-i18next";

/** The web interface's languages, as they name themselves. English is the source; the rest are translated on Crowdin (.crowdin.yml). */
export const LANGUAGES = [
  { code: "en", name: "English" },
  { code: "de", name: "Deutsch" },
  { code: "es", name: "Español" },
  { code: "fr", name: "Français" },
  { code: "it", name: "Italiano" },
] as const;

export type Language = (typeof LANGUAGES)[number]["code"];

/** Each language's name in itself, by code. */
export const LANGUAGE_NAMES = Object.fromEntries(LANGUAGES.map((l) => [l.code, l.name])) as Record<Language, string>;

/** One namespace per area of the interface, each a JSON file per language in locales/. common holds the shell, shared components and formatting. */
export const NAMESPACES = [
  "common",
  "auth",
  "overview",
  "kodi",
  "devices",
  "skill",
  "requests",
  "library",
  "settings",
  "setup",
] as const;

const LANGUAGE_STORAGE_KEY = "encore.language";

const files = import.meta.glob<Record<string, unknown>>("./locales/*/*.json", { eager: true, import: "default" });

const resources: Record<string, Record<string, Record<string, unknown>>> = {};
for (const [path, strings] of Object.entries(files)) {
  // ./locales/<language>/<namespace>.json, as the glob above only matches those.
  const [, , lang = "", file = ""] = path.split("/");
  const ns = file.replace(/\.json$/, "");
  resources[lang] ??= {};
  resources[lang][ns] = strings;
}

function isLanguage(code: string | null | undefined): code is Language {
  return LANGUAGES.some((l) => l.code === code);
}

/** The language chosen in Settings › General, if any. */
export function storedLanguage(): Language | undefined {
  try {
    const stored = window.localStorage.getItem(LANGUAGE_STORAGE_KEY);
    return isLanguage(stored) ? stored : undefined;
  } catch {
    return undefined;
  }
}

/** The browser's first supported language, or English. */
export function browserLanguage(): Language {
  for (const tag of navigator.languages ?? [navigator.language]) {
    const code = tag.toLowerCase().split("-")[0];
    if (isLanguage(code)) return code;
  }
  return "en";
}

/** Saves the choice in this browser (undefined follows the browser) and switches to it. */
export async function setLanguage(code: Language | undefined): Promise<void> {
  try {
    if (code) window.localStorage.setItem(LANGUAGE_STORAGE_KEY, code);
    else window.localStorage.removeItem(LANGUAGE_STORAGE_KEY);
  } catch {
    // Storage may be unavailable; the choice then lasts until the page reloads.
  }
  await i18n.changeLanguage(code ?? browserLanguage());
}

// dir lays the page out right to left for Arabic; Tailwind's logical classes (ms-, pe-, text-start) and rtl: variants follow it.
i18n.on("languageChanged", (lng) => {
  document.documentElement.lang = lng;
  document.documentElement.dir = i18n.dir(lng);
});

void i18n.use(initReactI18next).init({
  defaultNS: "common",
  fallbackLng: "en",
  // Resources are bundled, so initialise synchronously: the first render is already translated.
  initAsync: false,
  interpolation: { escapeValue: false },
  lng: storedLanguage() ?? browserLanguage(),
  ns: NAMESPACES,
  resources,
  returnNull: false,
});

export default i18n;
