package server

// preview/get, open/resolve and revision/text: what a selected result
// looks like, where opening it goes, and the text of a file at a revision.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/cruisinme30/spelunk/daemon/internal/codeowners"
	"github.com/cruisinme30/spelunk/daemon/internal/history"
	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/rpc"
	"github.com/cruisinme30/spelunk/daemon/internal/trigram"
)

// maxContextLines bounds preview/get's contextLines. The engines expect a
// small non-negative count: a negative one used to fail the request with
// a slice panic, and a huge one overflowed the line arithmetic.
const maxContextLines = 10_000

// preview answers preview/get: the lines around a result, with its matches
// marked, or a commit's diff.
func (s *Server) preview(ctx context.Context, raw json.RawMessage) (any, error) {
	var params protocol.PreviewParams
	if err := decode(raw, &params); err != nil {
		return nil, err
	}
	params.ContextLines = min(max(params.ContextLines, 0), maxContextLines)
	if ref, ok := history.ParseRef(params.Ref); ok {
		repo, err := s.historyRepoFor(ref)
		if err != nil {
			return nil, err
		}
		preview, err := history.Preview(ctx, repo, ref, s.plans.recall(ref.PlanID), params.ContextLines)
		return preview, staleAsRPCError(err)
	}
	repo, ref, err := s.repoForRef(params.Ref)
	if err != nil {
		return nil, err
	}
	plan := s.plans.recall(ref.PlanID)
	if ref.Revision != "" {
		doc, readErr := s.readAtRevision(ctx, repo.Root, ref)
		if readErr != nil {
			return nil, staleAsRPCError(readErr)
		}
		preview, previewErr := trigram.PreviewText(repo, ref, plan, params.ContextLines, doc)
		return preview, staleAsRPCError(previewErr)
	}
	maxBytes := int64(s.Settings().MaxFileSizeKB) * 1024
	preview, err := trigram.Preview(repo, ref, plan, params.ContextLines, maxBytes)
	return preview, staleAsRPCError(err)
}

// openResolve answers open/resolve: the file, line and column that opening
// a result goes to, or the commit.
func (s *Server) openResolve(ctx context.Context, raw json.RawMessage) (any, error) {
	var params protocol.OpenResolveParams
	if err := decode(raw, &params); err != nil {
		return nil, err
	}
	if ref, ok := history.ParseRef(params.Ref); ok {
		repo, err := s.historyRepoFor(ref)
		if err != nil {
			return nil, err
		}
		target, err := history.OpenTarget(ctx, repo, ref)
		return target, staleAsRPCError(err)
	}
	repo, ref, err := s.repoForRef(params.Ref)
	if err != nil {
		return nil, err
	}
	if ref.Revision != "" {
		doc, readErr := s.readAtRevision(ctx, repo.Root, ref)
		if readErr != nil {
			return nil, staleAsRPCError(readErr)
		}
		label := short(ref.Revision)
		if plan := s.plans.recall(ref.PlanID); plan != nil && plan.At != "" {
			label = plan.At
		}
		return trigram.RevisionTarget(repo, ref, label, doc), nil
	}
	target, err := trigram.OpenTarget(repo, ref)
	return target, staleAsRPCError(err)
}

// revisionText answers revision/text: the whole text of the file a result
// at a revision names, as it was then, for the read-only editor that
// opening the result shows. A notebook's is its JSON, which the notebook
// editor reads, not the cells' text a search reads.
func (s *Server) revisionText(ctx context.Context, raw json.RawMessage) (any, error) {
	var params protocol.RevisionTextParams
	if err := decode(raw, &params); err != nil {
		return nil, err
	}
	repo, ref, err := s.repoForRef(params.Ref)
	if err != nil {
		return nil, err
	}
	if ref.Revision == "" {
		return nil, rpc.Errorf(protocol.CodeRefStale, "not a result at a revision")
	}
	doc, err := s.readAtRevision(ctx, repo.Root, ref)
	if err != nil {
		return nil, staleAsRPCError(err)
	}
	if doc.Cells == nil {
		return protocol.RevisionText{Text: string(doc.Content)}, nil
	}
	blob, err := history.ReadFileAt(ctx, repo.Root, ref.Revision, ref.Path)
	if err != nil {
		return nil, staleAsRPCError(err)
	}
	content, ok := trigram.DecodeBlob(blob)
	if !ok {
		return nil, staleAsRPCError(history.ErrStale)
	}
	return protocol.RevisionText{Text: string(content)}, nil
}

