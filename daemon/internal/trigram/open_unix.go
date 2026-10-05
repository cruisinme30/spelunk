//go:build unix

package trigram

import "syscall"

// openFlags make opening a file that is listed as regular safe if it has
// since been replaced: O_NONBLOCK keeps a FIFO from blocking the open until
// a writer appears, and O_NOFOLLOW refuses a symlink. Neither changes how a
// regular file reads.
const openFlags = syscall.O_NONBLOCK | syscall.O_NOFOLLOW
