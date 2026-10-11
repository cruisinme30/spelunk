package server

// symbols/find: definitions by name for VS Code's Go to Symbol in
// Workspace (⌘T), Go to Definition and Find References.

import (
	"context"
	"encoding/json"
	"path/filepath"

	"github.com/cruisinme30/spelunk/daemon/internal/excludes"
	"github.com/cruisinme30/spelunk/daemon/internal/lang"
	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/trigram"
)

const (
	// defaultSymbolLimit is how many definitions symbols/find returns when
	// the request doesn't say.
	defaultSymbolLimit = 200
	// maxSymbolLimit caps a request's limit.
	maxSymbolLimit = 1000
)

// registerSymbols registers the handler behind the editor's navigation.
func (s *Server) registerSymbols() {
	s.handle(protocol.MethodSymbolsFind, s.findSymbols)
}

// findSymbols answers symbols/find from the current files' definitions,
// leaving out what VS Code's excludes hide, as a search does.
func (s *Server) findSymbols(ctx context.Context, raw json.RawMessage) (any, error) {
	var params protocol.SymbolsFindParams
	if err := decode(raw, &params); err != nil {
		return nil, err
	}
	limit := params.Limit
	if limit <= 0 {
		limit = defaultSymbolLimit
	}
	limit = min(limit, maxSymbolLimit)
	settings := s.Settings()
	repos := s.index.Repos()
	q := trigram.SymbolQuery{
		Text: params.Text, Exact: params.Exact, Limit: limit,
		Kinds:        lang.NewFileKinds(settings.VendoredFolders, settings.GeneratedSuffixes),
		IncludeKinds: params.IncludeGeneratedAndVendored,
		Excludes:     excludes.New(settings.EditorExcludes),
		Language:     languageOf(repos, params.LanguageOf),
	}
	result := protocol.SymbolsFindResult{Language: q.Language}
	for _, found := range trigram.FindSymbols(ctx, repos, q) {
		result.Symbols = append(result.Symbols, protocol.SymbolMatch{
			RepoID: found.Repo.ID, Path: filepath.Join(found.Repo.Root, filepath.FromSlash(found.Doc.Path)),
			Line: found.Symbol.Line, Column: found.Column(), Name: found.Symbol.Name,
			SymbolKind: found.Symbol.Kind, RankReason: found.Mark,
		})
	}
	return result, nil
}

// languageOf returns the language of the file at the absolute path: from
// its name, or, for a name that doesn't say (a script with no extension),
// as the index read it from its first line; "" when neither knows.
func languageOf(repos []trigram.Repo, path string) string {
	if path == "" {
		return ""
	}
	if language := lang.Detect(filepath.ToSlash(path), nil); language != "" {
		return language
	}
	for i := range repos {
		repo := &repos[i]
		rel, err := filepath.Rel(repo.Root, path)
		if err != nil || !filepath.IsLocal(rel) {
			continue
		}
		for _, doc := range repo.CurrentDocs() {
			if doc.Path == filepath.ToSlash(rel) {
				return doc.Lang
			}
		}
	}
	return ""
}
