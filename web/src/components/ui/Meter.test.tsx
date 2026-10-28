import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { Meter, Progress, SegmentBar } from "@components/ui/Meter";

describe("Meter", () => {
  it("reads out its label and caps the value at the maximum", () => {
    render(<Meter value={1_700_000} max={1_500_000} label="en-AU: 1.7 MB of 1.5 MB used" />);
    const meter = screen.getByRole("meter", { name: "en-AU: 1.7 MB of 1.5 MB used" });
    expect(meter).toHaveAttribute("aria-valuenow", "1500000");
    expect(meter).toHaveAttribute("aria-valuemax", "1500000");
    expect(meter).toHaveAttribute("aria-valuetext", "en-AU: 1.7 MB of 1.5 MB used");
  });
});

describe("Progress", () => {
  it("reports how far a task is, or that it hasn't measured yet", () => {
    const { rerender } = render(<Progress value={40} label="Deploy 40% done" />);
    expect(screen.getByRole("progressbar", { name: "Deploy 40% done" })).toHaveAttribute("aria-valuenow", "40");
    rerender(<Progress value={null} label="Starting" />);
    expect(screen.getByRole("progressbar", { name: "Starting" })).not.toHaveAttribute("aria-valuenow");
  });
});

describe("SegmentBar", () => {
  const widths = () =>
    [...screen.getByRole("img", { name: "timing" }).querySelectorAll("span")].map((s) => s.style.width);

  it("splits the bar in proportion to each segment", () => {
    render(
      <SegmentBar
        label="timing"
        segments={[
          { className: "a", value: 1 },
          { className: "b", value: 3 },
        ]}
      />,
    );
    expect(widths()).toEqual(["25%", "75%"]);
  });

  it("draws empty segments, not broken ones, when every value is zero", () => {
    render(
      <SegmentBar
        label="timing"
        segments={[
          { className: "a", value: 0 },
          { className: "b", value: 0 },
        ]}
      />,
    );
    expect(widths()).toEqual(["0%", "0%"]);
  });
});
