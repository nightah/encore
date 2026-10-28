import { useEffect, useState } from "react";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Controller, useForm, useWatch } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { z } from "zod";

import { changePassword, useAuthState } from "@api/auth";
import { buildUrl, errorMessage } from "@api/client";
import { confirmTOTP, disableTOTP, enrollTOTP } from "@api/generated/endpoints/auth/auth";
import type { AuthSettingsMode as AuthMode, Settings } from "@api/generated/model";
import { keys } from "@api/keys";
import { isManaged } from "@api/settings";
import { AnchorButton, Button } from "@components/ui/Button";
import { CodeBlock } from "@components/ui/CodeBlock";
import { Dialog } from "@components/ui/Dialog";
import { Checkbox, Field, FieldGrid, Input, RadioCards, Select, StaticField } from "@components/ui/Field";
import { Callout, CopyButton } from "@components/ui/States";
import { FormError } from "@features/settings/FormError";
import { type FieldMapping, type FormState, setPath, useSettingsForm } from "@features/settings/form";
import { OIDC_PRESETS, type OidcProvider, detectProvider } from "@features/settings/oidcProviders";
import i18n from "@i18n/index";
import { zodResolver } from "@lib/zodResolver";

const schema = z
  .object({
    break_glass: z.boolean(),
    header_admin_group: z.string().trim(),
    header_groups_header: z.string().trim(),
    header_trusted_proxies: z.string(),
    header_user_header: z.string().trim(),
    lockout: z.string(),
    mode: z.enum(["local", "oidc", "header"]),
    oidc_admin_group: z.string().trim(),
    oidc_client_id: z.string().trim(),
    oidc_client_secret: z.string(),
    oidc_groups_claim: z.string().trim(),
    oidc_issuer: z.string().trim(),
    oidc_scopes: z.string(),
    session_hours: z.string(),
  })
  .superRefine((v, ctx) => {
    if (v.mode === "oidc") {
      if (!/^https?:\/\/\S+$/.test(v.oidc_issuer)) {
        ctx.addIssue({
          code: "custom",
          message: i18n.t("settings:access.oidc.issuer.invalid"),
          path: ["oidc_issuer"],
        });
      }
      if (!v.oidc_client_id) {
        ctx.addIssue({
          code: "custom",
          message: i18n.t("settings:access.oidc.clientId.required"),
          path: ["oidc_client_id"],
        });
      }
    }
    if (v.mode === "header" && !v.header_user_header) {
      ctx.addIssue({
        code: "custom",
        message: i18n.t("settings:access.header.userHeader.required"),
        path: ["header_user_header"],
      });
    }
  });
type Values = z.infer<typeof schema>;

const list = (text: string, separator: RegExp) =>
  text
    .split(separator)
    .map((s) => s.trim())
    .filter(Boolean);

const mapping: FieldMapping<Values> = {
  break_glass: { path: "auth.oidc.break_glass" },
  header_admin_group: { path: "auth.header.admin_group" },
  header_groups_header: { path: "auth.header.groups_header" },
  header_trusted_proxies: {
    apply: (v, p) => setPath(p, "auth.header.trusted_proxies", list(v, /[,\s]+/)),
    path: "auth.header.trusted_proxies",
  },
  header_user_header: { path: "auth.header.user_header" },
  lockout: {
    apply: (v, p) => {
      const [attempts, minutes] = v.split("/").map(Number);
      setPath(p, "auth.local.lockout_attempts", attempts);
      setPath(p, "auth.local.lockout_minutes", minutes);
    },
    path: "auth.local.lockout_attempts",
  },
  mode: { path: "auth.mode" },
  oidc_admin_group: { path: "auth.oidc.admin_group" },
  oidc_client_id: { path: "auth.oidc.client_id" },
  oidc_client_secret: { path: "auth.oidc.client_secret" },
  oidc_groups_claim: { path: "auth.oidc.groups_claim" },
  oidc_issuer: { path: "auth.oidc.issuer" },
  oidc_scopes: {
    apply: (v, p) => setPath(p, "auth.oidc.scopes", list(v, /\s+/)),
    path: "auth.oidc.scopes",
  },
  session_hours: {
    apply: (v, p) => setPath(p, "auth.local.session_hours", Number(v)),
    path: "auth.local.session_hours",
  },
};

