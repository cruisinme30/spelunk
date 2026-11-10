# Refuse replacement plans that exceed bounded resources

**Issue.** An edit-count cap did not bound a replacement's memory or transport size. A short template could repeat
one large capture thousands of times. Capture-rich regexes and dense lines could allocate large index matrices
before the first permitted edit was returned.

**Example.** A 64 KiB captured group expanded by 100,000 `$1` references describes more than 6 GiB of new text,
although the input fixture is only about 264 KiB. Separately, replacing 256 matches across two files with 64 KiB
each exceeds a practical payload limit while remaining far below the 10,000-edit cap.

**Root cause.** Counting edits ignores replacement width, duplicated source lines and JSON escaping. Checking an
expanded string after `regexp.Expand` would be too late: allocation has already occurred. Materializing every
capture also defeats an otherwise small requested edit limit.

**Change.** One request-wide budget limits the serialized plan to 16 MiB, conservatively accounting for JSON-escaped
source/new text, paths, notebook metadata, hashes, fields, punctuation and maximum-width integers. Capture references
resolve to source slices; each slice is charged before it is appended. Templates also have a 16 MiB input cap, and
validation runs under the existing 10-second planning deadline. Capture materialization is limited to 262,144
integer indexes per line. Shared positional enumeration is being integrated to cover matching before edit creation,
including whole-word assertions and long runs of empty matches.

**Why it works.** A budget error discards the entire plan and returns `Overloaded` with narrowing guidance. Earlier
files cannot escape as a misleading partial success. The source of a repeated capture is inspected without first
copying its expansion. Conservative structural reserves prevent JSON serialization from crossing the promised cap.

**Validation.** `replace_budget_test.go` compares escaped-byte accounting with `encoding/json`, covers Unicode and
invalid UTF-8, tests maximum-width structural metadata, exercises cross-file exhaustion and rejects the potential
6 GiB expansion. Those trigram tests passed. Server error-mapping tests and final shared enumeration checks are
pending integration validation; the final scope will be updated with their results.

**Tradeoffs and status.** The 16 MiB figure bounds the plan payload, not total process RSS. Conservative accounting
can refuse a plan before it exactly fills that size. The capture-index limit excludes slice/runtime overhead and a
temporarily retained previous prefix. Main sources are `replace_budget.go`, `replace_template.go`,
`replace_captures.go` and server `replace.go`. Implemented on the working branch; final checks and publication pending.
