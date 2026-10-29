import { HttpResponse, delay, http, sse } from "msw";

import { RECENT_REQUESTS } from "@api/events";
import type {
  Accepted,
  AddRoomAliasRequest,
  AuthState,
  AuthorizeInfo,
  Candidate,
  ChangePasswordRequest,
  ClientErrorReport,
  ConfirmTOTPRequest,
  CreateAdminRequest,
  CreateAliasRequest,
  CreateRoomRequest,
  Deploy,
  DeployEvent,
  DeployEventStep,
  Device,
  DiscoveredInstance,
  ErrorModel,
  ImportResult,
  Instance,
  InstanceInput,
  Library,
  LoginRequest,
  Match,
  MatchResult,
  Overview,
  PasteAmazonAuthorizationRequest,
  Reachability,
  RecentTitle,
  Replay,
  RequestDetail,
  RequestPage,
  RequestSummary,
  Room,
  RoomAlias,
  SetAmazonCredentialsRequest,
  Settings,
  SettingsPatch,
  SkillInfo,
  SkillSettingsInput,
  StorageTest,
  TOTPEnrollment,
  TestMatchRequest,
  TestResult,
  TitleAlias,
  UpdateDeviceRequest,
  UpdateRoomRequest,
  UpdateSkillContentRequest,
} from "@api/generated/model";
import type { Health } from "@api/health";
import { mockArt } from "@mocks/art";
import type { MockDb, Scenario } from "@mocks/data";
import { createDb } from "@mocks/db";
import { fuzzyMatch } from "@mocks/requests";
import { basePath } from "@src/basePath";

/**
 * MSW handlers implementing the admin API (`internal/api/openapi.json`) against an in-memory database. Every response body goes through `json<T>` with a generated model type, so a contract change that the mocks don't follow is a type error. They enforce the same rules as the server where the UI depends on them: session auth, `X-CSRF-Token` on writes, 409 for config-managed fields, 422 problem+json for validation errors.
 */

const API = "*/api/v1";

let db: MockDb = createDb();
let persist: ((db: MockDb) => void) | undefined;

/** Replace the database (tests call this in `beforeEach`). */
export function resetDb(scenario: Scenario = "configured", next?: MockDb): MockDb {
  for (const timer of timers) clearTimeout(timer);
  timers.clear();
  db = next ?? createDb(scenario);
  return db;
}

export function getDb(): MockDb {
  return db;
}

/** Browser mock mode saves the database to sessionStorage so reloads keep state. */
export function onPersist(fn: (db: MockDb) => void) {
  persist = fn;
}

function save() {
  persist?.(db);
}

// Event bus feeding the SSE handler.

type Emitted = { event: string; data: unknown };
const subscribers = new Set<(message: Emitted) => void>();
let eventId = 0;

export function emit(event: string, data: unknown) {
  for (const subscriber of subscribers) subscriber({ data, event });
}

/** Receive every simulated server event (tests forward them to `FakeEventSource`). */
export function onEmit(fn: (message: Emitted) => void): () => void {
  subscribers.add(fn);
  return () => subscribers.delete(fn);
}

const timers = new Set<ReturnType<typeof setTimeout>>();

/** Simulated background work (sync, wake, deploy). Cancelled by `resetDb`. */
function later(ms: number, fn: () => void) {
  const timer = setTimeout(() => {
    timers.delete(timer);
    fn();
    save();
  }, ms);
  timers.add(timer);
}

/** A JSON response whose body must match the generated model `T` (pass it explicitly). */
function json<T>(data: NoInfer<T>, status = 200) {
  return HttpResponse.json(data as never, { status });
}

function noContent(status = 204) {
  return new HttpResponse(null, { status });
}

function accepted() {
  return json<Accepted>({ id: `job-${nextId()}` }, 202);
}

function problem(
  status: number,
  title: string,
  detail?: string,
  errors?: Array<{ location: string; message: string }>,
) {
  const body: ErrorModel = {
    detail: detail ?? title,
    status,
    title,
    type: "about:blank",
    ...(errors ? { errors } : {}),
  };
  return HttpResponse.json(body, {
    headers: { "Content-Type": "application/problem+json" },
    status,
  });
}

const unauthorized = () => problem(401, "Unauthorized", "Sign in to continue.");

function guard(request: Request, { write = request.method !== "GET" } = {}) {
  if (!db.authenticated) return unauthorized();
  if (write && request.headers.get("X-CSRF-Token") !== db.csrf) {
    return problem(403, "Forbidden", "Missing or invalid CSRF token. Reload the page and try again.");
  }
  return undefined;
}

async function body<T>(request: Request): Promise<T> {
  try {
    return (await request.json()) as T;
  } catch {
    return {} as T;
  }
}

function nextId() {
  db.nextId += 1;
  return db.nextId;
}

function summary(r: RequestDetail): RequestSummary {
  return {
    device_id: r.device_id,
    device_name: r.device_name,
    duration_ms: r.duration_ms,
    heard: r.heard,
    id: r.id,
    intent: r.intent,
    locale: r.locale,
    outcome: r.outcome,
    simulated: r.simulated,
    target: r.target,
    time: r.time,
  };
}

function instanceById(id: number) {
  return db.instances.find((i) => i.id === id);
}

function deriveAttention() {
  const unlinked = db.devices.filter((d) => d.kodi_instance_id === null);
  const others = db.attention.filter((a) => a.kind !== "unlinked_device");
  db.attention =
    unlinked.length && !db.instances.some((i) => i.default_for_unlinked)
      ? [
          {
            detail: "Pick which Kodi it controls.",
            kind: "unlinked_device",
            link: "/echo-devices",
            title:
              unlinked.length === 1
                ? `${unlinked[0]?.name || "A new Echo"} isn't linked to a Kodi instance`
                : `${unlinked.length} Echos aren't linked to a Kodi instance`,
          },
          ...others,
        ]
      : others;
  emit("attention", db.attention);
}

