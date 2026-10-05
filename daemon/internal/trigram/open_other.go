//go:build !unix

package trigram

// openFlags adds nothing where there are no FIFOs or O_NOFOLLOW: the file's
// type is still checked after it is opened.
const openFlags = 0
