import type { RecentTitle, Room, Settings, SkillInfo } from "@api/generated/model";
import {
  LOCALES,
  LOCALE_INVOCATIONS,
  type MockDb,
  type Scenario,
  ago,
  devices as sampleDevices,
  instances as sampleInstances,
  libraries as sampleLibraries,
} from "@mocks/data";
import { olderRequests, sampleRequests } from "@mocks/requests";

const MIN = 60_000;
const HOUR = 60 * MIN;

function rooms(): Room[] {
  return [
    {
      aliases: [
        { alias: "big tv", id: 3, kind: "device" },
        { alias: "living room", id: 2, kind: "room" },
        { alias: "lounge", id: 1, kind: "room" },
      ],
      id: 1,
      kodi_instance_id: 1,
      name: "Lounge",
    },
    {
      aliases: [
        { alias: "bedroom", id: 4, kind: "room" },
        { alias: "bedroom tv", id: 5, kind: "device" },
      ],
      id: 2,
      kodi_instance_id: 2,
      name: "Bedroom",
    },
    {
      aliases: [
        { alias: "kids room", id: 6, kind: "room" },
        { alias: "playroom", id: 7, kind: "room" },
      ],
      id: 3,
      kodi_instance_id: 3,
      name: "Kids' room",
    },
    {
      aliases: [
        { alias: "study", id: 8, kind: "room" },
        { alias: "study tv", id: 9, kind: "device" },
      ],
      id: 4,
      kodi_instance_id: 1,
      name: "Study",
    },
  ];
}

function skill(now: number, fresh: boolean): SkillInfo {
  return {
    amazon: fresh
      ? {
          connected: false,
          has_client_secret: false,
          needs_reauthorise: false,
          profile_client_id: "",
          vendor_id: "",
        }
      : {
          connected: true,
          has_client_secret: true,
          needs_reauthorise: false,
          profile_client_id: "amzn1.application-oa2-client.4f1c8d2b7a6e4e92a",
          vendor_id: "M2X8K4P1QF",
        },
    auto_deploy: !fresh,
    auto_deploy_paused: "",
    content: ["movies", "tv", "music"],
    endpoint: {
      url: "https://encore.example.net/api/v1/alexa",
    },
    invocation: "encore",
    invocation_name: "encore",
    last_deploy: fresh
      ? null
      : {
          at: ago(40 * MIN, now),
          automatic: false,
          error: "",
          hash: "9c1e4f7a",
          id: "d-42",
          locales: [
            {
              built_at: ago(40 * MIN, now),
              errors: [],
              locale: "en-AU",
              size_bytes: 1_120_000,
              status: "succeeded",
              titles: 3480,
            },
          ],
          state: "succeeded",
          warnings: [],
        },
    locale_invocations: LOCALE_INVOCATIONS,
    locales_available: LOCALES,
    locales_enabled: ["en-AU"],
    model_size: fresh
      ? null
      : {
          bytes: 1_120_000,
          limit_bytes: 1_500_000,
          locale: "en-AU",
          titles_in_model: 3480,
          titles_server_only: 1206,
        },
    needs_deploy: false,
    privacy: fresh
      ? {
          allows_purchases: null,
          contains_ads: null,
          is_child_directed: null,
          is_export_compliant: null,
          uses_personal_info: null,
        }
      : {
          allows_purchases: false,
          contains_ads: false,
          is_child_directed: false,
          is_export_compliant: true,
          uses_personal_info: false,
        },
    skill_id: fresh ? "" : "amzn1.ask.skill.3f2a6b0e-5c1d-4e8a-9b7f-2d4c6e8a91c0",
    skill_name: "Encore",
    state: fresh ? "not_deployed" : "live",
    update_reason: "none",
  };
}

