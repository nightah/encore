import { Controller, useForm, useWatch } from "react-hook-form";
import { useTranslation } from "react-i18next";

import { errorMessage } from "@api/client";
import type { Settings, StorageSettingsBackend as StorageBackend } from "@api/generated/model";
import { isManaged, useRunBackup, useTestStorage } from "@api/settings";
import { Button } from "@components/ui/Button";
import { Checkbox, Field, FieldGrid, Input, RadioCards, Select, StaticField, Textarea } from "@components/ui/Field";
import { FormError } from "@features/settings/FormError";
import { type FieldMapping, type FormState, setPath, useSettingsForm } from "@features/settings/form";
import { formatRelative } from "@lib/format";

interface Values {
  backend: StorageBackend;
  s3_endpoint: string;
  s3_region: string;
  s3_bucket: string;
  s3_prefix: string;
  s3_access_key: string;
  s3_secret_key: string;
  s3_path_style: boolean;
  s3_custom_ca: boolean;
  s3_ca_bundle: string;
  s3_sse: boolean;
  dav_url: string;
  dav_username: string;
  dav_password: string;
  local_path: string;
  backup_schedule: string;
  backup_keep: string;
  backup_prefix: string;
}

const mapping = {
  backend: { path: "storage.backend" },
  backup_keep: {
    apply: (v, p) => setPath(p, "storage.backups.keep", Number(v)),
    path: "storage.backups.keep",
  },
  backup_prefix: { path: "storage.backups.prefix" },
  backup_schedule: { path: "storage.backups.schedule" },
  dav_password: { path: "storage.webdav.password" },
  dav_url: { path: "storage.webdav.url" },
  dav_username: { path: "storage.webdav.username" },
  local_path: { path: "storage.local.path" },
  s3_access_key: { path: "storage.s3.access_key" },
  s3_bucket: { path: "storage.s3.bucket" },
  s3_ca_bundle: { path: "storage.s3.ca_bundle" },
  // Unticking "Custom CA certificate" clears the bundle; listed after it so it wins.
  s3_custom_ca: {
    apply: (v, p) => {
      if (!v) setPath(p, "storage.s3.ca_bundle", "");
    },
    path: "storage.s3.ca_bundle",
  },
  s3_endpoint: { path: "storage.s3.endpoint" },
  s3_path_style: {
    apply: (v, p) => setPath(p, "storage.s3.addressing", v ? "path" : "auto"),
    path: "storage.s3.addressing",
  },
  s3_prefix: { path: "storage.s3.prefix" },
  s3_region: { path: "storage.s3.region" },
  s3_secret_key: { path: "storage.s3.secret_key" },
  s3_sse: {
    apply: (v, p) => setPath(p, "storage.s3.sse", v ? "AES256" : ""),
    path: "storage.s3.sse",
  },
} satisfies FieldMapping<Values>;

const toValues = (s: Settings): Values => ({
  backend: s.storage.backend,
  backup_keep: String(s.storage.backups.keep),
  backup_prefix: s.storage.backups.prefix,
  backup_schedule: s.storage.backups.schedule,
  dav_password: "",
  dav_url: s.storage.webdav.url,
  dav_username: s.storage.webdav.username,
  local_path: s.storage.local.path,
  s3_access_key: s.storage.s3.access_key,
  s3_bucket: s.storage.s3.bucket,
  s3_ca_bundle: s.storage.s3.ca_bundle,
  s3_custom_ca: s.storage.s3.ca_bundle !== "",
  s3_endpoint: s.storage.s3.endpoint,
  s3_path_style: s.storage.s3.addressing === "path",
  s3_prefix: s.storage.s3.prefix,
  s3_region: s.storage.s3.region,
  s3_secret_key: "",
  s3_sse: s.storage.s3.sse !== "",
});

/** Each offered schedule and its settings:storage.backups.schedules key. */
const SCHEDULES = [
  ["daily@03:00", "daily"],
  ["weekly@sun 03:00", "weekly"],
  ["off", "off"],
] as const;

