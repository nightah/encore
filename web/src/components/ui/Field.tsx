import { type ComponentProps, type ReactElement, type ReactNode, cloneElement, isValidElement, useId } from "react";

import { Checkbox as BaseCheckbox } from "@base-ui/react/checkbox";
import { Radio } from "@base-ui/react/radio";
import { RadioGroup } from "@base-ui/react/radio-group";
import { Switch as BaseSwitch } from "@base-ui/react/switch";
import { Check } from "lucide-react";
import { useTranslation } from "react-i18next";

import { ManagedBadge } from "@components/ui/Chip";
import { cn, focusRing } from "@components/ui/cn";

const control =
  "min-h-11 min-w-0 rounded-lg border border-input-border bg-surface px-3 text-ink text-sm placeholder:text-muted read-only:bg-soft read-only:text-muted disabled:bg-soft disabled:text-muted aria-invalid:border-bad";

// React 19 passes ref as an ordinary prop, so it reaches the element with the rest (react-hook-form's register relies on it).
export function Input({ className, mono, ...props }: ComponentProps<"input"> & { mono?: boolean }) {
  return <input className={cn(control, focusRing, mono && "font-mono text-[13px]", className)} {...props} />;
}

export function Select({ className, ...props }: ComponentProps<"select">) {
  return <select className={cn(control, focusRing, "pe-8", className)} {...props} />;
}

export function Textarea({ className, ...props }: ComponentProps<"textarea">) {
  return <textarea className={cn(control, focusRing, "py-2.5", className)} {...props} />;
}

interface FieldProps {
  label: ReactNode;
  hint?: ReactNode;
  error?: string | undefined;
  managed?: boolean;
  /** Visually hide the label but keep it for assistive tech. */
  hideLabel?: boolean;
  className?: string;
  /** A single input/select/textarea; it receives `id`, `aria-describedby`, `aria-invalid` and, when managed, `readOnly`/`disabled`. */
  children: ReactElement<Record<string, unknown>>;
}

/** Label + control + hint + error, wired together with ids. */
export function Field({ label, hint, error, managed, hideLabel, className, children }: FieldProps) {
  const { t } = useTranslation("common");
  const id = useId();
  const hintId = `${id}-hint`;
  const errorId = `${id}-error`;
  const describedBy = [hint ? hintId : null, error ? errorId : null].filter(Boolean).join(" ");
  const isSelect = isValidElement(children) && children.type === Select;
  const child = cloneElement(children, {
    "aria-describedby": describedBy || undefined,
    "aria-invalid": error ? true : undefined,
    id,
    ...(managed ? (isSelect ? { disabled: true } : { readOnly: true }) : {}),
  });
  return (
    <div className={cn("flex min-w-0 flex-col gap-1.5", className)}>
      <label
        htmlFor={id}
        className={cn("flex flex-wrap items-center gap-2 font-medium text-sm", hideLabel && "sr-only")}
      >
        {label}
        {managed ? <span className="sr-only">{t("field.managedSuffix")}</span> : null}
      </label>
      {child}
      {/* Shown under the control rather than beside the label, so a managed field's control lines up with its neighbours'; the label carries it for screen readers. */}
      {managed ? (
        <span aria-hidden className="self-start">
          <ManagedBadge />
        </span>
      ) : null}
      {hint ? (
        <span id={hintId} className="text-[13px] text-muted">
          {hint}
        </span>
      ) : null}
      {error ? (
        <span id={errorId} className="text-[13px] text-bad">
          {error}
        </span>
      ) : null}
    </div>
  );
}

/** A field without a single control (e.g. a button or plain text) that still looks like the others. */
export function StaticField({ label, hint, children }: { label: ReactNode; hint?: ReactNode; children: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-1.5">
      <span className="font-medium text-sm">{label}</span>
      <div>{children}</div>
      {hint ? <span className="text-[13px] text-muted">{hint}</span> : null}
    </div>
  );
}

interface CheckboxProps {
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
  label: ReactNode;
  hint?: ReactNode;
  disabled?: boolean;
  name?: string;
  className?: string;
}

