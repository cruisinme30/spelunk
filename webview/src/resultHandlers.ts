// What the results view asks of the panel: fixes, Load more, the Context
// buttons, the order menu, Replace's previews, syntax colors and Broaden.
import { send } from "./host";
import { topLevelOperators } from "./parsedQuery";
import type { Fix } from "./protocol.gen";
import { joinOrder } from "./render/order";
import type { ResultsView } from "./render/results";
import { setOrder, type Edited } from "./queryEdit";
import type { ViewState } from "./state";

/** The results view's handlers, as it takes them. */
type Handlers = ConstructorParameters<typeof ResultsView>[2];

/** The panel's parts the handlers reach. */
export interface ResultHandlerDeps {
  state: ViewState;
  input: HTMLInputElement;
  applyFix: (fix: Fix, basis: string | undefined) => void;
  editQuery: (edit: (text: string) => Edited, basis: string | undefined) => void;
  replacementsFor: (repoId: string, path: string, line: number) => string[] | undefined;
  paintCode: Handlers["paintCode"];
  broaden: () => void;
}

/**
 * The handlers of one search's results view. `searchedText` is the text the
 * search ran on, which a note's undo indexes: it may be the box without the
 * word being completed.
 */
export function resultHandlers(
  { state, input, applyFix, editQuery, replacementsFor, paintCode, broaden }: ResultHandlerDeps,
  searchedText: string | undefined,
): Handlers {
  return {
    onApplyFix: (fix) => {
      applyFix(fix, searchedText);
    },
    // Load more fetches the next page of this same search; its batches carry the same searchId.
    onLoadMore: (id, cursor) => {
      send("results.more", { searchId: id, cursor });
    },
    // The host saves the setting, and the query runs again with it; the box keeps the keys meanwhile.
    onContextLines: (lines) => {
      send("context.set", { lines });
      input.focus();
    },
    // An order typed in the query is rewritten there; otherwise the host saves the setting and searches again.
    onOrder: (base, descending) => {
      if (topLevelOperators(state.parsed, "order").length === 0)
        send("order.set", { order: joinOrder(base, descending) });
      else editQuery((text) => setOrder(text, state.parsed, joinOrder(base, descending, true)), searchedText);
      input.focus();
    },
    replacementsFor: (item) => replacementsFor(item.repoId, item.path, item.line),
    paintCode,
    onBroaden: broaden,
  };
}
