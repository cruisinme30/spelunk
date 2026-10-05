// Package glob translates the wildcards of a path glob into a regular
// expression. Callers add the anchoring their globs mean: f: and repo:
// values match from any folder to the end of the path, and index.exclude
// patterns match from the repo root, or at any depth without a slash, and
// take a matching folder's contents with them.
package glob
