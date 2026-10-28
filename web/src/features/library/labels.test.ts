import { describe, expect, it } from "vitest";

import type { Candidate, MatchResult, RecentTitle } from "@api/generated/model";
import {
  aliasKind,
  boostText,
  explain,
  kindLabel,
  recentStatus,
  titleWithYear,
  verdict,
} from "@features/library/labels";

const candidate = (title: string, score: number, boost = 0) =>
  ({ breakdown: { boost }, kind: "movie", media_id: 1, score, title, year: 2000 }) as unknown as Candidate;

const result = (decision: MatchResult["decision"], candidates: Candidate[], margin = 0) => ({
  candidates,
  decision,
  margin,
});

describe("kindLabel", () => {
  it("names the kinds Kodi knows and capitalises any other", () => {
    expect(kindLabel("tvshow")).toBe("TV show");
    expect(kindLabel("musicvideo")).toBe("Music video");
    expect(kindLabel("audiobook")).toBe("Audiobook");
    expect(kindLabel("")).toBe("");
  });
});

describe("aliasKind", () => {
  it("accepts only kinds an alias can be saved for", () => {
    expect(aliasKind("movie")).toBe("movie");
    expect(aliasKind("tvshow")).toBe("tvshow");
    expect(aliasKind("audiobook")).toBeUndefined();
    expect(aliasKind("Movie")).toBeUndefined();
  });
});

describe("titleWithYear", () => {
  it("adds a known year", () => {
    expect(titleWithYear({ title: "Rocky II", year: 1979 })).toBe("Rocky II (1979)");
  });

  it.each([0, null, undefined])("leaves out a year of %j", (year) => {
    expect(titleWithYear({ title: "Radiohead", year })).toBe("Radiohead");
  });
});

describe("verdict", () => {
  it("labels and colours each decision", () => {
    expect(verdict("play")).toEqual({ label: "Would play", tone: "ok" });
    expect(verdict("ask")).toEqual({ label: "Would ask", tone: "ask" });
    expect(verdict("not_found")).toEqual({ label: "Not found", tone: "bad" });
  });
});

describe("explain", () => {
  it("says nothing came close without candidates", () => {
    expect(explain(result("not_found", []))).toBe("Nothing in this library comes close.");
  });

  it("explains a play by the lead over the next candidate", () => {
    expect(explain(result("play", [candidate("Rocky II", 0.96), candidate("Rocky", 0.65)], 0.31))).toBe(
      "Rocky II scores 0.96, 0.31 ahead of the next.",
    );
    expect(explain(result("play", [candidate("Conclave", 0.91)], 1))).toBe(
      "Conclave scores 0.91 and is the only close match.",
    );
  });

  it("works the margin out from the scores when the result has none", () => {
    expect(explain(result("play", [candidate("Rocky II", 0.96), candidate("Rocky", 0.65)], 0))).toBe(
      "Rocky II scores 0.96, 0.31 ahead of the next.",
    );
  });

  it("explains asking between close candidates", () => {
    expect(explain(result("ask", [candidate("Dune: Part Two", 0.82), candidate("Dune", 0.79)], 0.03))).toBe(
      "Dune: Part Two and Dune score within 0.03 of each other, so Alexa asks which one.",
    );
    expect(explain(result("ask", [candidate("Amélie", 0.7)]))).toBe(
      "Amélie scores 0.70, too close to call, so Alexa asks.",
    );
  });

  it("explains a miss by the best score", () => {
    expect(explain(result("not_found", [candidate("Hot Fuzz", 0.58), candidate("Hot Shots!", 0.44)], 0.14))).toBe(
      "The best score, 0.58 for Hot Fuzz, is below the 0.60 needed to play.",
    );
  });
});

describe("boostText", () => {
  it("signs a boost and shows a dash without one", () => {
    expect(boostText(candidate("a", 1, 0.1))).toBe("+0.10");
    expect(boostText(candidate("a", 1, -0.05))).toBe("-0.05");
    expect(boostText(candidate("a", 1, 0))).toBe("-");
  });
});

describe("recentStatus", () => {
  const recent = (patch: Partial<RecentTitle>) => ({ findable: false, in_skill_model: false, ...patch }) as RecentTitle;

  it("prefers in the skill model over findable over waiting", () => {
    expect(recentStatus(recent({ findable: true, in_skill_model: true }))).toEqual({
      label: "In skill model",
      tone: "neutral",
    });
    expect(recentStatus(recent({ findable: true }))).toEqual({ label: "Findable by voice", tone: "ok" });
    expect(recentStatus(recent({}))).toEqual({ label: "Waiting for next update", tone: "warn" });
  });
});
