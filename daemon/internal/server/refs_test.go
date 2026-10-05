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
