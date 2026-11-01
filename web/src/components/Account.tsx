import { useNavigate } from "@tanstack/react-router";
import { Trans, useTranslation } from "react-i18next";

import { useAuthState, useLogout } from "@api/auth";
import { useHealth } from "@api/health";

/** Who is signed in, the release and signing out: the rail's footer on wide screens, the More sheet on phones. */
export function useAccount() {
  const { data: auth } = useAuthState();
  const { data: health } = useHealth();
  const logout = useLogout();
  const navigate = useNavigate();
  return {
    canSignOut: auth?.mode !== "header",
    name: auth?.user?.name,
    signOut: () => logout.mutate(undefined, { onSuccess: () => navigate({ to: "/login" }) }),
    // Release builds report a semantic version; development builds report "dev".
    version: health?.version ? (/^\d/.test(health.version) ? `v${health.version}` : health.version) : undefined,
  };
}

export function SignedInAs({ name }: { name: string }) {
  const { t } = useTranslation("common");
  return (
    // data-sensitive: screenshots show "admin" instead of the real name.
    <Trans t={t} i18nKey="shell.signedInAs" values={{ name }} components={{ name: <span data-sensitive="admin" /> }} />
  );
}