const toValues = (s: Settings): Values => ({
  break_glass: s.auth.oidc.break_glass,
  header_admin_group: s.auth.header.admin_group,
  header_groups_header: s.auth.header.groups_header,
  header_trusted_proxies: s.auth.header.trusted_proxies.join(", "),
  header_user_header: s.auth.header.user_header,
  lockout: `${s.auth.local.lockout_attempts}/${s.auth.local.lockout_minutes}`,
  mode: s.auth.mode,
  oidc_admin_group: s.auth.oidc.admin_group,
  oidc_client_id: s.auth.oidc.client_id,
  oidc_client_secret: "",
  oidc_groups_claim: s.auth.oidc.groups_claim,
  oidc_issuer: s.auth.oidc.issuer,
  oidc_scopes: s.auth.oidc.scopes.join(" "),
  session_hours: String(s.auth.local.session_hours),
});

/** The offered values, with the saved one first if it isn't among them. */
function withCurrent<T extends string>(options: readonly T[], current: string): Array<T | string> {
  return (options as readonly string[]).includes(current) ? [...options] : [current, ...options];
}

const SESSION_HOURS = ["168", "24", "720"] as const;
const LOCKOUTS = ["5/15", "10/60"] as const;

export function AccessTab({
  settings,
  formId,
  onFormState,
}: {
  settings: Settings;
  formId: string;
  onFormState: (state: FormState) => void;
}) {
  const { t } = useTranslation("settings");
  const form = useForm<Values>({
    defaultValues: toValues(settings),
    resolver: zodResolver(schema),
  });
  const { onSubmit, formError } = useSettingsForm({
    form,
    mapping,
    onFormState,
    settings,
    toValues,
  });
  const { data: auth } = useAuthState();
  const [passwordOpen, setPasswordOpen] = useState(false);
  const [totpOpen, setTotpOpen] = useState(false);
  const mode = useWatch({ control: form.control, name: "mode" });
  // Not saved: it only fills in the fields below and picks the setup snippet.
  const [provider, setProvider] = useState<OidcProvider>(() => detectProvider(settings.auth.oidc.issuer));
  const preset = provider === "other" ? undefined : OIDC_PRESETS[provider];
  const choose = (next: OidcProvider) => {
    setProvider(next);
    if (next === "other") return;
    const p = OIDC_PRESETS[next];
    const dirty = { shouldDirty: true };
    if (!managed("oidc_scopes")) form.setValue("oidc_scopes", p.scopes, dirty);
    if (!managed("oidc_groups_claim")) form.setValue("oidc_groups_claim", p.groupsClaim, dirty);
    if (!managed("oidc_client_id") && !form.getValues("oidc_client_id")) {
      form.setValue("oidc_client_id", "encore", dirty);
    }
  };
  const clientId = useWatch({ control: form.control, name: "oidc_client_id" }) || "encore";
  const errors = form.formState.errors;
  const managed = (key: keyof Values) => {
    const path = mapping[key]?.path;
    return path ? isManaged(settings, path) : false;
  };
  const values = toValues(settings);
  // Shown with both OpenID Connect and trusted headers; it's one setting.
  const breakGlass = (
    <Controller
      control={form.control}
      name="break_glass"
      render={({ field }) => (
        <Checkbox
          checked={field.value}
          onCheckedChange={field.onChange}
          disabled={managed("break_glass")}
          label={t("access.breakGlass.label")}
          hint={managed("break_glass") ? t("access.breakGlass.managed") : t("access.breakGlass.hint")}
        />
      )}
    />
  );

  return (
    <>
      <form id={formId} onSubmit={onSubmit} noValidate className="flex flex-col gap-4.5">
        <h2 className="font-display font-semibold text-lg">{t("access.title")}</h2>
        <FormError message={formError} />
        <Controller
          control={form.control}
          name="mode"
          render={({ field }) => (
            <RadioCards<AuthMode>
              legend={t("access.mode.legend")}
              value={field.value}
              onValueChange={field.onChange}
              managed={managed("mode")}
              columns="grid-cols-[repeat(auto-fit,minmax(220px,1fr))]"
              options={[
                { hint: t("access.mode.local.hint"), label: t("access.mode.local.label"), value: "local" },
                { hint: t("access.mode.oidc.hint"), label: t("access.mode.oidc.label"), value: "oidc" },
                { hint: t("access.mode.header.hint"), label: t("access.mode.header.label"), value: "header" },
              ]}
            />
          )}
        />

        {mode === "local" ? (
          <div className="flex flex-col gap-4">
            <FieldGrid>
              <Field label={t("access.local.username.label")} hint={t("access.local.username.hint")}>
                <Input autoComplete="username" readOnly value={settings.auth.local.username} />
              </Field>
              <StaticField label={t("access.local.password.label")} hint={t("access.local.password.hint")}>
                <Button onClick={() => setPasswordOpen(true)}>{t("access.local.password.change")}</Button>
              </StaticField>
              <Field
                label={t("access.local.session.label")}
                managed={managed("session_hours")}
                error={errors.session_hours?.message}
              >
                <Select {...form.register("session_hours")}>
                  {withCurrent(SESSION_HOURS, values.session_hours).map((value) => {
                    const hours = Number(value);
                    return (
                      <option key={value} value={value}>
                        {(SESSION_HOURS as readonly string[]).includes(value)
                          ? t("access.local.session.days", { count: hours / 24 })
                          : t("access.local.session.hours", { count: hours })}
                      </option>
                    );
                  })}
                </Select>
              </Field>
              <Field
                label={t("access.local.lockout.label")}
                managed={managed("lockout")}
                error={errors.lockout?.message}
              >
                <Select {...form.register("lockout")}>
                  {withCurrent(LOCKOUTS, values.lockout).map((value) => {
                    const [attempts, minutes] = value.split("/");
                    return (
                      <option key={value} value={value}>
                        {value === "5/15" || value === "10/60"
                          ? t(`access.local.lockout.${value}`)
                          : t("access.local.lockout.custom", { attempts, minutes })}
                      </option>
                    );
                  })}
                </Select>
              </Field>
            </FieldGrid>
            <Checkbox
              checked={settings.auth.local.totp_enabled}
              onCheckedChange={() => setTotpOpen(true)}
              label={t("access.local.totp.label")}
              hint={t("access.local.totp.hint")}
            />
          </div>
        ) : null}

        {mode === "oidc" ? (
          <div className="flex flex-col gap-4">
            <FieldGrid>
              <Field label={t("access.oidc.provider.label")} hint={t("access.oidc.provider.hint")}>
                <Select value={provider} onChange={(event) => choose(event.target.value as OidcProvider)}>
                  {Object.entries(OIDC_PRESETS).map(([value, p]) => (
                    <option key={value} value={value}>
                      {p.label}
                    </option>
                  ))}
                  <option value="other">{t("access.oidc.provider.other")}</option>
                </Select>
              </Field>
              <Field
                label={t("access.oidc.issuer.label")}
                managed={managed("oidc_issuer")}
                hint={
                  provider === "other"
                    ? t("access.oidc.issuer.hint")
                    : t("access.oidc.issuer.hintPreset", { shape: t(`access.oidc.providers.${provider}.issuerHint`) })
                }
                error={errors.oidc_issuer?.message}
              >
                <Input mono {...form.register("oidc_issuer")} />
              </Field>
              <Field
                label={t("access.oidc.clientId.label")}
                managed={managed("oidc_client_id")}
                error={errors.oidc_client_id?.message}
              >
                <Input mono {...form.register("oidc_client_id")} />
              </Field>
              <Field
                label={t("access.oidc.clientSecret.label")}
                managed={managed("oidc_client_secret")}
                hint={
                  settings.auth.oidc.has_client_secret
                    ? t("access.oidc.clientSecret.stored")
                    : t("access.oidc.clientSecret.empty")
                }
              >
                <Input
                  type="password"
                  autoComplete="new-password"
                  placeholder={settings.auth.oidc.has_client_secret ? "••••••••••••" : ""}
                  {...form.register("oidc_client_secret")}
                />
              </Field>
              <Field
                label={t("access.oidc.scopes.label")}
                managed={managed("oidc_scopes")}
                hint={t("access.oidc.scopes.hint")}
              >
                <Input mono {...form.register("oidc_scopes")} />
              </Field>
              <Field
                label={t("access.oidc.adminGroup.label")}
                managed={managed("oidc_admin_group")}
                hint={t("access.oidc.adminGroup.hint")}
              >
                <Input {...form.register("oidc_admin_group")} />
              </Field>
              <Field label={t("access.oidc.groupsClaim.label")} managed={managed("oidc_groups_claim")}>
                <Input mono {...form.register("oidc_groups_claim")} />
              </Field>
            </FieldGrid>
            <div className="flex items-end gap-1">
              <Field label={t("access.oidc.redirectUri.label")} className="flex-1">
                <Input mono readOnly value={settings.auth.oidc.redirect_uri} />
              </Field>
              <CopyButton text={settings.auth.oidc.redirect_uri} label={t("access.oidc.redirectUri.copy")} />
            </div>
            {preset && provider !== "other" ? (
              <CodeBlock
                title={t(`access.oidc.providers.${provider}.snippetTitle`)}
                label={t("access.oidc.snippetLabel", { provider: preset.label })}
                code={preset.snippet({ clientId, redirectUri: settings.auth.oidc.redirect_uri })}
              />
            ) : null}
            {breakGlass}
            <div className="flex flex-wrap items-center gap-3">
              {settings.auth.mode === "oidc" ? (
                <AnchorButton
                  href={auth?.oidc_login_url ?? buildUrl("/auth/oidc/login")}
                  target="_blank"
                  rel="noreferrer"
                >
                  {t("access.oidc.testSignIn")}
                </AnchorButton>
              ) : (
                <>
                  <Button disabled>{t("access.oidc.testSignIn")}</Button>
                  <span className="text-[13px] text-muted">{t("access.oidc.saveToTest")}</span>
                </>
              )}
            </div>
          </div>
        ) : null}

        {mode === "header" ? (
          <div className="flex flex-col gap-4">
            <FieldGrid>
              <Field
                label={t("access.header.trustedProxies.label")}
                managed={managed("header_trusted_proxies")}
                hint={t("access.header.trustedProxies.hint")}
              >
                <Input mono {...form.register("header_trusted_proxies")} />
              </Field>
              <Field
                label={t("access.header.userHeader.label")}
                managed={managed("header_user_header")}
                error={errors.header_user_header?.message}
              >
                <Input mono {...form.register("header_user_header")} />
              </Field>
              <Field label={t("access.header.groupsHeader.label")} managed={managed("header_groups_header")}>
                <Input mono {...form.register("header_groups_header")} />
              </Field>
              <Field label={t("access.header.adminGroup.label")} managed={managed("header_admin_group")}>
                <Input {...form.register("header_admin_group")} />
              </Field>
            </FieldGrid>
            <Callout tone="warnStrong">{t("access.header.strip")}</Callout>
            {breakGlass}
          </div>
        ) : null}
      </form>
      <ChangePasswordDialog open={passwordOpen} onOpenChange={setPasswordOpen} />
      <TotpDialog open={totpOpen} onOpenChange={setTotpOpen} enabled={settings.auth.local.totp_enabled} />
    </>
  );
}

