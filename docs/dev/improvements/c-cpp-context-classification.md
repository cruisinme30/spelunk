# Preserve C and C++ lexical contexts across continuation lines

Status: unreleased; focused checks passed; integrated validation pending.

## Issue

Context-filtered replacement could select executable C or C++ text as a comment or string. The scanner read
physical lines, while these languages remove backslash-newline pairs before recognizing ordinary tokens. A split
delimiter could therefore move the scanner's boundary away from the compiler's lexical boundary.

## Example

Place a backslash and newline between the `*` and `/` of a block-comment closer, then write `dangerCall();` and
another ordinary comment. The old scanner missed the first closer and included the call in its comment range.
A split identifier ending in `R` could similarly be mistaken for a separate C++ raw-string prefix.

## Root cause

Comment and string helpers searched the original bytes independently. They did not share C/C++ line-splicing
semantics. Character-literal handling also assumed one character, while numeric apostrophes needed protection from
being mistaken for quote delimiters.

## Change

A logical-byte cursor skips LF and CRLF continuation pairs while retaining physical source offsets. The lexer uses
it for comments, ordinary literals, prefixes, identifiers and preprocessing numbers. Identifiers stay joined across
splices; numeric digit separators remain code. C++ raw-string bodies read the original bytes because their content
preserves those newlines. Other languages retain their own rules.

## Why

Search highlighting, structural tokens and replacement plans share the same classification. Fixing that common
boundary avoids query-specific exceptions and preserves the exact byte ranges needed to edit the original file.
The cursor adds no normalized copy of the whole source.

## Validation

The structural suite and scoped lint passed. Six end-to-end replacement cases preserve executable calls while
changing only the requested context. Tests include Unicode offsets, continued comments, raw prefixes and numeric
separators. A reduced Linux BPF assembly example verifies that assembly comments inside continued C strings stay
strings. A post-change, 30-second `FuzzLex` run completed 649,116 executions without failure; scoped lint reported
zero issues. Independent code review found no blocking regression.

## Tradeoffs and status

This remains lexical analysis: macro expansion, conditional compilation and legacy three-character escapes are
outside scope. Structural comparison still uses physical token spelling. No general scanning speedup is claimed;
the complete integration gate remains pending.

Sources: [cursor](../../../daemon/internal/structural/c.go),
[lexer regressions](../../../daemon/internal/structural/c_test.go),
[replacement regressions](../../../daemon/internal/trigram/c_context_test.go).
