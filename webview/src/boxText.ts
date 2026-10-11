// Puts a query in the box: restored with the cursor at its end, or typed in
// over the last one when it came from the editor or the Explorer.

/**
 * Sets the box's text. Text seeded from the editor is typed in, so the
 * browser's ⌘Z brings back what it replaced, and left selected, so typing
 * replaces it in turn; with `keepTyping`, as Search in Folder's scope is,
 * the cursor goes to its end instead, so typing adds to it. Any other text
 * goes in with the cursor at its end.
 */
export function setBoxText(input: HTMLInputElement, text: string, seeded: boolean, keepTyping = false): void {
  if (!seeded) {
    input.value = text;
    input.setSelectionRange(text.length, text.length);
    return;
  }
  input.focus();
  input.select();
  // execCommand is deprecated but still the only way to edit a field and keep its undo history.
  // eslint-disable-next-line @typescript-eslint/no-deprecated
  if (!document.execCommand("insertText", false, text)) input.value = text;
  if (keepTyping) input.setSelectionRange(text.length, text.length);
  else input.select();
}
