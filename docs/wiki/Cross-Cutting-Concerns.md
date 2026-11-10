# Cross Cutting Concerns

| Concern | Rule |
| --- | --- |
| **Cancellation** | Every keystroke sends `$/cancelRequest` for the previous search. Daemon loops check `ctx.Done()`; the webview drops responses with an older `seq` |
| **Offsets** | Every `Span`, `Range` and `Hit` counts UTF-16 code units, like JavaScript and VS Code |
| **Regexes** | RE2 (Go `regexp`): linear time, no backreferences or lookaround |
| **Limits** | Queries up to 1,000 characters; `count:all` stops at 50,000 and reports `truncated`; one text scan visits at most 32,768 raw match positions before the search is incomplete |
| **Crashes** | The host restarts the daemon after a pause that doubles from 200 ms to 30 s, at most 3 times a minute, then offers a Restart button. A daemon that stops answering while requests wait is killed and restarted the same way |
| **Observability** | `SPELUNK_TRACE=<file>` records the JSON-RPC transcript; the extension logs to the "Spelunk" output channel |
| **Testing** | Each contract has its own tests, so a change breaks the cheapest layer first. See [[Testing]] |

## Performance budgets (p95)

| Path | Budget |
| --- | --- |
| Keystroke → chips and diagnostics | 15 ms |
| Keystroke → first working-tree results | 100 ms after the typing delay |
| Keystroke → first history results | 250 ms after the typing delay |
| Select → preview | 50 ms |
| File saved → findable | 1 s |
| New commit → findable by `author:` | 5 s |

`scripts/checkBudgets.mjs` checks benchmark output against these on every release.
