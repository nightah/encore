import { queryOptions, useQuery } from "@tanstack/react-query";

import { getOverview } from "@api/generated/endpoints/overview/overview";
import { keys } from "@api/keys";

const overviewQuery = queryOptions({
  queryFn: ({ signal }) => getOverview({ signal }),
  queryKey: keys.overview,
  // SSE keeps it fresh; this is a safety net for missed events.
  refetchInterval: 60_000,
});

export function useOverview() {
  return useQuery(overviewQuery);
}
