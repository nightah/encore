import { type RefCallback, useCallback, useState } from "react";

/** Whether the element is at least `min` px wide, following its size as it changes. Before it mounts, it counts as wide enough. */
export function useMinWidth<T extends Element>(min: number): [RefCallback<T>, boolean] {
  const [wide, setWide] = useState(true);
  const ref = useCallback(
    (element: T | null) => {
      if (!element) return;
      const check = () => setWide(element.getBoundingClientRect().width >= min);
      check();
      const observer = new ResizeObserver(check);
      observer.observe(element);
      return () => observer.disconnect();
    },
    [min],
  );
  return [ref, wide];
}
