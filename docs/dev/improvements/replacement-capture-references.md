# Reject undefined replacement capture references

**Issue.** Go's replacement expansion silently substitutes an empty string for an undefined capture. In a bulk
replacement this can erase intended text without producing a syntax error or warning.

**Example.** With `/(retry)/`, `$1_suffix` means the single capture name `1_suffix`, not capture 1 followed by a
suffix. The intended replacement is `${1}_suffix`. Similarly, `$99` and `${missing}` previously expanded to empty
text when those groups did not exist. This surfaced while checking replacements against copied Linux source.

**Root cause.** The replacement path delegated directly to `regexp.Expand` without distinguishing an undefined
reference from a declared optional group that legitimately did not match. Reimplementing only common dollar forms
would create another incompatibility: Go also supports Unicode group names, numeric names, duplicate names,
literal `$$` and malformed dollar expressions that remain literal.

**Change.** Validate the template before producing any edits. A synthetic match marks every declared group present;
Go's own expansion determines how each reference is interpreted. Undefined references return `QueryInvalid`, name
the offending token and explain explicit braces. Runtime expansion caches the reference's group indexes but selects
the first actually matched duplicate name. Canonical numeric references keep Go's numeric meaning. Literal search
replacements retain literal dollar text.

**Why it works.** Validation can distinguish a nonexistent group from a valid unmatched optional group without
changing the regex engine's grammar. For `(?P<x>a)|(?P<x>b)`, `${x}` still yields `b` on the second alternative;
resolving that name to only the first group would incorrectly erase it. Valid optional groups continue to expand to
empty text, and `$0` continues to represent the whole match.

**Validation.** `daemon/internal/server/replace_test.go` covers undefined `$99`, `${missing}` and `$1_suffix`, valid
`${1}_suffix`, `$0`, `$$`, optional groups, named/numeric groups, duplicate-name fallback and literal queries.
`replace_budget_test.go` adds template-size and cancellation checks. Trigram tests passed after the final reference
resolution change; the server's corrected call site awaits the integrated validation window.

**Tradeoffs and status.** This deliberately fails closed for undefined references instead of copying Go's silent
deletion behavior. It does not invent a separate replacement language. Main sources are
`daemon/internal/trigram/replace_template.go` and `daemon/internal/server/replace_template.go`. Implemented on the
working branch; final checks and publication remain pending.
