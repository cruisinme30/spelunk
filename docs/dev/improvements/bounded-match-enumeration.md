# Bound raw match enumeration across search paths

**Issue.** Result, edit and highlight limits applied after regular-expression matching, so they did not bound the
temporary position lists built for a single text. Empty matches, candidates rejected by `word:yes` and dense
multi-megabyte lines could consume memory before a caller saw its first permitted result.

**Example.** A 16 MiB line with a zero-width expression can produce a position at every byte boundary. A whole-word
term can also reject thousands of early candidates before finding a valid later match. If enumeration silently
stopped, `NOT` could treat an unknown suffix as a proven absence and admit a file or commit that should be excluded.

**Root cause.** Search, history, previews, structural matching and replacement used separate helpers. Several of
those helpers asked Go's regular-expression package for every match up front, while downstream limits counted only
accepted results. Cached partial term results did not carry their incomplete status on later predicate evaluations.

**Change.** `query.Content.VisitMatches` now visits at most 32,768 raw positions per text scan, checks cancellation
between batches and preserves assertions against the original input. The budget counts empty matches and candidates
rejected by whole-word checks. Working-tree search, history, multiline and structural search, previews and Replace
share the visitor. Per-term caches retain both matches and their status; an exhausted term cannot make `NOT` succeed
or receive a hidden-filter count. `in:` classification runs before the display-highlight cap.

**Why it works.** Search treats exhaustion as an incomplete lower bound and discards the uncertain candidate.
Previews refuse incomplete highlights. Replacement rejects the entire plan and maps the failure to `Overloaded`, so
no earlier edit can escape as a partial success. Exact compatibility wrappers remain unbounded only where a caller
has no request context and explicitly requires complete enumeration.

**Validation.** Query tests cover exact exhaustion, zero-width matches before a late valid match, rejected whole-word
candidates, overlap, anchors, word boundaries, callback stop, cancellation and a dense 16 MiB line. Trigram tests
cover `in:` before highlight limits, uncertain `NOT`, multiline cancellation, previews and replacement refusal.
History tests cover uncertain `NOT`, hidden-filter credit and preview refusal. Server tests require `Overloaded` and
no edits. Focused tests, race tests, `go vet` and whole-daemon `golangci-lint` passed. The complete daemon gate passed,
including CLI/server Unix-socket integration. All 248 extension tests and 225 Playwright panel tests also passed.

**Tradeoffs and status.** The position ceiling bounds one scan's enumerated candidates, not total process RSS. A
broad expression can now return an incomplete search or refuse a replacement even when its accepted matches would
have been few; narrowing the query is the recovery. One RE2 call cannot be interrupted while it is executing, so
cancellation is observed between bounded calls. Implemented and focused validation complete; hosted integration and
performance publication remain separate release gates.
