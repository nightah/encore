import { queryOptions, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import {
  createKodiInstance,
  deleteKodiInstance,
  listDiscoveredKodi,
  listKodiInstances,
  setDefaultKodiInstance,
  syncKodiInstance,
  testKodiInstance,
  updateKodiInstance,
  wakeKodiInstance,
} from "@api/generated/endpoints/kodi/kodi";
import {
  groupLibrary,
  listLibraries,
  listRecentTitles,
  splitLibrary,
  updateLibrary,
} from "@api/generated/endpoints/libraries/libraries";
import type { Instance, InstanceInput, Library } from "@api/generated/model";
import { keys } from "@api/keys";
import i18n from "@i18n/index";

export const syncInstance = (id: number) => syncKodiInstance(id);

const instancesQuery = queryOptions({
  queryFn: ({ signal }) => listKodiInstances({ signal }),
  queryKey: keys.instances,
});

export function useInstances() {
  return useQuery(instancesQuery);
}

export function useDiscovered() {
  return useQuery({
    queryFn: ({ signal }) => listDiscoveredKodi({ signal }),
    queryKey: keys.discovered,
    refetchInterval: 30_000,
  });
}

function useInvalidateKodi() {
  const queryClient = useQueryClient();
  return () =>
    Promise.all([
      queryClient.invalidateQueries({ queryKey: keys.instances }),
      queryClient.invalidateQueries({ queryKey: keys.discovered }),
      queryClient.invalidateQueries({ queryKey: keys.libraries }),
      queryClient.invalidateQueries({ queryKey: keys.overview }),
    ]);
}

export function useCreateInstance() {
  const invalidate = useInvalidateKodi();
  return useMutation({
    meta: { inline: true, success: i18n.t("toast.instanceAdded") },
    mutationFn: (body: InstanceInput) => createKodiInstance(body),
    onSuccess: invalidate,
  });
}

export function useUpdateInstance() {
  const invalidate = useInvalidateKodi();
  return useMutation({
    meta: { inline: true, success: i18n.t("toast.instanceSaved") },
    mutationFn: ({ id, body }: { id: number; body: InstanceInput }) => updateKodiInstance(id, body),
    onSuccess: invalidate,
  });
}

export function useDeleteInstance() {
  const invalidate = useInvalidateKodi();
  return useMutation({
    meta: { success: i18n.t("toast.instanceRemoved") },
    mutationFn: (id: number) => deleteKodiInstance(id),
    onSuccess: invalidate,
  });
}

export function useTestInstance() {
  return useMutation({ mutationFn: (id: number) => testKodiInstance(id) });
}

export function useSyncInstance() {
  return useMutation({
    meta: { success: i18n.t("toast.syncStarted") },
    mutationFn: (id: number) => syncKodiInstance(id),
  });
}

export function useWakeInstance() {
  return useMutation({
    meta: { success: i18n.t("toast.wakeSent") },
    mutationFn: (id: number) => wakeKodiInstance(id),
  });
}

export function useMakeDefaultInstance() {
  const queryClient = useQueryClient();
  return useMutation({
    meta: { success: i18n.t("toast.defaultInstance") },
    mutationFn: (id: number) => setDefaultKodiInstance(id),
    onSuccess: (_data, id) => {
      queryClient.setQueryData<Instance[]>(keys.instances, (list) =>
        list?.map((item) => ({ ...item, default_for_unlinked: item.id === id })),
      );
      return queryClient.invalidateQueries({ queryKey: keys.instances });
    },
  });
}

const librariesQuery = queryOptions({
  queryFn: ({ signal }) => listLibraries({ signal }),
  queryKey: keys.libraries,
});

export function useLibraries() {
  return useQuery(librariesQuery);
}

export function useRecentTitles(libraryId: number | undefined, limit = 20) {
  return useQuery({
    enabled: libraryId !== undefined,
    queryFn: ({ signal }) => listRecentTitles(libraryId ?? 0, { limit }, { signal }),
    queryKey: keys.recent(libraryId ?? 0, limit),
  });
}

export function useSetLibrarySource() {
  const queryClient = useQueryClient();
  return useMutation({
    meta: { success: i18n.t("toast.librarySource") },
    mutationFn: ({ id, sourceInstanceId }: { id: number; sourceInstanceId: number }) =>
      updateLibrary(id, { source_instance_id: sourceInstanceId }),
    onSuccess: (library) => {
      queryClient.setQueryData<Library[]>(keys.libraries, (list) =>
        list?.map((item) => (item.id === library.id ? library : item)),
      );
      return queryClient.invalidateQueries({ queryKey: keys.instances });
    },
  });
}

export function useGroupLibrary() {
  const invalidate = useInvalidateKodi();
  return useMutation({
    meta: { success: i18n.t("toast.libraryGrouped") },
    mutationFn: (instanceIds: number[]) => groupLibrary({ instance_ids: instanceIds }),
    onSuccess: invalidate,
  });
}

export function useSplitLibrary() {
  const invalidate = useInvalidateKodi();
  return useMutation({
    meta: { success: i18n.t("toast.librarySplit") },
    mutationFn: ({ id, instanceId }: { id: number; instanceId: number }) =>
      splitLibrary(id, { instance_id: instanceId }),
    onSuccess: invalidate,
  });
}
