import { Outlet, createFileRoute, redirect } from "@tanstack/react-router";

import { authStateQuery } from "@api/auth";
import { AppShell } from "@components/AppShell";

export const Route = createFileRoute("/_shell")({
  // Gate every page in the shell: first run goes to the wizard, signed-out users to the login page.
  beforeLoad: async ({ context, location }) => {
    const auth = await context.queryClient.ensureQueryData(authStateQuery);
    if (auth.setup_required) throw redirect({ to: "/setup" });
    if (!auth.authenticated) {
      throw redirect({ search: { redirect: location.href }, to: "/login" });
    }
  },
  component: ShellLayout,
});

function ShellLayout() {
  return (
    <AppShell>
      <Outlet />
    </AppShell>
  );
}
