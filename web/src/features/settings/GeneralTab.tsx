import { type FormEvent, type ReactNode, useCallback, useEffect, useId } from "react";

import { useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";

import type { Settings } from "@api/generated/model";
import { isManaged } from "@api/settings";
import { useSkill } from "@api/skill";
import { Field, Input, RadioCards, Select } from "@components/ui/Field";
import { FormError } from "@features/settings/FormError";
import { type FieldMapping, type FormState, useSettingsForm } from "@features/settings/form";
import { LANGUAGES, LANGUAGE_NAMES, type Language, browserLanguage, setLanguage, storedLanguage } from "@i18n/index";
import { type ThemePreference, endThemePreview, previewTheme, setThemePreference, useTheme } from "@theme/store";

interface Values {
  base_path: string;
  timezone: string;
  default_locale: string;
  log_level: Settings["logging"]["level"];
  // Saved in this browser, not on the server, so it has no mapping.
  language: Language | "auto";
}

const mapping: FieldMapping<Values> = {
  base_path: { path: "server.base_path" },
  default_locale: { path: "locale.default_locale" },
  log_level: { path: "logging.level" },
  timezone: { path: "locale.timezone" },
};

const toValues = (s: Settings): Values => ({
  base_path: s.server.base_path,
  default_locale: s.locale.default_locale,
  language: storedLanguage() ?? "auto",
  log_level: s.logging.level,
  timezone: s.locale.timezone,
});

const LOG_LEVELS = ["debug", "info", "warn", "error"] as const;

export function GeneralTab({
  settings,
  formId,
  onFormState,
}: {
  settings: Settings;
  formId: string;
  onFormState: (state: FormState) => void;
}) {
  const { t } = useTranslation("settings");
  // The theme previews as soon as it's picked and is saved, in this browser, with the rest of the form.
  const { preference, saved } = useTheme();
  const themeDirty = preference !== saved;
  useEffect(() => endThemePreview, []);
  const onState = useCallback(
    (state: FormState) => onFormState({ ...state, dirty: state.dirty || themeDirty }),
    [onFormState, themeDirty],
  );
  const form = useForm<Values>({ defaultValues: toValues(settings) });
  // The default is always one of the skill's enabled locales; the server picks the first until one is chosen.
  const { data: skill } = useSkill();
  const locales = skill?.locales_enabled.length ? skill.locales_enabled : [settings.locale.default_locale];
  const { onSubmit, formError } = useSettingsForm({
    form,
    mapping,
    onFormState: onState,
    settings,
    toValues,
  });
  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setThemePreference(preference);
    const language = form.getValues("language");
    await setLanguage(language === "auto" ? undefined : language);
    return onSubmit(event);
  };
  const browser = LANGUAGE_NAMES[browserLanguage()];

  return (
    <form id={formId} onSubmit={submit} noValidate className="flex flex-col gap-6">
      <h2 className="font-display font-semibold text-lg">{t("general.title")}</h2>
      <FormError message={formError} />
      <Section title={t("general.sections.appearance")}>
        <div className="col-span-full flex flex-col gap-2">
          <RadioCards<ThemePreference>
            legend={t("general.theme.legend")}
            value={preference}
            onValueChange={previewTheme}
            options={[
              {
                hint: t("general.theme.system.hint"),
                label: t("general.theme.system.label"),
                value: "system",
              },
              { hint: t("general.theme.light.hint"), label: t("general.theme.light.label"), value: "light" },
              { hint: t("general.theme.dark.hint"), label: t("general.theme.dark.label"), value: "dark" },
            ]}
          />
          <p className="text-[13px] text-muted">{t("general.theme.note")}</p>
        </div>
        <Field label={t("general.language.label")} hint={t("general.language.hint")}>
          <Select {...form.register("language")}>
            <option value="auto">{t("general.language.auto", { language: browser })}</option>
            {LANGUAGES.map((l) => (
              <option key={l.code} value={l.code} lang={l.code}>
                {l.name}
              </option>
            ))}
          </Select>
        </Field>
      </Section>
      <Section title={t("general.sections.locale")}>
        <Field
          label={t("general.timezone.label")}
          managed={isManaged(settings, "locale.timezone")}
          hint={t("general.timezone.hint")}
          error={form.formState.errors.timezone?.message}
        >
          <Input {...form.register("timezone")} />
        </Field>
        <Field
          label={t("general.defaultLocale.label")}
          managed={isManaged(settings, "locale.default_locale")}
          error={form.formState.errors.default_locale?.message}
        >
          <Select {...form.register("default_locale")}>
            {locales.map((locale) => (
              <option key={locale} value={locale}>
                {locale}
              </option>
            ))}
          </Select>
        </Field>
      </Section>
      <Section title={t("general.sections.server")}>
        <Field
          label={t("general.basePath.label")}
          managed={isManaged(settings, "server.base_path")}
          hint={t("general.basePath.hint")}
          error={form.formState.errors.base_path?.message}
        >
          <Input mono placeholder="/encore" {...form.register("base_path")} />
        </Field>
      </Section>
      <Section title={t("general.sections.logging")}>
        <Field
          label={t("general.logLevel.label")}
          managed={isManaged(settings, "logging.level")}
          hint={t("general.logLevel.hint")}
          error={form.formState.errors.log_level?.message}
        >
          <Select {...form.register("log_level")}>
            {LOG_LEVELS.map((level) => (
              <option key={level} value={level}>
                {t(`general.logLevel.${level}`)}
              </option>
            ))}
          </Select>
        </Field>
      </Section>
    </form>
  );
}

/** A titled group of fields. The grid fills columns rather than stretching, so a lone field lines up with the fields in other sections. */
function Section({ title, children }: { title: string; children: ReactNode }) {
  const id = useId();
  return (
    <section aria-labelledby={id} className="flex flex-col gap-3 border-line-soft border-t pt-4.5">
      <h3 id={id} className="font-sans font-semibold">
        {title}
      </h3>
      <div className="grid grid-cols-[repeat(auto-fill,minmax(240px,1fr))] gap-4">{children}</div>
    </section>
  );
}
