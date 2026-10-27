import { queryOptions, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { setCsrfToken } from "@api/client";
import {
  changePassword,
  checkReachability,
  createAdmin,
  getAuthState as fetchAuthState,
  login,
  logout,
} from "@api/generated/endpoints/auth/auth";
import type { AuthState, CheckReachabilityRequest, CreateAdminRequest, LoginRequest } from "@api/generated/model";
import { keys } from "@api/keys";

/** GET /auth/state (public). Also stores the CSRF token for later writes. */
async function getAuthState(signal?: AbortSignal): Promise<AuthState> {
  const state = await fetchAuthState({ signal: signal ?? null });
  setCsrfToken(state.csrf_token);
  return state;
}

export const authStateQuery = queryOptions({
  queryFn: ({ signal }) => getAuthState(signal),
  queryKey: keys.auth,
  retry: 1,
  staleTime: 60_000,
});

export function useAuthState() {
  return useQuery(authStateQuery);
}

function useRefreshAuth() {
  const queryClient = useQueryClient();
  return () => queryClient.invalidateQueries({ queryKey: keys.auth });
}

export function useSetupAdmin() {
  const refresh = useRefreshAuth();
  return useMutation({
    meta: { inline: true },
    mutationFn: (body: CreateAdminRequest) => createAdmin(body),
    onSuccess: refresh,
  });
}

export function useLogin() {
  const refresh = useRefreshAuth();
  return useMutation({
    meta: { inline: true },
    mutationFn: (body: LoginRequest) => login(body),
    onSuccess: refresh,
  });
}

export function useLogout() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => logout(),
    onSuccess: async () => {
      queryClient.removeQueries({ predicate: (q) => q.queryKey[0] !== "auth" });
      await queryClient.invalidateQueries({ queryKey: keys.auth });
    },
  });
}

/** POST /setup/reachability: checks the public URL from outside. Before the admin exists it needs the setup token. */
export function useCheckReachability() {
  return useMutation({
    meta: { inline: true },
    mutationFn: (body: CheckReachabilityRequest) => checkReachability(body),
  });
}

export { changePassword };
