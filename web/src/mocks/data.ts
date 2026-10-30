import type {
  Attention,
  Deploy,
  Device,
  DiscoveredInstance,
  Instance,
  Library,
  LocaleInvocation,
  RecentTitle,
  RejectedRequests,
  RequestDetail,
  Room,
  Settings,
  SkillInfo,
  TitleAlias,
} from "@api/generated/model";
import { mockArtUrl } from "@mocks/art";

/**
 * Sample data for the mock API, typed against the generated API model so it can't drift from the server's contract (`pnpm dev:mock` and tests). The sample home has three Kodi instances (Lounge and Bedroom share a library, Bedroom is HTTP only, Kids' room is asleep), five Echos (one unlinked), and a request log with the typical outcomes.
 */

export type Scenario = "configured" | "fresh";

export interface MockDb {
  scenario: Scenario;
  setupToken: string;
  csrf: string;
  admin: { username: string; password: string } | null;
  authenticated: boolean;
  authMode: Settings["auth"]["mode"];
  publicUrl: string;
  instances: Instance[];
  libraries: Library[];
  discovered: DiscoveredInstance[];
  devices: Device[];
  rooms: Room[];
  requests: RequestDetail[];
  skill: SkillInfo;
  deploys: Record<string, Deploy>;
  aliases: TitleAlias[];
  recent: Record<number, RecentTitle[]>;
  settings: Settings;
  attention: Attention[];
  rejectedRequests: RejectedRequests[];
  nextId: number;
  helloSent: boolean;
}

const MIN = 60_000;
const HOUR = 60 * MIN;

export function ago(ms: number, now = Date.now()): string {
  return new Date(now - ms).toISOString();
}

export const LOCALES = [
  "ar-SA",
  "de-DE",
  "en-AU",
  "en-CA",
  "en-GB",
  "en-IN",
  "en-US",
  "es-ES",
  "es-MX",
  "es-US",
  "fr-CA",
  "fr-FR",
  "hi-IN",
  "it-IT",
  "ja-JP",
  "nl-NL",
  "pt-BR",
];

/** What people say per locale for each invocation choice: French and Brazilian Portuguese say kodi for both, and Arabic and Japanese write the names in their own script. */
export const LOCALE_INVOCATIONS: LocaleInvocation[] = LOCALES.map((locale) => {
  if (locale.startsWith("fr-") || locale === "pt-BR") return { encore: "kodi", kodi: "kodi", locale };
  if (locale === "ar-SA") return { encore: "انكور", kodi: "كودي", locale };
  if (locale === "ja-JP") return { encore: "アンコール", kodi: "コディ", locale };
  return { encore: "encore", kodi: "kodi", locale };
});

export function instances(now: number): Instance[] {
  return [
    {
      broadcast: "",
      default_for_unlinked: false,
      has_password: true,
      id: 1,
      language: "en",
      library: {
        episodes: 18406,
        group_id: 3,
        is_source: true,
        movies: 2184,
        newest_title: { findable: true, title: "Conclave", year: 2024 },
        shared: true,
        shows: 312,
        songs: 41907,
      },
      mac: "DC:A6:32:4B:19:0E",
      managed_by_config: false,
      name: "Lounge",
      now_playing: {
        art_url: mockArtUrl(1, "severance-poster"),
        duration_s: 2943,
        episode: 4,
        kind: "episode",
        paused: false,
        position_s: 1872,
        season: 2,
        show: "Severance",
        title: "Woe's Hollow",
      },
      shutdown: "inherit",
      status: {
        api_version: "13.5.0",
        kodi_version: "21.3",
        last_event_at: ago(4 * MIN, now),
        last_sync_at: ago(4 * MIN, now),
        next_sync_at: ago(-11 * MIN, now),
        state: "connected",
      },
      timezone: "",
      url: "http://192.168.1.20:8080/jsonrpc",
      username: "kodi",
      websocket_url: "ws://192.168.1.20:9090/jsonrpc",
    },
    {
      broadcast: "",
      default_for_unlinked: false,
      has_password: true,
      id: 2,
      language: "en",
      library: {
        episodes: 18406,
        group_id: 3,
        is_source: false,
        movies: 2184,
        newest_title: { findable: true, title: "Conclave", year: 2024 },
        shared: true,
        shows: 312,
        songs: 41907,
      },
      mac: "B8:27:EB:90:3C:51",
      managed_by_config: false,
      name: "Bedroom",
      now_playing: null,
      shutdown: "inherit",
      status: {
        api_version: "13.1000.0",
        kodi_version: "22.0-rc1",
        last_event_at: null,
        last_sync_at: ago(4 * MIN, now),
        next_sync_at: ago(-11 * MIN, now),
        state: "http_only",
      },
      timezone: "",
      url: "http://192.168.1.31:8080/jsonrpc",
      username: "kodi",
      websocket_url: "ws://192.168.1.31:9090/jsonrpc",
    },
    {
      broadcast: "192.168.1.255",
      default_for_unlinked: false,
      has_password: true,
      id: 3,
      language: "en",
      library: {
        episodes: 3120,
        group_id: 4,
        is_source: true,
        movies: 214,
        newest_title: { findable: true, title: "Bluey S3 E49", year: 0 },
        shared: false,
        shows: 58,
        songs: 0,
      },
      mac: "2C:F0:5D:11:8A:C7",
      managed_by_config: true,
      name: "Kids' room",
      now_playing: null,
      shutdown: "quit",
      status: {
        api_version: "13.5.0",
        kodi_version: "21.2",
        last_event_at: ago(52 * MIN, now),
        last_sync_at: ago(55 * MIN, now),
        next_sync_at: null,
        state: "asleep",
      },
      timezone: "",
      url: "http://192.168.1.44:8080/jsonrpc",
      username: "kodi",
      websocket_url: "ws://192.168.1.44:9090/jsonrpc",
    },
  ];
}

