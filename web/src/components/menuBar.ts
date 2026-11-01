import { useSyncExternalStore } from "react";

import { NAV_ITEMS, type NavItem } from "@components/nav";

type NavPath = NavItem["to"];

/** The sections in the phone menu bar, in order, saved per browser. The others are under More. */
export const MENU_BAR_STORAGE_KEY = "encore.menuBar";

/** How many sections fit beside More at phone width. */
export const MENU_BAR_MAX = 4;

export const DEFAULT_MENU_BAR: readonly NavPath[] = ["/", "/kodi", "/echo-devices", "/request-log"];

const listeners = new Set<() => void>();
let cache: { raw: string | null; value: readonly NavPath[] } = { raw: null, value: DEFAULT_MENU_BAR };

/** Known sections only, each once, at most MENU_BAR_MAX: a saved list from an older or newer Encore still loads. */
function clean(value: unknown): NavPath[] | null {
  if (!Array.isArray(value)) return null;
  const known = new Set<string>(NAV_ITEMS.map((item) => item.to));
  const paths = value.filter((path): path is NavPath => typeof path === "string" && known.has(path));
  return [...new Set(paths)].slice(0, MENU_BAR_MAX);
}

function read(): readonly NavPath[] {
  let raw: string | null = null;
  try {
    raw = window.localStorage.getItem(MENU_BAR_STORAGE_KEY);
  } catch {
    raw = null;
  }
  if (raw === cache.raw) return cache.value;
  let value: readonly NavPath[] = DEFAULT_MENU_BAR;
  try {
    value = (raw ? clean(JSON.parse(raw)) : null) ?? DEFAULT_MENU_BAR;
  } catch {
    value = DEFAULT_MENU_BAR;
  }
  cache = { raw, value };
  return value;
}

function write(value: readonly NavPath[] | null) {
  try {
    if (value) window.localStorage.setItem(MENU_BAR_STORAGE_KEY, JSON.stringify(value));
    else window.localStorage.removeItem(MENU_BAR_STORAGE_KEY);
  } catch {
    // Without storage the menu still works; the arrangement just lasts until the page reloads.
    cache = { raw: null, value: value ?? DEFAULT_MENU_BAR };
  }
  for (const listener of listeners) listener();
}

export function setMenuBar(paths: readonly NavPath[]) {
  write(clean(paths));
}

export function resetMenuBar() {
  write(null);
}

export function useMenuBar(): readonly NavPath[] {
  return useSyncExternalStore((listener) => {
    listeners.add(listener);
    return () => listeners.delete(listener);
  }, read);
}
