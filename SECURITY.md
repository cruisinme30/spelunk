# Security policy

The search daemon runs on your machine with read access to your workspace and its Git history, so security reports
are taken seriously.

## Supported versions

Only the latest released version gets security fixes while the project is on 0.x.

## Reporting a vulnerability

Please **don't open a public issue**. Use GitHub's private vulnerability reporting instead: the repository's
**Security** tab → **Report a vulnerability**. Include the version, your OS, and steps to reproduce.

You'll get an acknowledgement within seven days and a fix or mitigation plan within 30 days of confirmation.

## Scope

In scope: anything that lets a query, a workspace file, a Git object or a webview message make the daemon or the
extension read, write or run something outside what the user asked for. Examples: path traversal in `ref` handling,
regex denial of service (all regexes are RE2, which runs in linear time), or script injection into the webview.
