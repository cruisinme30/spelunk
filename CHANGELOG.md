# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses [Semantic Versioning](https://semver.org/).

## [Unreleased]

## [3.0.0] - 2026-10-11

### Added

- **Ask in plain words.** Type a question the way you'd say it and end it with `?`, or press the new **Ask** button,
  and Spelunk writes the query for you, shows it under the box (**You asked** … · **Searched as** …, with what it
  had to assume) and runs it. A question about who changed something, or when, becomes a history search; when the
  query finds nothing, **Broaden** asks for one looser query. The provider is Anthropic's Claude (`spelunk.ask.provider`,
  off by default; **Spelunk: Set Anthropic API Key** keeps the key in VS Code's secret storage and turns Ask on); only
  the question, the repo names and the CODEOWNERS team names go to it (`spelunk.ask.shareNames` sends the question
  alone), never your code. `spelunk.ask.model` picks the model and `spelunk.ask.effort` how long it thinks;
  **Spelunk: Choose Ask Model** lists the models your key can use, with the effort levels each takes, and sets both.
  The daemon's parser checks every query it writes, and a query that doesn't parse goes back once for a repair.
- **History follows files through renames.** `file:retry_policy.py` finds a file's commits under every name it had,
  each saying what the file is called now, and marks the commits that renamed it. A commit that only moved a file no
  longer counts every line as added, so `type:added` finds the commit that wrote a line. A search at an old release
  says a file was **renamed since**, with its name now, instead of deleted, and finds it by that name. A copy, or a file
  rewritten by more than half, still starts its own history. Saved history is read again once.
- **Search the commits on merged branches.** With `spelunk.index.mergedBranches` on (off by default), history also
  reads the commits each merge brought in from another branch, under their own authors, marked with the merge and
  dated by it for `since:` and `until:`, and shows a merge as only the lines it resolved a conflict with. Needs Git
  2.36 or later; it takes longer to read and more memory on a repo with many merges.
- **Uses inside definitions.** `kind:function ref:RetryPolicy` lists each line that uses `RetryPolicy` inside a
  function, each ending in the function it is in, instead of every function in a file that uses it; `content:`,
  `struct:` and a regex across lines work the same way, and `-ref:` leaves out the functions that use a name. The
  preview marks the definition's lines, a note counts the matches outside one, and agents and `spelunk search --json`
  get the definition too. Saved indexes are rebuilt once.
- **Search only in comments, in quoted text or in code.** `retry in:comments` finds what comments and Python
  docstrings say about retrying, `"budget spent" in:strings` lands on the message itself, and `retry in:code` skips
  every comment and string that only mentions it; `-in:comments` leaves comments out. Each match counts on its own, so
  on a line with the word in its code and its comment only the part asked for is marked, and Replace changes only
  that part. Plain text and Markdown always match. A note says how many matches it hid, with Show them to undo
  it. Current files only, at any `at:` revision.
- **Spelunk for agents and the terminal.** Claude Code, Copilot and a shell search with Spelunk's query language and
  index. **Spelunk: Install 'spelunk' Command in PATH** adds the `spelunk` command (`spelunk search`, `status` and
  `help query`; ripgrep-style lines, `--vimgrep` or `--json`, exit 0, 1 or 2). **Spelunk: Add to Claude Code** runs
  `claude mcp add` and offers a two-line note for `CLAUDE.md`, and VS Code 1.101 or later lists Spelunk's MCP tools
  for Copilot by itself. Answers come live from the VS Code window that has the folder open, or else from the saved
  index through a background reader that re-reads changed files and exits when idle; nothing outside VS Code writes an
  index. New settings: `spelunk.agent.shareWindow`, `spelunk.agent.offerToCopilot` and `spelunk.agent.timeLimitMs`.

- **Go to Symbol in Workspace finds Spelunk's definitions.** ⌘T (or `#` in Quick Open) lists the functions, classes
  and types in every indexed repo and language, by a few letters of their names in order, beside any language
  server's; a definition both find shows once. Generated and vendored files are left out, and VS Code's excludes
  apply. **Go to Definition** and **Find References** can answer from the index too, by name, in the file's language
  when it has any: `spelunk.editor.definitions` is `off` by default, `fallback` answers only when no language server
  does, and `always` adds to theirs. New settings: `spelunk.editor.workspaceSymbols`, `spelunk.editor.definitions`
  and `spelunk.editor.includeGeneratedAndVendored`.
- **Search in Folder with Spelunk**, on the Explorer's right-click menu under Find in Folder…, searches only inside the
  folders right-clicked: the box starts with each folder from its repo's root (and its repo, with several open), the
  cursor after it, and what was typed stays. Several folders, or a second pick, search any of them; a file selected
  with them is searched too; any folder name works. A note under the results says when a folder's files aren't
  searched (ignored by Git, left out by `spelunk.index.exclude`, or a symlink), with the setting to change, and in
  history when a folder is a Git repo of its own, whose commits aren't searched.
