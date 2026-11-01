import { type PointerEvent, type ReactNode, useEffect, useRef, useState } from "react";

import { Dialog as BaseDialog } from "@base-ui/react/dialog";
import { ChevronDown, ChevronUp, CircleMinus, CirclePlus, GripVertical } from "lucide-react";
import { useTranslation } from "react-i18next";

import { DEFAULT_MENU_BAR, MENU_BAR_MAX, resetMenuBar, setMenuBar, useMenuBar } from "@components/menuBar";
import { NAV_ITEMS, type NavItem } from "@components/nav";
import { Button } from "@components/ui/Button";
import { cn, focusRing } from "@components/ui/cn";

type NavPath = NavItem["to"];

/** Chooses which sections sit in the phone menu bar and in what order: drag a row by its handle, or use its buttons. */
export function ArrangeMenu({ onDone }: { onDone: () => void }) {
  const { t } = useTranslation("common");
  const bar = useMenuBar();
  const [announcement, setAnnouncement] = useState("");
  const [dragging, setDragging] = useState<NavPath | null>(null);
  const listRef = useRef<HTMLOListElement>(null);
  const sheetRef = useRef<HTMLDivElement>(null);
  // A row that changes list loses its buttons; focus follows it to the other list.
  const focusNextRef = useRef<string | null>(null);

  const inBar = bar.map((to) => NAV_ITEMS.find((item) => item.to === to)).filter((item) => item !== undefined);
  const inMore = NAV_ITEMS.filter((item) => !bar.includes(item.to));
  const full = bar.length >= MENU_BAR_MAX;
  const isDefault = bar.length === DEFAULT_MENU_BAR.length && bar.every((to, i) => to === DEFAULT_MENU_BAR[i]);

  useEffect(() => {
    if (!focusNextRef.current) return;
    sheetRef.current?.querySelector<HTMLElement>(`[data-focus-key="${focusNextRef.current}"]`)?.focus();
    focusNextRef.current = null;
  });

  function place(to: NavPath, index: number) {
    const rest = bar.filter((path) => path !== to);
    setMenuBar([...rest.slice(0, index), to, ...rest.slice(index)]);
  }

  function announcePosition(item: NavItem, index: number, count: number) {
    setAnnouncement(t("menu.moved", { count, name: item.label, position: index + 1 }));
  }

  // The ends of the list disable the buttons that would move past them.
  function move(item: NavItem, by: -1 | 1) {
    const index = bar.indexOf(item.to) + by;
    place(item.to, index);
    announcePosition(item, index, bar.length);
  }

  function toMore(item: NavItem) {
    setMenuBar(bar.filter((path) => path !== item.to));
    focusNextRef.current = `more:${item.to}`;
    setAnnouncement(t("menu.movedToMore", { name: item.label }));
  }

  function toBar(item: NavItem) {
    setMenuBar([...bar, item.to]);
    focusNextRef.current = `bar:${item.to}`;
    announcePosition(item, bar.length, bar.length + 1);
  }

  function startDrag(event: PointerEvent<HTMLElement>, to: NavPath) {
    event.preventDefault();
    try {
      event.currentTarget.setPointerCapture(event.pointerId);
    } catch {
      // A synthetic pointer can't be captured; the moves still reach the handle while it's under the pointer.
    }
    setDragging(to);
  }

  /** The dragged row goes after every other row whose middle is above the pointer. */
  function drag(event: PointerEvent<HTMLElement>) {
    if (!dragging) return;
    // The list is on screen while one of its rows is dragged.
    const others = [...(listRef.current as HTMLOListElement).children].filter(
      (row): row is HTMLElement => row instanceof HTMLElement && row.dataset.path !== dragging,
    );
    const index = others.filter((row) => {
      const box = row.getBoundingClientRect();
      return box.top + box.height / 2 < event.clientY;
    }).length;
    if (bar.indexOf(dragging) !== index) place(dragging, index);
  }

  function endDrag() {
    const item = NAV_ITEMS.find((candidate) => candidate.to === dragging);
    if (item) announcePosition(item, bar.indexOf(item.to), bar.length);
    setDragging(null);
  }

  return (
    <div ref={sheetRef} className="flex flex-col">
      <div className="flex items-center justify-between gap-3 px-5 pt-1.5 pb-3">
        <BaseDialog.Title className="font-display font-semibold text-[19px]">{t("menu.arrange")}</BaseDialog.Title>
        <Button variant="primary" onClick={onDone}>
          {t("menu.done")}
        </Button>
      </div>

      <h3 id="arrange-bar" className={listLabel}>
        {t("menu.inBar", { max: MENU_BAR_MAX })}
      </h3>
      {inBar.length ? (
        <ol ref={listRef} aria-labelledby="arrange-bar" className={listBox}>
          {inBar.map((item, index) => (
            <li
              key={item.to}
              data-path={item.to}
              className={cn(
                row,
                dragging === item.to && "relative z-10 rounded-lg bg-soft shadow-lg outline outline-accent",
              )}
            >
              {/* Pointer only: the buttons beside it do the same for keyboards and screen readers. */}
              <span
                aria-hidden="true"
                onPointerDown={(event) => startDrag(event, item.to)}
                onPointerMove={drag}
                onPointerUp={endDrag}
                onPointerCancel={endDrag}
                className="flex h-11 w-9 shrink-0 cursor-grab touch-none items-center justify-center text-muted active:cursor-grabbing"
              >
                <GripVertical className="size-5" />
              </span>
              <Name item={item} />
              <RowButton
                label={t("menu.moveUp", { name: item.label })}
                disabled={index === 0}
                onClick={() => move(item, -1)}
              >
                <ChevronUp aria-hidden="true" className="size-5" />
              </RowButton>
              <RowButton
                label={t("menu.moveDown", { name: item.label })}
                disabled={index === inBar.length - 1}
                onClick={() => move(item, 1)}
              >
                <ChevronDown aria-hidden="true" className="size-5" />
              </RowButton>
              <RowButton
                label={t("menu.toMore", { name: item.label })}
                focusKey={`bar:${item.to}`}
                onClick={() => toMore(item)}
              >
                <CircleMinus aria-hidden="true" className="size-5" />
              </RowButton>
            </li>
          ))}
        </ol>
      ) : (
        <p className="mx-3 rounded-xl border border-line border-dashed px-4 py-3 text-muted text-sm">
          {t("menu.empty")}
        </p>
      )}

      <h3 id="arrange-more" className={cn(listLabel, "pt-5")}>
        {t("menu.inMore")}
      </h3>
      {/* Seven sections and at most four in the bar: More always holds some. */}
      <ul aria-labelledby="arrange-more" className={listBox}>
        {inMore.map((item) => (
          <li key={item.to} className={cn(row, "pl-3")}>
            <Name item={item} />
            <RowButton
              label={t("menu.toBar", { name: item.label })}
              disabled={full}
              focusKey={`more:${item.to}`}
              onClick={() => toBar(item)}
            >
              <CirclePlus aria-hidden="true" className="size-5" />
            </RowButton>
          </li>
        ))}
      </ul>

      <p className="mx-5 mt-3 text-[13px] text-muted">
        {full ? `${t("menu.full")} ` : null}
        {t("menu.savedHere")}
      </p>
      {isDefault ? null : (
        <button
          type="button"
          onClick={() => {
            resetMenuBar();
            setAnnouncement("");
          }}
          className={cn("mx-3 mt-1 min-h-11 self-start rounded-lg px-2 text-link text-sm hover:underline", focusRing)}
        >
          {t("menu.reset")}
        </button>
      )}
      <p aria-live="polite" className="sr-only">
        {announcement}
      </p>
    </div>
  );
}

