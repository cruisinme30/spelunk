# 0002. Generate cross-process types from one JSON Schema

- Status: Accepted
- Date: 2026-10-04

## Context

The same shapes (`ParsedQuery`, `ResultItem`, `Preview`, …) cross three boundaries and exist in two languages. If each
side writes its own copy, the copies drift and bugs show up at runtime, far from where they started.

## Options considered

1. **Hand-written types on each side**, plus tests. Cheap to start, but drift is only caught when a test happens to
   cover the field.
2. **Go as the source**, with TypeScript generated from Go structs. Union types (`kind`-discriminated) don't
   translate well.
3. **JSON Schema as the source**, generating both. Neutral, can validate recorded transcripts, and expresses unions
   with `oneOf`.

## Decision

Option 3. `protocol/protocol.schema.json` holds every type plus tables of RPC methods and webview messages.
`protocol/gen.mjs` (dependency-free Node) writes TypeScript and Go. In Go, a union becomes one struct whose
`MarshalJSON` emits only the fields of the variant named by `kind`, and required arrays are never `null`. Generated
files are committed and checked with `gen.mjs --check`.

## Consequences

- A schema change and its regenerated output go in one commit.
- The generator only supports the schema features we use. If we outgrow it, quicktype or similar can replace it
  without changing the schema.
- The method and message tables drive the spec-coverage check.