- Sort results by when each file last changed: the order name above each section of results (**Best match first**)
  is now a menu of best match, path order and **Recently changed**, and an arrow beside it flips the direction
  (newest or oldest first, A to Z or Z to A). Recently changed goes by the save time of a file with uncommitted edits
  or outside Git, otherwise its newest commit, and each code result says when its file changed. Typed, it's
  `order:recent:desc` (or plain `order:recent`) and `order:recent:asc`. Path order is now `order:file` (`file:asc`, or
  `file:desc` for Z to A), as `file:` names paths; `order:path` offers the new name. The pick is saved in
  `spelunk.results.order`, which takes the same values; a value saved as `path` still works.
- Files you hide with VS Code's `files.exclude` and `search.exclude` are left out of searches, as in VS Code's own
  search, read per folder and the way VS Code reads them (`when` clauses too). A note under the results counts them
  and names the patterns; **Show them** adds the new `excludes:no`, which shows them last, marked excluded. History
  leaves out commits that only change hidden files, and Replace leaves hidden files alone. New setting:
  `spelunk.search.useExcludeSettings` (on by default).
- Select text in the editor and press the search key to search it: the box holds the selection, selected so typing
  replaces it, and ⌘Z brings back the query it replaced (also kept under Recent). The selection is matched exactly,
  never file names by their letters; text that looks like query syntax is searched as text, several lines as one
  regex across them, and Aa, ab and .* carry over. With nothing selected, or with the focus outside the editor, the
  key reopens the last query as before. New setting:
  `spelunk.search.seedFromSelection` (on by default).
- **Spelunk: Find Definitions** and **Spelunk: Find Uses**, on the editor's right-click menu and in the Command
  Palette, search where the name at the cursor is defined (exactly that name) and the lines that use it.
- Double-click a word in a code preview to open the file beside the editor with that word selected, at the line and
  column you double-clicked; a double-click on a line number opens at the start of that line. The panel stays open.
- Code in results and the preview is colored like the editor: the active color theme's syntax colors, by the language
  grammars VS Code itself colors with (vscode-textmate), following theme switches and `editor.tokenColorCustomizations`.
  The preview marks whitespace as `editor.renderWhitespace` says (a dot per space, an arrow per tab; by default on
  the matched line), and code follows `editor.tabSize`, `editor.fontSize` and `editor.fontLigatures`. New settings:
  `spelunk.ui.syntaxHighlighting` (on by default) and `spelunk.ui.renderWhitespace` (`editor` by default, to follow
  the editor).
- **Outline** in a code preview's header opens a column beside the code listing every definition in the file, as
  VS Code's Outline does: members under their class, type or interface (a Go method under its receiver's type, a
  Rust method under its `impl`), a letter for each kind, line numbers, and the definition the match is in marked.
  A click opens that definition beside the editor. It's closed until you open it, and stays as you leave it. It
  replaces the "In this file" line, which showed only the first eight names.
- The line between the results and the preview, and the Outline's left edge, can be dragged (or focused and moved
  with ← →) to resize them; a double-click resets either, and both widths are remembered, as the empty box's divider
  already was.
- Resting the mouse on any icon in the panel (⇄, Aa, the gear, ±1, Outline, a recent query's star or ×, a pinned
  query's bell or pencil...) shows its name in a third of a second, in the theme's hover colors. VS Code's webviews
  show no tooltips of their own, so these names were never seen before. The magnifying glass and the Replace row's
  icon have names now too.
- Jupyter notebooks (`.ipynb`) are searched by their cells' code and notes instead of their JSON: each result sits
  under a **Cell 2 · Python** heading with its line numbered within the cell, the preview shows the cells with their
  run numbers and what their outputs are, and opening a result opens the notebook editor at that cell with the match
  selected. `symbol:`, `kind:`, `ref:` and `language:` see notebook code, plots are never searched and don't count
  toward `spelunk.index.maxFileSizeKB`, history diffs a notebook by its cells (re-running one is no change), and `at:`
  opens a past notebook read-only in the notebook editor. New setting: `spelunk.index.notebookOutputs` (off) also
  searches the text cells printed.

### Fixed

- Files changed where VS Code doesn't look (a folder `files.watcherExclude` leaves out, a disk it can't watch) or
  while it was closed could show their old text until the next full rebuild. Each window now checks its repos' files
  a few seconds after it opens and every two minutes, and re-reads the changed ones.
- Searches that ask for two things one list can't hold now say so instead of quietly dropping one:
  `symbol:Parse OR timeout` (definitions on one side, code on the other) and `type:file symbol:Parse`, `type:code
  kind:function` or `type:file ref:Parse` (a `type:` that rules out what the operator finds) are errors, with a fix
  that removes either part.
