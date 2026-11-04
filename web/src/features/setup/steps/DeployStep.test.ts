import { describe, expect, it } from "vitest";

import { spokenElsewhere } from "@features/setup/steps/DeployStep";

const invocations = [
  { encore: "انكور", kodi: "كودي", locale: "ar-SA" },
  { encore: "encore", kodi: "kodi", locale: "en-AU" },
  { encore: "kodi", kodi: "kodi", locale: "fr-CA" },
  { encore: "kodi", kodi: "kodi", locale: "fr-FR" },
];

describe("spokenElsewhere", () => {
  it("groups the enabled locales that say the choice differently", () => {
    expect(spokenElsewhere(invocations, ["en-AU", "fr-CA", "fr-FR", "ar-SA"], "encore")).toEqual([
      { locales: ["ar-SA"], name: "انكور" },
      { locales: ["fr-CA", "fr-FR"], name: "kodi" },
    ]);
  });

  it("leaves out locales that say it as chosen or aren't enabled", () => {
    expect(spokenElsewhere(invocations, ["en-AU", "fr-FR"], "kodi")).toEqual([]);
    expect(spokenElsewhere(invocations, ["en-AU"], "encore")).toEqual([]);
  });
});
