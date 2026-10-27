import { useEffect, useState } from "react";

import { useTranslation } from "react-i18next";

import { errorMessage } from "@api/client";
import type { Instance, Library } from "@api/generated/model";
import { useGroupLibrary } from "@api/kodi";
import { Button } from "@components/ui/Button";
import { Dialog } from "@components/ui/Dialog";
import { Checkbox } from "@components/ui/Field";

interface ShareLibraryDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  instances: Instance[];
  libraries: Library[];
  /** Instances ticked when the dialog opens, such as an existing group's members. */
  initial?: number[] | undefined;
}

/** Groups Kodi instances that use one Kodi database (such as MySQL) into a shared library. The first ticked instance becomes the library source. */
export function ShareLibraryDialog({ open, onOpenChange, instances, libraries, initial }: ShareLibraryDialogProps) {
  const { t } = useTranslation("kodi");
  const group = useGroupLibrary();
  const [selected, setSelected] = useState<number[]>(initial ?? []);

  // Start from the given ticks each time the dialog opens, while rendering rather than in the effect below.
  const [wasOpen, setWasOpen] = useState(open);
  if (open !== wasOpen) {
    setWasOpen(open);
    if (open) setSelected(initial ?? []);
  }

  useEffect(() => {
    if (open) group.reset();
  }, [open, group.reset]);

  const sharedWith = (instance: Instance) => {
    const library = libraries.find((l) => l.members.length > 1 && l.members.some((m) => m.instance_id === instance.id));
    return library?.members.filter((m) => m.instance_id !== instance.id).map((m) => m.name);
  };

  const toggle = (id: number, on: boolean) =>
    setSelected((ids) => (on ? [...ids.filter((x) => x !== id), id] : ids.filter((x) => x !== id)));

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={t("library.share.title")}
      description={t("library.share.description")}
      footer={
        <>
          <Button onClick={() => onOpenChange(false)}>{t("library.share.cancel")}</Button>
          <Button
            variant="primary"
            disabled={selected.length < 2}
            pending={group.isPending}
            onClick={() => group.mutate(selected, { onSuccess: () => onOpenChange(false) })}
          >
            {t("library.share.confirm", { count: selected.length })}
          </Button>
        </>
      }
    >
      <fieldset className="flex flex-col gap-3">
        <legend className="mb-1 font-medium">{t("library.share.legend")}</legend>
        {instances.map((instance) => {
          const others = sharedWith(instance);
          return (
            <Checkbox
              key={instance.id}
              label={instance.name}
              checked={selected.includes(instance.id)}
              onCheckedChange={(on) => toggle(instance.id, on)}
              hint={others?.length ? t("library.share.alreadyShared", { names: others.join(", ") }) : undefined}
            />
          );
        })}
      </fieldset>
      <p className="mt-3 text-[13px] text-muted">{t("library.share.sourceHint")}</p>
      {group.error ? (
        <p role="alert" className="mt-3 rounded-[10px] bg-bad-bg px-3.5 py-2.5 text-bad">
          {errorMessage(group.error)}
        </p>
      ) : null}
    </Dialog>
  );
}
