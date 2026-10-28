import type { ReactNode } from "react";

import { QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import { http } from "msw";
import { beforeEach, describe, expect, it } from "vitest";

import { setCsrfToken } from "@api/client";
import type { Settings } from "@api/generated/model";
import { keys } from "@api/keys";
import {
  configExportUrl,
  isManaged,
  useImportConfig,
  useRotateSecrets,
  useRunBackup,
  useSaveSettings,
  useSettings,
  useTestStorage,
} from "@api/settings";
import { getDb } from "@mocks/handlers";
import { server } from "@mocks/node";
import { createQueryClient } from "@src/router";
import { recordRequests } from "@test/utils";

let queryClient: ReturnType<typeof createQueryClient>;
const wrapper = ({ children }: { children: ReactNode }) => (
  <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
);

beforeEach(() => {
  queryClient = createQueryClient();
  queryClient.setDefaultOptions({ queries: { retry: false } });
  setCsrfToken(getDb().csrf);
});

describe("isManaged", () => {
  const settings = { managed: ["server.trusted_proxies", "storage"] } as unknown as Settings;

  it.each([
    ["server.trusted_proxies", true],
    ["server.trusted_proxies.0", true],
    ["storage", true],
    ["storage.s3.bucket", true],
    ["server", false],
    ["server.trusted", false],
    ["server.trusted_proxies_extra", false],
    ["storagebox", false],
    ["auth.mode", false],
  ])("says whether %s comes from config", (key, expected) => {
    expect(isManaged(settings, key)).toBe(expected);
  });

  it("says nothing is managed before settings load, or when nothing is", () => {
    expect(isManaged(undefined, "storage")).toBe(false);
    expect(isManaged({ managed: [] } as unknown as Settings, "storage")).toBe(false);
  });
});

describe("configExportUrl", () => {
  it("asks for secrets only when told to", () => {
    expect(configExportUrl(true)).toBe("/api/v1/config/export?include_secrets=true");
    expect(configExportUrl(false)).toBe("/api/v1/config/export?include_secrets=false");
  });
});

describe("settings hooks", () => {
  it("loads the settings", async () => {
    const { result } = renderHook(() => useSettings(), { wrapper });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toEqual(getDb().settings);
  });

  it("keeps the saved settings the server returns, without refetching", async () => {
    const { result } = renderHook(() => ({ save: useSaveSettings(), settings: useSettings() }), { wrapper });
    await waitFor(() => expect(result.current.settings.isSuccess).toBe(true));
    const requests = recordRequests();

    await act(() => result.current.save.mutateAsync({ logging: { level: "debug" } }));

    expect(requests.map((r) => r.method)).toEqual(["PATCH"]);
    expect(queryClient.getQueryData<Settings>(keys.settings)?.logging.level).toBe("debug");
  });

  it("leaves the cached settings alone when saving fails", async () => {
    getDb().settings.managed = ["logging"];
    const { result } = renderHook(() => ({ save: useSaveSettings(), settings: useSettings() }), { wrapper });
    await waitFor(() => expect(result.current.settings.isSuccess).toBe(true));
    const before = queryClient.getQueryData<Settings>(keys.settings);

    await act(async () => {
      await result.current.save.mutateAsync({ logging: { level: "debug" } }).catch(() => undefined);
    });

    await waitFor(() => expect(result.current.save.error).toMatchObject({ status: 409 }));
    expect(queryClient.getQueryData<Settings>(keys.settings)).toBe(before);
  });

  it("refreshes the settings after rotating the encryption key", async () => {
    queryClient.setQueryData(keys.settings, getDb().settings);
    const { result } = renderHook(() => useRotateSecrets(), { wrapper });
    await act(() => result.current.mutateAsync());
    expect(queryClient.getQueryState(keys.settings)?.isInvalidated).toBe(true);
  });

  it("tests storage and starts a backup", async () => {
    const requests = recordRequests();
    const { result } = renderHook(() => ({ backup: useRunBackup(), test: useTestStorage() }), { wrapper });
    const outcome = await act(() => result.current.test.mutateAsync());
    await act(() => result.current.backup.mutateAsync());
    expect(outcome).toMatchObject({ ok: expect.any(Boolean) });
    expect(requests.map((r) => `${r.method} ${r.path}`)).toEqual([
      "POST /settings/storage/test",
      "POST /settings/backups/run",
    ]);
  });

  it("uploads a configuration as YAML and refreshes everything", async () => {
    let upload: { type: string | null; text: string; csrf: string | null } | undefined;
    server.events.on("request:start", async ({ request }) => {
      if (!request.url.endsWith("/config/import")) return;
      upload = {
        csrf: request.headers.get("X-CSRF-Token"),
        text: await request.clone().text(),
        type: request.headers.get("Content-Type"),
      };
    });
    queryClient.setQueryData(keys.settings, {});
    queryClient.setQueryData(keys.instances, []);
    const { result } = renderHook(() => useImportConfig(), { wrapper });

    const imported = await act(() => result.current.mutateAsync("kodi:\n  - name: Lounge\nsecrets: {}\n"));

    expect(upload).toEqual({
      csrf: getDb().csrf,
      text: "kodi:\n  - name: Lounge\nsecrets: {}\n",
      type: "application/yaml",
    });
    expect(imported).toMatchObject({ applied: ["kodi"] });
    expect(queryClient.getQueryState(keys.settings)?.isInvalidated).toBe(true);
    expect(queryClient.getQueryState(keys.instances)?.isInvalidated).toBe(true);
  });

  it("refreshes nothing when the import is refused", async () => {
    server.use(http.post("*/api/v1/config/import", () => Response.json({ detail: "bad" }, { status: 422 })));
    queryClient.setQueryData(keys.settings, {});
    const { result } = renderHook(() => useImportConfig(), { wrapper });
    await act(async () => {
      await result.current.mutateAsync("not yaml").catch(() => undefined);
    });
    expect(queryClient.getQueryState(keys.settings)?.isInvalidated).toBe(false);
  });
});