- A word left out with `-` or `NOT` left out the code lines of a file that has it, but not the file's name:
  `-inputGrace file:daemon/` still listed `daemon/main.go`, which mentions it. Files whose text has a left-out word are
  now left out too.
- `is:changed` found nothing for the first seconds after VS Code opened, until the history was next brought up to
  date. It now knows the uncommitted files as soon as the saved index loads.
- C functions written after an attribute macro, as in Linux's `static __printf(2, 3) void dev_err(…)` or
  `__alloc_size(1) void *kmalloc(…)`, were found under the macro's name. They are now found under their own:
  in the Linux kernel, 836 more functions answer to their names. Saved indexes are rebuilt once.
- A left-out `-symbol:Parse` no longer offers to "Search Parse as text".
- Show them, under a note for a left-out group such as `NOT (parse OR lex)`, left its closing parenthesis behind
  and broke the query. It now removes the whole group.
- Replace in a Jupyter notebook edited its JSON, so a replacement with a quote or a backslash left a notebook VS Code
  couldn't open, and it changed what cells had printed. It now edits the cells, through the notebook editor.
- When a daemon that stopped answering was restarted, the searches waiting on it could fail with "write EPIPE"
  instead of saying why; they now fail with "the daemon stopped answering, so it was restarted".
- A word next to `kind:` or `symbol:` now matches the definition's name: `kind:function parse` returns the functions
  whose names contain "parse", not every function in a file that mentions it. `kind:function content:parse`
  finds the lines inside functions that mention it.

### Changed

- Two VS Code windows on the same repo build and save its index once: the first writes it, and the other uses its
  saves and takes over when it closes. Building the daemon now needs Go 1.25.
- A word now matches its exact text: `RetryPolicy` no longer lists `retry_policy.py`, nor `retry` `registry.go`.
  `match:fuzzy` brings back file names that have a word's letters in order, as Quick Open finds files
  (`match:fuzzy usrsvc` finds `UserService.ts`), and without it a note under the results counts them, "3 file names
  have these letters in order", with Show them. New setting `spelunk.search.match` (`exact` by default; `fuzzy` makes
  it the default again).
- Only `file:`, `author:`, `symbol:`, `language:` and `message:` keep a short name, and it reads: `f:`, `a:`,
  `sym:`, `lang:` and `msg:`. The other one-letter names (`s:`, `i:`, `d:`, `r:` …) and `path:` are gone; typing one
  says **Unknown search word s: (now symbol:)** and the fix changes it to the full name.

## [2.0.0] - 2026-10-10

### Added

- New settings for what was fixed before: `spelunk.search.timeLimitMs` (how long a search runs before it shows
  what it has, 2000 by default), `spelunk.index.vendoredFolders` and `spelunk.index.generatedSuffixes` (which files
  `is:vendored` and `is:generated` match and ranking puts last), `spelunk.index.codeownersFiles` (where `owner:`
  looks for CODEOWNERS), `spelunk.index.commitCheckSeconds` (how often new commits are picked up, 5 by default) and
  `spelunk.ui.previewContextLines` (lines around the match in the preview, 7 by default).
- Context lines: **Context 0 · ±1 · ±2 · ±3** in the Code heading shows up to three dimmed lines above and below
  each code match, so you can often tell what a match is without opening it. Matches close together share one run of
  lines, and **⋯** marks the lines skipped between runs; ↑ ↓ still step from match to match. The buttons save
  `spelunk.results.contextLines` (0 by default), where its value comes from.
- `is:generated` and `is:vendored` keep the files a tool wrote (`*.pb.go`, `*_pb2.py`, `*.min.js`, or a header with
  `DO NOT EDIT` or `@generated`) or the ones in `vendor/`, `node_modules/`, `third_party/` and `dist/`, the same files
  best-match ranking puts last. `RetryPolicy -is:generated -is:vendored` keeps a search or a replace to code you
  wrote, and a note says how many matches each filter left out. In a commit search they go by path, and
  `is:generated` by file name only. Their suggestions say how many files are in each.
- Query watches: the bell on a pinned query that searches commits (`msg:revert f:payments/`, say) watches it. When new
  commits by others match, the pin counts them ("2 new") and a notification says so, with **Show** and **Stop
  watching**; when the query runs, those commits are tagged **New**. ⌥W toggles the bell on the selected pin. Only new
  commits count: not the first read of a repo, nor a switch to an older branch. `spelunk.watch.notify` turns the
  notification off and keeps the count.
