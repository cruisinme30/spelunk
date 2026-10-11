package server

// folder/explain: whether a folder's files are searched, and why not, and
// whether it is a Git repo of its own, for Search in Folder's notes.

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/rpc"
	"github.com/cruisinme30/spelunk/daemon/internal/trigram"
)

// registerFolders registers the handler behind Search in Folder's notes.
func (s *Server) registerFolders() {
	s.handle(protocol.MethodFolderExplain, s.folderExplain)
}

// folderExplain answers folder/explain. A folder the index holds no file
// of gets the reason ListFiles leaves its files out; one inside a Git repo
// of its own names that repo, whose commits history doesn't read.
func (s *Server) folderExplain(ctx context.Context, raw json.RawMessage) (any, error) {
	var params protocol.FolderExplainParams
	if err := decode(raw, &params); err != nil {
		return nil, err
	}
	folder := strings.TrimRight(params.Path, "/")
	if folder != "" && !filepath.IsLocal(filepath.FromSlash(folder)) {
		return nil, rpc.Errorf(rpc.CodeInvalidParams, "path must be inside the repo")
	}
	root, opts, indexed, ok := s.index.FolderState(params.RepoID, folder)
	if !ok {
		return nil, rpc.Errorf(protocol.CodeRefStale, "repo no longer open")
	}
	explanation := protocol.FolderExplanation{GitRepo: trigram.GitRepoOf(root, folder)}
	if folder != "" && !indexed {
		explanation.Skipped = string(trigram.WhySkipped(ctx, root, folder, opts))
	}
	return explanation, nil
}
