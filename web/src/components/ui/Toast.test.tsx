import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { ToastProvider, notifySuccess } from "@components/ui/Toast";
import { setLanguage } from "@i18n/index";

/** Drags the toast sideways by dx pixels and lets go. */
function swipe(target: Element, dx: number) {
  const at = (x: number) => ({ button: 0, clientX: x, clientY: 100, pointerId: 1, pointerType: "mouse" });
  fireEvent.pointerDown(target, at(300));
  fireEvent.pointerMove(target, at(299));
  fireEvent.pointerMove(target, at(299 + dx));
  fireEvent.pointerUp(target, at(299 + dx));
}

describe("ToastProvider", () => {
  it.each([
    ["en", 120, -120],
    ["ar", -120, 120],
  ] as const)("dismisses a toast swiped toward the edge it sits on in %s", async (lng, toEdge, toCentre) => {
    await setLanguage(lng);
    render(<ToastProvider>{null}</ToastProvider>);
    act(() => notifySuccess("Saved."));
    const title = await screen.findByText("Saved.");

    swipe(title, toCentre);
    expect(title.closest("[data-swipe-direction]")).toBeNull();
    expect(screen.getByText("Saved.")).toBeInTheDocument();

    swipe(title, toEdge);
    await waitFor(() => expect(screen.queryByText("Saved.")).not.toBeInTheDocument());
  });
});
