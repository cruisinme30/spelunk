package trigram

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/testutil"
)

// utf16File encodes text as UTF-16 with a byte order mark.
func utf16File(text string, bigEndian bool) string {
	out := []byte{0xff, 0xfe}
	if bigEndian {
		out = []byte{0xfe, 0xff}
	}
	for _, unit := range utf16.Encode([]rune(text)) {
		hi, lo := byte(unit>>8), byte(unit)
		if bigEndian {
			out = append(out, hi, lo)
		} else {
			out = append(out, lo, hi)
		}
	}
	return string(out)
}

func TestDecodeTextShowsWhatAnEditorShows(t *testing.T) {
	tests := []struct {
		name, raw, want string
	}{
		{"plain_text_is_unchanged", "a\nb\r\nc", "a\nb\r\nc"},
		{"utf8_bom_is_dropped", "\xef\xbb\xbf" + "package main\n", "package main\n"},
		{"utf16le_is_converted", utf16File("café 😀\r\nworld\n", false), "café 😀\r\nworld\n"},
		{"utf16be_is_converted", utf16File("café 😀\nworld\n", true), "café 😀\nworld\n"},
		{"utf16_odd_byte_is_a_replacement", utf16File("ab", false) + "\x63", "ab�"},
		{"lone_cr_ends_a_line", "one\rtwo\r\nthree\r", "one\ntwo\r\nthree\n"},
		// The expected text is what JavaScript's TextDecoder (and so VS Code) shows.
		{"invalid_byte_is_a_replacement", "caf\xe9 \xff\xfe\n", "caf� ��\n"},
		{"truncated_character_is_one_replacement", "\xe2\x82 x\xf0\x9f\x98A", "� x�A"},
		{"surrogate_is_one_replacement_per_byte", "\xed\xa0\x80", "���"},
		{"overlong_forms_are_one_replacement_per_byte", "\xc0\xaf\xe0\x80\x80", "�����"},
		{"past_u10ffff_is_one_replacement_per_byte", "\xf4\x90\x80\x80", "����"},
		{"stray_continuations_and_a_lone_lead", "\x80\x80\xf5\x80\xc3", "�����"},
		{"a_real_replacement_character_is_kept", "a�b", "a�b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(decodeText([]byte(tt.raw))); got != tt.want {
				t.Errorf("decodeText(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestAResultOpensWhereTheEditorShowsItAfterInvalidUTF8(t *testing.T) {
	// VS Code shows each of the truncated characters "\xe2\x82" and
	// "\xf0\x9f" as one U+FFFD, so "needle" is at column 4 there; one U+FFFD
	// per byte put it at column 6.
	root := testutil.WriteTree(t, map[string]string{"broken.txt": "\xe2\x82\xf0\x9f  needle\n"})
	listed, err := ListFiles(context.Background(), root, WalkOptions{})
	if err != nil {
		t.Fatal(err)
	}
	shard, err := Build(context.Background(), root, listed, BuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	repo := Repo{ID: "r", Name: "r", Root: root, Shard: shard}
	items, _ := runItems(t, "needle", defaultSettings, "", repo)
	if len(items) != 1 {
		t.Fatalf("results = %q, want one", summarize(items))
	}
	ref, _ := ParseRef(items[0].Ref)
	if items[0].Text != "��  needle" || ref.Column != 4 {
		t.Errorf("result text %q column %d, want %q at column 4", items[0].Text, ref.Column, "��  needle")
	}
}

func TestBinarySniffingKeepsUTF16Text(t *testing.T) {
	tests := []struct {
		name, head string
		want       bool
	}{
		{"nul_is_binary", "PNG\x00\x00", true},
		{"utf16_with_bom_is_text", utf16File("hello", false), false},
		{"utf32_bom_is_binary", "\xff\xfe\x00\x00h\x00\x00\x00", true},
		{"text_is_text", "hello", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isBinary([]byte(tt.head)); got != tt.want {
				t.Errorf("isBinary(%q) = %v, want %v", tt.head, got, tt.want)
			}
		})
	}
}

func TestBuildReadsFilesAsTheyAreNow(t *testing.T) {
	root := testutil.WriteTree(t, map[string]string{"a.txt": "alpha\n", "grew.txt": "small\n", "now-binary.txt": "text\n"})
	outside := testutil.WriteTree(t, map[string]string{"secret.txt": "secret\n"})
	// Each listed as a small text file, then changed before Build reads it.
	if err := os.WriteFile(filepath.Join(root, "grew.txt"), []byte(strings.Repeat("x", 4096)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "now-binary.txt"), []byte("bin\x00ary"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}
	files := []File{{Path: "a.txt"}, {Path: "grew.txt"}, {Path: "link.txt"}, {Path: "now-binary.txt"}}
	shard, err := Build(context.Background(), root, files, BuildOptions{MaxFileBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, doc := range shard.Docs {
		got = append(got, doc.Path)
	}
	if want := []string{"a.txt"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Build docs = %q, want %q: a file that grew past the limit, became binary or became a symlink is left out", got, want)
	}
}

func TestReadTextReportsWhyAFileIsNotRead(t *testing.T) {
	root := testutil.WriteTree(t, map[string]string{"big.txt": strings.Repeat("x", 100), "bin.dat": "\x00"})
	if err := os.Symlink("big.txt", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		path string
		want error
	}{
		{"big.txt", errTooBig},
		{"bin.dat", errBinary},
		{"link", errNotRegular},
		{".", errNotRegular},
		{"missing", os.ErrNotExist},
	}
	for _, tt := range tests {
		if _, _, err := readText(filepath.Join(root, tt.path), 50); !errors.Is(err, tt.want) {
			t.Errorf("readText(%s) error = %v, want %v", tt.path, err, tt.want)
		}
	}
}

func TestEncodingsAndLineEndsMatchTheEditor(t *testing.T) {
	// Each file's "needle" is on line 2 at UTF-16 column 4 as VS Code shows
	// the file: after a hidden BOM, in UTF-16 text, or after a lone CR.
	files := map[string]string{
		"bom.txt":   "\xef\xbb\xbf" + "first\n😀  needle\n",
		"utf16.txt": utf16File("first\r\n😀  needle\r\n", false),
		"mac.txt":   "first\r😀  needle\r",
	}
	root := testutil.WriteTree(t, files)
	listed, err := ListFiles(context.Background(), root, WalkOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 3 {
		t.Fatalf("ListFiles = %q, want all three files (UTF-16 is text)", paths(listed))
	}
	shard, err := Build(context.Background(), root, listed, BuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	repo := Repo{ID: "r", Name: "r", Root: root, Shard: shard}
	items, _ := runItems(t, "needle", defaultSettings, "", repo)
	if len(items) != 3 {
		t.Fatalf("results = %q, want one per file", summarize(items))
	}
	for _, item := range items {
		ref, _ := ParseRef(item.Ref)
		if item.Line != 2 || ref.Column != 4 || item.Text != "😀  needle" || !reflect.DeepEqual(item.Hits, []protocol.Hit{{Start: 4, End: 10}}) {
			t.Errorf("%s: line %d column %d text %q hits %+v; want line 2, column 4, the line without BOM or CR, hit 4-10", item.Path, item.Line, ref.Column, item.Text, item.Hits)
		}
		preview, err := Preview(&repo, ref, nil, 1)
		if err != nil || preview.Lines[0] != "first" || preview.Lines[1] != "😀  needle" {
			t.Errorf("%s: preview lines = %q, %v; want the decoded lines", item.Path, preview.Lines, err)
		}
	}
	anchored, _ := runItems(t, "/^first$/", defaultSettings, "", repo)
	if len(anchored) != 3 {
		t.Errorf("/^first$/ = %q, want line 1 of every file: the BOM and the lone CR are not part of the line", summarize(anchored))
	}
}
