# Industry comparison and red team

<!-- cspell:words PCRE -->

Reviewed 2026-11-10. Compare specific tasks, not an overall product rank. A documented feature is not a measured
performance result. Spelunk observations below refer to the development checkout; they do not imply a release.

| Reference | What it establishes | Spelunk comparison | Measurement rule |
| --- | --- | --- | --- |
| [ripgrep](https://github.com/BurntSushi/ripgrep/blob/master/README.md) | Fast local text/regex search, Unicode, optional PCRE2 and encoding support | Adds an index, lexical contexts, definitions, history, editor replacement and MCP; those features do not excuse slower equivalent text search | Match accepted files, case behavior and line semantics; distinguish a resident index from CLI startup and file reads |
| [git grep](https://git-scm.com/docs/git-grep) | Searches tracked content and Git trees; fixed/regex patterns, Boolean combinations and thread control | A useful dependency-free reference for repository text and revision searches | Pin tree, locale, threads and working-tree versus revision mode |
| [Zoekt](https://github.com/sourcegraph/zoekt/blob/main/doc/query_syntax.md) | Indexed regex, symbols, repository/branch filters and Boolean queries | Closest architectural reference for indexed search; compare build time, index size, retained memory and complete results | Run the same local corpus and index settings before claiming a latency advantage; currently unmeasured |
| [Sourcegraph](https://sourcegraph.com/docs/code-search/features) | Cross-repository search, symbols and saved searches | Spelunk concentrates on the local editor, terminal and working tree | Hosted latency and instance configuration are separate from local engine timing; currently unmeasured |
| [GitHub Code Search](https://docs.github.com/en/search-github/github-code-search/understanding-github-code-search-syntax) | Hosted regex, Boolean and symbol search | Useful feature reference; a hosted service is not a comparable local benchmark | Record host coverage and query limits; do not infer latency from documentation; currently unmeasured |

[Sourcegraph also exposes MCP](https://sourcegraph.com/docs/api/mcp). MCP support alone is therefore not a unique
advantage. Spelunk must demonstrate correct results, bounded resource use and easy setup in actual clients.

## Repeatable exercise

1. Pin the Spelunk commit, corpus commit, tool versions and accepted-file manifest. Retain the seeded queries and
   raw samples. Use a mixture of common, rare, absent, literal, regex, path, Boolean, context and symbol queries.
2. Compare answers before speed. Record result identities, counts, hidden-result statistics, timeouts and caps.
   Keep incomplete queries separate. An optimized query that returns less work is not a verified speedup.
3. Measure at least 20 samples, p50/p95, first response and completion, index build time, retained heap, process RSS
   and peak RSS. Run tools sequentially. State cache state, concurrency and memory limits; use a shared cache for
   the mixed-query workload. Profile in a separate run.
4. Red-team MCP against the protocol versions actually advertised: lifecycle, JSON-RPC errors, malformed/oversized
   input, argument schemas, cancellation, result limits, stale index state and process shutdown. Distinguish
   in-process tests from real client integration. Follow the [official protocol](https://modelcontextprotocol.io/specification).
5. Start setup from a clean user profile. Count commands, manual configuration edits and prerequisites. Check paths
   with spaces, an unrelated working directory, a repository that is not indexed, useful help and actionable failures.
   Test generated configuration by launching it, not only by checking its JSON shape.
6. Minimize each failure, fix its cause and add a regression. Keep Issue → Example → Fix changelog entries,
   architecture decisions, wiki and website synchronized. Integrate with merge commits after the develop gate;
   promote batches to main only after the full suite and real end-to-end tests.

Run this exercise during active development until each confirmed finding is fixed and validated. No recurring
schedule is enabled. Report access and environment blockers; a documented comparison does not imply that a hosted
competitor was benchmarked.
