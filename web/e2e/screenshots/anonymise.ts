import type { Page } from "@playwright/test";

/**
 * What identifies the network, the Amazon account or a device, and how each is replaced. Patterns are tried in order; where two overlap, the one that starts first (then the longer) wins.
 */
const PATTERNS: Array<[RegExp, Kind]> = [
  // URLs, including WebSocket ones: the host is replaced, the path kept.
  [/\b(?:https?|wss?):\/\/[^\s"'<>)]+/g, "url"],
  // Email addresses.
  [/\b[\w.+-]+@[\w-]+(?:\.[\w-]+)+\b/g, "email"],
  // IPv4 addresses, with an optional prefix length or port.
  [/\b(?:\d{1,3}\.){3}\d{1,3}(?:\/\d{1,2})?(?::\d{1,5})?\b/g, "ipv4"],
  // MAC addresses, before IPv6, which would also match them.
  [/\b(?:[0-9a-f]{2}[:-]){5}[0-9a-f]{2}\b/gi, "shape"],
  // IPv6 addresses: groups of hex with a letter or "::" among them, so times such as 1:42:00 aren't caught.
  [/(?<![\w:])(?=[0-9a-f:]*(?:[a-f]|::))[0-9a-f]{0,4}(?::[0-9a-f]{0,4}){2,7}(?:\/\d{1,3})?(?![\w:])/gi, "ipv6"],
  // Amazon IDs: skills, devices, users, Login with Amazon clients.
  [/\bamzn1\.[\w.-]+/g, "shape"],
  [/\bamzn-[\w.-]+/g, "shape"],
  // UUIDs.
  [/\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b/gi, "shape"],
  // Host names with a public suffix, such as encore.example.net.
  [/\b(?:[a-z0-9-]+\.)+(?:com|net|org|io|dev|app|au|uk|de|fr|es|it|ca|nz|lan|local|home|internal)\b/gi, "host"],
  // Home directories, which name the user.
  [/(?:\/home\/|\/Users\/|[A-Z]:\\Users\\)[^/\\\s"']+/g, "home"],
  // Long tokens and keys.
  [/\b[A-Za-z0-9_-]{32,}\b/g, "shape"],
  // Amazon vendor IDs: upper-case letters and digits.
  [/\b(?=[A-Z0-9]*\d)(?=[A-Z0-9]*[A-Z])[A-Z0-9]{10,16}\b/g, "shape"],
  // IDs shortened with an ellipsis, such as M2X8…QF or …91c0; words like "…on" have no digit and are left alone.
  [/[\w.:/-]*\d[\w.:/-]*…[\w-]+|[\w.:/-]*…[\w-]*\d[\w-]*/g, "shape"],
];

type Kind = "url" | "email" | "ipv4" | "ipv6" | "shape" | "host" | "home" | "literal";

export interface AnonymiseOptions {
  /** The captured Encore's host name, shown as encore.example.net. */
  host: string;
  /** Text to swap for an example, such as names of Kodi instances: original to replacement. An empty replacement blurs the text instead. */
  replace: Array<[string, string]>;
}

/**
 * Replaces sensitive details on the page with realistic examples: host names become example.net ones, addresses come from 192.168.1.0/24, and IDs keep their shape with made-up characters. The same original always gets the same example, so related values still match and repeated captures don't change. Elements marked data-sensitive show the attribute's value instead, or are blurred when it's empty, as are filled-in password fields.
 */
export async function anonymise(page: Page, options: AnonymiseOptions): Promise<void> {
  await page.evaluate(
    ({ patterns, host, replace }) => {
      const BLUR = "filter: blur(5px); user-select: none;";
      // Docs links and the like stay as they are.
      const allowed = [
        /github\.com\/nightah\/encore/i,
        /nightah\.github\.io/i,
        /developer\.amazon\.com/i,
        /example\.(net|com|org)/i,
      ];
      const keptIPs = /^(127\.|0\.0\.0\.0|255\.)/;
      const amazonPrefix =
        /^(amzn1\.(?:ask\.(?:skill|device|account|person)|application-oa2-client|application)\.|amzn-|amzn1\.)/;

      const regexes: Array<[RegExp, string, string?]> = [
        ...replace
          .filter(([from]) => from)
          .map(([from, to]): [RegExp, string, string] => [
            new RegExp(from.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"), "g"),
            "literal",
            to,
          ]),
        ...patterns.map(([source, flags, kind]): [RegExp, string] => [new RegExp(source, flags), kind]),
      ];

      // FNV-1a, then mulberry32: repeatable pseudo-random characters for each original.
      const seeded = (s: string) => {
        let h = 0x811c9dc5;
        for (const c of s) h = Math.imul(h ^ c.charCodeAt(0), 0x01000193) >>> 0;
        return () => {
          h = (h + 0x6d2b79f5) >>> 0;
          let t = h;
          t = Math.imul(t ^ (t >>> 15), t | 1);
          t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
          return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
        };
      };
      const pick = (chars: string, rnd: () => number) => chars[Math.floor(rnd() * chars.length)] ?? chars[0];

      // Keeps known prefixes and separators; swaps each letter or digit for one of the same kind, hex for hex.
      const shape = (s: string) => {
        const prefix = s.match(amazonPrefix)?.[0] ?? "";
        const rnd = seeded(s);
        const body = [...s.slice(prefix.length)]
          .map((c) => {
            if (/\d/.test(c)) return pick("0123456789", rnd);
            if (/[a-f]/.test(c)) return pick("0123456789abcdef", rnd);
            if (/[A-F]/.test(c)) return pick("0123456789ABCDEF", rnd);
            if (/[a-z]/.test(c)) return pick("abcdefghijklmnopqrstuvwxyz", rnd);
            if (/[A-Z]/.test(c)) return pick("ABCDEFGHIJKLMNOPQRSTUVWXYZ", rnd);
            return c;
          })
          .join("");
        return prefix + body;
      };

      const ipv4s = new Map<string, string>();
      const ipv4 = (s: string) => {
        const [, ip = s, rest = ""] = s.match(/^((?:\d{1,3}\.){3}\d{1,3})(.*)$/) ?? [];
        if (keptIPs.test(ip)) return s;
        if (!ipv4s.has(ip)) ipv4s.set(ip, `192.168.1.${10 + ipv4s.size}`);
        return `${ipv4s.get(ip)}${rest}`;
      };

      const ipv6s = new Map<string, string>();
      const ipv6 = (s: string) => {
        const [ip = s, prefix] = s.split("/");
        if (ip === "::1" || ip === "::") return s;
        if (!ipv6s.has(ip)) ipv6s.set(ip, `fd00::${(ipv6s.size + 1).toString(16)}`);
        return `${ipv6s.get(ip)}${prefix ? `/${prefix}` : ""}`;
      };

      const hostname = (s: string) => {
        const lower = s.toLowerCase();
        if (lower === host.toLowerCase()) return "encore.example.net";
        const labels = lower.split(".");
        return labels.length > 2 ? `${labels[0]}.example.net` : "example.net";
      };

      const url = (s: string) =>
        s.replace(/^([a-z]+:\/\/)(?:[^@/]*@)?(\[[^\]]+\]|[^/:?#]+)/i, (_, scheme: string, h: string) => {
          if (/^\d{1,3}(\.\d{1,3}){3}$/.test(h)) return scheme + ipv4(h);
          if (h.startsWith("[")) return `${scheme}[${ipv6(h.slice(1, -1))}]`;
          return scheme + hostname(h);
        });

      const examples: Record<string, (s: string) => string> = {
        email: () => "admin@example.net",
        home: (s) =>
          s.startsWith("/home/") ? "/home/encore" : s.startsWith("/Users/") ? "/Users/encore" : "C:\\Users\\encore",
        host: hostname,
        ipv4,
        ipv6,
        shape,
        url,
      };

      type Piece = string | { blur: string };

      const transform = (text: string): Piece[] | null => {
        const found: Array<{ start: number; end: number; kind: string; to?: string }> = [];
        for (const [re, kind, to] of regexes) {
          re.lastIndex = 0;
          for (const m of text.matchAll(re)) {
            if (kind !== "literal" && allowed.some((a) => a.test(m[0]))) continue;
            found.push({ end: (m.index ?? 0) + m[0].length, kind, start: m.index ?? 0, to });
          }
        }
        if (!found.length) return null;
        found.sort((a, b) => a.start - b.start || b.end - a.end);
        const pieces: Piece[] = [];
        let at = 0;
        for (const f of found) {
          if (f.start < at) continue;
          if (f.start > at) pieces.push(text.slice(at, f.start));
          const original = text.slice(f.start, f.end);
          if (f.kind === "literal") pieces.push(f.to ? f.to : { blur: shape(original) });
          else pieces.push(examples[f.kind]?.(original) ?? original);
          at = f.end;
        }
        if (at < text.length) pieces.push(text.slice(at));
        return pieces;
      };

      const flat = (pieces: Piece[]) => pieces.map((p) => (typeof p === "string" ? p : p.blur)).join("");

      // Elements that name their own example come first, so their text isn't matched again.
      for (const el of document.querySelectorAll<HTMLElement>("[data-sensitive]")) {
        const example = el.dataset.sensitive;
        if (el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement) {
          if (example) el.value = example;
          else if (el.value) el.style.cssText += BLUR;
        } else if (example) {
          el.textContent = example;
        } else {
          el.style.cssText += BLUR;
        }
        el.dataset.anonymised = "";
      }

      const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT, {
        acceptNode: (node) =>
          node.parentElement?.closest("script, style, [data-anonymised]")
            ? NodeFilter.FILTER_REJECT
            : NodeFilter.FILTER_ACCEPT,
      });
      const nodes: Text[] = [];
      while (walker.nextNode()) nodes.push(walker.currentNode as Text);

      for (const node of nodes) {
        const pieces = transform(node.data);
        if (!pieces) continue;
        if (pieces.every((p) => typeof p === "string")) {
          node.data = pieces.join("");
          continue;
        }
        const frag = document.createDocumentFragment();
        for (const p of pieces) {
          if (typeof p === "string") {
            frag.append(p);
          } else {
            const span = document.createElement("span");
            span.style.cssText = BLUR;
            span.textContent = p.blur;
            frag.append(span);
          }
        }
        node.replaceWith(frag);
      }

      for (const el of document.querySelectorAll<HTMLInputElement | HTMLTextAreaElement>("input, textarea")) {
        if (!el.value || el.closest("[data-anonymised]")) continue;
        if (el.type === "password") {
          el.style.cssText += BLUR;
          continue;
        }
        const pieces = transform(el.value);
        if (!pieces) continue;
        el.value = flat(pieces);
        if (pieces.some((p) => typeof p !== "string")) el.style.cssText += BLUR;
      }
    },
    {
      host: options.host,
      patterns: PATTERNS.map(([re, kind]) => [re.source, re.flags, kind] as const),
      replace: options.replace,
    },
  );
}
