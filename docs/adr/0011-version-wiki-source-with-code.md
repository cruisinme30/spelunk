# Version wiki source with code

- Status: Accepted
- Date: 2026-11-10

## Context

The user guide lived in a separate wiki Git repository. Code could pass CI while its documentation remained local or
could not be pushed. The old checker silently passed when no wiki clone existed, including in CI.

## Decision

Keep canonical pages and static assets in `docs/wiki/`. Review and merge code and documentation atomically. Validate
their content offline in every CI and presubmit run. Publish the GitHub wiki as a derived mirror only on relevant
pushes to `develop`, after a separate read-only documentation check. Publication receives a job-scoped `contents:
write` token; PR validation never receives wiki write credentials.

The publisher checks out the exact pushed source commit, rejects newer wiki or publication-tool content, copies only
regular Markdown pages and PNG images, excludes Git metadata and symlinks, and pushes normally to preserve wiki
history. Workflow concurrency serializes publications. Main promotion does not republish the wiki or trigger
duplicate documentation work on unrelated pushes.

## Consequences

A failed deployment does not lose reviewed documentation. It is visible as a failed workflow, and a fresh clone can
be compared with `checkWikiDeployment.mjs`. GitHub's repository token policy still needs verification on the first
hosted publication; local regression tests cannot establish hosted authentication support. The token's permission is
repository-wide, not a wiki-only capability, so it belongs only to the trusted develop publication job.

Separate Git repositories cannot update atomically. A relevant develop push can arrive after the publisher's final
source check; the next relevant publication brings the mirror forward. Non-fast-forward wiki pushes fail instead of
rewriting manual or concurrent edits. Contributors must edit the canonical source and avoid direct wiki edits.
