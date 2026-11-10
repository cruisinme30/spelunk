# 0010. Bind replacement plans to the complete source

- Status: Accepted
- Date: 2026-11-10

## Context

Checking only edited lines cannot prove that those lines still qualify for replacement. Changing a comment opener
elsewhere can move an unchanged call from code into a comment. A changed negated term or notebook cell boundary can
also change the query's answer without changing the target line. Equal match counts do not identify equal plans.

## Decision

Each replacement file carries a SHA-256 digest of the complete decoded source used by the query. Hash each source
line as UTF-8 followed by LF, so editor line-ending normalization does not cause false mismatches. Notebook plans
also carry source-cell kind, language and line count; output cells are excluded from the source digest and edits.
Languages are compared using the shared editor-language mapping; a raw cell's language does not affect its meaning.

Notebook output text can still affect query eligibility, for example through a negated term. When the indexed
generation searched outputs, an additional digest binds the output text and its cell mapping, including a generation
with no outputs yet. A first searchable output or an edit to output text then invalidates a stale plan. Generations
that did not search outputs need no output digest. Replacement edits source only and retains search's query semantics.

Before building the single `WorkspaceEdit`, the host checks the required digests, notebook structure, every target
line and each UTF-16 range. Changed files are skipped whole. The host also requires the current preview and checks
captured document versions again immediately before submitting the edit. The daemon still never writes source files.

Content equality does not establish path identity. Replacement also binds each repo-relative path and absolute file
to a currently selected root. The daemon rejects redirected indexed paths; the host checks the root, ancestors and
regular file before opening, applying and saving, rejecting descendant links and changed identities. A stable link
used as the workspace root is allowed; a descendant link does not inherit that permission.

## Consequences

Digests add fixed-size fields per file, plus notebook cell metadata, without sending another full source copy over
RPC. Validation reads and hashes each live document, so cost grows with source and searchable-output size. This cost binds
edits to the context the query evaluated; checking only target lines or counts would not establish that invariant.

This is a check against accidental stale edits, not an atomic filesystem transaction or an authenticity signature.
VS Code applies and saves asynchronously after the final validation, so these checks do not eliminate every possible
filesystem race between validation and the editor's write.
Normal editor undo and explicit saving remain unchanged. The required source digest changes the internal RPC
contract to version 2, with fields defined in the shared schema and generated for both peers. Host and daemon must
be updated together: an older host cannot enforce this invariant, and an older plan lacks the required digest.
