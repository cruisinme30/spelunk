package engine

import (
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
)

// Budget is how long one search may run before it returns what it has,
// marked truncated.
const Budget = 2 * time.Second

// Stats describe one page of a search.
type Stats struct {
	Total      int // results counted, at most query.MaxResults
	Truncated  bool
	NextOffset int // where the next page starts; 0 when this is the last page
	Hidden     []protocol.HiddenNote
}
