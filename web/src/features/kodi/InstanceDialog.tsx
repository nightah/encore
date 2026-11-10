import { useEffect, useMemo, useState } from "react";

import type { TFunction } from "i18next";
import { useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { z } from "zod";

import { ApiError, errorMessage, fieldErrorsOf } from "@api/client";
import type { Instance, InstanceInput as InstanceBody } from "@api/generated/model";
import { useCreateInstance, useDeleteInstance, useInstanceSkin, useSkinProfiles, useUpdateInstance } from "@api/kodi";
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

const QUALITY = ["inherit", "screen", "highest", "2160p", "1080p", "720p", "sd"] as const satisfies ReadonlyArray<
  Instance["preferred_quality"]
>;

const SEARCH_MODES = ["inherit", "library", "global_search", "skin"] as const satisfies ReadonlyArray<
  Instance["search_mode"]
>;

const makeSchema = (t: TFunction<"kodi">) =>
  z.object({
    broadcast: z.string().trim(),
    language: z.string(),
    mac: optionalUrl(/^([0-9A-Fa-f]{2}[:-]){5}[0-9A-Fa-f]{2}$/, t("dialog.errors.mac")),
    name: z.string().trim().min(1, t("dialog.errors.name")),
    password: z.string(),
    preferred_quality: z.enum(QUALITY),
    search_mode: z.enum(SEARCH_MODES),
    shutdown: z.enum(SHUTDOWN),
    skin_profile: z.string(),
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
  preferred_quality: "inherit",
  search_mode: "inherit",
  shutdown: "inherit",
  skin_profile: "",
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
    preferred_quality: instance.preferred_quality,
    search_mode: instance.search_mode,
    shutdown: instance.shutdown,
    skin_profile: instance.skin_profile,
    timezone: instance.timezone,
    url: instance.url,
    username: instance.username,
    websocket_url: instance.websocket_url,
  };
}

interface InstanceDialogProps {
  open: boolean;
  onClose: () => void;
  /** Edit this instance; omit to add a new one. */
  instance?: Instance | undefined;
  /** Prefilled values for a new instance (e.g. from discovery). */
  prefill?: Partial<InstanceForm> | undefined;
}

/** Add or edit a Kodi instance. Config-managed instances open read-only. */
export function InstanceDialog({ open, onClose, instance, prefill }: InstanceDialogProps) {
  const { t } = useTranslation("kodi");
  const schema = useMemo(() => makeSchema(t), [t]);
  const create = useCreateInstance();
  const update = useUpdateInstance();
  const remove = useDeleteInstance();
  const [confirmRemove, setConfirmRemove] = useState(false);
  const managed = instance?.managed_by_config ?? false;
  const profiles = useSkinProfiles();
  const form = useForm<InstanceForm>({
    defaultValues: EMPTY,
    resolver: zodResolver(schema),
  });

  useEffect(() => {
    if (!open) return;
    form.reset(instance ? fromInstance(instance) : { ...EMPTY, ...prefill });
    create.reset();
    update.reset();
  }, [open, instance, prefill, form, create.reset, update.reset]);

  // Every way out of the dialog comes through here, so it always reopens without the removal confirmation.
  const close = () => {
    setConfirmRemove(false);
    onClose();
  };

  const pending = create.isPending || update.isPending;
  const failure = create.error ?? update.error;
  const errors = form.formState.errors;

  const onSubmit = form.handleSubmit(async (values) => {
    const body: InstanceBody = {
      language: values.language,
      name: values.name,
      preferred_quality: values.preferred_quality,
      search_mode: values.search_mode,
      shutdown: values.shutdown,
      skin_profile: values.skin_profile,
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
      close();
    } catch (error) {
      for (const [name, message] of Object.entries(fieldErrorsOf(error))) {
        if (name in values) form.setError(name as keyof InstanceForm, { message });
      }
    }
  });

  const title = instance ? t("dialog.editTitle", { name: instance.name }) : t("dialog.addTitle");
  // Only errors shown next to a field; any other failure (such as one for the whole body) goes in the alert.
  const hasFieldErrors = failure instanceof ApiError && Object.keys(failure.fieldErrors).some((name) => name in EMPTY);

  return (
    <Dialog
      open={open}
      onClose={close}
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
            <span className="me-auto self-center">{t("dialog.confirmRemove", { name: instance.name })}</span>
            <Button onClick={() => setConfirmRemove(false)}>{t("dialog.keep")}</Button>
            <Button
              variant="danger"
              pending={remove.isPending}
              onClick={() => remove.mutate(instance.id, { onSuccess: close })}
            >
              {t("dialog.remove")}
            </Button>
          </>
        ) : (
          <>
            {instance && !managed ? (
              <Button variant="danger" className="me-auto" onClick={() => setConfirmRemove(true)}>
                {t("dialog.removeEllipsis")}
              </Button>
            ) : null}
            <Button onClick={close}>{managed ? t("dialog.close") : t("dialog.cancel")}</Button>
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
          <Field
            label={t("dialog.fields.preferred_quality.label")}
            error={errors.preferred_quality?.message}
            hint={
              <>
                {t("dialog.fields.preferred_quality.hint")}{" "}
                {instance?.screen
                  ? t("dialog.fields.preferred_quality.screen_known", {
                      height: instance.screen.height,
                      quality: t(`dialog.fields.preferred_quality.screen_quality.${instance.screen.quality}`),
                      width: instance.screen.width,
                    })
                  : t("dialog.fields.preferred_quality.screen_unknown")}
              </>
            }
          >
            <Select disabled={managed} {...form.register("preferred_quality")}>
              {QUALITY.map((value) => (
                <option key={value} value={value}>
                  {t(`dialog.fields.preferred_quality.${value}`)}
                </option>
              ))}
            </Select>
          </Field>
          <Field
            label={t("dialog.fields.search_mode.label")}
            error={errors.search_mode?.message}
            hint={
              <>
                {t("dialog.fields.search_mode.hint")} {open && instance ? <SkinStatus id={instance.id} /> : null}
              </>
            }
          >
            <Select disabled={managed} {...form.register("search_mode")}>
              {SEARCH_MODES.map((value) => (
                <option key={value} value={value}>
                  {t(`dialog.fields.search_mode.${value}`)}
                </option>
              ))}
            </Select>
          </Field>
          <Field
            label={t("dialog.fields.skin_profile.label")}
            error={errors.skin_profile?.message}
            hint={t("dialog.fields.skin_profile.hint")}
          >
            <Select disabled={managed} {...form.register("skin_profile")}>
              <option value="">{t("dialog.fields.skin_profile.none")}</option>
              {(profiles.data ?? [])
                .filter((p) => !p.native)
                .map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
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

/** The skin Kodi is running and what a search does on it, e.g. "Arctic Fuse 3: skin search supported." */
function SkinStatus({ id }: { id: number }) {
  const { t } = useTranslation("kodi");
  const skin = useInstanceSkin(id);
  const profiles = useSkinProfiles();
  if (skin.isPending) return <span>{t("dialog.fields.search_mode.status.checking")}</span>;
  const data = skin.data;
  const status = data?.skin;
  if (!status) return <span>{t("dialog.fields.search_mode.status.unknown")}</span>;
  const profile = profiles.data?.find((p) => p.id === status.profile)?.name ?? status.profile;
  const borrowed = status.search === "supported" && status.profile !== status.id;
  return (
    <span data-testid="skin-status">
      {borrowed
        ? t("dialog.fields.search_mode.status.supported_as", { profile, skin: status.name })
        : t(`dialog.fields.search_mode.status.${status.search}`, {
            addons: status.missing.join(", "),
            skin: status.name,
          })}
      {data?.search_mode !== "skin" ? ` ${t("dialog.fields.search_mode.status.off")}` : null}
    </span>
  );
}