const passwordSchema = z
  .object({
    confirm: z.string(),
    current: z.string().min(1, { error: () => i18n.t("settings:access.password.current.required") }),
    password: z.string().min(12, { error: () => i18n.t("settings:access.password.new.tooShort") }),
  })
  .refine((v) => v.password === v.confirm, {
    error: () => i18n.t("settings:access.password.confirm.mismatch"),
    path: ["confirm"],
  });
type PasswordValues = z.infer<typeof passwordSchema>;

function ChangePasswordDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const { t } = useTranslation("settings");
  const form = useForm<PasswordValues>({
    defaultValues: { confirm: "", current: "", password: "" },
    resolver: zodResolver(passwordSchema),
  });
  const change = useMutation({
    meta: { inline: true, success: t("access.password.changed") },
    mutationFn: ({ current, password }: PasswordValues) =>
      changePassword({ current_password: current, new_password: password }),
  });
  const close = (next: boolean) => {
    onOpenChange(next);
    if (!next) {
      form.reset();
      change.reset();
    }
  };
  const submit = form.handleSubmit(async (values) => {
    try {
      await change.mutateAsync(values);
      close(false);
    } catch {
      // Shown below.
    }
  });
  return (
    <Dialog
      open={open}
      onOpenChange={close}
      title={t("access.password.title")}
      description={t("access.password.description")}
      footer={
        <>
          <Button onClick={() => close(false)}>{t("access.cancel")}</Button>
          <Button type="submit" form="change-password" variant="primary" pending={change.isPending}>
            {t("access.password.submit")}
          </Button>
        </>
      }
    >
      <form id="change-password" onSubmit={submit} noValidate className="flex flex-col gap-4">
        <FormError message={change.error ? errorMessage(change.error) : undefined} />
        <Field label={t("access.password.current.label")} error={form.formState.errors.current?.message}>
          <Input type="password" autoComplete="current-password" {...form.register("current")} />
        </Field>
        <Field
          label={t("access.password.new.label")}
          hint={t("access.password.new.hint")}
          error={form.formState.errors.password?.message}
        >
          <Input type="password" autoComplete="new-password" {...form.register("password")} />
        </Field>
        <Field label={t("access.password.confirm.label")} error={form.formState.errors.confirm?.message}>
          <Input type="password" autoComplete="new-password" {...form.register("confirm")} />
        </Field>
      </form>
    </Dialog>
  );
}