export function StorageTab({
  settings,
  formId,
  onFormState,
}: {
  settings: Settings;
  formId: string;
  onFormState: (state: FormState) => void;
}) {
  const { t } = useTranslation("settings");
  const form = useForm<Values>({ defaultValues: toValues(settings) });
  const { onSubmit, formError } = useSettingsForm({
    form,
    mapping,
    onFormState,
    settings,
    toValues,
  });
  const test = useTestStorage();
  const backup = useRunBackup();
  const backend = useWatch({ control: form.control, name: "backend" });
  const customCa = useWatch({ control: form.control, name: "s3_custom_ca" });
  const dirty = form.formState.isDirty;
  const managed = (key: keyof typeof mapping) => isManaged(settings, mapping[key].path);
  const current = settings.storage.backups.schedule;
  const schedules: Array<[string, string]> = SCHEDULES.map(([value, key]) => [
    value,
    t(`storage.backups.schedules.${key}`),
  ]);
  if (!SCHEDULES.some(([v]) => v === current)) schedules.unshift([current, current]);
  const keeps = Array.from(new Set([7, 14, 30, settings.storage.backups.keep])).sort((a, b) => a - b);

  return (
    <form id={formId} onSubmit={onSubmit} noValidate className="flex flex-col gap-4.5">
      <div className="flex flex-col gap-1">
        <h2 className="font-display font-semibold text-lg">{t("storage.title")}</h2>
        <p className="max-w-[72ch] text-[13px] text-muted">{t("storage.intro")}</p>
      </div>
      <FormError message={formError} />
      <Controller
        control={form.control}
        name="backend"
        render={({ field }) => (
          <RadioCards<StorageBackend>
            legend={t("storage.backend.legend")}
            value={field.value}
            onValueChange={(value) => {
              field.onChange(value);
              test.reset();
            }}
            managed={managed("backend")}
            options={[
              {
                hint: "AWS, MinIO, Silo, Garage, Ceph, R2, B2",
                label: t("storage.backend.s3"),
                value: "s3",
              },
              { hint: t("storage.backend.webdavHint"), label: "WebDAV", value: "webdav" },
              { hint: t("storage.backend.localHint"), label: t("storage.backend.local"), value: "local" },
            ]}
          />
        )}
      />

      {backend === "s3" ? (
        <>
          <FieldGrid>
            <Field
              label={t("storage.s3.endpoint.label")}
              managed={managed("s3_endpoint")}
              hint={t("storage.s3.endpoint.hint")}
            >
              <Input mono {...form.register("s3_endpoint")} />
            </Field>
            <Field label={t("storage.s3.region")} managed={managed("s3_region")}>
              <Input mono {...form.register("s3_region")} />
            </Field>
            <Field label={t("storage.s3.bucket")} managed={managed("s3_bucket")}>
              <Input mono {...form.register("s3_bucket")} />
            </Field>
            <Field label={t("storage.s3.prefix")} managed={managed("s3_prefix")}>
              <Input mono {...form.register("s3_prefix")} />
            </Field>
            <Field label={t("storage.s3.accessKey")} managed={managed("s3_access_key")}>
              <Input mono autoComplete="off" {...form.register("s3_access_key")} />
            </Field>
            <Field
              label={t("storage.s3.secretKey.label")}
              managed={managed("s3_secret_key")}
              hint={
                settings.storage.s3.has_secret_key
                  ? t("storage.s3.secretKey.stored", { keyId: settings.secrets.key_id })
                  : t("storage.s3.secretKey.hint")
              }
            >
              <Input
                type="password"
                autoComplete="new-password"
                placeholder={settings.storage.s3.has_secret_key ? "••••••••••••" : ""}
                {...form.register("s3_secret_key")}
              />
            </Field>
          </FieldGrid>
          <div className="flex flex-wrap gap-5">
            <Controller
              control={form.control}
              name="s3_path_style"
              render={({ field }) => (
                <Checkbox
                  checked={field.value}
                  onCheckedChange={field.onChange}
                  disabled={managed("s3_path_style")}
                  label={t("storage.s3.pathStyle")}
                />
              )}
            />
            <Controller
              control={form.control}
              name="s3_custom_ca"
              render={({ field }) => (
                <Checkbox
                  checked={field.value}
                  onCheckedChange={field.onChange}
                  disabled={managed("s3_ca_bundle")}
                  label={t("storage.s3.customCa")}
                />
              )}
            />
            <Controller
              control={form.control}
              name="s3_sse"
              render={({ field }) => (
                <Checkbox
                  checked={field.value}
                  onCheckedChange={field.onChange}
                  disabled={managed("s3_sse")}
                  label={t("storage.s3.sse")}
                />
              )}
            />
          </div>
          {customCa ? (
            <Field label={t("storage.s3.caBundle")} managed={managed("s3_ca_bundle")}>
              <Textarea rows={5} className="font-mono text-[13px]" {...form.register("s3_ca_bundle")} />
            </Field>
          ) : null}
        </>
      ) : null}

      {backend === "webdav" ? (
        <FieldGrid>
          <Field label={t("storage.webdav.url.label")} managed={managed("dav_url")} hint={t("storage.webdav.url.hint")}>
            <Input mono {...form.register("dav_url")} />
          </Field>
          <Field label={t("storage.webdav.username")} managed={managed("dav_username")}>
            <Input autoComplete="off" {...form.register("dav_username")} />
          </Field>
          <Field
            label={t("storage.webdav.password.label")}
            managed={managed("dav_password")}
            hint={t("storage.webdav.password.hint")}
          >
            <Input
              type="password"
              autoComplete="new-password"
              placeholder={settings.storage.webdav.has_password ? "••••••••••••" : ""}
              {...form.register("dav_password")}
            />
          </Field>
        </FieldGrid>
      ) : null}

      {backend === "local" ? (
        <Field label={t("storage.local.label")} managed={managed("local_path")} hint={t("storage.local.hint")}>
          <Input mono {...form.register("local_path")} />
        </Field>
      ) : null}

      <div className="flex flex-wrap items-center gap-3">
        <Button disabled={dirty} pending={test.isPending} onClick={() => test.mutate()}>
          {t("storage.test.button")}
        </Button>
        <span aria-live="polite" className="contents">
          {dirty ? <span className="text-[13px] text-muted">{t("storage.test.saveFirst")}</span> : null}
          {!dirty && test.data?.ok ? (
            <span className="rounded-lg bg-ok-bg px-3 py-1.5 text-ok">
              {t("storage.test.ok", { ms: test.data.ms })}
            </span>
          ) : null}
          {!dirty && test.data && !test.data.ok ? (
            <span className="rounded-lg bg-bad-bg px-3 py-1.5 text-bad">
              {test.data.error || t("storage.test.failed")}
            </span>
          ) : null}
          {test.error ? (
            <span className="rounded-lg bg-bad-bg px-3 py-1.5 text-bad">{errorMessage(test.error)}</span>
          ) : null}
        </span>
      </div>

      <FieldGrid className="border-line-soft border-t pt-4.5">
        <Field label={t("storage.backups.label")} managed={managed("backup_schedule")} hint={t("storage.backups.hint")}>
          <Select {...form.register("backup_schedule")}>
            {schedules.map(([value, label]) => (
              <option key={value} value={value}>
                {label}
              </option>
            ))}
          </Select>
        </Field>
        <Field
          label={t("storage.backups.folder.label")}
          managed={managed("backup_prefix")}
          hint={t("storage.backups.folder.hint")}
        >
          <Input mono {...form.register("backup_prefix")} />
        </Field>
        <Field label={t("storage.backups.keep.label")} managed={managed("backup_keep")}>
          <Select {...form.register("backup_keep")}>
            {keeps.map((n) => (
              <option key={n} value={String(n)}>
                {t("storage.backups.keep.count", { count: n })}
              </option>
            ))}
          </Select>
        </Field>
        <StaticField
          label={t("storage.backups.last.label")}
          hint={
            settings.storage.backups.last_backup_at
              ? formatRelative(settings.storage.backups.last_backup_at)
              : t("storage.backups.last.never")
          }
        >
          <Button pending={backup.isPending} onClick={() => backup.mutate()}>
            {t("storage.backups.last.run")}
          </Button>
        </StaticField>
        <StaticField label={t("storage.backups.snapshot.label")}>
          <span>{t("storage.backups.snapshot.value")}</span>
        </StaticField>
      </FieldGrid>
    </form>
  );
}
