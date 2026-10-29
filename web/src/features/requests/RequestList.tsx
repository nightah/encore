import type { RequestSummary } from "@api/generated/model";
import { Heard, requestSource } from "@components/Heard";
import { outcomeChip } from "@components/status";
import { Chip } from "@components/ui/Chip";
import { cn } from "@components/ui/cn";
import { formatClock, formatDuration } from "@lib/format";

/** One row of the live request list: a toggle button that selects the request, or opens it under the row when `expands`. */
export function RequestRow({
  request,
  selected,
  expands = false,
  onSelect,
}: {
  request: RequestSummary;
  selected: boolean;
  expands?: boolean;
  onSelect: (id: number) => void;
}) {
  const chip = outcomeChip(request);
  return (
    <button
      type="button"
      aria-pressed={expands ? undefined : selected}
      aria-expanded={expands ? selected : undefined}
      onClick={() => onSelect(request.id)}
      className={cn(
        "grid min-h-11 w-full cursor-pointer grid-cols-[56px_minmax(0,1fr)_auto] items-center gap-x-3 gap-y-1 border-line-soft border-b px-4 py-3 text-start text-sm outline-none hover:bg-soft focus-visible:outline-2 focus-visible:outline-focus focus-visible:-outline-offset-2",
        selected && "bg-selected hover:bg-selected",
      )}
    >
      <span className="font-mono text-[13px] text-muted">{formatClock(request.time)}</span>
      <span className="flex min-w-0 flex-col">
        <span className="truncate">
          <Heard request={request} />
        </span>
        <span className="text-[12.5px] text-muted">
          {requestSource(request)}
          {request.heard && ` · ${request.intent}`}
        </span>
      </span>
      <span className="flex flex-col items-end gap-0.5">
        <Chip tone={chip.tone}>{chip.label}</Chip>
        <span className="font-mono text-muted text-xs">{formatDuration(request.duration_ms)}</span>
      </span>
    </button>
  );
}
