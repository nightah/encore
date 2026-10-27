import { createFileRoute, redirect } from "@tanstack/react-router";

import { authStateQuery } from "@api/auth";
import { SetupWizard } from "@features/setup/SetupWizard";

export const Route = createFileRoute("/setup")({
  // First run is public (the setup token proves ownership). Resuming later needs a signed-in admin.
  beforeLoad: async ({ context }) => {
    const auth = await context.queryClient.ensureQueryData(authStateQuery);
    if (!auth.setup_required && !auth.authenticated) {
      throw redirect({ search: { redirect: "/setup" }, to: "/login" });
    }
  },
  component: SetupWizard,
});
