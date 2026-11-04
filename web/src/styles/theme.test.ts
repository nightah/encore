import { describe, expect, it } from "vitest";

import theme from "@src/styles/theme.css?raw";

describe("font stacks", () => {
  // The web fonts are Latin, Greek and Cyrillic; Arabic, Devanagari and Japanese come from the system fonts each stack ends in.
  it.each([
    ["display", "sans-serif"],
    ["sans", "sans-serif"],
    ["mono", "monospace"],
  ])("--font-%s falls back to the system's %s", (name, generic) => {
    const stack = new RegExp(`--font-${name}:([^;]+);`).exec(theme)?.[1] ?? "";
    expect(
      stack
        .split(",")
        .map((f) => f.trim())
        .at(-1),
    ).toBe(generic);
  });
});
