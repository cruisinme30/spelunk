// How results from current files are sorted, as a section title says it.
import type { ResultOrder } from "../protocol.gen";

const ORDER_LABEL: Record<ResultOrder, string> = {
  best: "best match first",
  path: "path order",
  "path:asc": "path order",
  "path:desc": "path order, Z to A",
  recent: "recently changed, newest first",
  "recent:desc": "recently changed, newest first",
  "recent:asc": "recently changed, oldest first",
};

/** What a section title says for an order: "best match first", "recently changed, oldest first". */
export function orderLabel(order: ResultOrder): string {
  return ORDER_LABEL[order];
}
