import { type RefObject, useLayoutEffect, useState } from "react";

// Todo is positioned above the footer, outside its measured layout height.
export function useTranscriptObstruction(shellRef: RefObject<HTMLDivElement | null>): number {
  const [height, setHeight] = useState(0);
  useLayoutEffect(() => {
    const shell = shellRef.current;
    const layout = shell?.closest(".main")?.parentElement;
    if (!shell || !layout) return;
    let frame = 0;
    const measured = new Set<Element>();
    const measure = () => {
      const bounds = shell.getBoundingClientRect();
      const todo = layout.querySelector(".footer > .todobar");
      const popup = todo ? document.querySelector(".todo-popover") : null;
      for (const node of measured) {
        if (node !== todo && node !== popup) { resize.unobserve(node); measured.delete(node); }
      }
      let overlap = 0;
      for (const node of [todo, popup]) {
        if (!node) continue;
        if (!measured.has(node)) { measured.add(node); resize.observe(node); }
        const rect = node.getBoundingClientRect();
        if (rect.width && rect.height && rect.right > bounds.left && rect.left < bounds.right) {
          overlap = Math.max(overlap, bounds.bottom - rect.top);
        }
      }
      setHeight(Math.ceil(Math.max(0, Math.min(bounds.height - 60, overlap))));
    };
    const schedule = () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(measure);
    };
    const resize = new ResizeObserver(schedule);
    resize.observe(shell);
    // Includes the portalled Todo details and removal/dismissal of the bar.
    const mutation = new MutationObserver(schedule);
    mutation.observe(layout, { childList: true, subtree: true });
    mutation.observe(document.body, { childList: true });
    window.addEventListener("resize", schedule);
    measure();
    return () => {
      cancelAnimationFrame(frame);
      resize.disconnect();
      mutation.disconnect();
      window.removeEventListener("resize", schedule);
    };
  }, [shellRef]);
  return height;
}
