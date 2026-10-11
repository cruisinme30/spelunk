// The rank-reason badge on a result's file, and the dimmer look of a file
// that best-match order moved down.
import { element } from "../format";
import type { RankReason } from "../protocol.gen";

/** The badge a result's file shows for why best-match order put it where it is. */
const RANK_BADGE: Record<RankReason, string> = {
  definition: "definition",
  open: "open",
  test: "test",
  vendored: "vendored",
  generated: "generated",
  excluded: "excluded",
};

/** The badge for why best-match order put a result's file where it is, if it says. */
export function rankBadge(reason: RankReason | undefined): HTMLElement | null {
  return reason
    ? element("span", { class: `badge rank-${reason}`, "data-testid": "rank-reason" }, RANK_BADGE[reason])
    : null;
}

/** " demoted" for a test, vendored, generated or excluded file, which reads dimmer; "" otherwise. */
export function demoted(reason: RankReason | undefined): string {
  return ["test", "vendored", "generated", "excluded"].includes(reason ?? "") ? " demoted" : "";
}