function overview(): Overview {
  const today = db.requests.filter((r) => Date.now() - new Date(r.time).getTime() < 24 * 3600_000);
  const durations = today.map((r) => r.duration_ms).sort((a, b) => a - b);
  const deployed = db.skill.last_deploy?.state === "succeeded";
  return {
    attention: db.attention,
    endpoint: {
      checked_at: new Date(Date.now() - 60_000).toISOString(),
      latency_ms: db.publicUrl ? 182 : 0,
      reachable: !!db.publicUrl,
      url: db.skill.endpoint.url,
    },
    instances: db.instances,
    libraries: {
      count: db.libraries.length,
      shared_groups: db.libraries.filter((l) => l.members.length > 1).length,
      synced_at:
        db.libraries
          .map((l) => l.last_sync_at)
          .sort()
          .at(-1) ?? null,
    },
    recent_requests: db.requests
      .filter((r) => !r.simulated)
      .slice(0, RECENT_REQUESTS)
      .map(summary),
    requests_today: {
      failed: today.filter((r) => r.outcome === "error" || r.outcome === "not_found").length + (today.length ? 1 : 0),
      median_ms: durations.length ? (durations[Math.floor(durations.length / 2)] ?? 0) : 0,
      total: today.length ? 63 + (today.length - Math.min(today.length, 6)) : 0,
    },
    skill: {
      locales: db.skill.locales_enabled,
      needs_deploy: db.skill.needs_deploy,
      stage: "development",
      state: Object.values(db.deploys).some((d) => d.state === "in_progress")
        ? "deploying"
        : deployed
          ? "live"
          : db.skill.last_deploy?.state === "failed"
            ? "failed"
            : "not_deployed",
      update_reason: db.skill.update_reason,
    },
  };
}

function patchInstance(id: number, patch: Partial<Instance>) {
  const index = db.instances.findIndex((i) => i.id === id);
  const current = db.instances[index];
  if (!current) return undefined;
  const next = { ...current, ...patch, status: { ...current.status, ...patch.status } };
  db.instances[index] = next;
  emit("instance", next);
  return next;
}

const DEPLOY_STEPS: Array<[DeployEventStep, string]> = [
  ["package", "Built the skill package"],
  ["vendor", "Found vendor M2X8…QF"],
  ["skill", "Updated the skill manifest"],
  ["upload", "Uploaded the package"],
  ["import", "Imported the interaction model"],
  ["build", "Built the model for each locale"],
  ["enable", "Enabled for testing on your account"],
  ["simulate", "Smoke test passed"],
];

function runDeploy(id: string, stepMs: number) {
  db.deploys[id] = { error: "", events: [], id, state: "in_progress" };
  DEPLOY_STEPS.forEach(([step, message], index) => {
    later(stepMs * (index + 1), () => {
      const event: DeployEvent = {
        at: new Date().toISOString(),
        locales: {},
        message,
        state: "succeeded",
        step,
      };
      const record = db.deploys[id];
      if (!record) return;
      record.events = [...record.events, event];
      if (step === "simulate") {
        record.state = "succeeded";
        const now = new Date().toISOString();
        db.skill = {
          ...db.skill,
          last_deploy: {
            at: now,
            error: "",
            hash: Math.random().toString(16).slice(2, 10),
            id,
            locales: db.skill.locales_enabled.map((locale) => ({
              built_at: now,
              errors: [],
              locale,
              size_bytes: 1_120_000,
              status: "succeeded" as const,
              titles: locale.startsWith("en") ? 3480 : 3402,
            })),
            state: "succeeded",
            warnings: [],
          },
          model_size: db.skill.model_size ?? {
            bytes: 1_120_000,
            limit_bytes: 1_500_000,
            locale: db.skill.locales_enabled[0] ?? "en-AU",
            titles_in_model: 3480,
            titles_server_only: 1206,
          },
          needs_deploy: false,
          skill_id: db.skill.skill_id || "amzn1.ask.skill.3f2a6b0e-5c1d-4e8a-9b7f-2d4c6e8a91c0",
          state: "live",
          update_reason: "none",
        };
        db.attention = db.attention.filter((a) => a.kind !== "needs_deploy" && a.kind !== "skill_not_deployed");
        emit("attention", db.attention);
      }
      emit("deploy", { ...event, deploy_id: id });
    });
  });
}

/** In the fresh scenario, simulate the first "Alexa, ask encore what's playing" after the wizard starts waiting. */
function scheduleHello() {
  if (db.scenario !== "fresh" || db.helloSent || !db.skill.last_deploy) return;
  db.helloSent = true;
  later(2500, () => {
    const device: Device = {
      alexa_device_id: "amzn1.ask.device.AFQ8K2M9X4PL2LM",
      first_seen: new Date().toISOString(),
      id: nextId(),
      kodi_instance_id: db.instances.find((i) => i.default_for_unlinked)?.id ?? null,
      last_seen: new Date().toISOString(),
      name: "",
      request_count: 1,
      request_count_today: 1,
      room_id: null,
    };
    db.devices.push(device);
    const request: RequestDetail = {
      device_id: device.id,
      device_name: "",
      duration_ms: 52,
      heard: "what's playing",
      id: nextId(),
      intent: "WhatsPlaying",
      kodi_calls: [{ method: "Player.GetActivePlayers", ms: 14, ok: true }],
      locale: "en-AU",
      match: null,
      outcome: "ok",
      simulated: false,
      slots: {},
      speech: "Nothing is playing in the lounge.",
      target: db.instances[0]?.name ?? "",
      time: new Date().toISOString(),
      timing: { kodi_ms: 14, match_ms: 1, verify_ms: 3 },
    };
    db.requests.unshift(request);
    emit("device", device);
    emit("request", summary(request));
  });
}

function mergeDeep<T>(target: T, patch: unknown): T {
  if (!patch || typeof patch !== "object" || Array.isArray(patch)) return patch as T;
  const out: Record<string, unknown> = { ...(target as Record<string, unknown>) };
  for (const [key, value] of Object.entries(patch as Record<string, unknown>)) {
    out[key] = mergeDeep(out[key], value);
  }
  return out as T;
}

function flattenKeys(value: unknown, prefix = ""): string[] {
  if (!value || typeof value !== "object" || Array.isArray(value)) return [prefix];
  return Object.entries(value as Record<string, unknown>).flatMap(([key, child]) =>
    flattenKeys(child, prefix ? `${prefix}.${key}` : key),
  );
}

/** Browser error reports the mock API received, newest last. */
export const clientErrors: ClientErrorReport[] = [];