// repoForRef decodes a ref and finds the open root it points into. A ref
// that is malformed or names a root that is no longer open is stale.
func (s *Server) repoForRef(text string) (*trigram.Repo, trigram.Ref, error) {
	ref, ok := trigram.ParseRef(text)
	if !ok {
		return nil, trigram.Ref{}, rpc.Errorf(protocol.CodeRefStale, "malformed ref")
	}
	for _, root := range s.Roots() {
		if root.ID != ref.RepoID {
			continue
		}
		// A ref at a revision names a file in a commit, which git reads:
		// the file need not be on disk, and no link on disk is followed.
		if ref.Revision == "" {
			if err := insideRoot(root.Path, ref.Path); err != nil {
				return nil, trigram.Ref{}, err
			}
		}
		repo := &trigram.Repo{ID: root.ID, Name: root.Name, Root: root.Path}
		// Today's CODEOWNERS file can't speak for a past revision's files.
		if ref.Revision == "" {
			repo.Owners = s.codeOwners(root.ID)
		}
		return repo, ref, nil
	}
	return nil, trigram.Ref{}, rpc.Errorf(protocol.CodeRefStale, "repo no longer open")
}

// codeOwners is the CODEOWNERS file the index read for the root with id,
// for owner: in a preview and its "Owned by" line; nil when it has none.
func (s *Server) codeOwners(id string) *codeowners.File {
	for _, repo := range s.index.Repos() {
		if repo.ID == id {
			return repo.Owners
		}
	}
	return nil
}

// insideRoot checks that the file a ref names is a regular file inside
// root once symbolic links are followed. Refs are opaque, but a client can
// still send any string: a ref through a link in the workspace (link ->
// /etc) must not read or open a file outside the folders the user opened.
// The index never follows links, so no real result is refused.
func insideRoot(root, rel string) error {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return rpc.Errorf(protocol.CodeRefStale, "%v", trigram.ErrStale)
	}
	realPath, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return rpc.Errorf(protocol.CodeRefStale, "%v", trigram.ErrStale)
	}
	inside, err := filepath.Rel(realRoot, realPath)
	if err != nil || !filepath.IsLocal(inside) {
		return rpc.Errorf(protocol.CodeRefStale, "ref is outside its folder")
	}
	if info, err := os.Stat(realPath); err != nil || !info.Mode().IsRegular() {
		return rpc.Errorf(protocol.CodeRefStale, "%v", trigram.ErrStale)
	}
	return nil
}

// historyRepoFor finds the open root a commit ref points into, with its
// history index when it has one, which knows each file's name now.
func (s *Server) historyRepoFor(ref history.Ref) (*history.Repo, error) {
	for _, repo := range s.index.HistoryRepos() {
		if repo.ID == ref.RepoID {
			return &repo, nil
		}
	}
	for _, root := range s.Roots() {
		if root.ID == ref.RepoID {
			return &history.Repo{ID: root.ID, Name: root.Name, Root: root.Path}, nil
		}
	}
	return nil, rpc.Errorf(protocol.CodeRefStale, "repo no longer open")
}

// staleAsRPCError reports a result that no longer exists with the RefStale
// code, so the client can tell it from a failure.
func staleAsRPCError(err error) error {
	if errors.Is(err, trigram.ErrStale) || errors.Is(err, history.ErrStale) {
		return rpc.Errorf(protocol.CodeRefStale, "%v", err)
	}
	return err
}
