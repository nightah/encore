import { Meter as BaseMeter } from "@base-ui/react/meter";
import { Progress as BaseProgress } from "@base-ui/react/progress";

import { cn } from "@components/ui/cn";

/** A static measurement (model size, match score). */
export function Meter({
  value,
  max = 100,
  label,
  className,
  height = "h-2.5",
  tone = "bg-accent",
}: {
  value: number;
  max?: number;
  /** Accessible text, e.g. "1.12 of 1.5 MB used". */
  label: string;
  className?: string;
  height?: string;
  tone?: string;
}) {
  return (
    <BaseMeter.Root
      value={Math.min(value, max)}
      max={max}
      aria-label={label}
      getAriaValueText={() => label}
      className={className}
    >
      <BaseMeter.Track className={cn("block overflow-hidden rounded-full bg-track", height)}>
        <BaseMeter.Indicator className={cn("block h-full", tone)} />
      </BaseMeter.Track>
    </BaseMeter.Root>
  );
}

/** Task progress (deploys). `value: null` is indeterminate. */
export function Progress({ value, label, className }: { value: number | null; label: string; className?: string }) {
  return (
    <BaseProgress.Root value={value} aria-label={label} getAriaValueText={() => label} className={className}>
      <BaseProgress.Track className="block h-2 overflow-hidden rounded-full bg-track">
        <BaseProgress.Indicator className="block h-full bg-accent transition-[width] duration-500 data-indeterminate:w-1/3 data-indeterminate:animate-pulse" />
      </BaseProgress.Track>
    </BaseProgress.Root>
  );
}

/** Stacked horizontal bar of segments, e.g. the request timing breakdown. Purely visual: pair it with a text label. */
export function SegmentBar({
  segments,
  label,
}: {
  segments: Array<{ value: number; className: string }>;
  label: string;
}) {
  const total = segments.reduce((sum, s) => sum + s.value, 0) || 1;
  return (
    <div role="img" aria-label={label} className="flex h-2.5 overflow-hidden rounded-full bg-track">
      {segments.map((segment, index) => (
        <span
          // biome-ignore lint/suspicious/noArrayIndexKey: segments are positional
          key={index}
          className={cn("block h-full", segment.className)}
          style={{ width: `${(segment.value / total) * 100}%` }}
        />
      ))}
    </div>
  );
}
