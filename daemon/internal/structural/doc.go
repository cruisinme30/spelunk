// Package structural matches code by its shape: a template such as
// retry(:[call], attempts=:[n]) is code with holes, and a hole takes any
// code whose brackets pair up, so it can hold a call, a lambda or a list
// that runs over several lines.
//
// Both the template and the file are read as tokens: words, punctuation,
// brackets and whole strings, by each language's rules for strings and
// comments. So spaces and line breaks don't matter, except that they keep
// two words apart, and text inside a string or a comment never matches the
// template's code. A hole inside the template's brackets may run over
// lines; a hole outside them stays on the line where it starts, so a
// missing closer can't make one swallow the rest of a file. A name used
// twice must take the same code both times, and :[_] takes anything.
package structural
