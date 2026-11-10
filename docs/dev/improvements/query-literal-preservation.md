# Preserve literal text in query repairs

Status: unreleased; focused checks passed; integrated validation pending.

## Issue

A suggested repair could produce a valid-looking query that searched for different text. Completion values had the
same risk. These operations edit the query on the user's behalf, so preserving the selected value is part of their
correctness contract, alongside producing valid syntax.

## Example

For `unknown:\ schedule`, choosing **Search for the text instead** previously allowed the trailing backslash to
escape the new closing quote. The following `schedule` term was swallowed into the damaged literal. A value such
as `unknown:drivers\\net` could instead lose a backslash. Both failures changed the intended search.

## Root cause

The generated text escaped quotes without first escaping existing backslashes. The completion path also treated
only some whitespace and syntax-leading characters as reasons to quote a value. Encoding decisions therefore
disagreed with the query lexer that would read the result.

## Change

One `quoteText` helper now encodes literal values using the lexer's rules: escape backslashes first, then quotes,
then add the enclosing quotes. Repairs and completions share that operation. Completion values containing lexer
whitespace or beginning with `/` are quoted so they remain values rather than becoming regex syntax.

## Why

A shared encoder establishes a direct round-trip invariant: applying an offered edit and parsing again preserves
the original literal and every following term. The tests also require the repair to exist, preventing a missing
suggestion from making a preservation check pass accidentally.

## Validation

The focused query suite, scoped lint and spelling checks passed. Regression coverage includes trailing backslashes,
embedded quotes, doubled backslashes, Unicode, line breaks and slash-prefixed values. The suite includes 3,421
deterministic mutations; earlier fuzz results are not presented as validation of this change.

## Tradeoffs and status

This fixes generated query text without changing the query grammar or promising a search speedup. Full integration
and release remain separate gates. Maintain the round-trip cases whenever quoting rules change.

Sources: [encoder](../../../daemon/internal/query/source.go),
[preservation tests](../../../daemon/internal/query/fix_preservation_test.go),
[mutation tests](../../../daemon/internal/query/fix_mutation_test.go).
