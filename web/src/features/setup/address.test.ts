import { describe, expect, it } from "vitest";

import { suggestedAddress } from "@features/setup/address";

const https = { origin: "https://encore.example.net", protocol: "https:" };
const http = { origin: "http://192.168.1.10:8080", protocol: "http:" };

describe("suggestedAddress", () => {
  it("prefers the origin the skill's endpoint already uses", () => {
    expect(suggestedAddress("https://kodi.example.org/api/v1/alexa", https)).toBe("https://kodi.example.org");
    expect(suggestedAddress("https://kodi.example.org:8443/api/v1/alexa", http)).toBe("https://kodi.example.org:8443");
  });

  it("falls back to the page's own origin only when it is served over HTTPS", () => {
    expect(suggestedAddress(undefined, https)).toBe("https://encore.example.net");
    expect(suggestedAddress("", https)).toBe("https://encore.example.net");
    expect(suggestedAddress(undefined, http)).toBe("");
  });

  it("ignores an endpoint that isn't an address", () => {
    expect(suggestedAddress("not a url", https)).toBe("https://encore.example.net");
    expect(suggestedAddress("not a url", http)).toBe("");
  });
});
