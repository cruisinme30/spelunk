// The facet row between the chips and the results: the search's results
// counted by repo, language and top folder (author and month for commits).
// A click keeps only a bucket's results, Alt-click leaves them out, and a
// bucket the query already keeps or leaves out takes its filter back out.
import { element, plural } from "../format";
import type { Facet } from "../protocol.gen";
import { type FacetState, facetState, leaveOut } from "../queryEdit";
import { hasErrors, type ViewState } from "../state";

/** How many buckets a facet shows before its "+N" button. */
const SHOWN_BUCKETS = 5;

const FIELD_LABELS: Record<Facet["field"], string> = {
  repo: "Repo",
  lang: "Language",
  folder: "Folder",
  author: "Author",
  month: "Month",
};

/** A bucket as drawn: the daemon's, or a left-out one remembered from an earlier search. */
interface ShownBucket {
  label: string;
  count: number | undefined;
  filter: string;
  state: FacetState;
}

/** What the facet row does when a bucket is clicked. */
export interface FacetHandlers {
  onToggle: (filter: string, exclude: boolean) => void;
  onExpand: (field: Facet["field"]) => void;
}

/**
 * Draws the facet row for the finished search. It stays hidden until there
 * is a choice to make: a facet with two buckets, or a bucket the query
 * already keeps or leaves out.
 */
export function renderFacets(container: HTMLElement, state: ViewState, handlers: FacetHandlers): void {
  container.replaceChildren();
  const facets = state.done?.facets ?? [];
  if (hasErrors(state) || !state.parsed?.root) return;
  rememberLabels(state, facets);
  const groups = facets.map((facet) => ({ facet, buckets: bucketsOf(facet, state) }));
  addLeftOutBuckets(groups, state);
  const choice = groups.some(({ buckets }) => buckets.length > 1 || buckets.some((bucket) => bucket.state !== "none"));
  if (!choice) return;
  container.append(
    ...groups
      .filter(({ buckets }) => buckets.length > 0)
      .map(({ facet, buckets }) => facetGroup(facet, buckets, state.expandedFacets.has(facet.field), handlers)),
  );
}

function bucketsOf(facet: Facet, state: ViewState): ShownBucket[] {
  return facet.buckets.map((bucket) => ({ ...bucket, state: facetState(state.parsed, bucket.filter) }));
}

/** Remembers each bucket's field and label, to draw it struck through once a query leaves it out. */
function rememberLabels(state: ViewState, facets: Facet[]): void {
  for (const facet of facets) {
    for (const bucket of facet.buckets)
      state.facetLabels.set(bucket.filter, { field: facet.field, label: bucket.label });
  }
}

/** A facet and the buckets drawn for it. */
interface FacetGroup {
  facet: Facet;
  buckets: ShownBucket[];
}

/** A left-out bucket finds no results, so the daemon doesn't count it: it is added from memory. */
function addLeftOutBuckets(groups: FacetGroup[], state: ViewState): void {
  for (const [filter, { field, label }] of state.facetLabels) {
    if (facetState(state.parsed, filter) !== "left-out") continue;
    const group = groups.find((candidate) => candidate.facet.field === field) ?? newGroup(groups, field);
    if (!group.buckets.some((bucket) => bucket.filter === filter)) {
      group.buckets.push({ label, count: undefined, filter, state: "left-out" });
    }
  }
}

function newGroup(groups: FacetGroup[], field: Facet["field"]): FacetGroup {
  const group = { facet: { field, buckets: [], more: 0 }, buckets: [] };
  groups.push(group);
  return group;
}

function facetGroup(facet: Facet, buckets: ShownBucket[], expanded: boolean, handlers: FacetHandlers): HTMLElement {
  // Buckets in the query always show, however small.
  const shown = expanded
    ? buckets
    : buckets.filter((bucket, index) => index < SHOWN_BUCKETS || bucket.state !== "none");
  const hidden = buckets.length - shown.length + facet.more;
  const group = element(
    "div",
    { class: "facet", role: "group", "aria-label": FIELD_LABELS[facet.field], "data-field": facet.field },
    element("span", { class: "label" }, FIELD_LABELS[facet.field]),
    ...shown.map((bucket) => bucketButton(bucket, handlers)),
  );
  if (hidden > 0 && !expanded) {
    const more = element(
      "button",
      {
        type: "button",
        class: "facet-bucket more",
        "data-testid": "facet-more",
        title: `Show ${plural(hidden, "more bucket")}`,
      },
      `+${hidden}`,
    );
    more.addEventListener("click", () => {
      handlers.onExpand(facet.field);
    });
    group.append(more);
  } else if (facet.more > 0) {
    group.append(element("span", { class: "facet-more" }, `and ${facet.more} more`));
  }
  return group;
}

function bucketButton(bucket: ShownBucket, handlers: FacetHandlers): HTMLButtonElement {
  const button = element(
    "button",
    {
      type: "button",
      class: bucket.state === "none" ? "facet-bucket" : `facet-bucket ${bucket.state}`,
      "aria-pressed": bucket.state === "none" ? "false" : "true",
      "data-filter": bucket.filter,
      "data-testid": "facet-bucket",
      title: bucketTitle(bucket),
    },
    element("span", { class: "facet-label" }, bucket.label),
  );
  if (bucket.count !== undefined) button.append(element("span", { class: "facet-count" }, String(bucket.count)));
  button.addEventListener("click", (event) => {
    handlers.onToggle(bucket.filter, event.altKey);
  });
  // Alt+Enter leaves a bucket out from the keyboard, as Alt-click does.
  button.addEventListener("keydown", (event) => {
    if (event.key === "Enter" && event.altKey) {
      event.preventDefault();
      handlers.onToggle(bucket.filter, true);
    }
  });
  return button;
}

function bucketTitle({ state, filter }: ShownBucket): string {
  switch (state) {
    case "kept": {
      return `In the query as ${filter}. Click to remove it.`;
    }
    case "left-out": {
      return `Left out by ${leaveOut(filter)}. Click to bring it back.`;
    }
    case "none": {
      return `Click to keep only these (${filter}). Alt-click to leave them out (${leaveOut(filter)}).`;
    }
  }
}
