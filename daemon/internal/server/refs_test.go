package server

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/cruisinme30/unified-search/daemon/internal/protocol"
)

func TestForgedRefsCannotReachOutsideTheirFolder(t *testing.T) {
	// @covers rpc:preview/get rpc:open/resolve failure:ref-stale
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "client.py"), clientSource)
	outside := t.TempDir()
	mustWriteFile(t, filepath.Join(outside, "secret.txt"), "secret\n")
	for link, target := range map[string]string{"link.txt": filepath.Join(outside, "secret.txt"), "linkdir": outside} {
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
		"tree|1|r1|1|0|1|linkdir/secret.txt",
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
