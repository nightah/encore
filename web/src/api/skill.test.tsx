import type { ReactNode } from "react";

import { QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { setCsrfToken } from "@api/client";
import type { SkillInfo } from "@api/generated/model";
import { keys } from "@api/keys";
import {
  getAuthorize,
  skillPackageUrl,
  skillSettingsFrom,
  useActiveDeploy,
  useDeploy,
  useDisconnectAmazon,
  useInvocationName,
  usePasteAmazon,
  useSaveAmazonCredentials,
  useSaveSkillContent,
  useSaveSkillSettings,
  useSkill,
  useStartDeploy,
} from "@api/skill";
import { getDb } from "@mocks/handlers";
import { createQueryClient } from "@src/router";
import { server } from "@test/server";
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

const cachedSkill = () => queryClient.getQueryData<SkillInfo>(keys.skill);
const invalidated = (key: readonly unknown[]) => queryClient.getQueryState(key)?.isInvalidated;

const skillWith = (lastDeploy: SkillInfo["last_deploy"]) => ({ last_deploy: lastDeploy }) as SkillInfo;

describe("useActiveDeploy", () => {
  it("follows a deploy already in progress and keeps it after it ends", () => {
    const running = skillWith({ id: "d-7", state: "in_progress" } as SkillInfo["last_deploy"]);
    const { result, rerender } = renderHook(({ skill }) => useActiveDeploy(skill), {
      initialProps: { skill: running },
    });

    expect(result.current[0]).toBe("d-7");

    rerender({ skill: skillWith({ id: "d-7", state: "succeeded" } as SkillInfo["last_deploy"]) });
    expect(result.current[0]).toBe("d-7");
  });

  it("follows nothing when no deploy is running, until one is started", () => {
    const { result } = renderHook(() =>
      useActiveDeploy(skillWith({ id: "d-6", state: "failed" } as SkillInfo["last_deploy"])),
    );

    expect(result.current[0]).toBeUndefined();

    act(() => result.current[1]("d-8"));
    expect(result.current[0]).toBe("d-8");
  });
});

describe("skillSettingsFrom", () => {
  const skill = {
    content: ["movies"],
    endpoint: { url: "https://encore.example.net/alexa" },
    invocation: "encore",
    invocation_name: "encore",
    locales_enabled: ["en-AU"],
    privacy: { allows_purchases: false },
    skill_id: "amzn1.ask.skill.1",
    skill_name: "Encore",
  } as unknown as SkillInfo;

  it("copies the editable settings and nothing read-only", () => {
    expect(skillSettingsFrom(skill)).toEqual({
      content: ["movies"],
      endpoint: { url: "https://encore.example.net/alexa" },
      invocation: "encore",
      locales_enabled: ["en-AU"],
      privacy: { allows_purchases: false },
      skill_name: "Encore",
    });
  });

  it("applies the changes it is given", () => {
    expect(skillSettingsFrom(skill, { invocation: "kodi", locales_enabled: ["en-AU", "de-DE"] })).toMatchObject({
      invocation: "kodi",
      locales_enabled: ["en-AU", "de-DE"],
      skill_name: "Encore",
    });
  });
});

describe("Amazon authorisation", () => {
  it("refuses to redirect before the Login with Amazon credentials are saved", async () => {
    getDb().skill.amazon = { ...getDb().skill.amazon, has_client_secret: false, profile_client_id: "" };
    await expect(getAuthorize()).rejects.toThrow("Save the Login with Amazon client ID and secret first.");
  });

  it("returns the consent URL, asking Amazon to come back to the page named", async () => {
    getDb().skill.amazon = {
      ...getDb().skill.amazon,
      has_client_secret: true,
      profile_client_id: "amzn1.application-oa2-client.abc",
    };
    const requests = recordRequests();
    const info = await getAuthorize("setup");
    expect(info.url).toContain("setup?mock_lwa=1");
    expect(requests[0]?.path).toBe("/skill/amazon/authorize?then=setup");

    await getAuthorize();
    expect(requests[1]?.path).toBe("/skill/amazon/authorize");
  });

  it("refreshes the skill and overview after credentials, pasting and disconnecting", async () => {
    const { result } = renderHook(
      () => ({ credentials: useSaveAmazonCredentials(), disconnect: useDisconnectAmazon(), paste: usePasteAmazon() }),
      { wrapper },
    );
    const steps: Array<() => Promise<unknown>> = [
      () =>
        result.current.credentials.mutateAsync({
          client_id: "amzn1.application-oa2-client.abc",
          client_secret: "secret",
        }),
      () => result.current.paste.mutateAsync({ refresh_token: "Atzr|token" }),
      () => result.current.disconnect.mutateAsync(),
    ];
    for (const step of steps) {
      queryClient.setQueryData(keys.skill, {});
      queryClient.setQueryData(keys.overview, {});
      await act(step);
      expect(invalidated(keys.skill)).toBe(true);
      expect(invalidated(keys.overview)).toBe(true);
    }
  });
});

describe("skill hooks", () => {
  it("names the invocation, without guessing one until the skill loads", async () => {
    getDb().skill.invocation_name = "kodi";
    const { result } = renderHook(() => useInvocationName(), { wrapper });
    expect(result.current).toBeUndefined();
    await waitFor(() => expect(result.current).toBe("kodi"));
  });

  it("names the invocation encore when the server sends none", async () => {
    getDb().skill.invocation_name = "";
    const { result } = renderHook(() => ({ name: useInvocationName(), skill: useSkill() }), { wrapper });
    await waitFor(() => expect(result.current.skill.isSuccess).toBe(true));
    expect(result.current.name).toBe("encore");
  });

  it("shows saved settings at once, marks the skill as needing a deploy and refetches", async () => {
    const { result } = renderHook(() => ({ save: useSaveSkillSettings(), skill: useSkill() }), { wrapper });
    await waitFor(() => expect(result.current.skill.isSuccess).toBe(true));
    getDb().skill.needs_deploy = false;
    const skill = cachedSkill() as SkillInfo;
    const requests = recordRequests();

    act(() => {
      result.current.save.mutate(
        skillSettingsFrom(skill, { endpoint: { url: "https://new.example.net/alexa" }, skill_name: "Cinema" }),
      );
    });

    expect(cachedSkill()).toMatchObject({
      endpoint: { url: "https://new.example.net/alexa" },
      needs_deploy: true,
      skill_name: "Cinema",
    });
    await waitFor(() => expect(requests.map((r) => `${r.method} ${r.path}`)).toContain("GET /skill"));
    expect(requests[0]).toMatchObject({ method: "PUT", path: "/skill/settings" });
  });

  it("refetches the real skill after a failed save, undoing the optimistic change", async () => {
    const { result } = renderHook(() => ({ save: useSaveSkillSettings(null), skill: useSkill() }), { wrapper });
    await waitFor(() => expect(result.current.skill.isSuccess).toBe(true));
    const skill = cachedSkill() as SkillInfo;

    await act(async () => {
      await result.current.save.mutateAsync(skillSettingsFrom(skill, { locales_enabled: [] })).catch(() => undefined);
    });

    await waitFor(() => expect(cachedSkill()?.locales_enabled).toEqual(getDb().skill.locales_enabled));
    expect(queryClient.getMutationCache().getAll()[0]?.meta).toEqual({ inline: true });
  });

  it("shows saved content at once and refreshes the libraries", async () => {
    queryClient.setQueryData(keys.libraries, []);
    const { result } = renderHook(() => ({ save: useSaveSkillContent(), skill: useSkill() }), { wrapper });
    await waitFor(() => expect(result.current.skill.isSuccess).toBe(true));

    act(() => result.current.save.mutate(["music"]));
    expect(cachedSkill()?.content).toEqual(["music"]);

    await waitFor(() => expect(result.current.save.isSuccess).toBe(true));
    expect(invalidated(keys.libraries)).toBe(true);
  });

  it("doesn't make up a skill from a save sent before the skill has loaded", async () => {
    const requests = recordRequests();
    const { result } = renderHook(() => ({ content: useSaveSkillContent(), settings: useSaveSkillSettings(null) }), {
      wrapper,
    });

    act(() => result.current.content.mutate(["music"]));
    act(() => result.current.settings.mutate(skillSettingsFrom(getDb().skill, { skill_name: "Cinema" })));
    expect(cachedSkill()).toBeUndefined();

    await waitFor(() => expect(result.current.content.isSuccess).toBe(true));
    await waitFor(() => expect(result.current.settings.isPending).toBe(false));
    expect(cachedSkill()).toBeUndefined();
    expect(requests.map((r) => `${r.method} ${r.path}`)).toEqual(
      expect.arrayContaining(["PUT /skill/content", "PUT /skill/settings"]),
    );
  });

  it.each([
    [true, { force: true }],
    [false, {}],
    [undefined, {}],
  ])("asks for a forced deploy only when told to (%s)", async (force, body) => {
    getDb().skill.amazon = { ...getDb().skill.amazon, connected: true };
    for (const key of Object.keys(getDb().skill.privacy)) {
      (getDb().skill.privacy as unknown as Record<string, unknown>)[key] = false;
    }
    const requests = recordRequests();
    const { result } = renderHook(() => useStartDeploy(), { wrapper });

    const started = await act(() => result.current.mutateAsync(force));
    expect(started).toMatchObject({ id: expect.stringMatching(/^d-/) });
    expect(requests[0]?.body).toEqual(body);
  });

  it("links the skill package for download", () => {
    expect(skillPackageUrl()).toBe("/api/v1/skill/package");
  });
});

describe("useDeploy", () => {
  afterEach(() => vi.useRealTimers());

  it("polls a running deploy every three seconds and stops once it ends", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const states: Array<"in_progress" | "succeeded"> = ["in_progress", "in_progress", "succeeded"];
    let fetches = 0;
    server.use(
      http.get("*/api/v1/skill/deploy/:id", ({ params }) =>
        HttpResponse.json({ error: "", events: [], id: params.id, state: states[Math.min(fetches++, 2)] }),
      ),
    );
    const { result } = renderHook(() => useDeploy("d-1"), { wrapper });
    await waitFor(() => expect(result.current.data?.state).toBe("in_progress"));
    expect(fetches).toBe(1);

    await act(() => vi.advanceTimersByTimeAsync(2_900));
    expect(fetches).toBe(1);
    await act(() => vi.advanceTimersByTimeAsync(200));
    await waitFor(() => expect(fetches).toBe(2));
    await act(() => vi.advanceTimersByTimeAsync(3_000));
    await waitFor(() => expect(result.current.data?.state).toBe("succeeded"));

    await act(() => vi.advanceTimersByTimeAsync(10_000));
    expect(fetches).toBe(3);
  });

  it("fetches nothing without a deploy to follow", async () => {
    const requests = recordRequests();
    const { result } = renderHook(() => useDeploy(undefined), { wrapper });
    await act(async () => {});
    expect(result.current.fetchStatus).toBe("idle");
    expect(requests).toEqual([]);
  });
});
