import type { ReactNode } from "react";

import { House, Tv } from "lucide-react";

import { RoomAliasKind } from "@api/generated/model";
import { cn } from "@components/ui/cn";

const kindStyle: Record<RoomAliasKind, string> = {
  [RoomAliasKind.device]: "bg-ask-bg text-ask",
  [RoomAliasKind.room]: "bg-info-bg text-info",
};

/** A spoken name's pill, coloured and marked with an icon by kind so rooms and devices tell apart at a glance. */
export function KindPill({
  kind,
  children,
  className,
}: {
  kind: RoomAliasKind;
  children?: ReactNode;
  className?: string;
}) {
  const Icon = kind === RoomAliasKind.device ? Tv : House;
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 rounded-full py-0.5 ps-2 font-medium text-[12.5px]",
        kindStyle[kind],
        className,
      )}
    >
      <Icon aria-hidden="true" className="size-3.5" />
      {children}
    </span>
  );
}
