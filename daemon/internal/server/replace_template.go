package server

import (
	"context"
	"errors"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/query"
	"github.com/cruisinme30/spelunk/daemon/internal/rpc"
	"github.com/cruisinme30/spelunk/daemon/internal/trigram"
)

// validateReplacement reports invalid captures before building any edits.
func validateReplacement(ctx context.Context, term *query.Content, replacement string) error {
	if err := trigram.ValidateReplacement(ctx, term, replacement); err != nil {
		if ctx.Err() != nil {
			return context.Cause(ctx)
		}
		if errors.Is(err, trigram.ErrReplaceBudget) {
			return rpc.Errorf(protocol.CodeOverloaded, "%v", err)
		}
		return rpc.Errorf(protocol.CodeQueryInvalid, "%v", err)
	}
	return nil
}
