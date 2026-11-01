import type { ComponentProps } from "react";

import { cn } from "@components/ui/cn";

/**
 * Table that turns each row into a card of label and value pairs below 640px, so a phone never scrolls it sideways; wider screens scroll it inside its card if they must.
 *
 * On phones the header row is only read out, and each cell shows its Td `label` in front of its value.
 */
export function Table({ className, ...props }: ComponentProps<"table">) {
  return (
    <div className="sm:overflow-x-auto">
      <table
        className={cn(
          "w-full border-collapse text-sm",
          "max-sm:block max-sm:[&_tbody]:block max-sm:[&_thead]:sr-only",
          "max-sm:[&_tbody>tr]:flex max-sm:[&_tbody>tr]:flex-wrap max-sm:[&_tbody>tr]:items-center max-sm:[&_tbody>tr]:gap-x-3 max-sm:[&_tbody>tr]:gap-y-1.5 max-sm:[&_tbody>tr]:border-line-soft max-sm:[&_tbody>tr]:border-t max-sm:[&_tbody>tr]:px-4 max-sm:[&_tbody>tr]:py-3",
          className,
        )}
        {...props}
      />
    </div>
  );
}

export function Th({ className, ...props }: ComponentProps<"th">) {
  return (
    <th
      scope="col"
      className={cn(
        "whitespace-nowrap border-line border-b px-4 py-2.5 text-left font-medium font-mono text-[11.5px] text-muted uppercase tracking-[0.06em]",
        className,
      )}
      {...props}
    />
  );
}

interface TdProps extends ComponentProps<"td"> {
  /** The column's name, shown in front of the value on phones. */
  label?: string;
  /** On phones, opens the card on a line of its own: the row's name, say. With `action`, they share the line. */
  lead?: boolean;
  /** On phones, sits at the end of the card's first line: an Edit button, say. */
  action?: boolean;
}

export function Td({ className, label, lead = false, action = false, ...props }: TdProps) {
  return (
    <td
      data-label={label}
      className={cn(
        "border-line-soft border-b px-4 py-3 align-top [tr:last-child>&]:border-b-0",
        "max-sm:flex max-sm:min-w-0 max-sm:items-center max-sm:gap-3 max-sm:border-0 max-sm:p-0",
        label &&
          "max-sm:before:w-28 max-sm:before:shrink-0 max-sm:before:font-mono max-sm:before:text-[11px] max-sm:before:text-muted max-sm:before:uppercase max-sm:before:tracking-[0.06em] max-sm:before:content-[attr(data-label)]",
        lead ? "max-sm:order-first max-sm:basis-0 max-sm:grow" : null,
        action ? "max-sm:order-first max-sm:justify-end" : null,
        lead || action ? null : "max-sm:basis-full",
        className,
      )}
      {...props}
    />
  );
}