export const handlers = [
  http.get(`${API}/healthz`, () => json<Health>({ status: "ok", version: "0.1.0" })),

  // Browser error reports: recorded so tests can inspect them, as the server would log them.
  http.post(`${API}/client-errors`, async ({ request }) => {
    clientErrors.push(await body<ClientErrorReport>(request));
    return new HttpResponse(null, { status: 204 });
  }),

  http.get(`${API}/auth/state`, () =>
    json<AuthState>({
      authenticated: db.authenticated,
      csrf_token: db.csrf,
      local_login: db.authMode === "local",
      mode: db.authMode,
      oidc_login_url: db.authMode === "oidc" ? "/api/v1/auth/oidc/login" : null,
      setup_required: db.admin === null,
      user: db.authenticated ? { name: db.admin?.username ?? "admin" } : null,
    }),
  ),

  http.post(`${API}/setup/admin`, async ({ request }) => {
    if (db.admin) return problem(409, "Conflict", "An admin account already exists. Sign in instead.");
    const input = await body<Partial<CreateAdminRequest>>(request);
    const errors: Array<{ location: string; message: string }> = [];
    if (input.setup_token?.trim() !== db.setupToken) {
      errors.push({
        location: "body.setup_token",
        message: "That setup token doesn't match the one in the logs.",
      });
    }
    if (!input.username?.trim()) errors.push({ location: "body.username", message: "Enter a username." });
    if ((input.password ?? "").length < 12) {
      errors.push({ location: "body.password", message: "Use at least 12 characters." });
    }
    if (errors.length) return problem(422, "Unprocessable Entity", "Check the highlighted fields.", errors);
    db.admin = { password: input.password ?? "", username: input.username ?? "admin" };
    db.settings.auth.local.username = db.admin.username;
    db.authenticated = true;
    save();
    return noContent();
  }),

  http.post(`${API}/auth/login`, async ({ request }) => {
    const input = await body<Partial<LoginRequest>>(request);
    if (!db.admin || input.username !== db.admin.username || input.password !== db.admin.password) {
      return problem(401, "Unauthorized", "the username or password is incorrect");
    }
    if (db.settings.auth.local.totp_enabled && input.totp !== TOTP_CODE) {
      return input.totp
        ? problem(401, "Unauthorized", "the one-time code is incorrect")
        : problem(401, "Unauthorized", "a one-time code is required", [{ location: "body.totp", message: "required" }]);
    }
    db.authenticated = true;
    save();
    return noContent();
  }),

  http.post(`${API}/auth/logout`, ({ request }) => {
    const denied = guard(request);
    if (denied) return denied;
    db.authenticated = false;
    save();
    return noContent();
  }),

  http.put(`${API}/auth/password`, async ({ request }) => {
    const denied = guard(request);
    if (denied) return denied;
    const input = await body<Partial<ChangePasswordRequest>>(request);
    if (!db.admin || input.current_password !== db.admin.password) {
      return problem(401, "Unauthorized", "the username or password is incorrect");
    }
    db.admin.password = input.new_password ?? db.admin.password;
    save();
    return noContent();
  }),

  http.post(`${API}/auth/totp/enroll`, ({ request }) => {
    const denied = guard(request);
    if (denied) return denied;
    return json<TOTPEnrollment>({
      otpauth_url: "otpauth://totp/Encore:admin?secret=JBSWY3DPEHPK3PXP&issuer=Encore",
      secret: "JBSWY3DPEHPK3PXP",
    });
  }),

  http.post(`${API}/auth/totp/confirm`, async ({ request }) => {
    const denied = guard(request);
    if (denied) return denied;
    const { code } = await body<Partial<ConfirmTOTPRequest>>(request);
    if (code !== TOTP_CODE) return problem(401, "Unauthorized", "the one-time code is incorrect");
    db.settings.auth.local.totp_enabled = true;
    save();
    return noContent();
  }),

  http.post(`${API}/auth/totp/disable`, async ({ request }) => {
    const denied = guard(request);
    if (denied) return denied;
    const { code } = await body<Partial<ConfirmTOTPRequest>>(request);
    if (code !== TOTP_CODE) return problem(401, "Unauthorized", "the one-time code is incorrect");
    db.settings.auth.local.totp_enabled = false;
    save();
    return noContent();
  }),

  http.post(`${API}/setup/reachability`, async ({ request }) => {
    const denied = guard(request);
    if (denied) return denied;
    const { url } = await body<{ url?: string }>(request);
    if (!url || !/^https:\/\//.test(url)) {
      return problem(422, "Unprocessable Entity", "Enter an https:// address.", [
        { location: "body.url", message: "Alexa only calls HTTPS endpoints on port 443." },
      ]);
    }
    await delay(600);
    db.publicUrl = url.replace(/\/$/, "");
    db.skill.endpoint = { ...db.skill.endpoint, url: `${db.publicUrl}/api/v1/alexa` };
    save();
    return json<Reachability>({
      checks: [
        {
          detail: "POST /api/v1/alexa answers with HTTP 400 to an unsigned request, as expected",
          level: "ok",
          name: "alexa_endpoint",
        },
        {
          detail: "Certificate trusted by Amazon (Let's Encrypt R11, valid until 18 Jan 2027)",
          level: "ok",
          name: "certificate",
        },
        {
          detail: "the web interface and admin API are reachable from outside too",
          level: "ok",
          name: "admin_exposed",
          note: "Every admin route needs a sign-in.",
        },
      ],
      url: db.publicUrl,
    });
  }),

  http.get(`${API}/overview`, ({ request }) => guard(request) ?? json<Overview>(overview())),

  http.get(`${API}/kodi/instances`, ({ request }) => guard(request) ?? json<Instance[]>(db.instances)),

  http.get(`${API}/kodi/instances/:id/art`, ({ request }) => {
    const denied = guard(request);
    if (denied) return denied;
    const svg = mockArt(new URL(request.url).searchParams.get("ref") ?? "");
    if (!svg) return problem(404, "Not Found", "No such image.");
    return new HttpResponse(svg, { headers: { "Content-Type": "image/svg+xml" } });
  }),

  http.get(`${API}/kodi/instances/:id`, ({ request, params }) => {
    const denied = guard(request);
    if (denied) return denied;
    const instance = instanceById(Number(params.id));
    return instance ? json<Instance>(instance) : problem(404, "Not Found");
  }),

  http.post(`${API}/kodi/instances`, async ({ request }) => {
    const denied = guard(request);
    if (denied) return denied;
    const input = await body<InstanceInput>(request);
    const url = input.url ?? "";
    const errors: Array<{ location: string; message: string }> = [];
    if (!input.name?.trim()) errors.push({ location: "body.name", message: "Give it a name." });
    if (!/^https?:\/\//.test(url)) {
      errors.push({ location: "body.url", message: "Enter an http:// or https:// address." });
    }
    if (db.instances.some((i) => i.name.toLowerCase() === input.name?.trim().toLowerCase())) {
      errors.push({
        location: "body.name",
        message: "Another Kodi instance already uses this name.",
      });
    }
    if (errors.length) return problem(422, "Unprocessable Entity", "Check the highlighted fields.", errors);
    const id = nextId();
    const libraryId = nextId();
    const instance: Instance = {
      broadcast: input.broadcast ?? "",
      default_for_unlinked: db.instances.length === 0,
      has_password: !!input.password,
      id,
      language: input.language ?? "en",
      library: {
        episodes: 0,
        group_id: libraryId,
        is_source: true,
        movies: 0,
        newest_title: null,
        shared: false,
        shows: 0,
        songs: 0,
      },
      mac: input.mac ?? "",
      managed_by_config: false,
      name: input.name?.trim() ?? "",
      now_playing: null,
      shutdown: input.shutdown ?? "inherit",
      status: {
        api_version: "13.5.0",
        kodi_version: "21.3",
        last_event_at: new Date().toISOString(),
        last_sync_at: null,
        next_sync_at: null,
        state: input.websocket_url ? "connected" : "http_only",
      },
      timezone: input.timezone ?? "",
      url,
      username: input.username ?? "",
      websocket_url: input.websocket_url ?? "",
    };
    db.instances.push(instance);
    db.discovered = db.discovered.filter((d) => !url.includes(d.host));
    db.libraries.push({
      counts: { movies: 0, shows: 0, songs: 0 },
      evidence: "",
      id: libraryId,
      last_sync_at: null,
      members: [{ instance_id: id, name: instance.name }],
      name: instance.name,
      source_instance_id: id,
      state: "solo",
      syncing_instance_id: id,
    });
    later(1500, () => {
      const lib = db.libraries.find((l) => l.id === libraryId);
      if (lib) {
        lib.counts = { movies: 1240, shows: 188, songs: 9020 };
        lib.last_sync_at = new Date().toISOString();
        emit("library", lib);
      }
      patchInstance(id, {
        library: {
          ...instance.library,
          episodes: 9740,
          movies: 1240,
          shows: 188,
          songs: 9020,
        },
        status: { ...instance.status, last_sync_at: new Date().toISOString() },
      });
    });
    save();
    return json<Instance>(instance, 201);
  }),

  http.patch(`${API}/kodi/instances/:id`, async ({ request, params }) => {
    const denied = guard(request);
    if (denied) return denied;
    const instance = instanceById(Number(params.id));
    if (!instance) return problem(404, "Not Found");
    if (instance.managed_by_config) {
      return problem(409, "Conflict", "This Kodi instance is managed by config and can't be changed here.");
    }
    const input = await body<InstanceInput>(request);
    const { password, ...rest } = input;
    const next = patchInstance(instance.id, {
      ...rest,
      ...(password !== undefined ? { has_password: password !== "" } : {}),
    });
    save();
    return next ? json<Instance>(next) : problem(404, "Not Found");
  }),

  http.delete(`${API}/kodi/instances/:id`, ({ request, params }) => {
    const denied = guard(request);
    if (denied) return denied;
    const id = Number(params.id);
    const instance = instanceById(id);
    if (instance?.managed_by_config) return problem(409, "Conflict", "Managed by config.");
    db.instances = db.instances.filter((i) => i.id !== id);
    db.libraries = db.libraries
      .map((l) => ({ ...l, members: l.members.filter((m) => m.instance_id !== id) }))
      .filter((l) => l.members.length > 0);
    save();
    return noContent();
  }),

  http.post(`${API}/kodi/instances/:id/test`, async ({ request, params }) => {
    const denied = guard(request);
    if (denied) return denied;
    const instance = instanceById(Number(params.id));
    if (!instance) return problem(404, "Not Found");
    await delay(500);
    const port = instance.websocket_url.match(/:(\d+)/)?.[1] ?? "9090";
    const asleep = instance.status.state === "asleep" || instance.status.state === "offline";
    const ws = instance.status.state === "connected";
    return json<TestResult>({
      checks: [
        {
          detail: asleep ? "No answer from JSONRPC.Ping over HTTP (Kodi is asleep)" : "JSONRPC.Ping over HTTP",
          ms: asleep ? 0 : 12,
          name: "ping",
          ok: !asleep,
        },
        {
          detail: ws
            ? `WebSocket on :${port}, notifications enabled`
            : `WebSocket on :${port} refused. Enable “Allow remote control from applications on other systems”.`,
          ms: ws ? 8 : 0,
          name: "websocket",
          ok: ws,
        },
        {
          detail: asleep
            ? "Skipped"
            : `Credentials accepted, JSON-RPC API ${instance.status.api_version.split(".").slice(0, 2).join(".")} detected`,
          ms: asleep ? 0 : 15,
          name: "auth",
          ok: !asleep,
        },
        {
          detail: asleep ? "Skipped" : "Library probe: VideoLibrary.GetMovies (limit 1)",
          ms: asleep ? 0 : 41,
          name: "library",
          ok: !asleep,
        },
      ],
    });
  }),

  http.post(`${API}/kodi/instances/:id/sync`, ({ request, params }) => {
    const denied = guard(request);
    if (denied) return denied;
    const id = Number(params.id);
    const library = db.libraries.find((l) => l.members.some((m) => m.instance_id === id));
    if (library) {
      later(1800, () => {
        library.last_sync_at = new Date().toISOString();
        emit("library", library);
        for (const member of library.members) {
          const inst = instanceById(member.instance_id);
          if (inst)
            patchInstance(inst.id, {
              status: { ...inst.status, last_sync_at: library.last_sync_at },
            });
        }
      });
    }
    return accepted();
  }),

  http.post(`${API}/kodi/instances/:id/wake`, ({ request, params }) => {
    const denied = guard(request);
    if (denied) return denied;
    const id = Number(params.id);
    const instance = instanceById(id);
    if (!instance) return problem(404, "Not Found");
    if (!instance.mac) {
      return problem(
        409,
        "Conflict",
        "no MAC address is known for this Kodi instance; set one, or let encore learn it while Kodi is awake",
      );
    }
    later(2500, () => {
      const current = instanceById(id);
      if (current)
        patchInstance(id, {
          status: {
            ...current.status,
            last_event_at: new Date().toISOString(),
            state: "connected",
          },
        });
    });
    return accepted();
  }),

  http.post(`${API}/kodi/instances/:id/default`, ({ request, params }) => {
    const denied = guard(request);
    if (denied) return denied;
    const id = Number(params.id);
    db.instances = db.instances.map((i) => ({ ...i, default_for_unlinked: i.id === id }));
    for (const instance of db.instances) emit("instance", instance);
    deriveAttention();
    save();
    return noContent();
  }),

  http.get(`${API}/kodi/discovered`, ({ request }) => guard(request) ?? json<DiscoveredInstance[]>(db.discovered)),

  http.get(`${API}/libraries`, ({ request }) => guard(request) ?? json<Library[]>(db.libraries)),

  http.patch(`${API}/libraries/:id`, async ({ request, params }) => {
    const denied = guard(request);
    if (denied) return denied;
    const library = db.libraries.find((l) => l.id === Number(params.id));
    if (!library) return problem(404, "Not Found");
    const { source_instance_id } = await body<{ source_instance_id?: number }>(request);
    if (!library.members.some((m) => m.instance_id === source_instance_id)) {
      return problem(422, "Unprocessable Entity", "The library source must be a member of the group.");
    }
    library.source_instance_id = source_instance_id ?? library.source_instance_id;
    library.syncing_instance_id = library.source_instance_id;
    for (const member of library.members) {
      const inst = instanceById(member.instance_id);
      if (inst)
        patchInstance(inst.id, {
          library: { ...inst.library, is_source: inst.id === source_instance_id },
        });
    }
    save();
    return json<Library>(library);
  }),

  http.post(`${API}/libraries/group`, async ({ request }) => {
    const denied = guard(request);
    if (denied) return denied;
    const { instance_ids = [] } = await body<{ instance_ids?: number[] }>(request);
    const members = instance_ids.map((id) => ({
      instance_id: id,
      name: instanceById(id)?.name ?? `#${id}`,
    }));
    db.libraries = db.libraries
      .map((l) => ({
        ...l,
        members: l.members.filter((m) => !instance_ids.includes(m.instance_id)),
      }))
      .filter((l) => l.members.length > 0);
    const source = instance_ids[0] ?? 0;
    const library: Library = {
      counts: { movies: 0, shows: 0, songs: 0 },
      evidence: "Grouped by hand",
      id: nextId(),
      last_sync_at: new Date().toISOString(),
      members,
      name: members.map((m) => m.name).join(" and "),
      source_instance_id: source,
      state: "manual",
      syncing_instance_id: source,
    };
    db.libraries.push(library);
    save();
    return json<Library>(library);
  }),

  http.post(`${API}/libraries/:id/split`, async ({ request, params }) => {
    const denied = guard(request);
    if (denied) return denied;
    const library = db.libraries.find((l) => l.id === Number(params.id));
    if (!library) return problem(404, "Not Found");
    const { instance_id } = await body<{ instance_id?: number }>(request);
    const member = library.members.find((m) => m.instance_id === instance_id);
    if (!member) return problem(422, "Unprocessable Entity", "That instance isn't in this library.");
    library.members = library.members.filter((m) => m.instance_id !== instance_id);
    if (library.source_instance_id === instance_id) {
      library.source_instance_id = library.members[0]?.instance_id ?? 0;
      library.syncing_instance_id = library.source_instance_id;
    }
    if (library.members.length === 1) {
      library.state = "solo";
      library.name = library.members[0]?.name ?? library.name;
      library.evidence = "";
    }
    const solo: Library = {
      counts: { ...library.counts },
      evidence: "",
      id: nextId(),
      last_sync_at: null,
      members: [member],
      name: member.name,
      source_instance_id: member.instance_id,
      state: "solo",
      syncing_instance_id: member.instance_id,
    };
    db.libraries.push(solo);
    for (const id of [instance_id, ...library.members.map((m) => m.instance_id)]) {
      const inst = id === undefined ? undefined : instanceById(id);
      if (inst) {
        patchInstance(inst.id, {
          library: {
            ...inst.library,
            group_id: inst.id === instance_id ? solo.id : library.id,
            is_source: inst.id === instance_id || inst.id === library.source_instance_id,
            shared: library.members.length > 1 && inst.id !== instance_id,
          },
        });
      }
    }
    save();
    return noContent();
  }),

  http.get(`${API}/libraries/:id/recent`, ({ request, params }) => {
    const denied = guard(request);
    if (denied) return denied;
    const limit = Number(new URL(request.url).searchParams.get("limit") ?? 20);
    return json<RecentTitle[]>((db.recent[Number(params.id)] ?? []).slice(0, limit));
  }),

  http.get(`${API}/devices`, ({ request }) => guard(request) ?? json<Device[]>(db.devices)),

  http.patch(`${API}/devices/:id`, async ({ request, params }) => {
    const denied = guard(request);
    if (denied) return denied;
    const index = db.devices.findIndex((d) => d.id === Number(params.id));
    const current = db.devices[index];
    if (!current) return problem(404, "Not Found");
    const input = await body<UpdateDeviceRequest>(request);
    if (typeof input.name === "string" && !input.name.trim()) {
      return problem(422, "Unprocessable Entity", "Give the Echo a name.", [
        { location: "body.name", message: "Give the Echo a name." },
      ]);
    }
    const next: Device = { ...current, ...input, name: input.name?.trim() ?? current.name };
    db.devices[index] = next;
    for (const r of db.requests) if (r.device_id === next.id) r.device_name = next.name;
    emit("device", next);
    deriveAttention();
    save();
    return json<Device>(next);
  }),

  http.delete(`${API}/devices/:id`, ({ request, params }) => {
    const denied = guard(request);
    if (denied) return denied;
    db.devices = db.devices.filter((d) => d.id !== Number(params.id));
    deriveAttention();
    save();
    return noContent();
  }),

  http.get(`${API}/rooms`, ({ request }) => guard(request) ?? json<Room[]>(db.rooms)),

  http.post(`${API}/rooms`, async ({ request }) => {
    const denied = guard(request);
    if (denied) return denied;
    const input = await body<Partial<CreateRoomRequest>>(request);
    if (!input.name?.trim()) {
      return problem(422, "Unprocessable Entity", "Give the room a name.", [
        { location: "body.name", message: "Give the room a name." },
      ]);
    }
    const room: Room = {
      aliases: [],
      id: nextId(),
      kodi_instance_id: input.kodi_instance_id ?? null,
      name: input.name.trim(),
    };
    db.rooms.push(room);
    save();
    return json<Room>(room, 201);
  }),

  http.patch(`${API}/rooms/:id`, async ({ request, params }) => {
    const denied = guard(request);
    if (denied) return denied;
    const room = db.rooms.find((r) => r.id === Number(params.id));
    if (!room) return problem(404, "Not Found");
    Object.assign(room, await body<UpdateRoomRequest>(request));
    save();
    return json<Room>(room);
  }),

  http.delete(`${API}/rooms/:id`, ({ request, params }) => {
    const denied = guard(request);
    if (denied) return denied;
    const id = Number(params.id);
    db.rooms = db.rooms.filter((r) => r.id !== id);
    db.devices = db.devices.map((d) => (d.room_id === id ? { ...d, room_id: null } : d));
    save();
    return noContent();
  }),

  http.post(`${API}/rooms/:id/aliases`, async ({ request, params }) => {
    const denied = guard(request);
    if (denied) return denied;
    const room = db.rooms.find((r) => r.id === Number(params.id));
    if (!room) return problem(404, "Not Found");
    const input = await body<Partial<AddRoomAliasRequest>>(request);
    const alias = input.alias?.trim().toLowerCase() ?? "";
    if (!alias) return problem(422, "Unprocessable Entity", "Type the spoken name.");
    const owner = db.rooms.find((r) => r.aliases.some((a) => a.alias === alias));
    if (owner) return problem(409, "Conflict", `“${alias}” is already used by ${owner.name}.`);
    const created: RoomAlias = { alias, id: nextId(), kind: input.kind ?? "room" };
    room.aliases.push(created);
    save();
    return json<RoomAlias>(created, 201);
  }),

  http.delete(`${API}/rooms/:id/aliases/:aliasId`, ({ request, params }) => {
    const denied = guard(request);
    if (denied) return denied;
    const room = db.rooms.find((r) => r.id === Number(params.id));
    if (room) room.aliases = room.aliases.filter((a) => a.id !== Number(params.aliasId));
    save();
    return noContent();
  }),

  http.get(`${API}/skill`, ({ request }) => guard(request) ?? json<SkillInfo>(db.skill)),

  http.put(`${API}/skill/settings`, async ({ request }) => {
    const denied = guard(request);
    if (denied) return denied;
    const input = await body<Partial<SkillSettingsInput>>(request);
    const invocation = input.invocation ?? "encore";
    if (invocation !== "encore" && invocation !== "kodi") {
      return problem(422, "Unprocessable Entity", "validation failed", [
        { location: "body.invocation", message: "must be encore or kodi" },
      ]);
    }
    if (input.content && !input.content.length) {
      return problem(422, "Unprocessable Entity", "choose at least one of movies, tv or music", [
        { location: "body.content", message: "choose at least one of movies, tv or music" },
      ]);
    }
    if (!input.locales_enabled?.length) {
      return problem(422, "Unprocessable Entity", "Enable at least one locale.", [
        { location: "body.locales_enabled", message: "Enable at least one locale." },
      ]);
    }
    db.skill = {
      ...db.skill,
      content: input.content ?? db.skill.content,
      endpoint: input.endpoint
        ? {
            url: input.endpoint.url,
          }
        : db.skill.endpoint,
      invocation,
      // The server reports the first enabled locale's name; French always uses "kodi".
      invocation_name:
        invocation === "kodi" ? "kodi" : [...input.locales_enabled].sort()[0] === "fr-FR" ? "kodi" : "encore",
      locales_enabled: input.locales_enabled,
      needs_deploy: true,
      privacy: input.privacy ?? db.skill.privacy,
      skill_name: input.skill_name ?? db.skill.skill_name,
      update_reason: "settings",
    };
    save();
    return json<SkillInfo>(db.skill);
  }),

  http.put(`${API}/skill/content`, async ({ request }) => {
    const denied = guard(request);
    if (denied) return denied;
    const input = await body<Partial<UpdateSkillContentRequest>>(request);
    if (!input.content?.length) {
      return problem(422, "Unprocessable Entity", "choose at least one of movies, tv or music", [
        { location: "body.content", message: "choose at least one of movies, tv or music" },
      ]);
    }
    const content = input.content;
    db.skill = {
      ...db.skill,
      content: (["movies", "tv", "music"] as const).filter((c) => content.includes(c)),
      ...(db.skill.last_deploy ? { needs_deploy: true, update_reason: "settings" as const } : {}),
    };
    save();
    return json<SkillInfo>(db.skill);
  }),

  http.put(`${API}/skill/amazon/credentials`, async ({ request }) => {
    const denied = guard(request);
    if (denied) return denied;
    const input = await body<Partial<SetAmazonCredentialsRequest>>(request);
    if (!input.client_id?.startsWith("amzn1.application-oa2-client.")) {
      return problem(422, "Unprocessable Entity", "Check the client ID.", [
        {
          location: "body.client_id",
          message: "A Login with Amazon client ID starts with amzn1.application-oa2-client.",
        },
      ]);
    }
    if (!input.client_secret && !db.skill.amazon.has_client_secret) {
      return problem(422, "Unprocessable Entity", "Enter the client secret.", [
        { location: "body.client_secret", message: "Enter the client secret." },
      ]);
    }
    db.skill.amazon = {
      ...db.skill.amazon,
      has_client_secret: true,
      profile_client_id: input.client_id,
    };
    save();
    return noContent();
  }),

  http.get(`${API}/skill/amazon/authorize`, ({ request }) => {
    const denied = guard(request);
    if (denied) return denied;
    const origin = new URL(request.url).origin;
    const base = db.publicUrl || "https://encore.example.net";
    return json<AuthorizeInfo>({
      return_urls: [`${base}/api/v1/oauth/lwa/callback`, "http://127.0.0.1:9090/cb"],
      // The mock "Amazon" sends you straight back to the page that asked, already authorised. Like the server, no URL until credentials are saved.
      url:
        db.skill.amazon.profile_client_id && db.skill.amazon.has_client_secret
          ? `${origin}${basePath()}${new URL(request.url).searchParams.get("then") === "setup" ? "setup" : "alexa-skill"}?mock_lwa=1`
          : "",
    });
  }),

  http.post(`${API}/skill/amazon/paste`, async ({ request }) => {
    const denied = guard(request);
    if (denied) return denied;
    const input = await body<PasteAmazonAuthorizationRequest>(request);
    if (input.redirect_url !== undefined && !/[?&]code=/.test(input.redirect_url)) {
      return problem(
        422,
        "Unprocessable Entity",
        "That URL has no code parameter. Copy the whole address after signing in.",
        [
          {
            location: "body.redirect_url",
            message: "That URL has no code parameter. Copy the whole address after signing in.",
          },
        ],
      );
    }
    if (input.refresh_token !== undefined && !input.refresh_token.startsWith("Atzr|")) {
      return problem(422, "Unprocessable Entity", "A Login with Amazon refresh token starts with Atzr|.", [
        {
          location: "body.refresh_token",
          message: "A Login with Amazon refresh token starts with Atzr|.",
        },
      ]);
    }
    connectAmazon();
    return noContent();
  }),

  http.delete(`${API}/skill/amazon`, ({ request }) => {
    const denied = guard(request);
    if (denied) return denied;
    db.skill.amazon = {
      ...db.skill.amazon,
      connected: false,
      needs_reauthorise: false,
      vendor_id: "",
    };
    save();
    return noContent();
  }),

  http.post(`${API}/skill/deploy`, ({ request }) => {
    const denied = guard(request);
    if (denied) return denied;
    if (!db.skill.amazon.connected) return problem(409, "Conflict", "Connect your Amazon developer account first.");
    if (Object.values(db.skill.privacy).some((v) => v === null)) {
      return problem(422, "Unprocessable Entity", "Answer Amazon's privacy questions before deploying.");
    }
    if (Object.values(db.deploys).some((d) => d.state === "in_progress")) {
      return problem(409, "Conflict", "A deploy is already running.");
    }
    const id = `d-${nextId()}`;
    runDeploy(id, db.scenario === "fresh" ? 350 : 700);
    save();
    return json<Accepted>({ id }, 202);
  }),

  http.get(`${API}/skill/deploy/:id`, ({ request, params }) => {
    const denied = guard(request);
    if (denied) return denied;
    const deploy = db.deploys[String(params.id)];
    return deploy ? json<Deploy>(deploy) : problem(404, "Not Found");
  }),

  http.get(
    `${API}/skill/package`,
    ({ request }) =>
      guard(request) ??
      new HttpResponse(new Uint8Array([0x50, 0x4b, 0x05, 0x06, ...new Array(18).fill(0)]), {
        headers: {
          "Content-Disposition": 'attachment; filename="encore-skill.zip"',
          "Content-Type": "application/zip",
        },
      }),
  ),

  http.get(`${API}/requests`, ({ request }) => {
    const denied = guard(request);
    if (denied) return denied;
    const params = new URL(request.url).searchParams;
    const limit = Number(params.get("limit") ?? 50);
    const before = params.get("before");
    const device = params.get("device");
    const outcome = params.get("outcome");
    const q = params.get("q")?.toLowerCase();
    if (limit === 1) scheduleHello();
    let items = db.requests;
    if (before) items = items.filter((r) => r.id < Number(before));
    if (device) items = items.filter((r) => r.device_id === Number(device));
    if (outcome) items = items.filter((r) => r.outcome === outcome);
    if (params.get("echo") === "true") items = items.filter((r) => !r.simulated);
    if (q) items = items.filter((r) => r.heard.toLowerCase().includes(q));
    const page = items.slice(0, limit);
    return json<RequestPage>({
      items: page.map(summary),
      next_before: items.length > limit ? (page.at(-1)?.id ?? null) : null,
    });
  }),

  http.get(`${API}/requests/:id`, ({ request, params }) => {
    const denied = guard(request);
    if (denied) return denied;
    const found = db.requests.find((r) => r.id === Number(params.id));
    return found ? json<RequestDetail>(found) : problem(404, "Not Found");
  }),

  http.post(`${API}/requests/:id/replay`, async ({ request, params }) => {
    const denied = guard(request);
    if (denied) return denied;
    const found = db.requests.find((r) => r.id === Number(params.id));
    if (!found) return problem(404, "Not Found");
    await delay(300);
    const aliased = db.aliases.find((a) => a.alias === found.heard.replace(/^play /, ""));
    const match: Match | null = found.match
      ? aliased
        ? {
            ...found.match,
            candidates: [aliasCandidate(aliased), ...found.match.candidates.filter((c) => c.title !== aliased.title)],
            decision: "play",
            margin: 1,
          }
        : found.match
      : null;
    return json<Replay>({
      match,
      speech: found.speech,
      would_call: found.kodi_calls.map((c) => ({ method: c.method, params: {} })),
    });
  }),

  http.post(`${API}/match`, async ({ request }) => {
    const denied = guard(request);
    if (denied) return denied;
    const input = await body<Partial<TestMatchRequest>>(request);
    if (!input.query?.trim()) {
      return problem(422, "Unprocessable Entity", "Type what Alexa heard.", [
        { location: "body.query", message: "Type what Alexa heard." },
      ]);
    }
    await delay(150);
    const query = input.query.trim().toLowerCase();
    const aliased = db.aliases.find((a) => a.library_id === input.library_id && a.alias === query);
    if (aliased) {
      return json<MatchResult>({
        candidates: [aliasCandidate(aliased)],
        decision: "play",
        margin: 1,
        normalized: query,
      });
    }
    return json<MatchResult>(fuzzyMatch(input.query, input.kinds ?? undefined));
  }),

  http.get(`${API}/aliases`, ({ request }) => {
    const denied = guard(request);
    if (denied) return denied;
    const libraryId = Number(new URL(request.url).searchParams.get("library_id"));
    return json<TitleAlias[]>(db.aliases.filter((a) => !libraryId || a.library_id === libraryId));
  }),

  http.post(`${API}/aliases`, async ({ request }) => {
    const denied = guard(request);
    if (denied) return denied;
    const input = await body<CreateAliasRequest>(request);
    const alias = input.alias?.trim().toLowerCase();
    if (!alias) return problem(422, "Unprocessable Entity", "Type the alias.");
    if (db.aliases.some((a) => a.library_id === input.library_id && a.alias === alias)) {
      return problem(409, "Conflict", `“${alias}” is already an alias in this library.`);
    }
    const created: TitleAlias = {
      alias,
      id: nextId(),
      kind: input.kind,
      library_id: input.library_id,
      media_id: input.media_id,
      title: fuzzyMatch(alias).candidates.find((c) => c.media_id === input.media_id)?.title ?? `#${input.media_id}`,
    };
    db.aliases.push(created);
    save();
    return json<TitleAlias>(created, 201);
  }),

  http.delete(`${API}/aliases/:id`, ({ request, params }) => {
    const denied = guard(request);
    if (denied) return denied;
    db.aliases = db.aliases.filter((a) => a.id !== Number(params.id));
    save();
    return noContent();
  }),

  http.get(`${API}/settings`, ({ request }) => guard(request) ?? json<Settings>(db.settings)),

  http.patch(`${API}/settings`, async ({ request }) => {
    const denied = guard(request);
    if (denied) return denied;
    const input = await body<SettingsPatch>(request);
    const touched = flattenKeys(input);
    const managed = db.settings.managed;
    const blocked = touched.filter((key) => managed.some((m) => key === m || key.startsWith(`${m}.`)));
    if (blocked.length) {
      return problem(409, "Conflict", `Managed by config: ${blocked.join(", ")}.`);
    }
    const { auth, storage } = input;
    const secrets = {
      client_secret: auth?.oidc?.client_secret,
      dav_password: storage?.webdav?.password,
      secret_key: storage?.s3?.secret_key,
    };
    const clean = JSON.parse(JSON.stringify(input)) as Record<string, Record<string, Record<string, unknown>>>;
    // Read-only fields are accepted and ignored, like the server does.
    delete clean.auth?.local?.username;
    delete clean.auth?.local?.totp_enabled;
    delete clean.auth?.oidc?.client_secret;
    delete clean.storage?.s3?.secret_key;
    delete clean.storage?.webdav?.password;
    db.settings = mergeDeep(db.settings, clean);
    if (secrets.client_secret) db.settings.auth.oidc.has_client_secret = true;
    if (secrets.secret_key) db.settings.storage.s3.has_secret_key = true;
    if (secrets.dav_password) db.settings.storage.webdav.has_password = true;
    if (input.auth?.mode) db.authMode = input.auth.mode;
    save();
    return json<Settings>(db.settings);
  }),

  http.post(`${API}/settings/storage/test`, async ({ request }) => {
    const denied = guard(request);
    if (denied) return denied;
    await delay(400);
    const { backend, s3, webdav } = db.settings.storage;
    if (backend === "s3" && (!s3.bucket || !s3.has_secret_key)) {
      return json<StorageTest>({ error: "Set a bucket and secret key first.", ms: 0, ok: false });
    }
    if (backend === "webdav" && !webdav.has_password) {
      return json<StorageTest>({
        error: "401 Unauthorized from the WebDAV server. Check the app password.",
        ms: 211,
        ok: false,
      });
    }
    return json<StorageTest>({ error: "", ms: 64, ok: true });
  }),

  http.post(`${API}/settings/secrets/rotate`, ({ request }) => {
    const denied = guard(request);
    if (denied) return denied;
    const n = Number(db.settings.secrets.key_id.replace(/\D/g, "")) || 1;
    db.settings.secrets = {
      ...db.settings.secrets,
      key_id: `k${n + 1}`,
      rotated_at: new Date().toISOString(),
    };
    save();
    return noContent();
  }),

  http.post(`${API}/settings/backups/run`, ({ request }) => {
    const denied = guard(request);
    if (denied) return denied;
    later(1500, () => {
      db.settings.storage.backups.last_backup_at = new Date().toISOString();
    });
    return accepted();
  }),

  http.get(`${API}/config/export`, ({ request }) => {
    const denied = guard(request);
    if (denied) return denied;
    const secrets = new URL(request.url).searchParams.get("include_secrets") === "true";
    const yaml = [
      "# Encore configuration export",
      "kodi:",
      ...db.instances.map(
        (i) =>
          `  - name: ${JSON.stringify(i.name)}\n    url: ${i.url}${secrets ? "\n    password: !encrypted 'v1:…'" : ""}`,
      ),
      "rooms:",
      ...db.rooms.map(
        (r) =>
          `  - name: ${JSON.stringify(r.name)}\n    aliases: [${r.aliases.map((a) => JSON.stringify(a.alias)).join(", ")}]`,
      ),
    ].join("\n");
    return new HttpResponse(yaml, {
      headers: {
        "Content-Disposition": 'attachment; filename="encore.yml"',
        "Content-Type": "application/yaml",
      },
    });
  }),

  http.post(`${API}/config/import`, async ({ request }) => {
    const denied = guard(request);
    if (denied) return denied;
    const text = await request.text();
    if (!text.trim()) return problem(422, "Unprocessable Entity", "The file is empty.");
    if (!/^\s*[a-z_]+:/m.test(text)) return problem(422, "Unprocessable Entity", "That doesn't look like YAML.");
    const keys = [...text.matchAll(/^([a-z_]+):/gm)].map((m) => m[1] ?? "");
    return json<ImportResult>({
      applied: keys.filter((k) => k !== "secrets"),
      skipped: keys.filter((k) => k === "secrets").map(() => "secrets (encrypted with a different key)"),
    });
  }),
];