function settings(now: number): Settings {
  return {
    auth: {
      header: {
        admin_group: "encore-admins",
        groups_header: "Remote-Groups",
        trusted_proxies: ["172.20.0.0/16", "10.0.10.5/32"],
        user_header: "Remote-User",
      },
      local: {
        lockout_attempts: 5,
        lockout_minutes: 15,
        session_hours: 168,
        totp_enabled: false,
        username: "admin",
      },
      mode: "local",
      oidc: {
        admin_group: "encore-admins",
        break_glass: true,
        client_id: "encore",
        groups_claim: "groups",
        has_client_secret: true,
        issuer: "https://auth.example.net",
        redirect_uri: "https://encore.example.net/api/v1/auth/oidc/callback",
        scopes: ["openid", "profile", "email", "groups"],
      },
    },
    features: {
      auto_deploy: true,
      auto_deploy_minutes: 5,
      confirm_sound: true,
      deep_search: true,
      queue_limit: 100,
      shutdown_quits: false,
    },
    locale: { default_locale: "en-AU", timezone: "Australia/Melbourne" },
    logging: { level: "info" },
    managed: ["auth.header.trusted_proxies", "secrets.key_source", "storage.local.path"],
    secrets: { key_id: "k2", key_source: "file", rotated_at: ago(23 * 24 * HOUR, now) },
    server: { base_path: "/" },
    storage: {
      backend: "s3",
      backups: {
        keep: 14,
        last_backup_at: ago(17 * HOUR, now),
        prefix: "Encore",
        schedule: "daily@03:00",
      },
      local: { path: "/data" },
      s3: {
        access_key: "encore-svc",
        addressing: "path",
        bucket: "encore",
        ca_bundle: "",
        endpoint: "https://minio.home.example.net",
        has_secret_key: true,
        insecure_skip_verify: false,
        prefix: "homelab/",
        region: "us-east-1",
        sse: "",
      },
      webdav: {
        has_password: false,
        prefix: "",
        url: "https://cloud.example.net/remote.php/dav/files/encore/encore/",
        username: "encore",
      },
    },
  };
}

function recent(now: number): Record<number, RecentTitle[]> {
  return {
    3: [
      {
        added_at: ago(4 * MIN, now),
        findable: true,
        in_skill_model: false,
        instance: "Lounge",
        kind: "movie",
        title: "Conclave",
        year: 2024,
      },
      {
        added_at: ago(9 * MIN, now),
        findable: false,
        in_skill_model: false,
        instance: "Bedroom",
        kind: "movie",
        title: "The Wild Robot",
        year: 2024,
      },
      {
        added_at: ago(2 * HOUR, now),
        findable: true,
        in_skill_model: false,
        instance: "Lounge",
        kind: "episode",
        title: "Slow Horses S4 E6",
        year: 0,
      },
      {
        added_at: ago(27 * HOUR, now),
        findable: true,
        in_skill_model: false,
        instance: "Lounge",
        kind: "movie",
        title: "Flow",
        year: 2024,
      },
      {
        added_at: ago(50 * HOUR, now),
        findable: true,
        in_skill_model: true,
        instance: "Lounge",
        kind: "album",
        title: "Hounds of Love (2024 remaster)",
        year: 0,
      },
    ],
    4: [
      {
        added_at: ago(26 * HOUR, now),
        findable: true,
        in_skill_model: false,
        instance: "Kids' room",
        kind: "episode",
        title: "Bluey S3 E49",
        year: 0,
      },
    ],
  };
}

export function createDb(scenario: Scenario = "configured", now = Date.now()): MockDb {
  const fresh = scenario === "fresh";
  return {
    admin: fresh ? null : { password: "correct-horse-battery", username: "admin" },
    aliases: fresh
      ? []
      : [
          {
            alias: "the new dune",
            id: 1,
            kind: "movie",
            library_id: 3,
            media_id: 2101,
            title: "Dune: Part Two",
          },
        ],
    attention: fresh
      ? []
      : [
          {
            detail: "It asked “pause” twice today. Pick which Kodi it controls.",
            kind: "unlinked_device",
            link: "/echo-devices",
            title: "A new Echo isn't linked to a Kodi instance",
          },
          {
            detail: "Settings changed since the last deploy.",
            kind: "needs_deploy",
            link: "/alexa-skill",
            title: "Skill update pending",
          },
        ],
    authenticated: !fresh,
    authMode: "local",
    csrf: "mock-csrf-token",
    deploys: {},
    devices: fresh ? [] : sampleDevices(now),
    discovered: fresh
      ? [
          {
            host: "192.168.1.20",
            http_port: 8080,
            name: "Lounge",
            version: "21.3",
            ws_port: 9090,
          },
          {
            host: "192.168.1.31",
            http_port: 8080,
            name: "LibreELEC-Bedroom",
            version: "22.0-rc1",
            ws_port: 9090,
          },
        ]
      : [
          {
            host: "192.168.1.47",
            http_port: 8080,
            name: "LibreELEC-Study",
            version: "21.3",
            ws_port: 9090,
          },
          {
            host: "192.168.1.52",
            http_port: 8080,
            name: "SHIELD-Garage",
            version: "21.2",
            ws_port: 9090,
          },
        ],
    helloSent: false,
    instances: fresh ? [] : sampleInstances(now),
    libraries: fresh ? [] : sampleLibraries(now),
    nextId: 5000,
    publicUrl: fresh ? "" : "https://encore.example.net",
    recent: fresh ? {} : recent(now),
    requests: fresh ? [] : [...sampleRequests(now), ...olderRequests(now)],
    rooms: fresh ? [] : rooms(),
    scenario,
    settings: settings(now),
    setupToken: "K7QF-2M9X-PL4D",
    skill: skill(now, fresh),
  };
}
