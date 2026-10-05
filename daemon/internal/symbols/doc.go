// Package symbols finds the definitions in a source file (classes,
// interfaces, functions, methods and types) for symbol: and for the outline
// of a file preview.
//
// It reads one line at a time with a few regular expressions per language,
// as ctags does for most languages. That misses definitions split across
// lines and can't tell a nested function from a method in every language,
// but it needs no parser and no external tool, and it is fast enough to run
// on every file the index reads.
package symbols
