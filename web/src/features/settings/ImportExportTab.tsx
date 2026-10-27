import { useId, useState } from "react";

import { Upload } from "lucide-react";
import { useTranslation } from "react-i18next";

import { errorMessage } from "@api/client";
import { configExportUrl, useImportConfig } from "@api/settings";
import { skillPackageUrl } from "@api/skill";
import { AnchorButton, buttonClass } from "@components/ui/Button";
import { cn } from "@components/ui/cn";
import { Checkbox } from "@components/ui/Field";
import { FormError } from "@features/settings/FormError";

export function ImportExportTab() {
  const { t } = useTranslation("settings");
  const [includeSecrets, setIncludeSecrets] = useState(false);
  const importConfig = useImportConfig();
  const inputId = useId();

  async function onFile(file: File | undefined) {
    if (!file) return;
    importConfig.mutate(await file.text());
  }

  return (
    <div className="flex flex-col gap-3.5">
      <h2 className="font-display font-semibold text-lg">{t("backup.title")}</h2>
      <p className="text-[13px] text-muted">{t("backup.intro")}</p>
      <Checkbox
        checked={includeSecrets}
        onCheckedChange={setIncludeSecrets}
        label={t("backup.includeSecrets.label")}
        hint={t("backup.includeSecrets.hint")}
      />
      <div className="flex flex-wrap gap-2.5">
        <AnchorButton href={configExportUrl(includeSecrets)} download="encore.yml">
          {t("backup.export")}
        </AnchorButton>
        <label
          htmlFor={inputId}
          className={cn(
            buttonClass("default"),
            "cursor-pointer has-focus-visible:outline-2 has-focus-visible:outline-focus has-focus-visible:outline-offset-2",
          )}
        >
          <Upload aria-hidden="true" className="size-4" />
          {t("backup.import")}
          <input
            id={inputId}
            type="file"
            accept=".yml,.yaml,application/yaml,text/yaml"
            className="sr-only"
            onChange={(event) => {
              void onFile(event.target.files?.[0]);
              event.target.value = "";
            }}
          />
        </label>
        <AnchorButton href={skillPackageUrl()} download="encore-skill.zip">
          {t("backup.skillPackage")}
        </AnchorButton>
      </div>
      <div aria-live="polite">
        {importConfig.isPending ? <p className="text-muted">{t("backup.importing")}</p> : null}
        <FormError message={importConfig.error ? errorMessage(importConfig.error) : undefined} />
        {importConfig.data ? (
          <div className="flex flex-col gap-1 rounded-[10px] bg-soft px-3.5 py-3">
            <span className="font-semibold">
              {importConfig.data.applied.length
                ? t("backup.imported", { items: importConfig.data.applied.join(", ") })
                : t("backup.importedNothing")}
            </span>
            {importConfig.data.skipped.length ? (
              <span className="text-[13px] text-muted">
                {t("backup.skipped", { items: importConfig.data.skipped.join(", ") })}
              </span>
            ) : null}
          </div>
        ) : null}
      </div>
    </div>
  );
}
