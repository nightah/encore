import { infiniteQueryOptions, skipToken, useInfiniteQuery, useMutation, useQuery } from "@tanstack/react-query";

import { getRequest, listRequests, replayRequest } from "@api/generated/endpoints/requests/requests";
import type { RequestPage, RequestSummaryOutcome } from "@api/generated/model";
import { keys } from "@api/keys";

const PAGE_SIZE = 50;

/** The request log's filters, as kept in the URL and the query key. */
export interface RequestFilters {
  device?: number | undefined;
  outcome?: RequestSummaryOutcome | undefined;
  q?: string | undefined;
}

export { listRequests };

function requestsQuery(filters: RequestFilters) {
  return infiniteQueryOptions({
    // Typed here rather than inferred from queryFn, so the options can be in any order.
    getNextPageParam: (last: RequestPage) => last.next_before ?? undefined,
    initialPageParam: undefined as number | undefined,
    queryFn: ({ pageParam, signal }) =>
      listRequests(
        {
          limit: PAGE_SIZE,
          ...(pageParam !== undefined ? { before: pageParam } : {}),
          ...(filters.device !== undefined ? { device: filters.device } : {}),
          ...(filters.outcome ? { outcome: filters.outcome } : {}),
          ...(filters.q ? { q: filters.q } : {}),
        },
        { signal },
      ),
    queryKey: keys.requestList(filters),
  });
}

export function useRequests(filters: RequestFilters) {
  return useInfiniteQuery(requestsQuery(filters));
}

export function useRequest(id: number | undefined) {
  return useQuery({
    queryFn: id === undefined ? skipToken : ({ signal }) => getRequest(id, { signal }),
    queryKey: keys.request(id ?? 0),
  });
}

export function useReplayRequest() {
  return useMutation({ meta: { inline: true }, mutationFn: (id: number) => replayRequest(id) });
}
