import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ApiError } from "@api/client";
import { Callout, CopyButton, EmptyState, ErrorState, LoadingBlock } from "@components/ui/States";

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe("LoadingBlock", () => {
  it("announces that something is loading", () => {
    const { rerender } = render(<LoadingBlock />);
    expect(screen.getByRole("status")).toHaveTextContent("Loading");
    rerender(<LoadingBlock label="Loading requests…" />);
    expect(screen.getByRole("status")).toHaveTextContent("Loading requests…");
  });
});

describe("EmptyState", () => {
  it("shows its title, and its explanation and action only when given", () => {
    const { rerender } = render(<EmptyState title="No requests yet" />);
    expect(screen.getByText("No requests yet")).toBeInTheDocument();
    expect(screen.queryByRole("button")).not.toBeInTheDocument();

    rerender(
      <EmptyState title="No requests yet" action={<button type="button">Add one</button>}>
        Say something to Alexa.
      </EmptyState>,
    );
    expect(screen.getByText("Say something to Alexa.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Add one" })).toBeInTheDocument();
  });
});

describe("ErrorState", () => {
  it("shows the error's message and offers a retry when it can", async () => {
    const user = userEvent.setup();
    const onRetry = vi.fn();
    render(<ErrorState error={new ApiError("x", 503, { detail: "Kodi is busy." })} onRetry={onRetry} />);
    expect(screen.getByRole("alert")).toHaveTextContent("Kodi is busy.");
    await user.click(screen.getByRole("button", { name: "Try again" }));
    expect(onRetry).toHaveBeenCalledOnce();
  });

  it("offers no retry without a way to retry", () => {
    render(<ErrorState error="boom" />);
    expect(screen.getByRole("alert")).toHaveTextContent("Something went wrong.");
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });
});

describe("Callout", () => {
  it("passes attributes through to its box", () => {
    render(
      <Callout tone="bad" role="note" aria-label="Warning">
        Check the address.
      </Callout>,
    );
    expect(screen.getByRole("note", { name: "Warning" })).toHaveTextContent("Check the address.");
  });
});

describe("CopyButton", () => {
  it("copies its text, reads it out in its name and confirms for two seconds", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<CopyButton text="https://encore.example.net/alexa" label="Endpoint" />);

    await user.click(screen.getByRole("button", { name: "Endpoint: https://encore.example.net/alexa" }));

    expect(await navigator.clipboard.readText()).toBe("https://encore.example.net/alexa");
    expect(screen.getByRole("button", { name: "Copied" })).toBeInTheDocument();

    act(() => vi.advanceTimersByTime(2000));
    expect(screen.getByRole("button", { name: "Endpoint: https://encore.example.net/alexa" })).toBeInTheDocument();
  });

  it("names itself without the text when asked to, defaulting to Copy", () => {
    render(<CopyButton text={"a very long\ncode block"} speakText={false} />);
    expect(screen.getByRole("button", { name: "Copy" })).toBeInTheDocument();
  });

  it("doesn't claim to have copied when the clipboard refuses", async () => {
    const user = userEvent.setup();
    vi.spyOn(navigator.clipboard, "writeText").mockRejectedValue(new DOMException("denied", "NotAllowedError"));
    render(<CopyButton text="secret" label="Token" speakText={false} />);

    await user.click(screen.getByRole("button", { name: "Token" }));

    expect(screen.getByRole("button", { name: "Token" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Copied" })).not.toBeInTheDocument();
  });
});
