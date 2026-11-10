# Independent red-team review ledger

Review date: 2026-11-10. Scope: confirmed issues in this batch, their corrections and remaining evidence.
“Closed” below means the focused behavior is checked; it does not mean merged or released.
Final integration checks and publication remain separate gates in [progress.md](progress.md).

| Finding | Correction and evidence | Review status |
| --- | --- | --- |
| MCP `count:all` could override the caller's output limit. | Apply validated `resultLimit` after planning; `mcp_test.go` checks one shown result with the exact three-result fixture total. | Closed: owner tests passed; source reviewed. |
| Agent bursts could start unrestricted backend calls. | Eight active backend calls per MCP process, immediate retry on excess, reusable slots; `mcp_admission_test.go`. | Closed: owner tests passed; source reviewed. |
| An accepted socket could stall forever during `share/hello`. | Shared two-second handshake deadline closes blocked transports; four window/reader and unread/unanswered pipe cases in `mcp_handshake_test.go`. | Closed: owner tests passed; source reviewed. |
| Incomplete results falsely claimed a timeout and exact total. | Shared CLI/MCP “at least” count and cause-neutral incomplete notice; `mcp_summary_test.go` also covers exact totals with smaller output pages. | Closed: focused tests passed; wording reviewed. |
| Generated MCP JSON lacked executable-launch evidence. | Binary-backed `extension/src/test/mcpSetup.test.ts` launches generated argv from another directory with spaced paths, then proves the configured folder is honored. | Closed: owner integration test passed; source reviewed. |
| Notebook output changes could invalidate eligibility without changing source. | Persist output-indexing generation; shared Go/editor output fingerprints include initially empty output sets. Golden vectors and stale-output tests cover both implementations. | Closed in design review; final combined test gate pending. |
| Repeated captures or large literal edits could build an oversized replacement plan. | Shared 16 MiB serialized-plan accounting charges JSON-escaped text before append; cross-file, escaping and repeated-capture regressions in `replace_budget_test.go`. | Closed: focused trigram/server tests passed; source reviewed. |
| Bounded capture expansion initially lost duplicate-name fallback. | Resolve all duplicate named groups and choose the first matched one; preserve canonical numeric references. Server cases cover duplicate `x`, `01` and `1`. | Closed: focused server tests passed; source reviewed. |
| Replacement-template validation preceded its deadline. | Bound template input and run cancellable validation under the planning deadline; `TestReplacementValidationBoundsTemplatesAndCancellation`. | Closed: focused trigram/server tests passed; source reviewed. |
| Shared matching could allocate every raw position before replacement limits ran. | Replace unbounded whole-word assertion and all-empty-match enumeration with a shared bounded API; search must report incomplete work and replacement must refuse partial plans. | Open: shared implementation and integration in progress. |
| A code-only develop push could strand a pending wiki publication. | Compare canonical docs and publication-tool blob identities when develop advances; local-remote regressions cover unrelated code and newer tooling. | Implemented; final publisher tests pending. |
| Wiki Markdown image paths escaped validation. | Validate both HTML and Markdown image captures through a testable helper. | Implemented; final checker tests pending. |
| Benchmark completeness, cache state and interference could mislead comparisons. | Preserve raw session samples, identities, full counts, statistics and manifests; classify incomplete cases separately. Replay contaminated timings. | Method reviewed; final measurements pending. |

The replacement payload ceiling is not a total-process memory limit. Shared indexes, source buffers, temporary match
storage and serialization add memory. Bounded matcher exhaustion must remain visible rather than masquerading as a
complete search or replacement plan.

The full-kernel interrupted runs and integrated candidate v5/v6 timings are diagnostic only. The final comparison must
identify the finished candidate and use uninterrupted measurements after quality checks. Reference CLI benchmarks
remain pending that candidate and an exclusive CPU slot. First hosted wiki publication also requires remote evidence;
local Git tests do not establish hosted-token access or successful deployment.
