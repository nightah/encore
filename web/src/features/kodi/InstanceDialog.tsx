import { useEffect, useMemo, useState } from "react";

import type { TFunction } from "i18next";
import { useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { z } from "zod";

import { ApiError, errorMessage } from "@api/client";
import type { Instance, InstanceInput as InstanceBody } from "@api/generated/model";
import { useCreateInstance, useDeleteInstance, useUpdateInstance } from "@api/kodi";
import { Button } from "@components/ui/Button";
import { ManagedBadge } from "@components/ui/Chip";
import { Dialog } from "@components/ui/Dialog";
import { Field, FieldGrid, Input, Select } from "@components/ui/Field";
import { LANGUAGES } from "@i18n/index";
import { zodResolver } from "@lib/zodResolver";

const optionalUrl = (pattern: RegExp, message: string) =>
  z
    .string()
    .trim()
    .refine((value) => value === "" || pattern.test(value), message);

const SHUTDOWN = ["inherit", "quit", "power_off"] as const satisfies ReadonlyArray<Instance["shutdown"]>;

const makeSchema = (t: TFunction<"kodi">) =>
  z.object({
    broadcast: z.string().trim(),
    language: z.string(),
    mac: optionalUrl(/^([0-9A-Fa-f]{2}[:-]){5}[0-9A-Fa-f]{2}$/, t("dialog.errors.mac")),
    name: z.string().trim().min(1, t("dialog.errors.name")),
    password: z.string(),
    shutdown: z.enum(SHUTDOWN),
    timezone: z.string().trim(),
    url: z
      .string()
      .trim()
      .regex(/^https?:\/\/\S+$/, t("dialog.errors.url")),
    username: z.string().trim(),
    websocket_url: optionalUrl(/^wss?:\/\/\S+$/, t("dialog.errors.websocketUrl")),
  });

export type InstanceForm = z.infer<ReturnType<typeof makeSchema>>;

const EMPTY: InstanceForm = {
  broadcast: "",
  language: "en",
  mac: "",
  name: "",
  password: "",
  shutdown: "inherit",
  timezone: "",
  url: "",
  username: "kodi",
  websocket_url: "",
};

function fromInstance(instance: Instance): InstanceForm {
  return {
    broadcast: instance.broadcast,
    language: instance.language,
    mac: instance.mac,
    name: instance.name,
    password: "",
    shutdown: instance.shutdown,
    timezone: instance.timezone,
    url: instance.url,
    username: instance.username,
    websocket_url: instance.websocket_url,
  };
}

interface InstanceDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Edit this instance; omit to add a new one. */
  instance?: Instance | undefined;
  /** Prefilled values for a new instance (e.g. from discovery). */
  prefill?: Partial<InstanceForm> | undefined;
}