- `owner:` keeps the files a repo's CODEOWNERS file gives an owner: a team (`owner:@acme/payments-core`, or
  `owner:payments-core`), a person (`owner:jane`) or an email, matched whole and ignoring case. As on GitHub, the
  file in `.github/`, the root or `docs/` counts and the last line that matches a file decides; `owner:none` keeps the
  files no line gives an owner. Each result names its file's owners, the facet row counts the results by owner, and
  the preview says which CODEOWNERS line decided. Typing `owner:` lists the owners with how many files each has, and
  an owner no CODEOWNERS file names gets a warning that offers the nearest ones. Current files only.

- Multiline regex: a regex with `\n` in it matches across lines, as in VS Code's search, so
  `/except TransientError:\n\s*delay/` finds an `except` whose next line sets a delay. Each match is one code result
  that shows every line it spans (up to ten, then "14 more lines, to line 25"), the preview marks all of them, and
  opening it selects the whole match. `^` and `$` still mean the start and end of a line, lines that end in `\r\n`
  match the same, and every other regex still matches one line at a time. Current files only: commits are matched one
  changed line at a time, so a history query warns, and Replace explains it can't change such a match.

- Structural search: `struct:"backoff(:[call], tries=:[n])"` finds code by its shape. A template is code with holes,
  and a hole takes any code whose brackets pair up, so the template finds a call however its arguments are laid out
  over lines. Spaces don't matter except between words, and code in comments and strings never matches, by each
  file's language; a name used twice must take the same code, and `:[_]` takes anything. Each match is one code
  result showing the lines it spans, the template's code marked in the term's color and each hole's code in its own,
  with a chip for what each hole took; the preview marks the same and opening it selects the whole match. Current
  files only, and Replace explains it can't change a template.

- `at:` searches the files as they were at a tag, a branch or a commit (`at:v1.0.0 retry_after`, `at:HEAD~10`), or
  at a time (`at:6m`, `at:2026-01-31`: the newest commit by then), read from Git in each repo without indexing it.
  Everything that searches current files works there, ranking and `sym:` included; results say which files were
  deleted or changed since, the mode chip names the commit read and its date, and a repo without the revision says
  why. A result opens read-only as the file was, and **Compare with now** shows what changed since as a diff. Typing
  `at:` lists the repos' tags, newest first, then times. `is:open`, `is:changed`, `since:`, `until:` and `owner:`
  describe files as they are now, so they can't go with it, and Replace changes current files only.

### Changed

- Settings are grouped in VS Code's Settings as Shortcut, Search, Results, Opening results, Indexing, Display and
  Watches, and five keys moved into their group: `spelunk.caseSensitive`, `spelunk.wholeWord` and
  `spelunk.typingDelayMs` are now `spelunk.search.*`, and `spelunk.defaultCount` and `spelunk.order` are
  `spelunk.results.count` and `spelunk.results.order`. A value saved under an old key moves to the new one by itself.
- protoc's Python output (`*_pb2.py`) and any header that says `DO NOT EDIT` beside the word "generated" now count as
  generated code, so best-match ranking puts those files last too.
- The repo facet and the repo menu write an anchored filter, `repo:^api$`, so picking `api` no longer also keeps
  `payments-api`.
- A minus before punctuation is text, not a negation: `word:yes ->` searches for `->`, and `-=` and `--verbose` work
  the same. `-word`, `-2`, `-"phrase"`, `-/regex/` and `-(…)` still leave things out.
- With a workspace folder opened inside another, its files belong to the inner folder alone, so each is found,
  counted and replaced once; closing the inner folder gives them back to the outer one.
- A folder that becomes a Git repo after it was opened (`git init`, or a clone into it) gets its history within
  `spelunk.index.commitCheckSeconds`, and one whose `.git` goes away turns history off, without a reload.
- History follows `spelunk.index.historyDepth` as time passes: commits older than the window drop out, and a history
  saved under another depth or `index.exclude` is read again.
- F4 and ⇧F4 leave VS Code's Search view, search editors and References lists alone, and Next and Previous Result are
  listed in the Command Palette only while there are results.
