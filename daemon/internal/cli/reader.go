package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/rpc"
	"github.com/cruisinme30/spelunk/daemon/internal/server"
)

// readerIdle is how long the background reader waits for another request
// before it exits and frees the memory its indexes take.
var readerIdle = 10 * time.Minute

// RunReader is the background reader: it serves saved indexes read-only
// on its socket until no one has asked for readerIdle (SPELUNK_READER_IDLE
// in tests). configJSON is the Config its starter passed.
func RunReader(ctx context.Context, configJSON string, stderr io.Writer) int {
	var config Config
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		_, _ = fmt.Fprintln(stderr, "spelunk reader: bad --config:", err)
		return 2
	}
	idle := readerIdle
	if value, err := time.ParseDuration(os.Getenv("SPELUNK_READER_IDLE")); err == nil && value > 0 {
		idle = value // tests
	}
	srv := server.New(rpc.NewConn(strings.NewReader(""), io.Discard), server.Options{Reader: true}) //nolint:contextcheck // the server owns its work's lifetime; Close ends it
	defer srv.Close()
	if config.Settings.Location != "" {
		srv.Configure(config.Settings)
	}
	runDir := server.RunDir()
	listener, err := server.Listen(ctx, runDir, server.ReaderSocket(runDir))
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "spelunk reader:", err)
		return 1
	}
	acceptCtx, stop := context.WithCancel(ctx)
	defer stop()
	go srv.Accept(acceptCtx, listener)
	tick := time.NewTicker(min(idle/10, 30*time.Second))
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return 0
		case <-tick.C:
			if srv.IdleFor() >= idle {
				return 0
			}
		}
	}
}
