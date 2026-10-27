import { useState } from "react";

import { useTranslation } from "react-i18next";

import type { Settings } from "@api/generated/model";
import { isManaged, useRotateSecrets } from "@api/settings";
import { Button } from "@components/ui/Button";
import { Dialog } from "@components/ui/Dialog";
import { Field, FieldGrid, Input } from "@components/ui/Field";
import { formatDate } from "@lib/format";

export function SecretsTab({ settings }: { settings: Settings }) {
  const { t } = useTranslation("settings");
  const [open, setOpen] = useState(false);
  const rotate = useRotateSecrets();
  const { key_source, key_id, rotated_at } = settings.secrets;
  return (
    <div className="flex flex-col gap-4">
      <h2 className="font-display font-semibold text-lg">{t("secrets.title")}</h2>
      <FieldGrid>
        <Field label={t("secrets.source.label")} managed={isManaged(settings, "secrets.key_source")}>
          <Input mono readOnly value={t(`secrets.source.${key_source}`)} />
        </Field>
        <Field label={t("secrets.keyId.label")}>
          <Input
            mono
            readOnly
            value={rotated_at ? t("secrets.keyId.rotated", { date: formatDate(rotated_at), keyId: key_id }) : key_id}
          />
        </Field>
      </FieldGrid>
      <p className="text-[13px] text-muted">{t("secrets.encrypted")}</p>
      <div>
        <Button onClick={() => setOpen(true)}>{t("secrets.rotate.button")}</Button>
      </div>
      <Dialog
        open={open}
        onOpenChange={setOpen}
        title={t("secrets.rotate.title")}
        description={t("secrets.rotate.description")}
        footer={
          <>
            <Button onClick={() => setOpen(false)}>{t("secrets.rotate.cancel")}</Button>
            <Button
              variant="primary"
              pending={rotate.isPending}
              onClick={() => rotate.mutate(undefined, { onSuccess: () => setOpen(false) })}
            >
              {t("secrets.rotate.confirm")}
            </Button>
          </>
        }
      />
    </div>
  );
}
