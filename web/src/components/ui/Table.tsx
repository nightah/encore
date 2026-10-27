import type { ComponentProps } from "react";

import { cn } from "@components/ui/cn";

/** Table that scrolls horizontally inside its card on narrow screens. */
export function Table({ className, ...props }: ComponentProps<"table">) {
  return (
    <div className="overflow-x-auto">
      <table className={cn("w-full border-collapse text-sm", className)} {...props} />
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

export function Td({ className, ...props }: ComponentProps<"td">) {
  return (
    <td
      className={cn("border-line-soft border-b px-4 py-3 align-top [tr:last-child>&]:border-b-0", className)}
      {...props}
    />
  );
}