/** The SSE handler for `/api/v1/events`. Browser only: Node has no EventSource, so tests use `FakeEventSource` instead. */
export function createEventsHandler() {
  // The app keeps one EventSource per page, so a new connection replaces the previous (now closed) one.
  let live: ((message: Emitted) => void) | undefined;
  return sse(`${API}/events`, ({ client, request }) => {
    if (live) subscribers.delete(live);
    const send = (message: Emitted) => {
      eventId += 1;
      client.send({
        data: JSON.stringify(message.data),
        event: message.event,
        id: String(eventId),
      } as never);
    };
    live = send;
    subscribers.add(send);
    request.signal.addEventListener("abort", () => subscribers.delete(send));
  });
}

function connectAmazon() {
  db.skill.amazon = {
    connected: true,
    has_client_secret: true,
    needs_reauthorise: false,
    profile_client_id: db.skill.amazon.profile_client_id || "amzn1.application-oa2-client.4f1c8d2b7a6e4e92a",
    vendor_id: "M2X8K4P1QF",
  };
  save();
}

/** The one-time code the mock accepts for TOTP sign-in and enrolment. */
const TOTP_CODE = "123456";

function aliasCandidate(alias: TitleAlias): Candidate {
  return {
    breakdown: { alias: alias.alias, boost: 0, jaro_winkler: 1, osa: 1, phonetic: 1, token_set: 1 },
    kind: alias.kind,
    media_id: alias.media_id,
    score: 1,
    title: alias.title,
    year: 0,
  };
}

/** Mock "Login with Amazon" round trip: the authorize URL returns here with `?mock_lwa=1`. */
export function completeMockLwa() {
  connectAmazon();
}
