# Keep dense replacement work proportional to the edits

**Issue.** A line containing many matches made replacement planning repeat work over the same prefixes. UTF-16
column conversion started at the beginning for each match, and capture expansion repeatedly searched the line.
These costs grew much faster than the number of edits, making dense generated or minified source a poor case.

**Example.** The reproducible isolated benchmark repeats `😀retry` followed by a space and replaces `/(retry)/` with `$1`.
The supplementary character matters: Go's byte offsets and VS Code's UTF-16 columns differ. Fixtures with 1,000 and
4,000 matches expose scaling while retaining an exact expected edit sequence.

**Root cause.** Correct coordinate conversion was implemented independently for each edit instead of carrying
forward the previous byte and UTF-16 positions. Capture extraction likewise discarded reusable progress. A tempting
shortcut—rematching only the matched substring—would be incorrect: `(a$)|(a)` in `a b a` depends on the original
line's right boundary and must select different groups at its two matches.

**Change.** Replacement generation advances byte and UTF-16 cursors with ordered matches. Capture expansion keeps
a bounded, growing prefix of capture indexes from the original complete line. The first small edit limit requests
only the needed prefix. Whole-word matching may restart inside a rejected match; its assertion-free fallback keeps
the entire remaining suffix, preserving the right context rather than clipping a match.

**Why it works.** Each ordinary source prefix is decoded once for coordinate conversion, while captures retain the
regex engine's original group choices. Resource exhaustion fails the complete plan explicitly. This retains safe
preview/apply behavior while removing the repeated scans that dominated dense-line planning.

**Validation.** `daemon/internal/trigram/replace_test.go` covers astral UTF-16 columns, CRLF source, anchored
alternatives, whole-word restarts, optional captures, cancellation, filtered prefix growth and stopping at the edit
cap. Focused tests passed before the final shared matcher integration. The retained baseline is commit `a1b8baf`;
the 1,000/4,000-match benchmark runs use 20 iterations and one CPU. Final candidate measurements remain pending,
so earlier development timings are not presented as release numbers.

**Tradeoffs and status.** This is a replacement-planner isolated benchmark, not an end-to-end Linux search benchmark or
a claim about total daemon memory. Growing prefixes can temporarily retain the previous allocation. Main sources
are `daemon/internal/trigram/replace.go` and `replace_captures.go`; final integrated checks and publication remain
pending on the working branch.
