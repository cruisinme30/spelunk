package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cruisinme30/unified-search/daemon/internal/protocol"
	"github.com/cruisinme30/unified-search/daemon/internal/rpc"
)

func TestForgedRefsCannotReachOutsideTheirFolder(t *testing.T) {
	// @covers rpc:preview/get rpc:open/resolve failure:ref-stale
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "client.py"), clientSource)
	outside := t.TempDir()
	mustWriteFile(t, filepath.Join(outside, "secret.txt"), "secret\n")
	for link, target := range map[string]string{"link.txt": filepath.Join(outside, "secret.txt"), "linkedDir": outside} {
		if err := os.Symlink(target, filepath.Join(dir, link)); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	client := newTestClient(t)
	client.mustInitialize(t, protocol.Root{ID: "r1", Path: dir, Name: "r"})
	for _, ref := range []string{
		"tree|1|r1|1|0|1|../../../../../../etc/passwd",
		"tree|1|r1|1|0|1|/etc/passwd",
		"tree|1|r1|1|0|1|link.txt",
		"tree|1|r1|1|0|1|linkedDir/secret.txt",
		"tree|1|r1|1|0|0|.",
		"tree|1|r2|1|0|0|client.py",
		"hist|1|r1|--output=x",
		"garbage", "", "tree||||||",
	} {
		var preview protocol.Preview
		err := client.call(protocol.MethodPreviewGet, protocol.PreviewParams{Ref: ref, ContextLines: 2}, &preview)
		wantRPCCode(t, fmt.Sprintf("preview/get %q", ref), err, protocol.CodeRefStale)
		err = client.call(protocol.MethodOpenResolve, protocol.OpenResolveParams{Ref: ref}, nil)
		wantRPCCode(t, fmt.Sprintf("open/resolve %q", ref), err, protocol.CodeRefStale)
	}
}

func TestPreviewClampsContextLines(t *testing.T) {
	// @covers rpc:preview/get
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "client.py"), clientSource)
	client := newTestClient(t)
	client.mustInitialize(t, protocol.Root{ID: "r1", Path: dir, Name: "r"})
	tests := []struct {
		ref          string
		contextLines int
		wantLines    int
	}{
		{"tree|1|r1|5|0|0|client.py", -5, 1},
		{"tree|1|r1|5|0|0|client.py", -1 << 40, 1},
		{"tree|1|r1|5|0|0|client.py", 1 << 62, 6}, // the whole file, with its final empty line
		{"tree|1|r1|0|0|0|client.py", -5, 1},
		{"tree|1|r1|0|0|0|client.py", 1 << 62, 6},
	}
	for _, tt := range tests {
		var preview protocol.Preview
		err := client.call(protocol.MethodPreviewGet, protocol.PreviewParams{Ref: tt.ref, ContextLines: tt.contextLines}, &preview)
		if err != nil || len(preview.Lines) != tt.wantLines {
			t.Fatalf("preview/get %q with contextLines %d = %d lines, %v; want %d lines", tt.ref, tt.contextLines, len(preview.Lines), err, tt.wantLines)
		}
	}
}

func TestConcurrentSearchesWithCancelsLeaveNoGoroutinesBehind(t *testing.T) {
	// @covers rpc:search/start rpc:$/cancelRequest
	dir := t.TempDir()
	for i := range 200 {
		mustWriteFile(t, filepath.Join(dir, fmt.Sprintf("f%03d.txt", i)), strings.Repeat("hello world foo bar\n", 100))
	}
	client := newTestClient(t)
	batches := collectBatches(client)
	client.mustInitialize(t, protocol.Root{ID: "r1", Path: dir, Name: "r"})
	baseline := runtime.NumGoroutine()

	var mu sync.Mutex
	totals := map[string]int{} // the Total of each search that succeeded
	var wg sync.WaitGroup
	for i := range 300 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch i % 3 {
			case 0:
				cancel() // cancelled before it is sent
			case 1:
				time.AfterFunc(time.Duration(i%5)*time.Millisecond, cancel)
			}
			var result protocol.SearchResult
			err := client.conn.Call(ctx, protocol.MethodSearchStart, protocol.SearchStartParams{SearchID: strconv.Itoa(i), Text: "hello count:all"}, &result)
			var rpcErr *rpc.Error
			switch {
			case err == nil:
				// Under this load a search may run past its time budget and
				// return what it has, marked truncated.
				if result.Total != 20_000 && !result.Truncated {
					t.Errorf("search %d: total %d and not truncated, want 20000", i, result.Total)
				}
				mu.Lock()
				totals[strconv.Itoa(i)] = result.Total
				mu.Unlock()
			case ctx.Err() == nil:
				t.Errorf("search %d failed without a cancel: %v", i, err)
			case errors.As(err, &rpcErr) && rpcErr.Code != rpc.CodeRequestCancelled:
				t.Errorf("cancelled search %d error = %v, want RequestCancelled", i, err)
			}
			_ = client.conn.Notify("$/cancelRequest", map[string]any{"id": 1_000_000 + i}) // unknown id
		}()
	}
	wg.Wait()
	waitUntil(t, "search goroutines to finish", func() bool { return runtime.NumGoroutine() <= baseline+2 })
	// Every batch of a search that succeeded arrived before its response.
	for id, total := range totals {
		if n := len(batches.of(id)); n != total {
			t.Fatalf("search %s answered total %d after %d batched items, want them equal", id, total, n)
		}
	}
}

func TestSearchesKeepWorkingWhileTheFolderIsDeleted(t *testing.T) {
	// @covers rpc:search/start failure:ref-stale
	dir := t.TempDir()
	for i := range 50 {
		mustWriteFile(t, filepath.Join(dir, fmt.Sprintf("f%02d.txt", i)), "needle\n")
	}
	client := newTestClient(t)
	batches := collectBatches(client)
	client.mustInitialize(t, protocol.Root{ID: "r1", Path: dir, Name: "r"})
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = client.call(protocol.MethodSearchStart, protocol.SearchStartParams{SearchID: strconv.Itoa(i), Text: "needle"}, nil)
		}()
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	for _, item := range batches.all() {
		err := client.call(protocol.MethodPreviewGet, protocol.PreviewParams{Ref: item.Ref, ContextLines: 1}, nil)
		wantRPCCode(t, "preview/get after the folder was deleted", err, protocol.CodeRefStale)
		break
	}
	if err := client.call(protocol.MethodIndexStatus, nil, nil); err != nil {
		t.Fatalf("index/status after the folder was deleted: %v", err)
	}
}
