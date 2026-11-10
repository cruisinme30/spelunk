# Apply replacements only to the reviewed source and paths

**Issue.** Matching the edited lines alone did not prove that a replacement still applied to the document the query
evaluated. An unchanged target line could become ineligible after another line changed. A path could also become a
symbolic link after indexing, redirecting an otherwise valid edit.

**Example.** A preview for `retry -blocked` must become stale when an unrelated line gains `blocked`, even if the
`retry` line remains identical. With notebook output indexing enabled, adding `blocked` to a cell's searchable
output has the same consequence. Replacing an indexed file with a link to another file must not redirect Apply or Save.

**Root cause.** The old contract described local edit ranges, without binding the complete evaluated source,
notebook structure, searchable outputs or current trusted workspace path. A matching edit count was also too weak
to establish that the user had reviewed the current plan.

**Change.** The daemon supplies a complete-source SHA-256 digest, notebook cell metadata and, when that indexed
generation includes outputs, a separate output digest. The host compares exact preview identity, hashes each opened
source once and rechecks document versions after asynchronous edit assembly. Every duplicate plan entry must map to
a selected workspace root. Path guards reject descendant links and non-regular files, capture directory/file
identities, and run before opening, applying and saving. POSIX backslashes remain literal filename characters.

**Why it works.** Content outside an edited line can no longer change a query predicate unnoticed. Notebook outputs
remain outside editable source but participate in freshness when the query reads them. A source mismatch skips the
whole file; an expired final version/path guard prevents submission of the assembled edit.

**Validation.** Focused tests in `extension/src/test/replace.test.ts`, `controller.test.ts` and
`replacementPaths.test.ts` passed, including same-count changed previews, stale predicates, duplicate roots and
symlink swaps. Go and TypeScript share eight notebook output hash vectors. A copied Linux fixture verified 119
edits across three files against an independent substitution, leaving the source checkout unchanged.

**Tradeoffs and status.** Internal RPC version 2 rejects incompatible peers. These checks do not provide an
OS-atomic filesystem transaction: VS Code's asynchronous Apply/Save API cannot prevent a path swap after the final
check. Implemented on the working branch; final integrated validation and publication remain pending. Main sources
are `replaceEdits.ts`, `replacementEditor.ts`, `replacementPaths.ts`, `notebookEditor.ts` and daemon `replace_source.go`.
