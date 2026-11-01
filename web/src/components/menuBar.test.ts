import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { DEFAULT_MENU_BAR, MENU_BAR_STORAGE_KEY, resetMenuBar, setMenuBar, useMenuBar } from "@components/menuBar";

afterEach(() => vi.restoreAllMocks());

describe("menu bar store", () => {
  it("starts with Overview, Kodi, Echo devices and the request log", () => {
    const { result } = renderHook(() => useMenuBar());
    expect(result.current).toEqual(["/", "/kodi", "/echo-devices", "/request-log"]);
    expect(result.current).toBe(DEFAULT_MENU_BAR);
  });

  it("saves an arrangement in this browser and tells every reader", () => {
    const { result } = renderHook(() => useMenuBar());

    act(() => setMenuBar(["/request-log", "/"]));

    expect(result.current).toEqual(["/request-log", "/"]);
    expect(JSON.parse(window.localStorage.getItem(MENU_BAR_STORAGE_KEY) ?? "null")).toEqual(["/request-log", "/"]);
  });

  it("keeps only known sections, each once, and at most four", () => {
    window.localStorage.setItem(
      MENU_BAR_STORAGE_KEY,
      JSON.stringify(["/library", "/gone", "/library", 7, "/", "/kodi", "/settings", "/alexa-skill"]),
    );
    const { result } = renderHook(() => useMenuBar());
    expect(result.current).toEqual(["/library", "/", "/kodi", "/settings"]);
  });

  it("falls back to the default for a saved value that isn't a list", () => {
    window.localStorage.setItem(MENU_BAR_STORAGE_KEY, "{not json");
    expect(renderHook(() => useMenuBar()).result.current).toBe(DEFAULT_MENU_BAR);

    window.localStorage.setItem(MENU_BAR_STORAGE_KEY, JSON.stringify({ bar: ["/"] }));
    expect(renderHook(() => useMenuBar()).result.current).toBe(DEFAULT_MENU_BAR);
  });

  it("allows an empty bar, leaving only More", () => {
    const { result } = renderHook(() => useMenuBar());
    act(() => setMenuBar([]));
    expect(result.current).toEqual([]);
  });

  it("still rearranges, until a reload, when site data is blocked", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new DOMException("blocked", "SecurityError");
    });
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new DOMException("blocked", "SecurityError");
    });
    vi.spyOn(Storage.prototype, "removeItem").mockImplementation(() => {
      throw new DOMException("blocked", "SecurityError");
    });
    const { result } = renderHook(() => useMenuBar());

    act(() => setMenuBar(["/library"]));
    expect(result.current).toEqual(["/library"]);

    act(() => resetMenuBar());
    expect(result.current).toEqual(DEFAULT_MENU_BAR);
  });

  it("forgets the arrangement on reset", () => {
    const { result } = renderHook(() => useMenuBar());
    act(() => setMenuBar(["/settings"]));

    act(() => resetMenuBar());

    expect(result.current).toBe(DEFAULT_MENU_BAR);
    expect(window.localStorage.getItem(MENU_BAR_STORAGE_KEY)).toBeNull();
  });
});
