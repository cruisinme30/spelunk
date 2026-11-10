# Versioned wiki source and publication

## Issue and example

Code and its user guide lived in separate Git repositories. A fix could pass code review while its wiki edits
remained local. For example, a corrected replacement safety rule could merge while the published guide still
described the old rule. CI silently skipped the wiki check when no sibling clone existed.

## Root cause and change

Separate source repositories made review and validation depend on extra credentials and an external checkout.
The canonical pages and PNG assets now live in `docs/wiki/`, so implementation, tests and documentation merge in
one reviewed change. The mandatory offline `checkWikiSync.mjs` validates screen/mock correspondence, screenshots,
page links, sidebar entries and reference coverage. Missing source fails. An existing Markdown-image capture bug
was also fixed, so both HTML and Markdown image links are checked.

Publication is a distinct deployment. Relevant `develop` pushes run only source validation and publisher
regressions, then a job with an ephemeral `contents: write` token mirrors the exact checked-out source. PR jobs
receive no write token, and application tests are not repeated merely to publish documentation. The publisher
allows regular Markdown pages and PNG images, rejects symlinks and metadata, preserves Git history, and makes no
commit when content already matches.

Two source checks compare the current `develop` wiki and publication-tool identities with the validated source.
Newer relevant content supersedes an older run; unrelated code commits do not strand queued documentation.
Publication is serialized and uses a normal push, so concurrent wiki edits cannot be forcefully overwritten.

## Evidence and limitations

Regression tests live in `scripts/wikiPublication.test.mjs`; their local bare repositories exercise publication,
unchanged content, stale source, unrelated advances, safe assets, traversal, symlinks and concurrent mirror edits.
On 2026-11-10, `node --test --test-reporter=tap scripts/wikiPublication.test.mjs` passed all 12 cases; the
[raw TAP output](../evidence/wiki-publication-tests.tap) records the run. Scoped ESLint, Prettier, cspell,
Markdown lint, the mandatory content checker and repository-wide knip passed. No performance claim follows from
these checks.

GitHub token support must still be verified by the first hosted publication. Separate repositories cannot update
atomically: a relevant push can arrive after the final identity check, and the next publication brings the mirror
forward. `checkWikiDeployment.mjs` compares a freshly fetched mirror explicitly; it does not replace source validation.

## Status

Implemented and locally validated for review. Hosted publication and remote-content verification remain pending a
successful develop merge and workflow run.
