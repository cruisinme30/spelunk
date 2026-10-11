// What a hidden-results note says a filter hid: "3 commits", "38 code matches for retry", "2 matches outside a method".
import { plural } from "../format";
import { textTerms } from "../parsedQuery";
import type { HiddenNote, ParsedQuery } from "../protocol.gen";
import { definitionKind, outsideDefinitions } from "./withinLabel";

const SINGULAR_UNIT: Record<HiddenNote["unit"], string> = {
  matches: "match",
  files: "file",
  commits: "commit",
  definitions: "definition",
};

/**
 * "3 commits", for type:file "38 code matches for retry" (the query being `parsed`), or for kind: on uses "2
 * matches outside a function": what the filter hid.
 */
export function hiddenWhat(note: HiddenNote, parsed: ParsedQuery | undefined): string {
  const one = SINGULAR_UNIT[note.unit];
  if (outsideDefinitions(note)) return `${plural(note.count, one, note.unit)} outside ${definitionKind(note)}`;
  if (note.reason !== "type") return plural(note.count, one, note.unit);
  const terms = textTerms(parsed);
  const what =
    note.unit === "commits"
      ? plural(note.count, one, note.unit)
      : plural(note.count, `code ${one}`, `code ${note.unit}`);
  return what + (terms.length > 0 ? ` for ${terms.join(" and ")}` : "");
}
