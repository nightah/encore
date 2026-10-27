import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { Heard, heardSuffix } from "@components/Heard";

describe("Heard", () => {
  it("quotes what Alexa heard", () => {
    render(<Heard request={{ heard: "hot fuzz", intent: "WatchMovie" }} />);
    expect(screen.getByText("“hot fuzz”")).toBeInTheDocument();
  });

  it("shows the intent when no title was heard, not empty quotes", () => {
    const { container } = render(<Heard request={{ heard: "", intent: "CurrentPlayItemEnquiry" }} />);
    expect(screen.getByText("CurrentPlayItemEnquiry")).toBeInTheDocument();
    expect(container.textContent).not.toContain("“”");
  });

  it("leaves announcements without a quote when nothing was heard", () => {
    expect(heardSuffix({ heard: "dune" })).toBe(": “dune”");
    expect(heardSuffix({ heard: "" })).toBe("");
  });
});
