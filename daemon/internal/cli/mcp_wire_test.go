package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type mcpWire struct {
	input  *io.PipeWriter
	output *json.Decoder
	done   chan int
}

// newMCPWire drives the same command and streams an installed MCP client
// launches, without a socket or a test-only transport in the application.
func newMCPWire(t *testing.T, args []string) *mcpWire {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	input, toServer := io.Pipe()
	fromServer, output := io.Pipe()
	wire := &mcpWire{input: toServer, output: json.NewDecoder(fromServer), done: make(chan int, 1)}
	stop := context.AfterFunc(ctx, func() {
		_ = input.CloseWithError(ctx.Err())
		_ = output.CloseWithError(ctx.Err())
	})
	t.Cleanup(func() {
		cancel()
		stop()
		_ = input.Close()
		_ = toServer.Close()
		_ = fromServer.Close()
		_ = output.Close()
	})
	cwd := t.TempDir()
	go func() {
		wire.done <- Main(ctx, Env{Args: args, Stdin: input, Stdout: output, Stderr: io.Discard, Cwd: cwd})
	}()
	return wire
}

func (w *mcpWire) send(t *testing.T, message string) {
	t.Helper()
	if _, err := io.WriteString(w.input, message+"\n"); err != nil {
		t.Fatal(err)
	}
}

func (w *mcpWire) response(t *testing.T) (json.RawMessage, *jsonrpc.Error) {
	t.Helper()
	var response struct {
		Version string          `json:"jsonrpc"`
		Result  json.RawMessage `json:"result"`
		Error   *jsonrpc.Error  `json:"error"`
	}
	if err := w.output.Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Version != "2.0" {
		t.Fatalf("stdout contained a non-JSON-RPC response: %+v", response)
	}
	return response.Result, response.Error
}

func TestMCPWireLegacyVersionsAndInvalidRequests(t *testing.T) {
	for _, version := range []string{"2025-11-25", "2025-06-18", "2099-01-01"} {
		t.Run(version, func(t *testing.T) {
			wire := newMCPWire(t, []string{"mcp"})
			wire.send(t, fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":%q,"capabilities":{},"clientInfo":{"name":"raw-test","version":"1"}}}`, version))
			result, rpcErr := wire.response(t)
			want := version
			if version == "2099-01-01" {
				want = "2025-11-25"
			}
			var init mcp.InitializeResult
			if err := json.Unmarshal(result, &init); err != nil || rpcErr != nil || init.ProtocolVersion != want || init.Capabilities.Tools == nil {
				t.Fatalf("initialize = %s, %v, %v", result, rpcErr, err)
			}
			wire.send(t, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
			for _, request := range []struct {
				body string
				code int64
			}{
				{`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":[1]}`, jsonrpc.CodeInvalidParams},
				{`{"jsonrpc":"2.0","id":3,"method":"tools/list","params":{"cursor":42}}`, jsonrpc.CodeInvalidParams},
				{`{"jsonrpc":"2.0","id":4,"method":"not/a/method","params":{}}`, jsonrpc.CodeMethodNotFound},
			} {
				wire.send(t, request.body)
				_, rpcErr = wire.response(t)
				if rpcErr == nil || rpcErr.Code != request.code {
					t.Fatalf("%s error=%v; want code %d", request.body, rpcErr, request.code)
				}
			}
			wire.send(t, `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":"not-running"}}`)
			wire.send(t, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"spelunk_syntax","arguments":{}}}`)
			result, rpcErr = wire.response(t)
			if rpcErr != nil || !strings.Contains(string(result), "Spelunk query language") {
				t.Fatalf("session failed after invalid requests: %s, %v", result, rpcErr)
			}
			if err := wire.input.Close(); err != nil {
				t.Fatal(err)
			}
			select {
			case code := <-wire.done:
				if code != exitFound {
					t.Errorf("clean EOF exit=%d", code)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("MCP did not stop after stdin closed")
			}
		})
	}
}

func TestMCPWireCurrentProtocolWithoutInitialize(t *testing.T) {
	wire := newMCPWire(t, []string{"mcp", "--in", t.TempDir()})
	wire.send(t, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}`)
	result, rpcErr := wire.response(t)
	var listed mcp.ListToolsResult
	if err := json.Unmarshal(result, &listed); err != nil || rpcErr != nil || len(listed.Tools) != 4 {
		t.Fatalf("current protocol tools/list = %s, %v, %v", result, rpcErr, err)
	}
	for _, tool := range listed.Tools {
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint || tool.Annotations.OpenWorldHint == nil || *tool.Annotations.OpenWorldHint {
			t.Errorf("%s lost local read-only annotations", tool.Name)
		}
	}
}

// repeatedByte supplies a hostile unfinished JSON string without first
// allocating it; the transport must stop reading at its existing frame cap.
type repeatedByte struct{ read int }

func (r *repeatedByte) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	r.read += len(p)
	return len(p), nil
}

func TestMCPWireRejectsOversizedFrame(t *testing.T) {
	var out, errOut bytes.Buffer
	payload := &repeatedByte{}
	const prefix = `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"spelunk_search","arguments":{"query":"`
	input := io.MultiReader(strings.NewReader(prefix), io.LimitReader(payload, mcp.DefaultMaxLineLength+1))
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	code := ServeMCP(ctx, Env{Stdin: input, Stdout: &out, Stderr: &errOut})
	if code != exitError || payload.read > mcp.DefaultMaxLineLength || !strings.Contains(errOut.String(), "exceed") {
		t.Errorf("oversized frame exit=%d read=%d stderr=%q", code, payload.read, errOut.String())
	}
}

func TestMCPWireMalformedJSONStopsCleanly(t *testing.T) {
	var out, errOut bytes.Buffer
	code := ServeMCP(t.Context(), Env{Stdin: strings.NewReader("{invalid}\n"), Stdout: &out, Stderr: &errOut})
	if code != exitError || out.Len() != 0 || !strings.Contains(errOut.String(), "invalid") {
		t.Errorf("malformed JSON exit=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
}
