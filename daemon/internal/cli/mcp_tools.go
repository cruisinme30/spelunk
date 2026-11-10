package cli

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/query"
	"github.com/cruisinme30/spelunk/daemon/internal/rpc"
)

type mcpTools struct {
	env          Env
	connect      sessionConnector
	backendSlots chan struct{}
}

func (t *mcpTools) search(ctx context.Context, _ *mcp.CallToolRequest, in SearchInput) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(in.Query) == "" {
		return textResult("query must contain something to search for; spelunk_syntax lists examples.", true), nil, nil
	}
	if in.Limit < 0 || in.Limit > query.MaxResults {
		return textResult(fmt.Sprintf("limit must be between 1 and %d, or 0 for the default of %d.", query.MaxResults, mcpDefaultLimit), true), nil, nil
	}
	for _, diagnostic := range query.Parse(in.Query, nil).Diagnostics {
		if diagnostic.Severity == protocol.SeverityError {
			return textResult(mcpQueryError(diagnostic.Message), true), nil, nil
		}
	}
	return t.withSession(ctx, in.Path, func(session *Session) (string, error) {
		limit := in.Limit
		if limit == 0 {
			limit = mcpDefaultLimit
		}
		results, summary, err := search(ctx, session, in.Query, SearchOptions{Count: limit, ResultLimit: limit})
		if err != nil {
			return "", err
		}
		return searchText(session, results, &summary, t.env.Now()), nil
	})
}

func (t *mcpTools) preview(ctx context.Context, _ *mcp.CallToolRequest, in PreviewInput) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(in.Ref) == "" {
		return textResult("ref must be a result ref returned by spelunk_search.", true), nil, nil
	}
	if in.ContextLines < 0 {
		return textResult("contextLines must not be negative; 0 uses the default of 10.", true), nil, nil
	}
	return t.withSession(ctx, in.Path, func(session *Session) (string, error) {
		lines := in.ContextLines
		if lines == 0 {
			lines = 10
		}
		preview, err := session.Preview(ctx, in.Ref, lines)
		if err != nil {
			return "", err
		}
		return previewText(&preview), nil
	})
}

func (t *mcpTools) status(ctx context.Context, _ *mcp.CallToolRequest, in FolderInput) (*mcp.CallToolResult, any, error) {
	return t.withSession(ctx, in.Path, func(session *Session) (string, error) {
		session.refresh(ctx)
		repo := session.Repo
		text := fmt.Sprintf("Repo %s at %s. Files: %s. History: %s. ", repo.Name, repo.Root, repo.Tree, repo.History)
		if session.Live {
			text += "A VS Code window answers, live."
		} else {
			text += "The background reader answers from the " + sourceText("saved", repo.SavedAt, repo.Reread, t.env.Now()) + "."
		}
		return text, nil
	})
}

// withSession connects for folder (the agent's folder when empty), releases
// the connection, and turns failures into tool errors the agent can act on.
func (t *mcpTools) withSession(ctx context.Context, folder string, do func(*Session) (string, error)) (*mcp.CallToolResult, any, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if folder == "" {
		folder = t.env.Cwd
	}
	if !filepath.IsAbs(folder) || strings.ContainsRune(folder, '\x00') {
		return textResult("path must be an absolute folder without NUL characters.", true), nil, nil
	}
	select {
	case t.backendSlots <- struct{}{}:
		defer func() { <-t.backendSlots }()
	default:
		return textResult("Spelunk is busy with other tool calls. Retry after one finishes; spelunk_syntax remains available.", true), nil, nil
	}
	session, err := t.connect(ctx, filepath.Clean(folder), t.env.Options)
	if err != nil {
		return textResult(mcpErrorText(err), true), nil, nil
	}
	defer session.Close()
	text, err := do(session)
	if err != nil {
		return textResult(mcpErrorText(err), true), nil, nil
	}
	return textResult(text, false), nil, nil
}

func mcpErrorText(err error) string {
	if errors.Is(err, ErrNotIndexed) {
		return notIndexedText(err) + " Ask the user to build or refresh the index; use other tools meanwhile."
	}
	if message, ok := QueryError(err); ok {
		return mcpQueryError(message)
	}
	var rpcErr *rpc.Error
	if errors.As(err, &rpcErr) && rpcErr.Code == protocol.CodeRefStale {
		return "This result is stale. Run spelunk_search again in the same path, then preview a new ref."
	}
	return "Spelunk couldn't answer: " + err.Error()
}

func mcpQueryError(message string) string {
	return "Query error: " + message + ". Fix the query and try again; spelunk_syntax lists the operators."
}
