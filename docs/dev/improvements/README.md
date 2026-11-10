# Engineering improvement notes

Each note explains an actual improvement and keeps its evidence separate from its integration status.
Performance notes distinguish isolated microbenchmarks, whole-query timings and configuration changes.

| Note | Focus |
| --- | --- |
| [Preserve C and C++ lexical contexts across continuation lines](c-cpp-context-classification.md) | Example, cause, mechanism, validation and limits |
| [Distinguish C and C++ declarations from definition bodies](c-cpp-definition-boundaries.md) | Example, cause, mechanism, validation and limits |
| [Case-insensitive search: remember where each scan stopped](folded-literal-cursor.md) | Example, cause, mechanism, validation and limits |
| [Reject incompatible internal RPC peers](internal-rpc-compatibility.md) | Example, cause, mechanism, validation and limits |
| [Reuse lexical classification within immutable snapshots](lexical-region-cache.md) | Example, cause, mechanism, validation and limits |
| [Bounded agent calls and stalled handshakes](mcp-admission-and-handshakes.md) | Example, cause, mechanism, validation and limits |
| [MCP result limits and honest summaries](mcp-result-limits-and-summaries.md) | Example, cause, mechanism, validation and limits |
| [Discoverable MCP setup with executable configuration](mcp-setup.md) | Example, cause, mechanism, validation and limits |
| [Preserve literal text in query repairs](query-literal-preservation.md) | Example, cause, mechanism, validation and limits |
| [Align Unicode case matching across names and text](unicode-name-matching.md) | Example, cause, mechanism, validation and limits |

Update the affected note whenever its implementation, measurements or validation changes. Retain baseline and
candidate identities, raw measurements, reproduction steps and controls for numerical claims. Artifact copies and
the website must reflect the same evidence and release status. Correctness-only fixes do not need invented speedups.
