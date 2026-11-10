package server

// replace/plan: the edits that replacing a query's matches in current files
// would make, for the panel's preview and the host's WorkspaceEdit.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/query"
	"github.com/cruisinme30/spelunk/daemon/internal/rpc"
	"github.com/cruisinme30/spelunk/daemon/internal/trigram"
)

const (
	// maxReplaceMatches is the most matches one replace changes. A query
	// with more gets only the count, so the panel can ask for a narrower one.
	maxReplaceMatches = 10_000
	// replaceBudget is how long planning a replace may take. Unlike a
	// search, which returns what it found in its budget, a replace that runs
	// out of time fails: part of a replace must never pass for all of it.
	replaceBudget = 10 * time.Second
)

// replacePlan answers replace/plan. The query must search current files
// for code and have exactly one text term outside a NOT, the one replaced.
func (s *Server) replacePlan(ctx context.Context, raw json.RawMessage) (any, error) {
	var params protocol.ReplacePlanParams
	if err := decode(raw, &params); err != nil {
		return nil, err
	}
	parsed := query.Parse(params.Text, s.resolver(params.OpenFiles))
	plan, _, err := query.NewPlan(parsed, s.Settings(), s.now(), "")
	if err != nil {
		return nil, rpc.Errorf(protocol.CodeQueryInvalid, "%v", err)
	}
	term, err := replacedTerm(plan)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeoutCause(ctx, replaceBudget,
		rpc.Errorf(protocol.CodeOverloaded, "finding every match took too long; narrow the search to replace"))
	defer cancel()
	if templateErr := validateReplacement(ctx, term, params.Replacement); templateErr != nil {
		return nil, templateErr
	}
	repos := trigram.WithOpenFiles(s.index.Repos(), params.OpenFiles)
	replace, err := trigram.Replacements(ctx, plan, repos, term, params.Replacement, maxReplaceMatches)
	if errors.Is(err, trigram.ErrCaptureBudget) || errors.Is(err, trigram.ErrReplaceBudget) || errors.Is(err, trigram.ErrMatchBudget) {
		return nil, rpc.Errorf(protocol.CodeOverloaded, "%v", err)
	}
	if errors.Is(err, trigram.ErrIncomplete) {
		return nil, rpc.Errorf(protocol.CodeQueryInvalid, "%v", err)
	}
	if err != nil {
		return nil, err
	}
	if err := checkReplacementPaths(ctx, replace.Files, repos); err != nil {
		return nil, err
	}
	return replace, nil
}

// checkReplacementPaths refuses indexed paths whose live files can no
// longer be edited inside the selected roots. The extension repeats this
// check against its trusted workspace immediately before applying/saving;
// a plan cannot hold a filesystem path still across editor interaction.
func checkReplacementPaths(ctx context.Context, files []protocol.ReplaceFile, repos []trigram.Repo) error {
	roots := make(map[string]string, len(repos))
	for _, repo := range repos {
		roots[repo.ID] = repo.Root
	}
	for i := range files {
		file := &files[i]
		if ctx.Err() != nil {
			return context.Cause(ctx)
		}
		root, ok := roots[file.RepoID]
		if !ok || !filepath.IsLocal(filepath.FromSlash(file.Path)) {
			return rpc.Errorf(protocol.CodeRefStale, "replacement file is outside its folder")
		}
		if err := insideRoot(root, file.Path); err != nil {
			return err
		}
		// Unlike read-only opening, replacement never follows even an
		// in-root link: the index did not index that redirected file.
		full := root
		for _, part := range strings.Split(file.Path, "/") {
			full = filepath.Join(full, part)
			info, err := os.Lstat(full)
			if err != nil || info.Mode()&os.ModeSymlink != 0 {
				return rpc.Errorf(protocol.CodeRefStale, "replacement path changed since indexing: %s", file.Path)
			}
		}
	}
	return nil
}

// replacedTerm is the one text term a replace changes, or why plan can't
// be replaced.
func replacedTerm(plan *query.Plan) (*query.Content, error) {
	if plan.Mode != protocol.ModeWorkingTree {
		return nil, rpc.Errorf(protocol.CodeQueryInvalid, "replace changes current files, not commits")
	}
	if plan.At != "" {
		return nil, rpc.Errorf(protocol.CodeQueryInvalid, "replace changes current files, not the files at:%s", plan.At)
	}
	if !plan.Kinds[query.KindLine] {
		return nil, rpc.Errorf(protocol.CodeQueryInvalid, "replace changes code matches, and this query finds none")
	}
	var terms []*query.Content
	query.VisitPositive(plan.Pred, func(term *query.Content) { terms = append(terms, term) })
	if len(terms) != 1 {
		return nil, rpc.Errorf(protocol.CodeQueryInvalid, "replace needs exactly one search term")
	}
	if terms[0].Structural != nil {
		// A template's holes say what to keep, which a replacement can't use yet.
		return nil, rpc.Errorf(protocol.CodeQueryInvalid, "replace changes text or a regex, so it can't replace a struct: template")
	}
	if terms[0].Multiline {
		// The host checks and edits one line at a time (see Invariants).
		return nil, rpc.Errorf(protocol.CodeQueryInvalid, "replace changes one line at a time, so it can't replace a regex with \\n")
	}
	return terms[0], nil
}
