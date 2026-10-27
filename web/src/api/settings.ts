import { queryOptions, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { buildUrl } from "@api/client";
import {
  getSettings,
  importConfig,
  rotateSecretsKey,
  runBackup,
  testStorage,
  updateSettings,
} from "@api/generated/endpoints/settings/settings";
import type { Settings, SettingsPatch } from "@api/generated/model";
import { keys } from "@api/keys";
import i18n from "@i18n/index";

export const configExportUrl = (includeSecrets: boolean) =>
  buildUrl("/config/export", { include_secrets: includeSecrets });

const settingsQuery = queryOptions({
  queryFn: ({ signal }) => getSettings({ signal }),
  queryKey: keys.settings,
});

export function useSettings() {
  return useQuery(settingsQuery);
}

/** Settings that come from encore.yml or ENCORE_* are listed in `managed` as dotted settings-document paths (the server translates config keys such as `server.trusted_proxies`). */
export function isManaged(settings: Settings | undefined, key: string): boolean {
  if (!settings) return false;
  return settings.managed.some((entry) => entry === key || key.startsWith(`${entry}.`));
}

export function useSaveSettings() {
  const queryClient = useQueryClient();
  return useMutation({
    meta: { inline: true, success: i18n.t("toast.settingsSaved") },
    mutationFn: (body: SettingsPatch) => updateSettings(body),
    onSuccess: (settings) => queryClient.setQueryData(keys.settings, settings),
  });
}

export function useTestStorage() {
  return useMutation({ meta: { inline: true }, mutationFn: () => testStorage() });
}

export function useRotateSecrets() {
  const queryClient = useQueryClient();
  return useMutation({
    meta: { success: i18n.t("toast.secretsRotated") },
    mutationFn: () => rotateSecretsKey(),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: keys.settings }),
  });
}

export function useRunBackup() {
  return useMutation({ meta: { success: i18n.t("toast.backupStarted") }, mutationFn: () => runBackup() });
}

export function useImportConfig() {
  const queryClient = useQueryClient();
  return useMutation({
    meta: { inline: true },
    mutationFn: (yaml: string) => importConfig(new Blob([yaml], { type: "application/yaml" })),
    onSuccess: () => queryClient.invalidateQueries(),
  });
}
