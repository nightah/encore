import i18n from "@i18n/index";

/** Times render in the interface language and the browser's timezone. */

/** The locale for dates, times and numbers: the interface language, with English written the British way ("20:41", "24 Oct"). */
function locale(): string {
  return !i18n.language || i18n.language === "en" ? "en-GB" : i18n.language;
}

type Format = Intl.NumberFormat | Intl.RelativeTimeFormat | Intl.ListFormat | Intl.DisplayNames;

const formats = new Map<string, Format>();

/** One formatter per kind and locale, kept: building them is slow and the language rarely changes. */
function cached<F extends Format>(kind: string, make: (locale: string) => F): F {
  const lang = locale();
  const key = `${kind}|${lang}`;
  let format = formats.get(key) as F | undefined;
  if (!format) {
    format = make(lang);
    formats.set(key, format);
  }
  return format;
}

/** A language's name in the interface language ("ja" is "Japanese" in English), or the code when there is none. */
export function formatLanguage(code: string | undefined): string {
  if (!code) return "";
  try {
    return cached("language", (l) => new Intl.DisplayNames(l, { fallback: "none", type: "language" })).of(code) ?? code;
  } catch {
    return code;
  }
}

export function formatNumber(value: number): string {
  return cached("number", (l) => new Intl.NumberFormat(l)).format(value);
}

/** "Dune, Heat and Alien", joined the interface language's way. */
export function formatList(items: string[]): string {
  return cached("list", (l) => new Intl.ListFormat(l, { type: "conjunction" })).format(items);
}

/** "20:41", always 24-hour. */
export function formatClock(iso: string | null | undefined): string {
  if (!iso) return "-";
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return "-";
  return date.toLocaleTimeString(locale(), { hour: "2-digit", hourCycle: "h23", minute: "2-digit" });
}

/** "yesterday", "3 days ago". */
function relativeDays(): Intl.RelativeTimeFormat {
  return cached("days", (l) => new Intl.RelativeTimeFormat(l, { numeric: "auto" }));
}

/** "20:41" today, "Yesterday", or "24 Oct". */
export function formatDayOrClock(iso: string | null | undefined, now = new Date()): string {
  if (!iso) return "-";
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return "-";
  const startOfToday = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
  if (date.getTime() >= startOfToday) return formatClock(iso);
  if (date.getTime() >= startOfToday - 86_400_000) {
    const yesterday = relativeDays().format(-1, "day");
    return yesterday.charAt(0).toLocaleUpperCase(locale()) + yesterday.slice(1);
  }
  return date.toLocaleDateString(locale(), { day: "numeric", month: "short" });
}

export function formatDate(iso: string | null | undefined): string {
  if (!iso) return "-";
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return "-";
  return date.toLocaleDateString(locale(), { day: "numeric", month: "short", year: "numeric" });
}

/** "just now", "4 min ago", "2 h ago", "yesterday", "3 days ago". */
export function formatRelative(iso: string | null | undefined, now = Date.now()): string {
  if (!iso) return i18n.t("time.never");
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) return i18n.t("time.never");
  const seconds = Math.round((now - then) / 1000);
  if (seconds < 0) {
    const ahead = -seconds;
    if (ahead < 60) return i18n.t("time.soon");
    if (ahead < 3600) return i18n.t("time.inMinutes", { value: formatNumber(Math.round(ahead / 60)) });
    return i18n.t("time.inHours", { value: formatNumber(Math.round(ahead / 3600)) });
  }
  if (seconds < 45) return i18n.t("time.justNow");
  if (seconds < 3600) return i18n.t("time.minutesAgo", { value: formatNumber(Math.max(1, Math.round(seconds / 60))) });
  if (seconds < 86_400) return i18n.t("time.hoursAgo", { value: formatNumber(Math.round(seconds / 3600)) });
  return relativeDays().format(-Math.round(seconds / 86_400), "day");
}

/** "148 ms" or "6.4 s". */
export function formatDuration(ms: number): string {
  if (ms < 1000) {
    return cached("ms", (l) => new Intl.NumberFormat(l, { style: "unit", unit: "millisecond" })).format(Math.round(ms));
  }
  const seconds = cached(
    "seconds",
    (l) => new Intl.NumberFormat(l, { maximumFractionDigits: 1, minimumFractionDigits: 1 }),
  );
  return i18n.t("time.seconds", { value: seconds.format(ms / 1000) });
}

/** "31:12" or "1:02:05". */
export function formatPosition(totalSeconds: number): string {
  const s = Math.max(0, Math.floor(totalSeconds));
  const hours = Math.floor(s / 3600);
  const minutes = Math.floor((s % 3600) / 60);
  const seconds = String(s % 60).padStart(2, "0");
  return hours > 0 ? `${hours}:${String(minutes).padStart(2, "0")}:${seconds}` : `${minutes}:${seconds}`;
}

/** "1.12 MB" (decimal megabytes, as Amazon counts them). */
export function formatMegabytes(bytes: number): string {
  const megabytes = cached(
    "megabytes",
    (l) => new Intl.NumberFormat(l, { maximumFractionDigits: 2, style: "unit", unit: "megabyte" }),
  );
  return megabytes.format(bytes / 1_000_000);
}

/** "amzn1.ask.device.AEX…7QK": keeps the readable prefix and the last three characters. */
export function truncateDeviceId(id: string): string {
  const prefix = "amzn1.ask.device.";
  const rest = id.startsWith(prefix) ? id.slice(prefix.length) : id;
  if (rest.length <= 8) return id;
  return `${id.startsWith(prefix) ? prefix : ""}${rest.slice(0, 3)}…${rest.slice(-3)}`;
}

/** "0.87" */
export function formatScore(score: number | null | undefined): string {
  if (score === null || score === undefined) return "-";
  const format = cached(
    "score",
    (l) => new Intl.NumberFormat(l, { maximumFractionDigits: 2, minimumFractionDigits: 2 }),
  );
  return format.format(score);
}

/** "http://192.168.1.20:8080/jsonrpc" → "192.168.1.20:8080". */
export function hostPort(url: string): string {
  try {
    return new URL(url).host;
  } catch {
    return url;
  }
}
