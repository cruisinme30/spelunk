// Sashes: the lines between panes side by side that a drag, or ← → once focused, moves, as VS Code's sashes do. The
// results and preview have one, and so has the preview's Outline; each width is kept across reloads.
import { element } from "../format";
import { loadPaneWidth, savePaneWidth } from "../host";

/** How far one ← or → moves a sash. */
const KEY_STEP = 24;

/** What a sash sizes and how. */
export interface SashOptions {
  /** Its class, besides "sash", its name for screen readers, and its test id. */
  className: string;
  label: string;
  testId: string;
  /** The pane whose width it sets: the one before it, or with `sizes: "after"` the one after it. */
  pane: HTMLElement;
  sizes?: "after";
  /** The narrowest the pane can be dragged. */
  min: number;
  /** The widest the pane can be now, leaving the other its minimum. */
  max: () => number;
  /** Sizes the pane to `width`, or back to the default split without one. */
  apply: (width?: number) => void;
  /** The sash settled at a new width for the pane; no width after a double-click reset it. */
  onSettle: (width?: number) => void;
}

/** A sash beside `options.pane`: drag it, or focus it and press ← →, to resize the pane; double-click resets it. */
export function sash(options: SashOptions): HTMLElement {
  const { pane, min, max, apply, onSettle } = options;
  // Moving right widens a pane before the sash and narrows one after it.
  const sign = options.sizes === "after" ? -1 : 1;
  const divider = element(
    "div",
    {
      class: `sash ${options.className}`,
      role: "separator",
      tabindex: 0,
      "aria-orientation": "vertical",
      "aria-label": options.label,
      title: "Drag to resize · double-click to reset",
      "data-testid": options.testId,
    },
    element("span", { class: "grip", "aria-hidden": "true" }),
  );
  const width = () => pane.getBoundingClientRect().width;
  // Tells assistive technology where the sash is: the pane's width, and its range.
  const announce = () => {
    divider.setAttribute("aria-valuemin", String(min));
    divider.setAttribute("aria-valuemax", String(Math.max(min, max())));
    divider.setAttribute("aria-valuenow", String(Math.round(width())));
  };
  const resize = (wanted: number) => {
    const clamped = Math.round(Math.max(min, Math.min(wanted, max())));
    apply(clamped);
    announce();
    return clamped;
  };
  divider.addEventListener("focus", announce);
  divider.addEventListener("pointerdown", (event) => {
    event.preventDefault();
    divider.setPointerCapture(event.pointerId);
    divider.classList.add("active");
    const startX = event.clientX;
    const startWidth = width();
    let settled = startWidth;
    const move = (moved: PointerEvent) => {
      settled = resize(startWidth + sign * (moved.clientX - startX));
    };
    const end = () => {
      divider.classList.remove("active");
      divider.removeEventListener("pointermove", move);
      divider.removeEventListener("pointerup", end);
      divider.removeEventListener("pointercancel", end);
      if (settled !== startWidth) onSettle(settled);
    };
    divider.addEventListener("pointermove", move);
    divider.addEventListener("pointerup", end);
    divider.addEventListener("pointercancel", end);
  });
  divider.addEventListener("dblclick", () => {
    apply();
    onSettle();
  });
  divider.addEventListener("keydown", (event) => {
    const step = { ArrowLeft: -KEY_STEP, ArrowRight: KEY_STEP }[event.key];
    if (step === undefined) return;
    event.preventDefault();
    event.stopPropagation();
    onSettle(resize(width() + sign * step));
  });
  return divider;
}

/** The narrowest the results and the preview, and the Outline and the code beside it, can be dragged. */
const MIN_RESULTS_WIDTH = 240;
const MIN_PREVIEW_WIDTH = 320;
const MIN_OUTLINE_WIDTH = 140;
const MIN_CODE_WIDTH = 240;

/** Sets `property` on `element` to `width`, or removes it without one. */
function setWidth(target: HTMLElement, property: string, width?: number): void {
  if (width === undefined) target.style.removeProperty(property);
  else target.style.setProperty(property, `${String(width)}px`);
}

/** The line between `results` and the preview after it; it starts at the width last dragged. */
export function resultsSash(results: HTMLElement): HTMLElement {
  const apply = (width?: number) => {
    results.classList.toggle("sized", width !== undefined);
    setWidth(results, "--results-width", width);
  };
  apply(loadPaneWidth("resultsWidth"));
  const divider: HTMLElement = sash({
    className: "results-divider",
    label: "Resize results",
    testId: "results-divider",
    pane: results,
    min: MIN_RESULTS_WIDTH,
    max: () => (results.parentElement?.clientWidth ?? 0) - divider.offsetWidth - MIN_PREVIEW_WIDTH,
    apply,
    onSettle: (width) => {
      savePaneWidth("resultsWidth", width);
    },
  });
  return divider;
}

/** The line at the Outline's left edge, inside `pane`; it starts at the width last dragged. */
export function outlineSash(pane: HTMLElement): HTMLElement {
  const apply = (width?: number) => {
    setWidth(pane, "--outline-width", width);
  };
  apply(loadPaneWidth("outlineWidth"));
  return sash({
    className: "outline-divider",
    label: "Resize the Outline",
    testId: "outline-divider",
    pane,
    sizes: "after",
    min: MIN_OUTLINE_WIDTH,
    max: () => (pane.parentElement?.clientWidth ?? 0) - MIN_CODE_WIDTH,
    apply,
    onSettle: (width) => {
      savePaneWidth("outlineWidth", width);
    },
  });
}
