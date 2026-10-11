// What the box searches while the suggestions are open: the query without
// the word they would replace, so results don't flicker as an operator is
// typed.
import type { Completion } from "./protocol.gen";

/**
 * The box without the word the suggestions would replace, when the cursor
 * is in it; undefined when there are no suggestions or nothing else is left.
 * Offsets are UTF-16, like JavaScript strings.
 */
export function textWithoutCompletedWord(text: string, cursor: number, completions: Completion[]): string | undefined {
  const span = completions[0]?.insert.edits[0]?.span;
  if (!span || cursor < span.start || cursor > span.end) return undefined;
  const before = text.slice(0, span.start).trimEnd();
  const after = text.slice(span.end).trimStart();
  const rest = before && after ? `${before} ${after}` : before || after;
  return rest || undefined;
}
