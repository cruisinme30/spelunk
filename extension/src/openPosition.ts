// Where a result opens when a double-click in its preview, not its match, says where.
import type { OpenPosition, OpenTarget } from "./protocol.gen";

/** A file target moved to `position`, its word selected in place of the match; a commit, or no position, as it was. */
export function movedTo(target: OpenTarget, position?: OpenPosition): OpenTarget {
  if (!position || target.sha) return target;
  const moved = { ...target, ...position };
  delete moved.endLine;
  delete moved.endColumn;
  return moved;
}
