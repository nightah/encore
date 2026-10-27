import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { createAlias, deleteAlias, listAliases, testMatch } from "@api/generated/endpoints/match/match";
import type { CreateAliasRequest, TestMatchRequest } from "@api/generated/model";
import { keys } from "@api/keys";
import i18n from "@i18n/index";

/** The match tester is a read-only probe, but it is a POST, so it is a mutation. */
export function useMatch() {
  return useMutation({
    meta: { inline: true },
    mutationFn: (body: TestMatchRequest) => testMatch(body),
  });
}

export function useAliases(libraryId: number | undefined) {
  return useQuery({
    enabled: libraryId !== undefined,
    queryFn: ({ signal }) => listAliases({ library_id: libraryId ?? 0 }, { signal }),
    queryKey: keys.aliases(libraryId ?? 0),
  });
}

export function useCreateAlias() {
  const queryClient = useQueryClient();
  return useMutation({
    meta: { success: i18n.t("toast.aliasSaved") },
    mutationFn: (body: CreateAliasRequest) => createAlias(body),
    onSuccess: (alias) => queryClient.invalidateQueries({ queryKey: keys.aliases(alias.library_id) }),
  });
}

export function useDeleteAlias() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => deleteAlias(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["aliases"] }),
  });
}
