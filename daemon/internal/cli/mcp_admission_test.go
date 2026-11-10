package cli

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPBackendBurstHasBoundedAdmission(t *testing.T) {
	started := make(chan struct{}, mcpConcurrentCalls+1)
	release := make(chan struct{})
	var entered atomic.Int32
	client := connectMCPTestClient(t, mcpServerWithConnector(Env{Cwd: t.TempDir()}, func(ctx context.Context, _ string, _ Options) (*Session, error) {
		entered.Add(1)
		started <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil, errors.New("backend finished")
	}))
	var wg sync.WaitGroup
	for range mcpConcurrentCalls {
		wg.Go(func() {
			_, _ = client.CallTool(t.Context(), &mcp.CallToolParams{Name: "spelunk_status", Arguments: map[string]any{}})
		})
	}
	for range mcpConcurrentCalls {
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("admitted calls never reached the backend")
		}
	}
	for range mcpConcurrentCalls {
		text, isError := callText(t, client, "spelunk_status", nil)
		if !isError || !strings.Contains(text, "busy") {
			t.Fatalf("overflow call = %q, error=%v", text, isError)
		}
	}
	if entered.Load() != mcpConcurrentCalls {
		t.Fatalf("burst started %d backend calls, want at most %d", entered.Load(), mcpConcurrentCalls)
	}
	if _, isError := callText(t, client, "spelunk_syntax", nil); isError {
		t.Error("busy backend blocked syntax help")
	}
	close(release)
	wg.Wait()
	text, isError := callText(t, client, "spelunk_status", nil)
	if !isError || !strings.Contains(text, "backend finished") || entered.Load() != mcpConcurrentCalls+1 {
		t.Errorf("backend error did not release admission: %q, entered=%d", text, entered.Load())
	}
}

func TestMCPBackendAdmissionReleasesOnCancellationAndSuccess(t *testing.T) {
	started := make(chan struct{})
	closed := 0
	connects := 0
	backend := &mcpTools{env: Env{Cwd: t.TempDir()}, backendSlots: make(chan struct{}, 1)}
	backend.connect = func(ctx context.Context, _ string, _ Options) (*Session, error) {
		connects++
		if connects == 1 {
			close(started)
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return &Session{close: func() { closed++ }}, nil
	}
	do := func(*Session) (string, error) { return "ready", nil }
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() {
		_, _, _ = backend.withSession(ctx, "", do)
		close(done)
	}()
	<-started
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled backend did not finish")
	}
	for range 2 {
		result, _, err := backend.withSession(t.Context(), "", do)
		if err != nil || result.IsError {
			t.Fatalf("admission not released: %+v, %v", result, err)
		}
	}
	if connects != 3 || closed != 2 {
		t.Errorf("connects=%d closed=%d; want 3 and 2", connects, closed)
	}
}
