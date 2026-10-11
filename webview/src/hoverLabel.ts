// The hover label: the name a control shows when the mouse rests on it. VS
// Code's webviews show no native title tooltips, so this draws one in VS
// Code's hover colors for every element with a title. While the mouse is on
// the element its title moves to data-hover-title, so a native tooltip
// can't show as well; it moves back when the mouse leaves.

/** How long the mouse rests on a control before its label shows. */
export const HOVER_DELAY_MS = 300;

/** The gap between a control and its label, and the label and the panel's edge. */
const GAP_PX = 4;

const STASHED = "data-hover-title";

/** Shows a label for every element with a title in `root`; returns the label element. */
export function installHoverLabels(root: HTMLElement = document.body): HTMLElement {
  const label = document.createElement("div");
  label.className = "hover-label";
  label.setAttribute("role", "tooltip");
  label.dataset["testid"] = "hover-label";
  label.hidden = true;
  document.body.append(label);

  let target: HTMLElement | undefined;
  let timer: ReturnType<typeof setTimeout> | undefined;

  const hide = () => {
    clearTimeout(timer);
    label.hidden = true;
  };
  const leave = () => {
    hide();
    // A title set while the mouse was on it (Aa's, say) is the newer one; keep it.
    if (target && !target.hasAttribute("title")) target.setAttribute("title", target.getAttribute(STASHED) ?? "");
    target?.removeAttribute(STASHED);
    target = undefined;
  };
  const show = () => {
    if (!target?.isConnected) return;
    label.textContent = target.getAttribute(STASHED);
    label.hidden = false;
    place(label, target);
  };

  root.addEventListener("mouseover", (event) => {
    const over = (event.target as Element).closest<HTMLElement>(`[title], [${STASHED}]`);
    if (over === target) return;
    // Moving straight from one label to the next shows the next at once, as VS Code's toolbars do.
    const showing = !label.hidden;
    leave();
    if (!over?.getAttribute("title") && !over?.getAttribute(STASHED)) return;
    target = over;
    stash(target);
    if (showing) show();
    else timer = setTimeout(show, HOVER_DELAY_MS);
  });
  root.addEventListener("mouseleave", leave);
  // A click, a key or a scroll puts the label away; a title the click changed takes over from the stashed one.
  root.addEventListener("mousedown", hide, true);
  root.addEventListener("click", () => {
    if (target) stash(target);
  });
  globalThis.addEventListener("keydown", hide, true);
  globalThis.addEventListener("scroll", hide, true);
  globalThis.addEventListener("blur", leave);
  return label;
}

/** Moves `element`'s title to data-hover-title, so only the hover label shows it. */
function stash(element: HTMLElement): void {
  const title = element.getAttribute("title");
  if (title === null) return;
  element.setAttribute(STASHED, title);
  element.removeAttribute("title");
}

/** Under `target`, centered on it, inside the window; above it when there's no room below. */
function place(label: HTMLElement, target: HTMLElement): void {
  const anchor = target.getBoundingClientRect();
  const size = label.getBoundingClientRect();
  const maxLeft = window.innerWidth - size.width - GAP_PX;
  const left = Math.max(GAP_PX, Math.min(anchor.left + anchor.width / 2 - size.width / 2, maxLeft));
  const below = anchor.bottom + GAP_PX;
  const top = below + size.height <= window.innerHeight ? below : Math.max(GAP_PX, anchor.top - GAP_PX - size.height);
  label.style.left = `${left}px`;
  label.style.top = `${top}px`;
}