- `spelunk.index.location` takes an absolute path or one starting with `~` (`~\` on Windows too); anything else keeps
  the indexes in the default place, with a line in the log.
- Building Spelunk needs Go 1.24.
- No search can hang the daemon any more: matching checks the time limit and cancellation as it goes inside a file,
  and a file cut short marks the results as cut short. A regex that can cross lines, like `/[^;]*zz/`, is matched a
  line at a time (33 s on an 8,000-line file before, 7 ms now), and `struct:` or a `\n` regex on a long minified
  line takes milliseconds instead of minutes.
- Saving files and adding folders no longer hold up searches: they are applied in the background, back-to-back saves
  in one go (20 saves in a 50,000-file repo delayed the next search by 5 s; now by under a millisecond). A
  `git checkout` that changes thousands of folders is applied in milliseconds instead of seconds.
- Indexing reads each file once, several at a time: a 50,000-file workspace indexes in about 2 s instead of 5.
- New commits merge into a few history segments instead of adding one per commit, so searching history stays as
  fast after a day of commits as after a fresh read.
- `at:` reads every repo's revision at once, so a workspace waits for its slowest repo, not the sum of them.
- Replace all checks its files 16 at a time.
- The extension stops writing to the daemon while it isn't reading, keeping only the newest settings and folders
  while it waits.

### Fixed

- A daemon that stops answering is restarted: while requests wait, 20 s of silence makes the extension ask it, and
  no answer in 10 s restarts it. The connection also finds its place again after stray output, instead of every
  later request waiting forever.
- A folder created, copied in or renamed is searchable at once; a renamed folder's files used to vanish until the
  next rebuild.
- `since:` and `until:` suggestions count files as the search does, by their last commit, with uncommitted edits as
  now.
- A note saying how many matches case or whole words hid is left out when the search ran out of time to count them
  all, instead of showing a number that stopped short.
- A query of only `case:` or `count:` says it has nothing to search for, instead of saying every term is excluded.
- A file a replace plan listed twice was edited twice, which VS Code refuses; it is edited once.
- A result that doesn't open says why in a message and in the log.
- Lowering `spelunk.ui.recentQueries` hides the extra saved queries at once, a query pinned twice keeps its watch, and
  a pasted query too long to run isn't saved for next time.
- The welcome page says that taking over the find-in-files key leaves VS Code's Search view without one, and the help
  page's `struct:` example is a template the daemon accepts.

- A code line, path or name wider than the results no longer pushes the preview off the side of the panel.
- A facet bucket, the repo menu, **Aa** or **ab** on a query with OR at its top level put it in parentheses first, so
  the filter narrows every branch instead of only one (`retry OR timeout lang:python` meant `retry OR (timeout
  lang:python)`).
- Closing the Replace row while **Replace all** ran left Replace stuck on "Replacing…" for as long as the panel stayed
  open; the row now previews again once the replace finishes.
- After a click on a result, ↑ ↓, ↵ and Esc did nothing until you clicked back into the box; the box now keeps the
  keys.
- Between 641 and about 795 px wide, the preview wrapped below the results and was cut off out of sight; it now sits
  beside them at every width above 640 px. At 640 px or less, where it hides, the panel no longer asks for previews
  nobody sees.
- On the welcome page, moving through the shortcut choices with ↑ ↓ opened Keyboard Shortcuts on reaching **Choose my
  own**, and each saved choice redrew the page and lost the focus. Picking it now only clears the key; its **Open
  Keyboard Shortcuts** button opens the editor, and the focus stays on the choice.
- Large result sets (up to `count:50000`) streamed, stepped and previewed Replace in time that grew with every row:
  each batch laid out the whole list again, each ↑↓ searched every row, and each change in the Replace row redrew
  every row. The list now lays out only the rows near the view, ↑↓ go straight to the next row, and Replace redraws
  only the lines it changed, the ones out of sight as they come into view. At 50,000 results a batch takes 4 ms
  instead of 65, ↑↓ under a millisecond instead of 12, and a Replace preview 24–53 ms instead of over a second.
- Under the High Contrast Light theme, operator names, chips and highlights used the dark theme’s pale colors (1.7:1
  on white); they now use the light theme’s, and under either high contrast theme the selected result and suggestion
  are outlined, as in VS Code’s own lists.
- Screen readers heard a code result as its line number and text alone; it now starts with its file’s path, which the
  panel shows once above the file’s lines.
- Typing `?` while an operator’s values were suggested opened Help and dropped the character, so `f:*.ts?` couldn’t be
  typed; `?` now opens Help only while operators are suggested, and among values it is typed.
- A facet’s **+N**, once clicked, stayed open for every later search; it now shows every bucket of that search only.
- On the help page, screen readers heard each of the 32 **Try** buttons as just “Try”; each now says the query it
  runs.
- Typing `since:6m` suggested only “6min · Last 6 minutes”, though `6m` means six months; a whole window you have
  typed is now listed first as it reads (“6m · Last 6 months”), ahead of the units it could still become. The same
  goes for `until:` and `at:`.
- An operator suggestion said nearly the same thing twice: `struct:` read “Code shaped like a template, whose :[holes]
  take any code” above “Code shaped like a template; each :[hole] takes any code.”, and `content:`, `message:`,
  `owner:` and `at:` repeated their line word for word. Each now has a short title (**Structural search**, **File text
  only** …) above its one-line explanation.
- The preview of a file without an owner read “No owner in   .github/CODEOWNERS line 5”, with a wide gap and no
  separator; it now reads “No owner · .github/CODEOWNERS line 5 names none”, or “No owner · no line of CODEOWNERS
  matches this file”.
- The empty box’s key hints offered **⌥W watch** whatever row was selected; they now offer it only on a pin that can
  be watched (or **stop watching** on a watched one), and say **unpin** and **run pinned query** on a pin.
- Esc runs the search again as typed, and ↑ ↓ pressed before its results came were undone when they arrived: the
  selection jumped back to the first row. The result you were on now stays selected when the new results still have
  it.
- In a narrow results column a file name’s path was cut to a few letters (“src/p…”) to make room for its badges, last
  author and repo; the path now comes first and those go on a line of their own, where the author and time give way
  first. A highlight cut by the path’s ellipsis no longer leaves a stray bar of color after it.
- A commit’s body line ran past the edge of the results column and was cut off there; it now ends in an ellipsis
  inside its row, like other rows.
- A long path pushed a code group’s repo name onto two lines (“payments-” / “api”), and the preview’s title wrapped
  beside the revision chip; both keep to one line now, and the path is what gives way.

### Security

- A preview never follows a symbolic link out of its folder, even a folder replaced by a link after the search
  checked it.

## [1.1.0] - 2026-10-05

### Added

- `is:open`, `is:changed` and `is:test` (short form `i:`) keep the files open in editor tabs, the files with
  uncommitted changes, or the test files; `retry -is:test` leaves tests out. In a commit search `is:test` keeps the
  commits' test-file changes. Its suggestions say how many files are in each state now.
- `word:yes` (short form `w:`) matches whole words only, so `retry` no longer finds `retryCount` or `autoretry`. The
  **ab** button between **Aa** and **.\*** adds and removes it, the `spelunk.wholeWord` setting makes it the
  default, and `word:no` turns it off for one query. A note says how many matches were only parts of words, with
  **Match parts of words** to show them.
- `case:smart` matches case only when the query has a capital letter: `retry` finds `Retry`, but `RetryPolicy`
  doesn't find `retrypolicy`. `spelunk.caseSensitive` is now `off`, `on` or `smart` (still `off` by default); a saved
  `true` or `false` becomes `on` or `off`. Under smart case **Aa** carries a dot and shows which way the query's
  capitals sent it, and a note says what matching case hid, with **Ignore case**.
- `type:added` and `type:removed` search commits for the lines they added or removed, not their messages:
  `type:added retry_policy` finds who introduced it, `type:removed` when it went away. Commit rows count "hits in
  added lines", previews mark only that side, and a note counts the commits that matched only elsewhere, with
  **Search every changed line**. The history index now keeps each line's `+` or `-`, so it is read again once after
  upgrading.
- `kind:` (short form `k:`) keeps definitions of one kind: `function`, `method`, `class`, `interface`, `type` or
  `other`. `sym:Retry kind:class` narrows a name, `kind:class f:models/` lists one kind, and a note says how many
  definitions it hid, with **Show every kind**.
- `ref:` (short form `x:`) finds the lines that use a name as a whole word and leaves out the lines that define it:
  `ref:RetryPolicy` finds the imports and calls, not `class RetryPolicy`. It matches by name, so a different
  `RetryPolicy` in another repo counts too.
- A word also finds file names that have its letters in order, as Quick Open does: `usrsvc` finds `UserService.ts`
  and `cfg` finds `config.py`. The match starts at a word of the name (a word with a `/` matches the whole path),
  the matched letters are highlighted, and such names come after the ones that contain the word. Code lines,
  phrases, regexes, `-` exclusions and `word:yes` stay exact.
- Pinned queries: the star on a recent query keeps it at the top of the empty box, with a name if you give it one.
  The pencil renames a pin, and the filled star or ⇧⌫ unpins it. Pins are saved in `spelunk.ui.pinnedQueries`, so
  they sync like other settings; set it in a workspace's settings to share pins, and pinning there saves there.
- `content:retry` matches text inside files but never file names, and in a commit search only the changed lines, not
  the message. Its value is searched as written, so `content:"type:file"` finds that text.
- `path:` is another spelling of `file:`, and `NOT x` of `-x`, as GitHub and Sourcegraph write them.
- `since:` takes dates: `since:2026-09-30` starts at that day's midnight and `since:2026-09` at the month's first, in
  your time zone. A date that doesn't exist, such as `2026-02-30`, says why and offers the nearest one.
- `until:` (short form `u:`) is the other end of `since:`, with the same values. A day or a month counts in full, so
  `until:2026-09` keeps everything before October and `since:2026-09 until:2026-09` is September; `until:2w` keeps
  what is older than two weeks. A warning says when `until:` ends before `since:` starts.
- Facets: a row above the results counts every result by repo, language and top folder, or a commit search's by
  repo, author and month. Click a bucket to keep only it (`repo:payments-api`, `f:^src/`,
  `since:2026-09 until:2026-09`), Alt-click to leave it out; a bucket the query keeps shows pressed, one it leaves
  out stays struck through, and clicking either takes its filter back out.
- Results from current files come best match first: files that define what you searched for, are open in the editor
  or are named after it lead, then recently changed ones, and tests, vendored and generated files come last. Each
  section title says the order, and a badge says why a file moved (**definition**, **open**, **test**, **vendored**
  or **generated**). `order:path` (short form `o:`), or the `spelunk.order` setting, keeps the old repo, path and line
  order. Commits stay newest first.
- Replace in results: **⇄** beside **.\*** (or ⌥⌘F) opens a Replace row. Each code match shows struck out with its
  replacement after it, in the results and the preview, and the row counts the matches and files; with a `/regex/`,
  `$1` or `${name}` inserts what a group matched. **Replace all** (⌘↵) changes every match of a one-term query in
  current files as one edit that ⌘Z undoes in every file, leaves the files unsaved, and skips whole any file edited
  since it was indexed.

### Fixed

- A commit's preview said its other files were hidden by `f:` even when `lang:` hid them; it now says "hidden by
  filters".
- The help page lists `min` and `h` among the time-window units.

## [1.0.1] - 2026-10-05

### Changed

- The README and Getting Started say to install the `.vsix` from the GitHub release instead of building it from a
  clone.

## [1.0.0] - 2026-10-05

### Added

- A plain word with a pipe, like `daemon|search`, now warns that it matches the pipe too, and offers
  `daemon OR search` or `/daemon|search/` instead.
- A plain word in a history query matches a commit's subject and body as well as its changed lines, so
  `author:jane retry` finds a commit that only says "retry" in its message. `msg:` still searches the message alone.
- Commit rows say whether a commit matched in its message, its diff or both, and the commit preview marks the
  matched words in the message.

### Changed

- Unified Search is now called Spelunk. Its settings, commands and keybinding conditions moved from
  `unifiedSearch.*` to `spelunk.*`, and the index from `~/.unified-search` to `~/.spelunk`. Settings you changed
  don't carry over, and the index rebuilds once.

### Fixed

- A recent-queries width saved in a wide panel no longer squeezes the operator cheat sheet when the panel narrows,
  and the divider between them shows from the first width where both fit.
- Screen readers announce where the divider between recent queries and the operators is.
- `?` in an empty query box brings the operator cheat sheet into view when a narrow panel stacks it below the
  recent queries.

## [0.2.0] - 2026-10-05

### Added

- Drag the line between recent queries and the operators to resize them, or focus it and press ← →. Double-click it
  to reset; the panel remembers the width.
- Every operator has a full name and a one-letter short name, such as `file:` and `f:` or `author:` and `a:`.
  Suggestions and help show the full name with the short one beside it; `lang:`, `sym:` and `msg:` still work.
- Remove a recent query with the × at the end of its row, or select it and press ⇧⌫.

### Fixed

- A query can no longer hang or crash the daemon: queries with many misplaced globals parse in linear time, a
  pasted megabyte parses in under a second, deeply nested parentheses no longer overflow the stack, and one query
  reports at most 100 problems.
- Fix-its no longer join words or land inside an unclosed quote, and closing a quote or regex that ends in a
  backslash really closes it. A fix-it, completion or toggle computed for older text is not applied to newer text,
  and ⌘Z undoes it.
- Indexing never blocks on a FIFO, never reads through a symlink out of the root, and indexes nested repos,
  submodules and roots reached through a symlink. Very long or minified lines are searched and previewed in
  bounded time and memory.
- A damaged saved index is rebuilt instead of crashing a search, saved indexes are synced before they replace the
  old ones, and temp files left by a crash are removed.
- History reads every commit whatever its message, date or the user's Git config (`diff.noPrefix`,
  `log.showSignature` and others); a failed read no longer publishes a partial history.
- A forged result ref can't open a file outside its folder through a symlink.
- The daemon refuses oversized or malformed JSON-RPC frames without allocating them, answers every malformed
  message, survives a panicking handler, stops writing once its output fails, and exits soon after VS Code goes away.
- The extension recovers from garbage on the daemon's stdout, times out a daemon that never finishes starting, stops
  at once when the binary is missing or not executable, and uses defaults for settings of the wrong type.
- The search panel drops malformed or late messages, ignores a corrupt saved state, leaves keys to an input method
  while it composes, keeps the query box in sight in a short panel, and fits the help and welcome pages in a narrow
  editor. Highlights never split an emoji.
- A pasted query of any length costs little: only its first 2000 characters are read, and it is reported as too
  long.
- A daemon that keeps crashing is restarted after a pause that doubles each time, up to 30 seconds, instead of every
  200 ms forever.
- A result after invalid UTF-8 in a file opens at the column VS Code shows: invalid bytes are replaced the way the
  editor replaces them.

### Changed

- Files are searched as the editor shows them: a UTF-8 byte order mark is dropped, UTF-16 files with a byte order
  mark are indexed (they were treated as binary), and a lone CR ends a line.
- `sym:` finds Java and C# methods by their line, whatever its indent, and finds constructors explicitly.

## [0.1.0] - 2026-10-05

### Added

- Search daemon skeleton: JSON-RPC 2.0 over stdio, initialize/shutdown lifecycle, crash restarts.
- `protocol/` JSON Schema with generated TypeScript and Go types.
- VS Code extension host: daemon supervisor, search controller and search panel.
- Search panel webview: query box with Aa / .* toggles, completions, fix-its, chips, results and preview.
- Query language: parser, diagnostics with fix-its, completions and planner for every operator.
- Working-tree search over a trigram index: file names and code, `f:`, `case:`, `/regex/`, `lang:`, `repo:`,
  `type:`, `count:` with Load more, AND / OR / NOT, and "N hidden by …" notes with an undo.
- Background indexing per workspace root, saved between sessions, with `index/status`, `index/rebuild` and progress.
- Previews highlight every term of the query that produced the result.
- Operator suggestions with descriptions and example values; results stay on the finished words while a word is
  being completed, and Enter or Esc searches it as typed.
- A repo menu that scopes the query to one repo, a "Show code too" undo for `type:file`, and "Ignore case" for
  `case:yes`.
- The help page: every operator with an example to try, worked examples, keys and settings.
- Typing an operator lists the values it takes: time windows for `since:` with when each starts and how many files
  changed, file types and folders for `f:`, repos with their index state, the workspace's languages, and what each
  `type:`, `case:` and `count:` value does.
- `f:` and `repo:` also take globs such as `*.go` and `src/**/*.ts`.
- `since:` also takes `today`, `yesterday`, minutes (`30min`) and hours (`2h`). `since:30m` (30 months) warns and
  offers `30min`.
- `npm run package` builds a `.vsix` for any platform, and F5 runs the extension on the sample repos.
- Git history search: `author:`, `msg:` and `type:commit` find commits by their added and removed lines, messages and
  authors (after `.mailmap`), newest first, with the diff in the preview. Recent commits are searchable while older
  history is still being read, and new commits within seconds. `index.historyDepth` sets how far back.
- `since:` on current files uses each file's last commit, and counts files with uncommitted edits as changed now.
- `author:` lists the workspace's authors with their commit counts, and `msg:` suggests the words and phrases of
  recent commit subjects.
- Saved, created and deleted files are searchable within a second, without rebuilding the index.
- `sym:` finds definitions (classes, interfaces, functions, methods and types) in 14 languages, suggests the
  workspace's names as you type, and offers to search a name as text with how many matches that finds. A file
  preview lists the members of the class it shows.
- A welcome page, shown once after installing and by **Unified Search: Show Welcome**: pick the search shortcut and
  how much to index, and watch each repo's indexing.
- After opening a result, the status bar says which one it is ("Opened from search · result 2 of 5"); F4 and ⇧F4
  step through the rest, and clicking it goes back to the results.
- When a search finds nothing, the panel says why (for example that `case:yes` is on) above what each filter hid.

### Fixed

- A reopened search panel no longer ignores the queries typed into it.
- A search panel opened after indexing finished shows the workspace's repos ("All repos · 0" before).
- Opening a result to the side (⌘↵) keeps the search panel open; `open.closeOnOpen` hides it only for results opened
  in its place.
- Indexing progress bars fill in VS Code: they were set with style attributes, which webviews block.
- A saved index that can't be read says so while it is rebuilt, and an index that can't be saved (a full disk) keeps
  serving with a warning.
- Case-insensitive searches no longer copy each file to lowercase it, and regex terms are found by their literal
  part first, so both are faster.
- Files inside a folder named in `unifiedSearch.index.exclude`, such as `node_modules`, are no longer indexed.
- A query whose words never occur in the same file no longer scans every file.
- Restarting the search from the panel while a crash restart was pending no longer starts a second daemon.
- A repo whose index failed no longer shows as "Indexing" in the status bar.
- The `case:` suggestion shows its description and values like the other operators.
- `sym:` queries return no results instead of wrong ones until symbol search exists.

[Unreleased]: https://github.com/cruisinme30/spelunk/compare/v2.0.0...HEAD
[3.0.0]: https://github.com/cruisinme30/spelunk/compare/v2.0.0...v3.0.0
[2.0.0]: https://github.com/cruisinme30/spelunk/compare/v1.1.0...v2.0.0
[1.1.0]: https://github.com/cruisinme30/spelunk/compare/v1.0.1...v1.1.0
[1.0.1]: https://github.com/cruisinme30/spelunk/compare/v1.0.0...v1.0.1
[1.0.0]: https://github.com/cruisinme30/spelunk/compare/v0.2.0...v1.0.0
[0.2.0]: https://github.com/cruisinme30/spelunk/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/cruisinme30/spelunk/releases/tag/v0.1.0
