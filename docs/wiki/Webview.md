# Webview

The search panel, help page and welcome page. A pure view: it never reads files, calls Git or reaches the daemon.
Lives in [`webview/src`](https://github.com/cruisinme30/spelunk/blob/main/webview/src).

| File | Owns |
| --- | --- |
| [`panel.ts`](https://github.com/cruisinme30/spelunk/blob/main/webview/src/panel.ts) | `SearchPanel`: input, keyboard, host messages; calls the renderers |
| [`state.ts`](https://github.com/cruisinme30/spelunk/blob/main/webview/src/state.ts) | `ViewState`, and `menuContext`: what each result row's `data-vscode-context` tells VS Code's right-click menu (the result's ref and kind, and its repo's Git host from `remotes.state`, which `render/menuContext.ts` applies to rows already drawn) |
| [`resultSelection.ts`](https://github.com/cruisinme30/spelunk/blob/main/webview/src/resultSelection.ts) | Which result is selected: ↑↓ and clicks move it, and the host is asked for its preview |
| [`host.ts`](https://github.com/cruisinme30/spelunk/blob/main/webview/src/host.ts) | The only code that talks to VS Code |
| [`render/`](https://github.com/cruisinme30/spelunk/blob/main/webview/src/render) | DOM writers: chrome, suggestions, repo menu, empty state, facets, results, preview. The results go in chunks of 100 rows that the browser lays out only near the view (`chunkedList.ts`), and an index finds a row, the next one for ↑↓ and the lines a replace changes without scanning them all (`resultIndex.ts`) |
| [`syntax.ts`](https://github.com/cruisinme30/spelunk/blob/main/webview/src/syntax.ts) | Code in the theme's syntax colors, from `syntax.lines`, and the preview's whitespace marks; it wraps the text already drawn, and colors code drawn again |
| [`operators.ts`](https://github.com/cruisinme30/spelunk/blob/main/webview/src/operators.ts) | The one operator reference, read by the cheat sheet, suggestions and help page |
| [`emptyBox.ts`](https://github.com/cruisinme30/spelunk/blob/main/webview/src/emptyBox.ts) | What the empty box's recent and pinned queries do: run, pin, name, remove, and ↑↓ through them |
| [`replace.ts`](https://github.com/cruisinme30/spelunk/blob/main/webview/src/replace.ts) | The Replace row: asks for a replace's preview, shows it on the rows, and asks the host to apply it |
| [`hoverLabel.ts`](https://github.com/cruisinme30/spelunk/blob/main/webview/src/hoverLabel.ts) | The label that shows a control's `title` when the mouse rests on it, since VS Code's webviews show no tooltips of their own |
| [`queryEdit.ts`](https://github.com/cruisinme30/spelunk/blob/main/webview/src/queryEdit.ts) | Pure text edits behind fix-its, the Aa / ab / .* toggles and the repo menu |
| [`boxText.ts`](https://github.com/cruisinme30/spelunk/blob/main/webview/src/boxText.ts) | Puts a restored query in the box, or types one from the editor (selected) or a folder's scope (the cursor after it) over the last, keeping ⌘Z |
| [`pinnedQueries.ts`](https://github.com/cruisinme30/spelunk/blob/main/webview/src/pinnedQueries.ts) | What starring, naming, watching, unpinning and removing an empty-box query change, and the message for the host |
| [`helpPage.ts`](https://github.com/cruisinme30/spelunk/blob/main/webview/src/helpPage.ts), [`helpContent.ts`](https://github.com/cruisinme30/spelunk/blob/main/webview/src/helpContent.ts) | The help page; its examples are data, and a test runs each one against the real daemon |
| [`welcomePage.ts`](https://github.com/cruisinme30/spelunk/blob/main/webview/src/welcomePage.ts) | First run: shortcut choice, what to index, per-repo progress |

Fix-its, completions and toggles are text edits followed by an ordinary `query.changed`. See [[Screens]] for what
each state looks like.

---

**Next:** [[Testing]]: the test layers and oracles.
