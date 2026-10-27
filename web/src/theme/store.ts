import { useSyncExternalStore } from "react";

export type ThemePreference = "system" | "light" | "dark";
export type ResolvedTheme = "light" | "dark";

export const THEME_STORAGE_KEY = "encore.theme";

const DARK_QUERY = "(prefers-color-scheme: dark)";

function isPreference(value: unknown): value is ThemePreference {
  return value === "system" || value === "light" || value === "dark";
}

function readStored(): ThemePreference {
  try {
    const value = window.localStorage.getItem(THEME_STORAGE_KEY);
    return isPreference(value) ? value : "system";
  } catch {
    return "system";
  }
}

function writeStored(value: ThemePreference): void {
  try {
    if (value === "system") {
      window.localStorage.removeItem(THEME_STORAGE_KEY);
    } else {
      window.localStorage.setItem(THEME_STORAGE_KEY, value);
    }
  } catch {
    // Storage may be unavailable (private mode, blocked site data); the choice just won't persist.
  }
}

function systemPrefersDark(): boolean {
  try {
    return window.matchMedia?.(DARK_QUERY).matches ?? false;
  } catch {
    return false;
  }
}

export interface ThemeState {
  preference: ThemePreference;
  resolved: ResolvedTheme;
  /** The preference saved in this browser; it differs from preference while the settings form previews another. */
  saved: ThemePreference;
}

function resolve(preference: ThemePreference): ResolvedTheme {
  if (preference === "system") return systemPrefersDark() ? "dark" : "light";
  return preference;
}

let state: ThemeState = { preference: "system", resolved: "light", saved: "system" };
const listeners = new Set<() => void>();
let mediaQuery: MediaQueryList | undefined;

function apply(): void {
  document.documentElement.dataset.theme = state.resolved;
}

function emit(next: ThemeState): void {
  if (next.preference === state.preference && next.resolved === state.resolved && next.saved === state.saved) return;
  state = next;
  apply();
  for (const listener of listeners) listener();
}

function onSystemChange(): void {
  if (state.preference === "system") emit({ ...state, resolved: resolve("system") });
}

/** Reads the persisted preference, applies data-theme to <html> and follows OS changes. */
export function initTheme(): void {
  const preference = readStored();
  state = { preference, resolved: resolve(preference), saved: preference };
  apply();
  if (!mediaQuery && typeof window.matchMedia === "function") {
    mediaQuery = window.matchMedia(DARK_QUERY);
    mediaQuery.addEventListener?.("change", onSystemChange);
  }
}

export function setThemePreference(preference: ThemePreference): void {
  writeStored(preference);
  emit({ preference, resolved: resolve(preference), saved: preference });
}

/** Shows preference without saving it, for the settings form. */
export function previewTheme(preference: ThemePreference): void {
  emit({ preference, resolved: resolve(preference), saved: state.saved });
}

/** Goes back to the saved preference after a preview. */
export function endThemePreview(): void {
  previewTheme(state.saved);
}

function getThemeState(): ThemeState {
  return state;
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function useTheme(): ThemeState & { setPreference: typeof setThemePreference } {
  const snapshot = useSyncExternalStore(subscribe, getThemeState, getThemeState);
  return { ...snapshot, setPreference: setThemePreference };
}
