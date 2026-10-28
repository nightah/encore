import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { installStaleModuleReload, isStaleModuleError, reloadForStaleModule } from "@lib/staleReload";

describe("reloadForStaleModule", () => {
  let reload: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    sessionStorage.clear();
    reload = vi.fn();
    vi.stubGlobal("location", { ...window.location, reload });
  });

  afterEach(() => vi.unstubAllGlobals());

  it("reloads once, then lets the error show instead of looping", () => {
    expect(reloadForStaleModule()).toBe(true);
    expect(reload).toHaveBeenCalledTimes(1);

    expect(reloadForStaleModule()).toBe(false);
    expect(reload).toHaveBeenCalledTimes(1);
  });

  it("reloads again once the last reload is 30 seconds old", () => {
    vi.useFakeTimers({ now: new Date("2026-10-28T09:00:00+11:00"), toFake: ["Date"] });
    try {
      expect(reloadForStaleModule()).toBe(true);
      vi.setSystemTime(Date.now() + 29_999);
      expect(reloadForStaleModule()).toBe(false);
      vi.setSystemTime(Date.now() + 1);
      expect(reloadForStaleModule()).toBe(true);
      expect(reload).toHaveBeenCalledTimes(2);
    } finally {
      vi.useRealTimers();
    }
  });

  it("never reloads without session storage, since nothing would stop a loop", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new DOMException("blocked", "SecurityError");
    });
    try {
      expect(reloadForStaleModule()).toBe(false);
      expect(reload).not.toHaveBeenCalled();
    } finally {
      vi.restoreAllMocks();
    }
  });
});

describe("installStaleModuleReload", () => {
  let reload: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    sessionStorage.clear();
    reload = vi.fn();
    vi.stubGlobal("location", { ...window.location, reload });
  });

  afterEach(() => vi.unstubAllGlobals());

  it("reloads on Vite's preload error and stops Vite throwing, but only once", () => {
    installStaleModuleReload();

    const first = new Event("vite:preloadError", { cancelable: true });
    window.dispatchEvent(first);
    expect(reload).toHaveBeenCalledTimes(1);
    expect(first.defaultPrevented).toBe(true);

    const second = new Event("vite:preloadError", { cancelable: true });
    window.dispatchEvent(second);
    expect(reload).toHaveBeenCalledTimes(1);
    expect(second.defaultPrevented).toBe(false);
  });
});

describe("isStaleModuleError", () => {
  it("recognises each browser's failed chunk load", () => {
    expect(
      isStaleModuleError(new TypeError("error loading dynamically imported module: https://x/deps/a.js?v=1")),
    ).toBe(true); // Firefox
    expect(isStaleModuleError(new TypeError("Failed to fetch dynamically imported module: https://x/a.js"))).toBe(true); // Chrome
    expect(isStaleModuleError(new TypeError("Importing a module script failed."))).toBe(true); // Safari
    expect(isStaleModuleError(new TypeError("chip is undefined"))).toBe(false);
    expect(isStaleModuleError("error loading dynamically imported module")).toBe(true);
    expect(isStaleModuleError(undefined)).toBe(false);
  });
});