/** Turns TOTP on (enrol, add to an authenticator app, confirm a code) or off (confirm a code). */
function TotpDialog({
  open,
  onOpenChange,
  enabled,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  enabled: boolean;
}) {
  const { t } = useTranslation("settings");
  const queryClient = useQueryClient();
  const [code, setCode] = useState("");
  const enroll = useMutation({ meta: { inline: true }, mutationFn: () => enrollTOTP() });
  const submit = useMutation({
    meta: {
      inline: true,
      success: enabled ? t("access.totp.turnedOff") : t("access.totp.turnedOn"),
    },
    mutationFn: (value: string) => (enabled ? disableTOTP({ code: value }) : confirmTOTP({ code: value })),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: keys.settings }),
  });
  const { mutate: startEnroll, reset: resetEnroll } = enroll;
  const { reset: resetSubmit } = submit;

  // A fresh code field each time the dialog opens, while rendering rather than in the effect below.
  const [wasOpen, setWasOpen] = useState(open);
  if (open !== wasOpen) {
    setWasOpen(open);
    if (open) setCode("");
  }

  useEffect(() => {
    if (!open) return;
    resetSubmit();
    resetEnroll();
    if (!enabled) startEnroll();
  }, [open, enabled, startEnroll, resetEnroll, resetSubmit]);

  const confirm = async () => {
    try {
      await submit.mutateAsync(code.trim());
      onOpenChange(false);
    } catch {
      // Shown below.
    }
  };
  const failure = enroll.error ?? submit.error;

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={enabled ? t("access.totp.offTitle") : t("access.totp.onTitle")}
      description={enabled ? t("access.totp.offDescription") : t("access.totp.onDescription")}
      footer={
        <>
          <Button onClick={() => onOpenChange(false)}>{t("access.cancel")}</Button>
          {/* A submit button for the form, so Enter in the code field confirms even while the secret field makes it a two-field form. */}
          <Button
            type="submit"
            form="totp-form"
            variant={enabled ? "danger" : "primary"}
            pending={submit.isPending}
            disabled={!enabled && !enroll.data}
          >
            {enabled ? t("access.totp.off") : t("access.totp.on")}
          </Button>
        </>
      }
    >
      <form
        id="totp-form"
        className="flex flex-col gap-4"
        onSubmit={(event) => {
          event.preventDefault();
          void confirm();
        }}
      >
        <FormError message={failure ? errorMessage(failure) : undefined} />
        {!enabled && enroll.data ? (
          <div className="flex items-end gap-1">
            <Field
              label={t("access.totp.secret")}
              className="flex-1"
              hint={
                <a className="text-link underline-offset-2 hover:underline" href={enroll.data.otpauth_url}>
                  {t("access.totp.openApp")}
                </a>
              }
            >
              <Input mono readOnly value={enroll.data.secret} />
            </Field>
            <CopyButton text={enroll.data.secret} label={t("access.totp.copySecret")} />
          </div>
        ) : null}
        <Field label={t("access.totp.code")}>
          <Input
            inputMode="numeric"
            autoComplete="one-time-code"
            value={code}
            onChange={(event) => setCode(event.target.value)}
          />
        </Field>
      </form>
    </Dialog>
  );
}
