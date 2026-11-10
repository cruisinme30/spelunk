# Distinguish C and C++ declarations from definition bodies

<!-- Upstream Linux filenames used by the reproductions. -->
<!-- cspell:words workqueue locktorture -->

Status: unreleased; focused checks passed; integrated validation pending.

## Issue

Definition search and outlines sometimes presented declarations as functions, missed compact definitions, or ended
a real function at its signature. These mistakes also affected searches inside definitions: an incorrect body
range could hide a call that the user expected `kind:function content:...` to find.

## Example

A prototype spread across two lines, such as `static int declared(int first,` followed by `int second);`, appeared
as a function. Conversely, `int actual(void) { return 1; }` was missed. Linux functions with trailing implementation
attributes, including lock annotations, could receive a range that stopped before their body.

## Root cause

The initial line rule used the presence of a semicolon to distinguish declarations from definitions. That could
not handle a semicolon on a later line or inside a one-line function body. The body scanner also stopped at an
unrecognized continuation after a signature, without accounting for reserved implementation attributes.

## Change

A shared C/C++ header classifier now distinguishes confirmed declarations and initializers from body openers.
Extraction and outline generation both use its decisions. Complete bodies end at their matching code brace;
comments and literal contents are already masked by the shared lexer. One-line definitions and trailing reserved
attributes are recognized. Saved shard format 7 rebuilds indexes containing the earlier symbol data.

## Why

The shared classifier keeps definition results and outlines consistent. Each header scan stops at the next
candidate, bounding total scanning work even when a malformed signature never closes. Only recognized definition
braces require retained tracking state; unrelated generated braces do not create a large auxiliary map.

## Validation

The full symbol suite and scoped lint passed. Regressions cover prototypes, macro initializers, one-line bodies,
trailing attributes and outline parent relationships. Real Linux examples in `workqueue.c` and `locktorture.c`
retain their full bodies after classification. Independent review found no blocking regression.

## Tradeoffs and status

This is a bounded lexical heuristic, not a C++ parser. Unknown continuations keep the existing fallback; macro
expansion and constraint expressions remain limitations. Other languages retain their established span rules.
No general speedup or complete compiler-equivalent symbol coverage is claimed.

Sources: [header classification](../../../daemon/internal/symbols/code.go),
[line rules](../../../daemon/internal/symbols/rules.go),
[regressions](../../../daemon/internal/symbols/code_test.go).
