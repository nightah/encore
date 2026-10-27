import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { isStaleModuleError, reloadForStaleModule } from "@lib/staleReload";

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
});

describe("isStaleModuleError", () => {
  it("recognises each browser's failed chunk load", () => {
    expect(
      isStaleModuleError(new TypeError("error loading dynamically imported module: https://x/deps/a.js?v=1")),
    ).toBe(true); // Firefox
    expect(isStaleModuleError(new TypeError("Failed to fetch dynamically imported module: https://x/a.js"))).toBe(true); // Chrome
    expect(isStaleModuleError(new TypeError("Importing a module script failed."))).toBe(true); // Safari
    expect(isStaleModuleError(new TypeError("chip is undefined"))).toBe(false);
  });
});
