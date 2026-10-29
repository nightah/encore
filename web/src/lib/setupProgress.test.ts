import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import {
  SETUP_STORAGE_KEY,
  type SetupProgress,
  clearSetupProgress,
  getSetupProgress,
  saveSetupProgress,
  useSetupProgress,
} from "@lib/setupProgress";

const progress: SetupProgress = { done: [1, 2], skipped: [3], step: 4 };

describe("setup progress", () => {
  afterEach(() => vi.restoreAllMocks());

  it("saves to this browser and reads back", () => {
    saveSetupProgress(progress);
    expect(JSON.parse(window.localStorage.getItem(SETUP_STORAGE_KEY) ?? "")).toEqual(progress);
    expect(getSetupProgress()).toEqual(progress);
  });

  it("is gone once cleared", () => {
    saveSetupProgress(progress);
    clearSetupProgress();
    expect(window.localStorage.getItem(SETUP_STORAGE_KEY)).toBeNull();
    expect(getSetupProgress()).toBeNull();
  });

  it("returns the same object until storage changes, as useSyncExternalStore requires", () => {
    saveSetupProgress(progress);
    const first = getSetupProgress();
    expect(getSetupProgress()).toBe(first);

    saveSetupProgress({ ...progress, step: 5 });
    expect(getSetupProgress()).not.toBe(first);
    expect(getSetupProgress()?.step).toBe(5);
  });

  it.each([
    ["broken JSON", "{step: 2"],
    ["a value without a step", JSON.stringify({ done: [1] })],
    ["a step that isn't a number", JSON.stringify({ done: [], skipped: [], step: "2" })],
    ["null", "null"],
  ])("ignores %s in storage", (_name, raw) => {
    window.localStorage.setItem(SETUP_STORAGE_KEY, raw);
    expect(getSetupProgress()).toBeNull();
  });

  it("works without storage, it just can't resume", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new DOMException("blocked", "SecurityError");
    });
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new DOMException("blocked", "SecurityError");
    });
    expect(() => saveSetupProgress(progress)).not.toThrow();
    expect(getSetupProgress()).toBeNull();
  });

  it("re-renders subscribers when progress is saved or cleared", () => {
    const { result, unmount } = renderHook(() => useSetupProgress());
    expect(result.current).toBeNull();

    act(() => saveSetupProgress(progress));
    expect(result.current).toEqual(progress);

    act(() => clearSetupProgress());
    expect(result.current).toBeNull();

    unmount();
  });
});
