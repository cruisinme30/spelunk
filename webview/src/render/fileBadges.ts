// The badges a result's file shows beside its path: why best-match order put
// it where it is (and the dimmer look of a file it moved down), how it
// differs now when searched at a revision, and who owns it.
import { element } from "../format";
import type { RankReason, SinceRevision } from "../protocol.gen";

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

/**
 * How a file found at a revision (at:) differs now: "deleted since", "changed since" or "renamed since"; nothing when
 * it's the same.
 */
const SINCE_BADGE: Record<SinceRevision, { label: string; title: string }> = {
  deleted: { label: "deleted since", title: "This file isn't in the current files" },
  changed: { label: "changed since", title: "This file is different in the current files" },
  renamed: { label: "renamed since", title: "This file has another name in the current files" },
};

/**
 * The badge for how a file at a revision differs now, with a renamed file's name now ("renamed since →
 * retry_policy.py", its full path in the tooltip); none for a value this webview doesn't know.
 */
export function sinceBadge(since: SinceRevision | undefined, renamedTo?: string): HTMLElement | null {
  if (since === undefined || !Object.hasOwn(SINCE_BADGE, since)) return null;
  const { label, title } = SINCE_BADGE[since];
  const now = since === "renamed" && renamedTo ? renamedTo : undefined;
  return element(
    "span",
    { class: `badge since-${since}`, title: now ? `${title}: ${now}` : title, "data-testid": "since-revision" },
    now ? `${label} → ${now.slice(now.lastIndexOf("/") + 1)}` : label,
  );
}

/**
 * The badge naming who owns a result's file by its repo's CODEOWNERS file:
 * the first owner, a team by its name alone (payments-core for the team
 * `@acme/payments-core`), then "+1" for each other one, all of them in
 * full in its tooltip.
 */
export function ownerBadge(owners: string[] | undefined): HTMLElement | null {
  // Only names count: anything else in a broken message is left out.
  const names = Array.isArray(owners)
    ? (owners as unknown[]).filter((owner): owner is string => typeof owner === "string")
    : [];
  const [first, ...others] = names;
  if (first === undefined) return null;
  const name = first.startsWith("@") && first.includes("/") ? first.slice(first.indexOf("/") + 1) : first;
  const label = others.length > 0 ? `${name} +${others.length}` : name;
  return element(
    "span",
    { class: "badge owner", title: `Owned by ${names.join(", ")}`, "data-testid": "owner" },
    label,
  );
}
