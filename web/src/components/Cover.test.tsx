import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { Cover } from "@components/Cover";

describe("Cover", () => {
  it("shows Kodi's artwork through encore", () => {
    const { container } = render(
      <Cover artUrl="/api/v1/kodi/instances/1/art?ref=image%3A%2F%2Fx%2F" title="A Bug's Life" />,
    );
    const img = container.querySelector("img");
    expect(img?.getAttribute("src")).toMatch(/\/api\/v1\/kodi\/instances\/1\/art\?ref=image%3A%2F%2Fx%2F$/);
  });

  it("falls back to the title's initials without artwork or when it fails to load", () => {
    const { container, rerender } = render(<Cover artUrl="" title="A Bug's Life" />);
    expect(screen.getByText("ABU")).toBeInTheDocument();

    rerender(<Cover artUrl="/api/v1/kodi/instances/1/art?ref=broken" title="A Bug's Life" />);
    const img = container.querySelector("img");
    expect(img).not.toBeNull();
    fireEvent.error(img as HTMLImageElement);
    expect(screen.getByText("ABU")).toBeInTheDocument();
  });
});