export function libraries(now: number): Library[] {
  return [
    {
      counts: { movies: 2184, shows: 312, songs: 41907 },
      evidence: "200 of 200 sampled items match on Kodi's internal IDs, file paths and date added",
      id: 3,
      last_sync_at: ago(4 * MIN, now),
      members: [
        { instance_id: 1, name: "Lounge" },
        { instance_id: 2, name: "Bedroom" },
      ],
      name: "Lounge and Bedroom",
      source_instance_id: 1,
      state: "likely_shared",
      syncing_instance_id: 1,
    },
    {
      counts: { movies: 214, shows: 58, songs: 0 },
      evidence: "",
      id: 4,
      last_sync_at: ago(55 * MIN, now),
      members: [{ instance_id: 3, name: "Kids' room" }],
      name: "Kids' room",
      source_instance_id: 3,
      state: "solo",
      syncing_instance_id: 3,
    },
  ];
}

export function devices(now: number): Device[] {
  const base = (d: Partial<Device> & Pick<Device, "id" | "alexa_device_id">): Device => ({
    first_seen: ago(40 * 24 * HOUR, now),
    kodi_instance_id: null,
    last_seen: ago(HOUR, now),
    name: "",
    request_count: 0,
    request_count_today: 0,
    room_id: null,
    ...d,
  });
  return [
    base({
      alexa_device_id: "amzn1.ask.device.AFQ8K2M9X4PL2LM",
      id: 1,
      kodi_instance_id: 1,
      last_seen: ago(5 * MIN, now),
      name: "Lounge Echo Show",
      request_count: 1412,
      request_count_today: 31,
      room_id: 1,
    }),
    base({
      alexa_device_id: "amzn1.ask.device.AHT3N7Q2WK59WD",
      id: 2,
      kodi_instance_id: 2,
      last_seen: ago(2 * HOUR + 41 * MIN, now),
      name: "Bedroom Echo",
      request_count: 388,
      request_count_today: 9,
      room_id: 2,
    }),
    base({
      alexa_device_id: "amzn1.ask.device.AB2X6L0R8TYK0P",
      id: 3,
      kodi_instance_id: 3,
      last_seen: ago(48 * MIN, now),
      name: "Kids' room Dot",
      request_count: 640,
      request_count_today: 14,
      room_id: 3,
    }),
    base({
      alexa_device_id: "amzn1.ask.device.AJ7M4C1V9ZQE3R",
      id: 4,
      kodi_instance_id: 1,
      last_seen: ago(26 * HOUR, now),
      name: "Study Echo Spot",
      request_count: 57,
      request_count_today: 0,
      room_id: 4,
    }),
    base({
      alexa_device_id: "amzn1.ask.device.AEX5D8H2J6NB7QK",
      first_seen: ago(76 * MIN, now),
      id: 5,
      last_seen: ago(76 * MIN, now),
      request_count: 2,
      request_count_today: 2,
    }),
  ];
}
