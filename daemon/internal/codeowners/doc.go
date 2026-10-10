// Package codeowners reads a repo's CODEOWNERS file and says who owns each
// path, by the rules GitHub uses: the first file found of
// .github/CODEOWNERS, CODEOWNERS and docs/CODEOWNERS counts, its patterns
// match like .gitignore's, and the last line that matches a path decides
// its owners.
package codeowners
