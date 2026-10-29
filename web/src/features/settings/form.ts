import { useEffect, useState } from "react";

import type { FieldValues, Path, UseFormReturn } from "react-hook-form";

import { errorMessage, problemErrorsOf } from "@api/client";
import type { Settings, SettingsPatch } from "@api/generated/model";
import { isManaged, useSaveSettings } from "@api/settings";

/** Each settings form keeps flat field names; this table maps them to dotted API paths. `apply` handles fields that don't map one to one (e.g. "5 attempts for 15 minutes" sets two keys). */
export type FieldMapping<V> = {
  [K in keyof V]?: { path: string; apply?: (value: V[K], patch: Record<string, unknown>) => void };
};

export function setPath(target: Record<string, unknown>, path: string, value: unknown): void {
  const parts = path.split(".");
  // split always returns at least one part.
  const last = parts.pop() as string;
  let node = target;
  for (const part of parts) {
    if (typeof node[part] !== "object" || node[part] === null) node[part] = {};
    node = node[part] as Record<string, unknown>;
  }
  node[last] = value;
}

/** Builds a PATCH body from the dirty fields only, skipping anything managed by config. */
function buildPatch<V extends FieldValues>(
  values: V,
  dirty: Partial<Record<keyof V, unknown>>,
  mapping: FieldMapping<V>,
  settings: Settings,
): SettingsPatch {
  const patch: Record<string, unknown> = {};
  for (const key of Object.keys(mapping) as Array<keyof V>) {
    const entry = mapping[key];
    if (!entry || !dirty[key] || isManaged(settings, entry.path)) continue;
    if (entry.apply) entry.apply(values[key], patch);
    else setPath(patch, entry.path, values[key]);
  }
  return patch as SettingsPatch;
}

export interface FormState {
  dirty: boolean;
  pending: boolean;
}

/** Submit handler shared by the settings forms: PATCH the dirty fields, reset the form to the saved values, and put server validation errors next to their fields (anything else shows above the form). */
export function useSettingsForm<V extends FieldValues>({
  form,
  mapping,
  settings,
  toValues,
  onFormState,
}: {
  form: UseFormReturn<V>;
  mapping: FieldMapping<V>;
  settings: Settings;
  toValues: (settings: Settings) => V;
  onFormState: (state: FormState) => void;
}) {
  const save = useSaveSettings();
  const [formError, setFormError] = useState<string>();
  const dirty = form.formState.isDirty;

  useEffect(() => {
    onFormState({ dirty, pending: save.isPending });
  }, [dirty, save.isPending, onFormState]);

  const onSubmit = form.handleSubmit(async (values) => {
    setFormError(undefined);
    const patch = buildPatch(
      values,
      form.formState.dirtyFields as Partial<Record<keyof V, unknown>>,
      mapping,
      settings,
    );
    if (Object.keys(patch).length === 0) {
      form.reset(values);
      return;
    }
    try {
      const next = await save.mutateAsync(patch);
      form.reset(toValues(next));
    } catch (error) {
      let placed = false;
      for (const problem of problemErrorsOf(error)) {
        const location = problem.location?.replace(/^body\./, "");
        const key = (Object.keys(mapping) as Array<keyof V>).find((k) => mapping[k]?.path === location);
        if (key) {
          form.setError(key as Path<V>, { message: problem.message });
          placed = true;
        }
      }
      if (!placed) setFormError(errorMessage(error));
    }
  });

  return { formError, onSubmit };
}
