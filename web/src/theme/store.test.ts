import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

import {
  THEME_STORAGE_KEY,
  endThemePreview,
  initTheme,
  previewTheme,
  setThemePreference,
  useTheme,
} from "@theme/store";

/** A stand-in for the OS colour scheme, which jsdom doesn't have. */
const os = {
  dark: false,
  listeners: new Set<() => void>(),
  set(dark: boolean) {
    os.dark = dark;
    for (const listener of os.listeners) listener();
  },
};

beforeAll(() => {
  window.matchMedia = ((query: string) => ({
    addEventListener: (_type: string, listener: () => void) => os.listeners.add(listener),
    get matches() {
      return query === "(prefers-color-scheme: dark)" && os.dark;
    },
    media: query,
    removeEventListener: (_type: string, listener: () => void) => os.listeners.delete(listener),
  })) as unknown as typeof window.matchMedia;
});

beforeEach(() => {
  os.dark = false;
  window.localStorage.clear();
  initTheme();
});

afterEach(() => vi.restoreAllMocks());

const theme = () => document.documentElement.dataset.theme;

describe("theme store", () => {
  it("follows the OS by default", () => {
    os.dark = true;
    initTheme();
    expect(theme()).toBe("dark");

    const { result } = renderHook(() => useTheme());
    expect(result.current).toMatchObject({ preference: "system", resolved: "dark", saved: "system" });

    act(() => os.set(false));
    expect(theme()).toBe("light");
    expect(result.current.resolved).toBe("light");
  });

  it("ignores the OS once light or dark is chosen", () => {
    const { result } = renderHook(() => useTheme());
    act(() => result.current.setPreference("light"));
    act(() => os.set(true));
    expect(theme()).toBe("light");
    expect(result.current).toMatchObject({ preference: "light", resolved: "light" });
  });

  it("saves light and dark, and forgets the choice for system", () => {
    setThemePreference("dark");
    expect(window.localStorage.getItem(THEME_STORAGE_KEY)).toBe("dark");
    setThemePreference("system");
    expect(window.localStorage.getItem(THEME_STORAGE_KEY)).toBeNull();
    expect(theme()).toBe("light");
  });

  it("previews a theme without saving it, then goes back", () => {
    setThemePreference("light");
    const { result } = renderHook(() => useTheme());

    act(() => previewTheme("dark"));
    expect(theme()).toBe("dark");
    expect(result.current).toMatchObject({ preference: "dark", resolved: "dark", saved: "light" });
    expect(window.localStorage.getItem(THEME_STORAGE_KEY)).toBe("light");

    act(() => endThemePreview());
    expect(theme()).toBe("light");
    expect(result.current).toMatchObject({ preference: "light", saved: "light" });
  });

  it("keeps working, unsaved, when storage is blocked", () => {
    const blocked = () => {
      throw new DOMException("blocked", "SecurityError");
    };
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(blocked);
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(blocked);
    vi.spyOn(Storage.prototype, "removeItem").mockImplementation(blocked);

    initTheme();
    expect(theme()).toBe("light");
    expect(() => setThemePreference("dark")).not.toThrow();
    expect(theme()).toBe("dark");
    expect(() => setThemePreference("system")).not.toThrow();
  });

  it("treats a failing media query as a light OS", () => {
    os.dark = true;
    setThemePreference("dark");
    vi.spyOn(window, "matchMedia").mockImplementation(() => {
      throw new Error("unsupported");
    });
    setThemePreference("system");
    expect(theme()).toBe("light");
  });
});
