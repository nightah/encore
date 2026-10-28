import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, redirect, useNavigate } from "@tanstack/react-router";
import type { TFunction } from "i18next";
import { LogIn } from "lucide-react";
import { useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { z } from "zod";

import { authStateQuery, useLogin } from "@api/auth";
import { ApiError, errorMessage, fieldErrorsOf } from "@api/client";
import { AuthFrame } from "@components/AuthFrame";
import { AnchorButton, Button } from "@components/ui/Button";
import { Field, Input } from "@components/ui/Field";
import { zodResolver } from "@lib/zodResolver";

/** `error` is set by the server when OpenID Connect sign-in fails. */
const searchSchema = z.object({ error: z.string().optional(), redirect: z.string().optional() });

export const Route = createFileRoute("/login")({
  beforeLoad: async ({ context }) => {
    const auth = await context.queryClient.ensureQueryData(authStateQuery);
    if (auth.setup_required) throw redirect({ to: "/setup" });
    if (auth.authenticated) throw redirect({ to: "/" });
  },
  component: LoginPage,
  validateSearch: searchSchema,
});

const loginSchema = (t: TFunction<"auth">) =>
  z.object({
    password: z.string().min(1, t("login.errors.password")),
    totp: z
      .string()
      .trim()
      .regex(/^(\d{6})?$/, t("login.errors.totp"))
      .optional(),
    username: z.string().trim().min(1, t("login.errors.username")),
  });
type LoginForm = z.infer<ReturnType<typeof loginSchema>>;
const EMPTY: LoginForm = { password: "", totp: "", username: "" };

/** Only same-app paths are allowed as a post-login destination. */
function safeRedirect(target: string | undefined): string {
  return target?.startsWith("/") && !target.startsWith("//") ? target : "/";
}

function LoginPage() {
  const { t } = useTranslation("auth");
  const { redirect: target, error: signInError } = Route.useSearch();
  const navigate = useNavigate();
  // Loaded by beforeLoad, so this never suspends.
  const { data: auth } = useSuspenseQuery(authStateQuery);
  const login = useLogin();
  const form = useForm<LoginForm>({
    defaultValues: EMPTY,
    resolver: zodResolver(loginSchema(t)),
  });

  const onSubmit = form.handleSubmit(async (values) => {
    try {
      await login.mutateAsync({
        password: values.password,
        username: values.username,
        ...(values.totp ? { totp: values.totp } : {}),
      });
      await navigate({ to: safeRedirect(target) });
    } catch (error) {
      for (const [name, message] of Object.entries(fieldErrorsOf(error))) {
        if (!(name in values)) continue;
        form.setError(name as keyof LoginForm, {
          message: name === "totp" ? t("login.errors.totp") : message,
        });
      }
    }
  });

  const mode = auth.mode;
  const errors = form.formState.errors;

  return (
    <AuthFrame subtitle={t("login.subtitle")}>
      <main className="flex w-full max-w-md flex-col gap-5 rounded-2xl bg-surface p-6 sm:p-8">
        <h1 className="font-display font-semibold text-[28px] leading-tight">{t("login.title")}</h1>
        {signInError ? (
          <p role="alert" className="rounded-[10px] bg-bad-bg px-3.5 py-2.5 text-bad">
            {signInError}
          </p>
        ) : null}

        {mode === "oidc" && auth.oidc_login_url ? (
          <div className="flex flex-col gap-3">
            <p className="text-muted">{t("login.oidc.intro")}</p>
            <AnchorButton variant="primary" href={auth.oidc_login_url}>
              <LogIn aria-hidden="true" className="size-4" />
              {t("login.oidc.button")}
            </AnchorButton>
          </div>
        ) : null}

        {mode === "header" ? <p className="text-muted">{t("login.header")}</p> : null}

        {auth.local_login ? (
          <form onSubmit={onSubmit} noValidate className="flex flex-col gap-4">
            <Field label={t("login.username")} error={errors.username?.message}>
              <Input autoComplete="username" autoFocus {...form.register("username")} />
            </Field>
            <Field label={t("login.password")} error={errors.password?.message}>
              <Input type="password" autoComplete="current-password" {...form.register("password")} />
            </Field>
            <Field label={t("login.totp.label")} hint={t("login.totp.hint")} error={errors.totp?.message}>
              <Input inputMode="numeric" autoComplete="one-time-code" mono {...form.register("totp")} />
            </Field>
            {login.error && !Object.keys(fieldErrorsOf(login.error)).some((name) => name in EMPTY) ? (
              <p role="alert" className="rounded-[10px] bg-bad-bg px-3.5 py-2.5 text-bad">
                {login.error instanceof ApiError && login.error.status === 429
                  ? t("login.tooManyAttempts")
                  : errorMessage(login.error)}
              </p>
            ) : null}
            <Button type="submit" variant="primary" pending={login.isPending}>
              {t("login.submit")}
            </Button>
          </form>
        ) : null}
      </main>
    </AuthFrame>
  );
}
