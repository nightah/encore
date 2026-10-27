import { describe, expect, it } from "vitest";

import type { RequestSummary } from "@api/generated/model";
import { instanceStatus, outcomeChip } from "@components/status";
import { instances } from "@mocks/data";

describe("outcomeChip", () => {
  it("labels known outcomes", () => {
    expect(outcomeChip({ intent: "WatchMovie", outcome: "played", target: "" })).toEqual({
      label: "Played",
      tone: "ok",
    });
  });

  it("doesn't break on an outcome it doesn't know", () => {
    const odd = {
      intent: "SessionEndedRequest",
      outcome: "" as RequestSummary["outcome"],
      target: "",
    };
    expect(outcomeChip(odd)).toEqual({ label: "Done", tone: "neutral" });
  });
});

describe("instanceStatus", () => {
  const [playing] = instances(Date.now());
  if (!playing?.now_playing) throw new Error("the sample's first instance is playing");
  const nowPlaying = playing.now_playing;

  it("is green while playing and amber while paused", () => {
    expect(instanceStatus(playing)).toEqual({ label: "Playing", tone: "ok" });
    expect(instanceStatus({ ...playing, now_playing: { ...nowPlaying, paused: true } })).toEqual({
      label: "Paused",
      tone: "warn",
    });
  });

  it("knows paused without a WebSocket too", () => {
    const httpOnly = { ...playing, status: { ...playing.status, state: "http_only" as const } };
    expect(instanceStatus({ ...httpOnly, now_playing: { ...nowPlaying, paused: true } }).label).toBe("Paused");
  });
});
