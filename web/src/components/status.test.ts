import { describe, expect, it } from "vitest";

import type { Instance, RequestSummary } from "@api/generated/model";
import {
  CHECK_LABELS,
  CHECK_ORDER,
  checkLabel,
  connectionStatus,
  instanceStatus,
  outcomeChip,
  transportLabel,
  versionLine,
} from "@components/status";
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

describe("instance status helpers", () => {
  const [sample] = instances(Date.now());
  if (!sample) throw new Error("no sample instance");
  const inState = (state: string, patch: Partial<Instance> = {}) =>
    ({ ...sample, now_playing: null, ...patch, status: { ...sample.status, state } }) as Instance;

  it("shows idle, asleep and offline instances", () => {
    expect(instanceStatus(inState("connected"))).toEqual({ label: "Idle", tone: "info" });
    expect(instanceStatus(inState("asleep"))).toEqual({ label: "Asleep", tone: "neutral" });
    expect(instanceStatus(inState("offline"))).toEqual({ label: "Offline", tone: "bad" });
  });

  it("names the connection", () => {
    expect(connectionStatus(inState("connected"))).toEqual({ label: "Connected · WebSocket", tone: "ok" });
    expect(connectionStatus(inState("http_only"))).toEqual({ label: "Connected · HTTP only", tone: "warn" });
    expect(connectionStatus(inState("asleep"))).toEqual({ label: "Asleep", tone: "neutral" });
    expect(connectionStatus(inState("offline"))).toEqual({ label: "Offline", tone: "bad" });
  });

  it("says how Encore reaches Kodi, or whether it can wake it", () => {
    expect(transportLabel(inState("connected"))).toBe("WebSocket");
    expect(transportLabel(inState("http_only"))).toBe("HTTP only");
    expect(transportLabel(inState("asleep", { mac: "DC:A6:32:4B:19:0E" }))).toBe("MAC saved");
    expect(transportLabel(inState("offline", { mac: "" }))).toBe("No MAC address");
  });

  it("names the Kodi and API versions it knows", () => {
    const withVersions = (kodi: string, api: string) =>
      ({ ...sample, status: { ...sample.status, api_version: api, kodi_version: kodi } }) as Instance;
    expect(versionLine(withVersions("21.3", "13.5.0"))).toBe("Kodi 21.3 · API 13.5");
    expect(versionLine(withVersions("21.3", ""))).toBe("Kodi 21.3");
    expect(versionLine(withVersions("", "13"))).toBe("API 13");
    expect(versionLine(withVersions("", ""))).toBe("Not known until Encore reaches Kodi");
  });
});

describe("checkLabel", () => {
  it("uses the server's detail, or names the check", () => {
    expect(checkLabel({ detail: "Kodi 21.3 answered in 4 ms", name: "ping" })).toBe("Kodi 21.3 answered in 4 ms");
    expect(checkLabel({ detail: "", name: "ping" })).toBe("JSON-RPC over HTTP");
    expect(checkLabel({ detail: "", name: "websocket" })).toBe("WebSocket");
    expect(CHECK_ORDER.map((name) => CHECK_LABELS[name])).toEqual([
      "JSON-RPC over HTTP",
      "WebSocket",
      "Credentials",
      "Library",
    ]);
  });
});