const listLabel = "px-5 pb-2 font-medium font-mono text-[11.5px] text-muted uppercase tracking-[0.06em]";
const listBox = "mx-3 flex flex-col rounded-xl border border-line bg-bg";
const row = "flex min-h-13 items-center gap-1 border-line-soft border-t pr-1.5 pl-1 first:border-t-0";

function Name({ item }: { item: NavItem }) {
  const Icon = item.icon;
  return (
    <span className="flex min-w-0 flex-1 items-center gap-3 text-[15px]">
      <Icon aria-hidden="true" className="size-5 shrink-0 text-muted" />
      <span className="truncate">{item.label}</span>
    </span>
  );
}

/** aria-disabled rather than disabled, so a button that just reached the end of the list keeps focus. */
function RowButton({
  label,
  disabled = false,
  focusKey,
  onClick,
  children,
}: {
  label: string;
  disabled?: boolean;
  focusKey?: string;
  onClick: () => void;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      aria-disabled={disabled || undefined}
      data-focus-key={focusKey}
      onClick={disabled ? undefined : onClick}
      className={cn(
        "inline-flex size-11 shrink-0 items-center justify-center rounded-lg text-ink hover:bg-btn-hover aria-disabled:cursor-not-allowed aria-disabled:opacity-35 aria-disabled:hover:bg-transparent",
        focusRing,
      )}
    >
      {children}
    </button>
  );
}
