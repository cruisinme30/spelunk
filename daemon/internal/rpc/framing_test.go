package rpc

import (
	"bufio"
	"errors"
	"io"
	"runtime"
	"strings"
	"testing"
)

func TestReadMessage(t *testing.T) {
	tests := []struct {
		name     string
		wire     string
		wantBody string
		wantErr  error // matched with errors.Is; nil with wantFail means any error
		wantFail bool
	}{
		{name: "plain_message", wire: "Content-Length: 2\r\n\r\n{}", wantBody: "{}"},
		{name: "header_names_ignore_case", wire: "content-length: 2\r\n\r\n{}", wantBody: "{}"},
		{name: "other_headers_are_skipped", wire: "Content-Type: application/vscode-jsonrpc; charset=utf-8\r\nContent-Length: 2\r\n\r\n{}", wantBody: "{}"},
		{name: "bare_newlines_are_accepted", wire: "Content-Length: 2\n\n{}", wantBody: "{}"},
		{name: "repeated_equal_lengths_are_accepted", wire: "Content-Length: 2\r\nContent-Length: 2\r\n\r\n{}", wantBody: "{}"},
		{name: "empty_input_is_a_clean_end", wire: "", wantErr: io.EOF},
		{name: "input_ending_inside_the_header_is_unexpected", wire: "Content-Len", wantErr: io.ErrUnexpectedEOF},
		{name: "input_ending_before_the_blank_line_is_unexpected", wire: "Content-Length: 2\r\n", wantErr: io.ErrUnexpectedEOF},
		{name: "header_without_body_is_unexpected_eof", wire: "Content-Length: 10\r\n\r\n", wantErr: io.ErrUnexpectedEOF},
		{name: "truncated_body_is_unexpected_eof", wire: "Content-Length: 100\r\n\r\n{\"jsonrpc\"", wantErr: io.ErrUnexpectedEOF},
		{name: "huge_length_is_refused_unallocated", wire: "Content-Length: 10000000000\r\n\r\n{}", wantErr: ErrMessageTooLarge},
		{name: "length_over_int64_is_bad", wire: "Content-Length: 99999999999999999999999\r\n\r\n", wantFail: true},
		{name: "negative_length_is_bad", wire: "Content-Length: -5\r\n\r\n", wantFail: true},
		{name: "non_numeric_length_is_bad", wire: "Content-Length: ten\r\n\r\n", wantFail: true},
		{name: "conflicting_lengths_are_bad", wire: "Content-Length: 2\r\nContent-Length: 3\r\n\r\n{} ", wantFail: true},
		{name: "missing_length_is_bad", wire: "Foo: bar\r\n\r\n{}", wantFail: true},
		{name: "json_without_header_is_bad", wire: "{\"jsonrpc\":\"2.0\"}\r\n\r\n", wantFail: true},
		{name: "endless_header_line_is_refused", wire: "X-Junk: " + strings.Repeat("a", 1<<20), wantErr: ErrMessageTooLarge},
		{name: "endless_header_lines_are_refused", wire: strings.Repeat("X: y\r\n", 1<<16), wantErr: ErrMessageTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, err := ReadMessage(bufio.NewReaderSize(strings.NewReader(tt.wire), readBufferSize))
			switch {
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("ReadMessage(%.40q) = %q, %v; want error %v", tt.wire, body, err, tt.wantErr)
				}
			case tt.wantFail:
				if err == nil || errors.Is(err, io.EOF) {
					t.Fatalf("ReadMessage(%.40q) = %q, %v; want a framing error", tt.wire, body, err)
				}
			default:
				if err != nil || string(body) != tt.wantBody {
					t.Fatalf("ReadMessage(%.40q) = %q, %v; want %q, nil", tt.wire, body, err, tt.wantBody)
				}
			}
		})
	}
}

func TestReadMessageDoesNotAllocateADeclaredLengthItRefuses(t *testing.T) {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, err := ReadMessage(bufio.NewReader(strings.NewReader("Content-Length: 10000000000\r\n\r\n")))
	runtime.ReadMemStats(&after)
	if !errors.Is(err, ErrMessageTooLarge) {
		t.Fatalf("ReadMessage(10 GB Content-Length) error = %v, want ErrMessageTooLarge", err)
	}
	if grew := after.TotalAlloc - before.TotalAlloc; grew > 1<<20 {
		t.Fatalf("ReadMessage(10 GB Content-Length) allocated %d bytes, want under 1 MB", grew)
	}

	maxBodySize = 1 << 10
	t.Cleanup(func() { maxBodySize = 64 << 20 })
	_, err = ReadMessage(bufio.NewReader(strings.NewReader("Content-Length: 1025\r\n\r\n")))
	if !errors.Is(err, ErrMessageTooLarge) {
		t.Fatalf("ReadMessage(1025 bytes over a 1024 limit) error = %v, want ErrMessageTooLarge", err)
	}
}
