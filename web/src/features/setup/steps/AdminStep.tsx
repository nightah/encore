import type { TFunction } from "i18next";
import { useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { z } from "zod";

import { useAuthState, useSetupAdmin } from "@api/auth";
import { errorMessage, fieldErrorsOf } from "@api/client";
import { Button } from "@components/ui/Button";
import { Field, FieldGrid, Input } from "@components/ui/Field";
import { Callout } from "@components/ui/States";
import { StepFooter, StepHeading, type StepProps } from "@features/setup/shared";
import { zodResolver } from "@lib/zodResolver";

const schema = (t: TFunction<"setup">) =>
  z.object({
    password: z.string().min(12, t("admin.errors.password")),
    setup_token: z.string().trim().min(1, t("admin.errors.token")),
    username: z.string().trim().min(1, t("admin.errors.username")),
  });
type Values = z.infer<ReturnType<typeof schema>>;
const DEFAULTS: Values = { password: "", setup_token: "", username: "admin" };

export function AdminStep({ nav }: StepProps) {
  const { t } = useTranslation("setup");
  const { data: auth } = useAuthState();
  const setup = useSetupAdmin();
  const form = useForm<Values>({
    defaultValues: DEFAULTS,
    resolver: zodResolver(schema(t)),
  });
  const errors = form.formState.errors;

  if (auth && !auth.setup_required) {
    return (
      <div className="flex flex-col gap-4.5">
        <StepHeading>{t("admin.heading")}</StepHeading>
        <Callout tone="ok">
          {auth.user ? t("admin.existsSignedIn", { name: auth.user.name }) : t("admin.exists")}
        </Callout>
        <StepFooter
          nav={nav}
          primary={
            <Button variant="primary" onClick={nav.done}>
              {t("nav.continue")}
            </Button>
          }
        />
      </div>
    );
  }

  const onSubmit = form.handleSubmit(async (values) => {
    try {
      await setup.mutateAsync(values);
      nav.done();
    } catch (error) {
      for (const [name, message] of Object.entries(fieldErrorsOf(error))) {
        if (name in values) form.setError(name as keyof Values, { message });
      }
    }
  });

  const generalError =
    // Shown unless an error went next to a field.
    setup.error && !Object.keys(fieldErrorsOf(setup.error)).some((name) => name in DEFAULTS)
      ? errorMessage(setup.error)
      : null;

  return (
    <form onSubmit={onSubmit} noValidate className="flex flex-col gap-4.5">
      <StepHeading intro={t("admin.intro")}>{t("admin.heading")}</StepHeading>
      <div className="flex flex-col gap-2">
        <pre className="overflow-x-auto whitespace-pre-wrap rounded-[10px] bg-[#161d26] px-4 py-3.5 font-mono text-[#fcd34d] text-[13px]">
          level=WARN msg="no admin account exists yet; open the web interface and create one with this one-time setup
          token" setup_token=…
        </pre>
        <LogCommand title={t("admin.logs.docker")} command='docker logs encore 2>&1 | grep "setup token"' />
        <LogCommand title={t("admin.logs.systemd")} command='journalctl -u encore | grep "setup token"' />
      </div>
      <Field label={t("admin.token")} error={errors.setup_token?.message}>
        <Input mono autoComplete="off" spellCheck={false} {...form.register("setup_token")} />
      </Field>
      <FieldGrid className="grid-cols-[repeat(auto-fit,minmax(220px,1fr))] gap-3.5">
        <Field label={t("admin.username")} error={errors.username?.message}>
          <Input autoComplete="username" {...form.register("username")} />
        </Field>
        <Field label={t("admin.password.label")} hint={t("admin.password.hint")} error={errors.password?.message}>
          <Input type="password" autoComplete="new-password" {...form.register("password")} />
        </Field>
      </FieldGrid>
      <p className="text-[13px] text-muted">{t("admin.sso")}</p>
      {generalError ? (
        <p role="alert" className="rounded-[10px] bg-bad-bg px-3.5 py-2.5 text-bad">
          {generalError}
        </p>
      ) : null}
      <StepFooter
        nav={nav}
        primary={
          <Button type="submit" variant="primary" pending={setup.isPending}>
            {t("nav.continue")}
          </Button>
        }
      />
    </form>
  );
}

/** A collapsed hint with the command that finds the setup token in one kind of deployment's log. */
function LogCommand({ title, command }: { title: string; command: string }) {
  return (
    <details className="rounded-[10px] border border-line px-4 py-2">
      <summary className="min-h-9 cursor-pointer content-center font-medium text-sm">{title}</summary>
      <pre className="mt-1.5 mb-1 overflow-x-auto rounded-lg bg-[#161d26] px-3.5 py-2.5 font-mono text-[#c3ccd6] text-[13px]">
        $ {command}
      </pre>
    </details>
  );
}