/** Add or edit a Kodi instance. Config-managed instances open read-only. */
export function InstanceDialog({ open, onOpenChange, instance, prefill }: InstanceDialogProps) {
  const { t } = useTranslation("kodi");
  const schema = useMemo(() => makeSchema(t), [t]);
  const create = useCreateInstance();
  const update = useUpdateInstance();
  const remove = useDeleteInstance();
  const [confirmRemove, setConfirmRemove] = useState(false);
  const managed = instance?.managed_by_config ?? false;
  const form = useForm<InstanceForm>({
    defaultValues: EMPTY,
    resolver: zodResolver(schema),
  });

  useEffect(() => {
    if (!open) return;
    form.reset(instance ? fromInstance(instance) : { ...EMPTY, ...prefill });
    setConfirmRemove(false);
    create.reset();
    update.reset();
  }, [open, instance, prefill, form, create.reset, update.reset]);

  const pending = create.isPending || update.isPending;
  const failure = create.error ?? update.error;
  const errors = form.formState.errors;

  const onSubmit = form.handleSubmit(async (values) => {
    const body: InstanceBody = {
      language: values.language,
      name: values.name,
      shutdown: values.shutdown,
      timezone: values.timezone,
      url: values.url,
    };
    for (const key of ["websocket_url", "username", "mac", "broadcast"] as const) {
      if (values[key] || instance) body[key] = values[key];
    }
    if (values.password) body.password = values.password;
    try {
      if (instance) await update.mutateAsync({ body, id: instance.id });
      else await create.mutateAsync(body);
      onOpenChange(false);
    } catch (error) {
      if (error instanceof ApiError) {
        for (const [name, message] of Object.entries(error.fieldErrors)) {
          if (name in values) form.setError(name as keyof InstanceForm, { message });
        }
      }
    }
  });

  const title = instance ? t("dialog.editTitle", { name: instance.name }) : t("dialog.addTitle");
  const hasFieldErrors = failure instanceof ApiError && Object.keys(failure.fieldErrors).length > 0;

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      size="lg"
      title={title}
      description={
        managed ? (
          <span className="flex flex-wrap items-center gap-2">
            <ManagedBadge /> {t("dialog.managed")}
          </span>
        ) : (
          t("dialog.description")
        )
      }
      footer={
        confirmRemove && instance ? (
          <>
            <span className="mr-auto self-center">{t("dialog.confirmRemove", { name: instance.name })}</span>
            <Button onClick={() => setConfirmRemove(false)}>{t("dialog.keep")}</Button>
            <Button
              variant="danger"
              pending={remove.isPending}
              onClick={() => remove.mutate(instance.id, { onSuccess: () => onOpenChange(false) })}
            >
              {t("dialog.remove")}
            </Button>
          </>
        ) : (
          <>
            {instance && !managed ? (
              <Button variant="danger" className="mr-auto" onClick={() => setConfirmRemove(true)}>
                {t("dialog.removeEllipsis")}
              </Button>
            ) : null}
            <Button onClick={() => onOpenChange(false)}>{managed ? t("dialog.close") : t("dialog.cancel")}</Button>
            {managed ? null : (
              <Button type="submit" form="instance-form" variant="primary" pending={pending}>
                {instance ? t("dialog.save") : t("dialog.add")}
              </Button>
            )}
          </>
        )
      }
    >
      <form id="instance-form" onSubmit={onSubmit} noValidate className="flex flex-col gap-4">
        <FieldGrid>
          <Field label={t("dialog.fields.name.label")} error={errors.name?.message} hint={t("dialog.fields.name.hint")}>
            <Input readOnly={managed} {...form.register("name")} />
          </Field>
          <Field label={t("dialog.fields.url.label")} error={errors.url?.message}>
            <Input mono readOnly={managed} placeholder="http://192.168.1.20:8080/jsonrpc" {...form.register("url")} />
          </Field>
          <Field
            label={t("dialog.fields.websocketUrl.label")}
            error={errors.websocket_url?.message}
            hint={t("dialog.fields.websocketUrl.hint")}
          >
            <Input
              mono
              readOnly={managed}
              placeholder="ws://192.168.1.20:9090/jsonrpc"
              {...form.register("websocket_url")}
            />
          </Field>
          <Field label={t("dialog.fields.username.label")} error={errors.username?.message}>
            <Input readOnly={managed} autoComplete="off" {...form.register("username")} />
          </Field>
          <Field
            label={t("dialog.fields.password.label")}
            error={errors.password?.message}
            hint={instance?.has_password ? t("dialog.fields.password.saved") : t("dialog.fields.password.hint")}
          >
            <Input type="password" readOnly={managed} autoComplete="new-password" {...form.register("password")} />
          </Field>
          <Field label={t("dialog.fields.mac.label")} error={errors.mac?.message} hint={t("dialog.fields.mac.hint")}>
            <Input mono readOnly={managed} placeholder="DC:A6:32:4B:19:0E" {...form.register("mac")} />
          </Field>
          <Field
            label={t("dialog.fields.broadcast.label")}
            error={errors.broadcast?.message}
            hint={t("dialog.fields.broadcast.hint")}
          >
            <Input mono readOnly={managed} placeholder="192.168.1.255" {...form.register("broadcast")} />
          </Field>
          <Field
            label={t("dialog.fields.language.label")}
            error={errors.language?.message}
            hint={t("dialog.fields.language.hint")}
          >
            <Select disabled={managed} {...form.register("language")}>
              {LANGUAGES.map((l) => (
                <option key={l.code} value={l.code} lang={l.code}>
                  {l.name}
                </option>
              ))}
            </Select>
          </Field>
          <Field
            label={t("dialog.fields.timezone.label")}
            error={errors.timezone?.message}
            hint={t("dialog.fields.timezone.hint")}
          >
            <Input mono readOnly={managed} placeholder="Australia/Melbourne" {...form.register("timezone")} />
          </Field>
          <Field
            label={t("dialog.fields.shutdown.label")}
            error={errors.shutdown?.message}
            hint={t("dialog.fields.shutdown.hint")}
          >
            <Select disabled={managed} {...form.register("shutdown")}>
              {SHUTDOWN.map((value) => (
                <option key={value} value={value}>
                  {t(`dialog.fields.shutdown.${value}`)}
                </option>
              ))}
            </Select>
          </Field>
        </FieldGrid>
        {failure && !hasFieldErrors ? (
          <p role="alert" className="rounded-[10px] bg-bad-bg px-3.5 py-2.5 text-bad">
            {errorMessage(failure)}
          </p>
        ) : null}
      </form>
    </Dialog>
  );
}
