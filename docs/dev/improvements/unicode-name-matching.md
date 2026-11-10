# Align Unicode case matching across names and text

Status: unreleased; focused checks passed; integrated validation pending.

## Issue

Ignoring case did not mean the same thing across Spelunk's search surfaces. Fuzzy file names and workspace-symbol
matching could miss names that text search treated as equivalent. They could also equate characters that the
ordinary matcher kept distinct, producing inconsistent results for the same intended name.

## Example

Greek final sigma and the long-s character were missed through simple lowercasing, even though they belong to case
equivalence classes used by the regular-expression matcher. In the other direction, dotted capital I could compare
equal to ordinary `i`, which Unicode simple case folding does not require.

## Root cause

Lowercasing selects a lowercase character; it is not the same operation as choosing a representative of every
Unicode case-equivalence cycle. Separate helpers in fuzzy matching and symbol lookup let that semantic difference
reach users. Correcting only one caller would leave the inconsistency in the other surface.

## Change

One `foldRune` helper follows Unicode simple-fold cycles and chooses a stable representative. Fuzzy eligibility,
fuzzy alignment and workspace-symbol matching reuse it. Case-sensitive matching still compares the original
characters. Names retain their original spelling, and highlight offsets continue to refer to the original path
or symbol rather than to a rewritten display string.

## Why

Sharing equivalence with ordinary ignore-case matching makes related search features agree without adding a
language-specific exception for each reported character. The implementation changes comparison, not the user's
stored files, displayed names or query grammar. The existing scoring and word-start rules still determine which
fuzzy alignments qualify and rank best.

## Validation

Focused fuzzy and workspace-symbol tests passed, including final sigma, long s and dotted I. The broader review
also passed 255 oracle queries, ten focused fuzzy cases and 18 existing ripgrep parity cases. These are correctness
checks, not evidence of a throughput improvement. Relevant scoped checks passed before the final integration gate.

## Tradeoffs and status

Simple case folding does not provide locale-specific casing, Unicode normalization or arbitrary multi-character
substitution. Fuzzy matching still applies to names rather than making code search approximate. Full integration
and release remain pending; future matching changes should extend the shared equivalence tests.

Sources: [shared folding](../../../daemon/internal/trigram/definitions.go),
[fuzzy matching](../../../daemon/internal/trigram/fuzzy.go),
[fuzzy tests](../../../daemon/internal/trigram/fuzzy_test.go).
