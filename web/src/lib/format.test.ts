import { describe, expect, it } from "vitest";

import i18n from "@i18n/index";
import {
  formatClock,
  formatDate,
  formatDayOrClock,
  formatDuration,
  formatMegabytes,
  formatNumber,
  formatPosition,
  formatRelative,
  formatScore,
  hostPort,
  truncateDeviceId,
} from "@lib/format";

/** An ISO timestamp for a local wall-clock time, so the tests don't depend on the machine's time zone. */
const local = (...parts: [number, number, number, number?, number?, number?]) =>
  new Date(parts[0], parts[1], parts[2], parts[3] ?? 0, parts[4] ?? 0, parts[5] ?? 0).toISOString();

describe("formatNumber", () => {
  it("groups digits the British way in English and follows the interface language", async () => {
    expect(formatNumber(1234567.5)).toBe("1,234,567.5");
    await i18n.changeLanguage("de");
    expect(formatNumber(1234567.5)).toBe("1.234.567,5");
  });
});

describe("formatClock", () => {
  it("shows a 24-hour clock", () => {
    expect(formatClock(local(2026, 9, 28, 20, 41))).toBe("20:41");
    expect(formatClock(local(2026, 9, 28, 0, 5))).toBe("00:05");
  });

  it.each([null, undefined, "", "not a date"])("shows a dash for %j", (value) => {
    expect(formatClock(value)).toBe("-");
  });
});

describe("formatDayOrClock", () => {
  const now = new Date(2026, 9, 28, 9, 30);

  it("shows the time for today, including midnight", () => {
    expect(formatDayOrClock(local(2026, 9, 28, 8, 15), now)).toBe("08:15");
    expect(formatDayOrClock(local(2026, 9, 28), now)).toBe("00:00");
  });

  it("says yesterday, capitalised, for any time the day before", () => {
    expect(formatDayOrClock(local(2026, 9, 27, 23, 59), now)).toBe("Yesterday");
    expect(formatDayOrClock(local(2026, 9, 27), now)).toBe("Yesterday");
  });

  it("shows the date for anything older", () => {
    expect(formatDayOrClock(local(2026, 9, 26, 23, 59), now)).toBe("26 Oct");
    expect(formatDayOrClock(local(2025, 11, 31, 12), now)).toBe("31 Dec");
  });

  it("speaks the interface language", async () => {
    await i18n.changeLanguage("de");
    expect(formatDayOrClock(local(2026, 9, 27, 12), now)).toBe("Gestern");
  });

  it.each([null, undefined, "", "garbage"])("shows a dash for %j", (value) => {
    expect(formatDayOrClock(value, now)).toBe("-");
  });
});

describe("formatDate", () => {
  it("shows day, short month and year", () => {
    expect(formatDate(local(2026, 0, 3, 12))).toBe("3 Jan 2026");
  });

  it.each([null, undefined, "", "nope"])("shows a dash for %j", (value) => {
    expect(formatDate(value)).toBe("-");
  });
});

describe("formatRelative", () => {
  const now = Date.UTC(2026, 9, 28, 12, 0, 0);
  const ago = (seconds: number) => new Date(now - seconds * 1000).toISOString();

  it.each([
    [0, "just now"],
    [44, "just now"],
    [45, "1 min ago"],
    [89, "1 min ago"],
    [4 * 60, "4 min ago"],
    [3600, "1 h ago"],
    [2 * 3600 + 10, "2 h ago"],
    [86_399, "24 h ago"],
    [86_400, "yesterday"],
    [3 * 86_400, "3 days ago"],
  ])("says how long ago %i seconds was", (seconds, expected) => {
    expect(formatRelative(ago(seconds), now)).toBe(expected);
  });

  it.each([
    [-1, "in a moment"],
    [-59, "in a moment"],
    [-60, "in 1 min"],
    [-30 * 60, "in 30 min"],
    [-3600, "in 1 h"],
    [-5 * 3600, "in 5 h"],
  ])("says how soon %i seconds is", (seconds, expected) => {
    expect(formatRelative(ago(seconds), now)).toBe(expected);
  });

  it.each([null, undefined, "", "yesterday-ish"])("says never for %j", (value) => {
    expect(formatRelative(value, now)).toBe("never");
  });
});

describe("formatDuration", () => {
  it("shows milliseconds under a second, rounded", () => {
    expect(formatDuration(0)).toBe("0 ms");
    expect(formatDuration(148.4)).toBe("148 ms");
    expect(formatDuration(999)).toBe("999 ms");
  });

  it("shows seconds with one decimal from a second on", () => {
    expect(formatDuration(1000)).toBe("1.0 s");
    expect(formatDuration(6420)).toBe("6.4 s");
    expect(formatDuration(61_000)).toBe("61.0 s");
  });
});

describe("formatPosition", () => {
  it.each([
    [0, "0:00"],
    [5.9, "0:05"],
    [59, "0:59"],
    [60, "1:00"],
    [31 * 60 + 12, "31:12"],
    [3599, "59:59"],
    [3600, "1:00:00"],
    [3600 + 2 * 60 + 5, "1:02:05"],
    [-12, "0:00"],
  ])("shows %f seconds as %s", (seconds, expected) => {
    expect(formatPosition(seconds)).toBe(expected);
  });
});

describe("formatMegabytes", () => {
  it("counts decimal megabytes with up to two decimals", () => {
    expect(formatMegabytes(1_120_000)).toBe("1.12 MB");
    expect(formatMegabytes(1_000_000)).toBe("1 MB");
    expect(formatMegabytes(1_234_567)).toBe("1.23 MB");
    expect(formatMegabytes(0)).toBe("0 MB");
  });
});

describe("truncateDeviceId", () => {
  it("keeps Amazon's prefix and the first and last three characters", () => {
    expect(truncateDeviceId("amzn1.ask.device.AEXABCDEFGH7QK")).toBe("amzn1.ask.device.AEX…7QK");
  });

  it("shortens ids without the prefix too", () => {
    expect(truncateDeviceId("ABCDEFGHIJ")).toBe("ABC…HIJ");
  });

  it("leaves ids short enough to read whole", () => {
    expect(truncateDeviceId("amzn1.ask.device.ABCDEFGH")).toBe("amzn1.ask.device.ABCDEFGH");
    expect(truncateDeviceId("ABCDEFGH")).toBe("ABCDEFGH");
    expect(truncateDeviceId("")).toBe("");
  });
});

describe("formatScore", () => {
  it("shows two decimals", () => {
    expect(formatScore(0.8666)).toBe("0.87");
    expect(formatScore(1)).toBe("1.00");
    expect(formatScore(0)).toBe("0.00");
  });

  it("shows a dash without a score", () => {
    expect(formatScore(null)).toBe("-");
    expect(formatScore(undefined)).toBe("-");
  });
});

describe("hostPort", () => {
  it("keeps the host and port of a URL", () => {
    expect(hostPort("http://192.168.1.20:8080/jsonrpc")).toBe("192.168.1.20:8080");
    expect(hostPort("https://kodi.example.com/jsonrpc")).toBe("kodi.example.com");
    expect(hostPort("http://[fd00::1]:8080/")).toBe("[fd00::1]:8080");
  });

  it("returns anything that isn't a URL unchanged", () => {
    expect(hostPort("not a url")).toBe("not a url");
    expect(hostPort("")).toBe("");
  });
});
