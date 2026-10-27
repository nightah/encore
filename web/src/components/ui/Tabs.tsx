import type { ComponentProps } from "react";

import { Tabs as BaseTabs } from "@base-ui/react/tabs";

import { cn } from "@components/ui/cn";

export const TabsRoot = BaseTabs.Root;

export function TabsList({ className, ...props }: ComponentProps<typeof BaseTabs.List>) {
  return (
    <BaseTabs.List
      className={cn("relative flex flex-wrap gap-1 border-line border-b", className as string)}
      {...props}
    />
  );
}

export function Tab({ className, ...props }: ComponentProps<typeof BaseTabs.Tab>) {
  return (
    <BaseTabs.Tab
      className={cn(
        "-mb-px inline-flex min-h-11 cursor-pointer items-center border-transparent border-b-2 px-3.5 py-2.5 font-medium text-muted outline-none hover:text-ink focus-visible:outline-2 focus-visible:outline-focus focus-visible:-outline-offset-2 data-active:border-accent data-active:text-ink",
        className as string,
      )}
      {...props}
    />
  );
}

export function TabPanel({ className, ...props }: ComponentProps<typeof BaseTabs.Panel>) {
  return (
    <BaseTabs.Panel
      className={cn(
        "flex flex-col gap-5 rounded-xl border border-line bg-surface px-5 py-5 outline-none focus-visible:outline-2 focus-visible:outline-focus sm:px-6",
        className as string,
      )}
      {...props}
    />
  );
}
