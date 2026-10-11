// Package permalink works out what a permanent link to a result names: the
// Git remote it goes to, the commit, the file's path in the repository and
// the result's lines in that commit. The extension host turns that into the
// remote host's web address.
//
// A link only works if the remote has the commit, and the line numbers are
// only right if that commit's file has the result's lines where the link
// says. So a working-tree result links to HEAD when the remote has it, and
// otherwise to the newest commit of the remote's branch that HEAD builds
// on; its lines are mapped from the file on disk (the one the index read,
// whose line numbers the result has) to that commit's file by a diff. A
// result whose own lines differ there gets no link (LinkProblemLineChanged),
// and a file renamed since links to its old path.
package permalink
