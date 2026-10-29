import { type ReactNode, useState } from "react";

import { useTranslation } from "react-i18next";

import { errorMessage } from "@api/client";
import type { Settings } from "@api/generated/model";
import { isManaged, useSaveSettings } from "@api/settings";
import { ManagedBadge } from "@components/ui/Chip";
import { Input, Switch } from "@components/ui/Field";
import { FormError } from "@features/settings/FormError";

/** Titled by settings:features.<key>. */
const FEATURES = ["confirm_sound", "deep_search", "auto_deploy", "shutdown_quits"] as const satisfies ReadonlyArray<
  keyof Settings["features"]
>;

const QUEUE_LIMIT_MAX = 1000;
const AUTO_DEPLOY_MINUTES_MAX = 24 * 60;

/** Each switch saves straight away. */
export function FeaturesTab({ settings }: { settings: Settings }) {
  const { t } = useTranslation("settings");
  const save = useSaveSettings();
  return (
    <div className="flex flex-col">
      <h2 className="mb-1.5 font-display font-semibold text-lg">{t("features.title")}</h2>
      <FormError message={save.error ? errorMessage(save.error) : undefined} />
      {FEATURES.map((key) => {
        const managed = isManaged(settings, `features.${key}`);
        const title = t(`features.${key}.title`);
        return (
          <div key={key} className="flex flex-col border-line-soft border-b py-3.5">
            <div className="flex items-center justify-between gap-4">
              <span className="flex flex-col">
                <span className="flex flex-wrap items-center gap-2 font-semibold">
                  {title}
                  {managed ? <ManagedBadge /> : null}
                </span>
                <span className="text-[13px] text-muted">{t(`features.${key}.hint`)}</span>
              </span>
              <Switch
                label={title}
                checked={settings.features[key]}
                disabled={managed || save.isPending}
                onCheckedChange={(checked) => save.mutate({ features: { [key]: checked } })}
              />
            </div>
            {key === "auto_deploy" && settings.features.auto_deploy ? (
              <div className="mt-3 ms-1 border-line border-s-2 ps-4">
                <NumberSetting
                  key={settings.features.auto_deploy_minutes}
                  settings={settings}
                  save={save}
                  field="auto_deploy_minutes"
                  max={AUTO_DEPLOY_MINUTES_MAX}
                  fractional
                  unit={t("features.auto_deploy_minutes.unit")}
                  hint={(n) => <AutoDeployHint minutes={n} />}
                />
              </div>
            ) : null}
          </div>
        );
      })}
      <div className="py-3.5">
        <NumberSetting
          key={settings.features.queue_limit}
          settings={settings}
          save={save}
          field="queue_limit"
          max={QUEUE_LIMIT_MAX}
          hint={() => t("features.queue_limit.hint")}
        />
      </div>
    </div>
  );
}

/** How soon a change goes live: within two checks. */
function AutoDeployHint({ minutes }: { minutes: number | undefined }) {
  const { t } = useTranslation("settings");
  if (minutes === undefined) return t("features.auto_deploy_minutes.hintInvalid", { max: AUTO_DEPLOY_MINUTES_MAX });
  // Rounded to a tenth so the hint doesn't spell out every digit typed.
  const within = Math.round(minutes * 20) / 10;
  return within % 60 === 0
    ? t("features.auto_deploy_minutes.hint_hours", { count: within / 60 })
    : t("features.auto_deploy_minutes.hint_minutes", { count: within });
}

type NumberField = "auto_deploy_minutes" | "queue_limit";

/** Keyed by its value so a saved or reloaded value resets the field. Saves when the field loses focus or Enter is pressed, if the number changed and is in range; only a fractional field takes fractions. hint gets the typed number, or undefined while it's out of range. */
function NumberSetting({
  settings,
  save,
  field,
  max,
  fractional = false,
  unit,
  hint,
}: {
  settings: Settings;
  save: ReturnType<typeof useSaveSettings>;
  field: NumberField;
  max: number;
  fractional?: boolean;
  unit?: string;
  hint: (value: number | undefined) => ReactNode;
}) {
  const { t } = useTranslation("settings");
  const current = settings.features[field];
  const [value, setValue] = useState(String(current));
  const managed = isManaged(settings, `features.${field}`);
  const title = t(`features.${field}.title`);
  const id = `feature-${field}`;
  const parse = (text: string) => {
    const n = Number(text);
    return text.trim() !== "" && (fractional ? Number.isFinite(n) : Number.isInteger(n)) && n >= 1 && n <= max
      ? n
      : undefined;
  };
  const commit = () => {
    const n = parse(value);
    if (n === undefined) {
      setValue(String(current));
      return;
    }
    if (n !== current) save.mutate({ features: { [field]: n } });
  };
  return (
    <div className="flex items-center justify-between gap-4">
      <label htmlFor={id} className="flex flex-col">
        <span className="flex flex-wrap items-center gap-2 font-semibold">
          {title}
          {managed ? <ManagedBadge /> : null}
        </span>
        <span className="text-[13px] text-muted">{hint(parse(value))}</span>
      </label>
      <span className="flex shrink-0 items-center gap-2">
        <Input
          id={id}
          type="number"
          inputMode={fractional ? "decimal" : "numeric"}
          step={fractional ? "any" : 1}
          min={1}
          max={max}
          className="w-24 text-end"
          value={value}
          disabled={managed || save.isPending}
          onChange={(event) => setValue(event.target.value)}
          onBlur={commit}
          onKeyDown={(event) => {
            if (event.key === "Enter") commit();
          }}
        />
        {unit ? <span className="text-[13px] text-muted">{unit}</span> : null}
      </span>
    </div>
  );
}
