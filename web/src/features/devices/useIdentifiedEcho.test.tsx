import type { ReactNode } from "react";

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { IDENTIFY_HIGHLIGHT_MS, useIdentifiedEcho } from "@features/devices/useIdentifiedEcho";
import { FakeEventSource } from "@test/eventSource";

const wrapper = ({ children }: { children: ReactNode }) => (
  <QueryClientProvider client={new QueryClient()}>{children}</QueryClientProvider>
);

describe("useIdentifiedEcho", () => {
  afterEach(() => vi.useRealTimers());

  it("highlights the Echo asked “which Echo is this?” for a while", () => {
    vi.useFakeTimers();
    const { result } = renderHook(() => useIdentifiedEcho(), { wrapper });

    act(() => FakeEventSource.emit("request", { device_id: 3, intent: "CurrentPlayItemEnquiry" }));
    expect(result.current).toBeNull();

    act(() => FakeEventSource.emit("request", { device_id: 7, intent: "IdentifyEcho" }));
    expect(result.current).toBe(7);

    act(() => vi.advanceTimersByTime(IDENTIFY_HIGHLIGHT_MS));
    expect(result.current).toBeNull();
  });
});
