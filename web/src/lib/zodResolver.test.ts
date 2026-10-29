import { describe, expect, it } from "vitest";
import { z } from "zod";

import { zodResolver } from "@lib/zodResolver";

const options = { fields: {}, shouldUseNativeValidation: false };

describe("zodResolver", () => {
  const schema = z
    .object({
      address: z.object({ host: z.string().min(1, "Enter the host.") }),
      name: z.string().trim().min(1, "Enter a name."),
      password: z.string().min(12, "Use at least 12 characters.").regex(/\d/, "Include a digit."),
    })
    .superRefine((v, ctx) => {
      if (v.name === "admin") ctx.addIssue({ code: "custom", message: "Choose another name.", path: ["name"] });
    });

  it("returns the parsed values when valid", async () => {
    const result = await zodResolver(schema)(
      { address: { host: "kodi" }, name: "  Lounge  ", password: "correct-horse-1" },
      undefined,
      options,
    );
    expect(result).toEqual({
      errors: {},
      values: { address: { host: "kodi" }, name: "Lounge", password: "correct-horse-1" },
    });
  });

  it("puts each field's first error at its path", async () => {
    const result = await zodResolver(schema)(
      { address: { host: "" }, name: " ", password: "short" },
      undefined,
      options,
    );
    expect(result.values).toEqual({});
    expect(result.errors).toEqual({
      address: { host: { message: "Enter the host.", type: "too_small" } },
      name: { message: "Enter a name.", type: "too_small" },
      password: { message: "Use at least 12 characters.", type: "too_small" },
    });
  });

  it("includes refinements", async () => {
    const result = await zodResolver(schema)(
      { address: { host: "kodi" }, name: "admin", password: "correct-horse-1" },
      undefined,
      options,
    );
    expect(result.errors).toEqual({ name: { message: "Choose another name.", type: "custom" } });
  });

  it("keeps a form-wide refinement as the root error, so the form doesn't submit", async () => {
    const pair = z
      .object({ from: z.number(), to: z.number() })
      .refine((v) => v.from < v.to, { message: "The range is back to front." });
    const result = await zodResolver(pair)({ from: 5, to: 1 }, undefined, options);
    expect(result.values).toEqual({});
    expect(result.errors).toEqual({ root: { message: "The range is back to front.", type: "custom" } });
  });
});
