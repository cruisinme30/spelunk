## What and why

<!-- One or two sentences. Link the issue: Fixes #123 -->

## How it was tested

<!-- Paste the relevant part of `scripts/test-all.sh`, or explain why a layer doesn't apply. -->

## Checklist

- [ ] The PR title is a Conventional Commit, e.g. `feat(daemon): add since: to the planner`
- [ ] `scripts/test-all.sh` passes
- [ ] New behaviour has tests tagged with `@covers` (see CONTRIBUTING.md)
- [ ] Docs updated (reference docs, ADR for a decision, CHANGELOG under Unreleased)
- [ ] Breaking change to `protocol/`? The protocol version is bumped and the schema changed first
