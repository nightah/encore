import { useState } from "react";

import { useTranslation } from "react-i18next";

import { errorMessage } from "@api/client";
import type { Settings } from "@api/generated/model";
import { isManaged, useSaveSettings } from "@api/settings";
import { ManagedBadge } from "@components/ui/Chip";
import { Input, Switch } from "@components/ui/Field";
import { FormError } from "@features/settings/FormError";

/** Titled by settings:features.<key>. */
const FEATURES = ["confirm_sound", "deep_search", "shutdown_quits"] as const satisfies ReadonlyArray<
  keyof Settings["features"]
>;

const QUEUE_LIMIT_MAX = 1000;

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
          <div key={key} className="flex items-center justify-between gap-4 border-line-soft border-b py-3.5">
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
        );
      })}
      {/* Keyed so a saved or reloaded value resets the field. */}
      <QueueLimit key={settings.features.queue_limit} settings={settings} save={save} />
    </div>
  );
}

/** Saves when the field loses focus or Enter is pressed, if the number changed and is in range. */
function QueueLimit({ settings, save }: { settings: Settings; save: ReturnType<typeof useSaveSettings> }) {
  const { t } = useTranslation("settings");
  const current = settings.features.queue_limit;
  const [value, setValue] = useState(String(current));
  const managed = isManaged(settings, "features.queue_limit");
  const title = t("features.queue_limit.title");
  const commit = () => {
    const n = Number(value);
    if (!Number.isInteger(n) || n < 1 || n > QUEUE_LIMIT_MAX) {
      setValue(String(current));
      return;
    }
    if (n !== current) save.mutate({ features: { queue_limit: n } });
  };
  return (
    <div className="flex items-center justify-between gap-4 py-3.5">
      <label htmlFor="queue-limit" className="flex flex-col">
        <span className="flex flex-wrap items-center gap-2 font-semibold">
          {title}
          {managed ? <ManagedBadge /> : null}
        </span>
        <span className="text-[13px] text-muted">{t("features.queue_limit.hint")}</span>
      </label>
      <Input
        id="queue-limit"
        type="number"
        inputMode="numeric"
        min={1}
        max={QUEUE_LIMIT_MAX}
        className="w-24 text-right"
        value={value}
        disabled={managed || save.isPending}
        onChange={(event) => setValue(event.target.value)}
        onBlur={commit}
        onKeyDown={(event) => {
          if (event.key === "Enter") commit();
        }}
      />
    </div>
  );
}
