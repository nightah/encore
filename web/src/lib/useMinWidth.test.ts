import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { useMinWidth } from "@lib/useMinWidth";

const boxes: HTMLElement[] = [];

function box(width: number) {
  const element = document.createElement("div");
  element.style.width = `${width}px`;
  document.body.append(element);
  boxes.push(element);
  return element;
}

afterEach(() => {
  for (const element of boxes.splice(0)) element.remove();
});

describe("useMinWidth", () => {
  it("counts as wide enough before anything is attached", () => {
    const { result } = renderHook(() => useMinWidth<HTMLDivElement>(500));
    act(() => {
      result.current[0](null);
    });
    expect(result.current[1]).toBe(true);
  });

  it("measures the element and follows it as it resizes", async () => {
    const element = box(300);
    const { result } = renderHook(() => useMinWidth<HTMLDivElement>(500));

    act(() => {
      result.current[0](element as HTMLDivElement);
    });
    expect(result.current[1]).toBe(false);

    element.style.width = "600px";
    await waitFor(() => expect(result.current[1]).toBe(true));
  });

  it("stops following an element once detached", async () => {
    const element = box(600);
    const { result } = renderHook(() => useMinWidth<HTMLDivElement>(500));
    let cleanup: (() => void) | undefined;
    act(() => {
      cleanup = result.current[0](element as HTMLDivElement) ?? undefined;
    });
    expect(result.current[1]).toBe(true);

    act(() => cleanup?.());
    element.style.width = "300px";
    // Give a ResizeObserver callback the chance to run, had it still been observing.
    await new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve)));
    expect(result.current[1]).toBe(true);
  });
});
