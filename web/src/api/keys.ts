import type { RequestFilters } from "@api/requests";

/** TanStack Query keys. Prefixes are shared on purpose: invalidating `keys.instances` also refreshes each instance detail. */
export const keys = {
  aliases: (libraryId: number) => ["aliases", libraryId] as const,
  auth: ["auth", "state"] as const,
  authorize: ["skill", "authorize"] as const,
  // Not under "skill": invalidating the skill must not refetch (and clobber) a deploy that SSE is updating.
  deploy: (id: string) => ["deploy", id] as const,
  devices: ["devices"] as const,
  discovered: ["kodi", "discovered"] as const,
  instance: (id: number) => ["kodi", "instances", id] as const,
  instances: ["kodi", "instances"] as const,
  libraries: ["libraries"] as const,
  overview: ["overview"] as const,
  reachability: (url: string) => ["setup", "reachability", url] as const,
  recent: (libraryId: number, limit: number) => ["libraries", libraryId, "recent", limit] as const,
  request: (id: number) => ["requests", "detail", id] as const,
  requestList: (filters: RequestFilters) => ["requests", "list", filters] as const,
  requests: ["requests", "list"] as const,
  rooms: ["rooms"] as const,
  settings: ["settings"] as const,
  skill: ["skill"] as const,
};
