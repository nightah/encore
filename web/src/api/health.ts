import { queryOptions, useQuery } from "@tanstack/react-query";

import { orvalFetch } from "@api/orvalFetch";

/** GET /api/v1/healthz: served outside the admin API, so it is not in the OpenAPI spec. */
export interface Health {
  status: string;
  version?: string;
}

export function health(signal?: AbortSignal): Promise<Health> {
  return orvalFetch<Health>("/healthz", { signal: signal ?? null });
}

const healthQuery = queryOptions({
  queryFn: ({ signal }) => health(signal),
  queryKey: ["health"],
  refetchInterval: 30_000,
  retry: 1,
});

export function useHealth() {
  return useQuery(healthQuery);
}