export function Checkbox({ checked, onCheckedChange, label, hint, disabled, name, className }: CheckboxProps) {
  const id = useId();
  return (
    <div className={cn("flex items-start gap-2.5", className)}>
      <BaseCheckbox.Root
        id={id}
        name={name}
        checked={checked}
        disabled={disabled}
        onCheckedChange={(value) => onCheckedChange(value)}
        aria-describedby={hint ? `${id}-hint` : undefined}
        className={cn(
          "relative mt-0.5 flex size-5 shrink-0 items-center justify-center rounded-[5px] border border-input-border bg-surface text-primary-ink after:absolute after:-inset-3 after:content-[''] data-checked:border-primary data-checked:bg-primary data-disabled:opacity-60",
          focusRing,
        )}
      >
        <BaseCheckbox.Indicator className="flex data-unchecked:hidden">
          <Check aria-hidden="true" className="size-3.5" strokeWidth={3} />
        </BaseCheckbox.Indicator>
      </BaseCheckbox.Root>
      <span className="flex flex-col">
        <label htmlFor={id} className="cursor-pointer font-medium">
          {label}
        </label>
        {hint ? (
          <span id={`${id}-hint`} className="text-[13px] text-muted">
            {hint}
          </span>
        ) : null}
      </span>
    </div>
  );
}

export function Switch({
  checked,
  onCheckedChange,
  label,
  disabled,
}: {
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
  label: string;
  disabled?: boolean;
}) {
  return (
    <BaseSwitch.Root
      checked={checked}
      onCheckedChange={(value) => onCheckedChange(value)}
      disabled={disabled}
      aria-label={label}
      className={cn(
        "relative flex h-6 w-11 shrink-0 cursor-pointer rounded-full border border-input-border bg-track p-0.5 transition-colors after:absolute after:-inset-2.5 after:content-[''] data-checked:border-primary data-checked:bg-primary data-disabled:cursor-not-allowed data-disabled:opacity-60",
        focusRing,
      )}
    >
      <BaseSwitch.Thumb className="size-4.5 rounded-full bg-surface shadow-sm transition-transform data-checked:translate-x-5 rtl:data-checked:-translate-x-5" />
    </BaseSwitch.Root>
  );
}

export interface RadioCardOption<V extends string> {
  value: V;
  label: ReactNode;
  hint?: ReactNode;
  disabled?: boolean;
}

/** A radio group where each option is a bordered card. */
export function RadioCards<V extends string>({
  legend,
  value,
  onValueChange,
  options,
  disabled,
  managed,
  className,
  columns = "grid-cols-[repeat(auto-fit,minmax(200px,1fr))]",
}: {
  legend: ReactNode;
  value: V;
  onValueChange: (value: V) => void;
  options: RadioCardOption<V>[];
  disabled?: boolean;
  managed?: boolean;
  className?: string;
  columns?: string;
}) {
  const id = useId();
  return (
    <div className={cn("flex flex-col gap-2", className)}>
      <span id={id} className="flex flex-wrap items-center gap-2 font-medium">
        {legend}
        {managed ? <ManagedBadge /> : null}
      </span>
      <RadioGroup
        aria-labelledby={id}
        value={value}
        onValueChange={(next) => onValueChange(next as V)}
        disabled={disabled || managed}
        className={cn("grid gap-3", columns)}
      >
        {options.map((option) => {
          const optionId = `${id}-${option.value}`;
          return (
            <label
              key={option.value}
              htmlFor={optionId}
              className={cn(
                "flex cursor-pointer items-start gap-3 rounded-[10px] border px-3.5 py-3 has-data-disabled:cursor-not-allowed has-data-disabled:opacity-70",
                value === option.value ? "border-2 border-ink px-[13px] py-[11px]" : "border-line",
              )}
            >
              <Radio.Root
                id={optionId}
                value={option.value}
                disabled={option.disabled}
                className={cn(
                  "mt-0.5 flex size-4.5 shrink-0 items-center justify-center rounded-full border border-input-border bg-surface data-checked:border-primary",
                  focusRing,
                )}
              >
                <Radio.Indicator className="size-2.5 rounded-full bg-primary data-unchecked:hidden" />
              </Radio.Root>
              <span className="flex flex-col">
                <span className="font-semibold">{option.label}</span>
                {option.hint ? <span className="text-[13px] text-muted">{option.hint}</span> : null}
              </span>
            </label>
          );
        })}
      </RadioGroup>
    </div>
  );
}

/** Responsive two-column form grid. */
export function FieldGrid({ className, ...props }: ComponentProps<"div">) {
  return <div className={cn("grid grid-cols-[repeat(auto-fit,minmax(240px,1fr))] gap-4", className)} {...props} />;
}
