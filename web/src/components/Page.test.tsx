import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { Page } from "@components/Page";

function frame(width?: "wide" | "text") {
  render(<Page title="Example" width={width} />);
  return screen.getByRole("heading", { level: 1, name: "Example" }).closest("header")?.parentElement as HTMLElement;
}

describe("Page", () => {
  it("lets pages of tables and cards grow on large screens", () => {
    expect(frame().className).toContain("min-[2400px]:max-w-[2160px]");
  });

  it("keeps pages of forms at a readable width", () => {
    const el = frame("text");
    expect(el.className).toContain("max-w-[1200px]");
    expect(el.className).not.toContain("min-[1800px]");
  });
});
