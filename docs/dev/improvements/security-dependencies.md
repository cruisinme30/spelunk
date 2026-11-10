# Security dependency remediation

## Issue and example

The dependency lockfile included Markdown parsers, glob matching and validation packages with known
denial-of-service advisories. Malformed Markdown or amplified glob patterns could consume excessive resources.
GitHub also reported incomplete hostname matching in Azure permalink normalization.

## Changes and mechanism

PR #5 updates fast-uri, qs and undici. The follow-up refreshes vulnerable transitive dependencies, including
markdown-it 14.3.3 and zod 4.6.7. Targeted overrides select patched glob, minimatch and markdown-it 15 releases
where upstream ranges still select vulnerable versions. Unaffected minimatch 3 consumers retain their versions.
The existing Markdown linter is retained because the newer linter introduces a vulnerable regular-expression engine.

Azure normalization now rewrites only ssh.dev.azure.com. Explicitly configured custom hosts, including names
ending in dev.azure.com, keep their own hostname. Regression coverage checks lookalikes and both official hosts.

## Evidence and limitations

On November 10, 2026, npm audit reported zero vulnerabilities after installation. The full local presubmit
passed with the patched dependencies. Hosted CI must validate the final commits before integration.
No performance improvement is claimed. Overrides should be removed when upstream dependencies adopt patched ranges.

The wiki heading code-scanning alert was reviewed as a false positive: heading strings are used only for set
membership checks, and a final Unicode allowlist removes HTML punctuation. They are never rendered as HTML.
The alert was dismissed with this evidence. No secret-scanning alerts were open.
