import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

// Link previews and browser tabs show these, so they name the product the way the rest of the UI does.
describe("index.html", () => {
  const html = readFileSync(join(__dirname, "../../index.html"), "utf8");

  it("names Encore in the title and description", () => {
    expect(html).toContain("<title>Encore</title>");
    expect(html).toMatch(/<meta name="description" content="Encore: /);
  });
});
