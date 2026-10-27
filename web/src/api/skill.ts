import { useState } from "react";

import { queryOptions, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { buildUrl } from "@api/client";
import {
  authorizeAmazon,
  deploySkill,
  disconnectAmazon,
  getDeploy,
  getSkill,
  pasteAmazonAuthorization,
  setAmazonCredentials,
  updateSkillContent,
  updateSkillSettings,
} from "@api/generated/endpoints/skill/skill";
import type {
  AuthorizeAmazonThen,
  PasteAmazonAuthorizationRequest,
  SetAmazonCredentialsRequest,
  SkillInfo,
  SkillSettingsInput,
  UpdateSkillContentRequestContentItem,
} from "@api/generated/model";
import { keys } from "@api/keys";
import i18n from "@i18n/index";

/** The Login with Amazon consent URL, for a redirect; Amazon's answer comes back to the page named by `then`. The server leaves it empty until the client ID and secret are saved. */
export async function getAuthorize(then?: AuthorizeAmazonThen) {
  const info = await authorizeAmazon(then ? { then } : undefined);
  if (!info.url) throw new Error(i18n.t("error.lwaCredentials"));
  return info;
}

/** Download link for the skill package zip (a plain navigation, so the browser handles the file). */
export const skillPackageUrl = () => buildUrl("/skill/package");

/** The request body for PUT /skill/settings built from the current skill, with overrides. The endpoint certificate is write-only: leaving it out keeps the stored one. */
export function skillSettingsFrom(skill: SkillInfo, patch: Partial<SkillSettingsInput> = {}): SkillSettingsInput {
  return {
    content: skill.content,
    endpoint: { url: skill.endpoint.url },
    invocation: skill.invocation,
    locales_enabled: skill.locales_enabled,
    privacy: skill.privacy,
    skill_name: skill.skill_name,
    ...patch,
  };
}

const skillQuery = queryOptions({
  queryFn: ({ signal }) => getSkill({ signal }),
  queryKey: keys.skill,
});

export function useSkill() {
  return useQuery(skillQuery);
}

/** What people say to reach the skill, "Alexa, ask <name> to…": encore or kodi as chosen, in the first enabled locale. */
export function useInvocationName(): string {
  const { data: skill } = useSkill();
  return skill?.invocation_name || "encore";
}

export function useAuthorize(enabled = true) {
  return useQuery({
    enabled,
    queryFn: ({ signal }) => authorizeAmazon(undefined, { signal }),
    queryKey: keys.authorize,
    retry: false,
  });
}

/** The deploy a page should follow: the one it started, or one already in progress when it opened (for example after a reload or from another tab). Once followed, a deploy stays followed after it ends, so its result stays on screen. */
export function useActiveDeploy(skill: SkillInfo | undefined) {
  const [deployId, setDeployId] = useState<string>();
  const inFlight = skill?.last_deploy?.state === "in_progress" ? skill.last_deploy.id : undefined;

  // Follow one found in progress, while rendering rather than in an effect.
  if (inFlight && !deployId) setDeployId(inFlight);

  return [deployId, setDeployId] as const;
}

/** Polls a deploy while it runs; SSE `deploy` events also append to this cache. */
export function useDeploy(id: string | undefined) {
  return useQuery({
    enabled: !!id,
    queryFn: ({ signal }) => getDeploy(id ?? "", { signal }),
    queryKey: keys.deploy(id ?? ""),
    refetchInterval: (query) => (query.state.data?.state === "in_progress" ? 3_000 : false),
  });
}

function useInvalidateSkill() {
  const queryClient = useQueryClient();
  return () =>
    Promise.all([
      queryClient.invalidateQueries({ queryKey: keys.skill }),
      queryClient.invalidateQueries({ queryKey: keys.overview }),
    ]);
}

/** `success` is the toast shown once saved; null shows none. */
export function useSaveSkillSettings(success: string | null = i18n.t("toast.skillSaved")) {
  const queryClient = useQueryClient();
  const invalidate = useInvalidateSkill();
  return useMutation({
    meta: { inline: true, ...(success ? { success } : {}) },
    mutationFn: (body: SkillSettingsInput) => updateSkillSettings(body),
    onMutate: (body) => {
      queryClient.setQueryData<SkillInfo>(keys.skill, (skill) =>
        skill
          ? {
              ...skill,
              ...body,
              endpoint: { ...skill.endpoint, ...body.endpoint },
              needs_deploy: true,
            }
          : skill,
      );
    },
    onSettled: invalidate,
  });
}

/** Saves what the skill plays on its own; the libraries follow at once, the skill model on the next deploy. */
export function useSaveSkillContent() {
  const queryClient = useQueryClient();
  const invalidate = useInvalidateSkill();
  return useMutation({
    meta: { inline: true },
    mutationFn: (content: UpdateSkillContentRequestContentItem[]) => updateSkillContent({ content }),
    onMutate: (content) => {
      queryClient.setQueryData<SkillInfo>(keys.skill, (skill) => (skill ? { ...skill, content } : skill));
    },
    onSettled: () => {
      void invalidate();
      void queryClient.invalidateQueries({ queryKey: keys.libraries });
    },
  });
}

export function useSaveAmazonCredentials() {
  const invalidate = useInvalidateSkill();
  return useMutation({
    meta: { inline: true },
    mutationFn: (body: SetAmazonCredentialsRequest) => setAmazonCredentials(body),
    onSuccess: invalidate,
  });
}

export function usePasteAmazon() {
  const invalidate = useInvalidateSkill();
  return useMutation({
    meta: { inline: true, success: i18n.t("toast.amazonConnected") },
    mutationFn: (body: PasteAmazonAuthorizationRequest) => pasteAmazonAuthorization(body),
    onSuccess: invalidate,
  });
}

export function useDisconnectAmazon() {
  const invalidate = useInvalidateSkill();
  return useMutation({
    meta: { success: i18n.t("toast.amazonDisconnected") },
    mutationFn: () => disconnectAmazon(),
    onSuccess: invalidate,
  });
}

export function useStartDeploy() {
  const invalidate = useInvalidateSkill();
  return useMutation({
    mutationFn: (force?: boolean) => deploySkill(force ? { force } : {}),
    onSuccess: invalidate,
  });
}
