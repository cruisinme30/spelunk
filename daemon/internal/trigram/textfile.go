package trigram

import (
	"bytes"
	"errors"
	"io"
	"os"
	"unicode/utf16"
	"unicode/utf8"
)

// errNotRegular means a path names something other than a regular file: a
// directory, a symlink, a FIFO, a socket or a device.
var errNotRegular = errors.New("not a regular file")

// errTooBig means a file grew past the size limit after it was listed.
var errTooBig = errors.New("file is over the size limit")

// openRegular opens a regular file for reading. It never blocks on a FIFO
// and never follows a symlink, even when one replaced the file after it was
// listed: either fails with errNotRegular.
func openRegular(path string) (*os.File, os.FileInfo, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|openFlags, 0) //nolint:gosec // G304: paths come from listing the indexed repo
	if err != nil {
		if isSymlinkRefusal(path) {
			return nil, nil, errNotRegular
		}
		return nil, nil, err
	}
	info, err := f.Stat()
	if err != nil {
		closeReadOnly(f)
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		closeReadOnly(f)
		return nil, nil, errNotRegular
	}
	return f, info, nil
}

// isSymlinkRefusal reports whether opening path failed because it is a
// symlink, which openFlags refuses to follow.
func isSymlinkRefusal(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&os.ModeSymlink != 0
}

// readText reads a regular file's text as searches and previews see it
// (see decodeText). maxBytes, when positive, bounds what is read: a file
// that is bigger now than when it was listed fails with errTooBig rather
// than filling memory, and binary content (it may have changed since it
// was listed) fails with errBinary.
func readText(path string, maxBytes int64) ([]byte, os.FileInfo, error) {
	f, info, err := openRegular(path)
	if err != nil {
		return nil, nil, err
	}
	defer closeReadOnly(f)
	var r io.Reader = f
	if maxBytes > 0 {
		r = io.LimitReader(f, maxBytes+1)
	}
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, nil, err
	}
	if maxBytes > 0 && int64(len(raw)) > maxBytes {
		return nil, nil, errTooBig
	}
	if isBinary(raw[:min(len(raw), binarySniffBytes)]) {
		return nil, nil, errBinary
	}
	return decodeText(raw), info, nil
}

// errBinary means a file's content is binary (see isBinary).
var errBinary = errors.New("binary file")

// Byte order marks that decodeText recognizes.
var (
	bomUTF8    = []byte{0xef, 0xbb, 0xbf}
	bomUTF16LE = []byte{0xff, 0xfe}
	bomUTF16BE = []byte{0xfe, 0xff}
	bomUTF32LE = []byte{0xff, 0xfe, 0x00, 0x00}
)

// decodeText turns a file's bytes into the UTF-8 text an editor shows, so
// line numbers and UTF-16 columns agree with VS Code's:
//   - a UTF-8 byte order mark is dropped, as editors hide it;
//   - UTF-16 text with a byte order mark is converted to UTF-8;
//   - a lone "\r" (not followed by "\n") becomes "\n", because VS Code
//     ends a line there too. "\r\n" stays: lines are split on "\n" and the
//     "\r" before it is trimmed;
//   - invalid UTF-8 becomes U+FFFD the way VS Code's decoder does it: one
//     per maximal invalid subsequence, not one per byte, so a column after
//     a truncated character still lands where the editor shows it.
func decodeText(raw []byte) []byte {
	switch {
	case bytes.HasPrefix(raw, bomUTF8):
		raw = raw[len(bomUTF8):]
	case hasUTF16BOM(raw):
		raw = decodeUTF16(raw)
	}
	return loneCRToLF(replaceInvalidUTF8(raw))
}

// replaceInvalidUTF8 replaces each maximal invalid subsequence of text with
// one U+FFFD, as the WHATWG UTF-8 decoder (which VS Code uses) does. Valid
// text is returned as it is.
func replaceInvalidUTF8(text []byte) []byte {
	if utf8.Valid(text) {
		return text
	}
	out := make([]byte, 0, len(text)+len(text)/2)
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRune(text[i:])
		if r != utf8.RuneError || size > 1 { // a real U+FFFD is 3 bytes long
			out = append(out, text[i:i+size]...)
			i += size
			continue
		}
		out = utf8.AppendRune(out, utf8.RuneError)
		i += invalidSubpartLength(text[i:])
	}
	return out
}

// invalidSubpartLength is how many bytes at the start of text, which does
// not start with a valid character, make up one maximal subpart: a lead
// byte and the continuation bytes that could still follow it.
func invalidSubpartLength(text []byte) int {
	need, lo, hi := leadRule(text[0])
	n := 1
	for ; n <= need && n < len(text) && text[n] >= lo && text[n] <= hi; n++ {
		lo, hi = 0x80, 0xbf
	}
	return n
}

// leadRule says how many continuation bytes follow lead in a valid
// character, and the range the first of them must be in (later ones are
// always 0x80-0xBF). A byte that never starts a character needs none.
func leadRule(lead byte) (need int, lo, hi byte) {
	switch {
	case lead >= 0xc2 && lead <= 0xdf:
		return 1, 0x80, 0xbf
	case lead == 0xe0:
		return 2, 0xa0, 0xbf // no overlong forms
	case lead == 0xed:
		return 2, 0x80, 0x9f // no surrogates
	case lead >= 0xe1 && lead <= 0xef:
		return 2, 0x80, 0xbf
	case lead == 0xf0:
		return 3, 0x90, 0xbf // no overlong forms
	case lead == 0xf4:
		return 3, 0x80, 0x8f // nothing past U+10FFFF
	case lead >= 0xf1 && lead <= 0xf3:
		return 3, 0x80, 0xbf
	}
	return 0, 0, 0 // a continuation byte, or a byte that never starts a character
}

// hasUTF16BOM reports whether head starts with a UTF-16 byte order mark
// (and not the UTF-32 one, which starts with the same two bytes).
func hasUTF16BOM(head []byte) bool {
	if bytes.HasPrefix(head, bomUTF32LE) {
		return false
	}
	return bytes.HasPrefix(head, bomUTF16LE) || bytes.HasPrefix(head, bomUTF16BE)
}

// decodeUTF16 converts UTF-16 text that starts with a byte order mark to
// UTF-8, without the mark. An unpaired surrogate or a final odd byte
// becomes U+FFFD.
func decodeUTF16(raw []byte) []byte {
	bigEndian := bytes.HasPrefix(raw, bomUTF16BE)
	raw = raw[2:]
	units := make([]uint16, len(raw)/2)
	for i := range units {
		hi, lo := raw[2*i+1], raw[2*i]
		if bigEndian {
			hi, lo = lo, hi
		}
		units[i] = uint16(hi)<<8 | uint16(lo)
	}
	out := make([]byte, 0, len(raw)+len(raw)/2)
	for _, r := range utf16.Decode(units) {
		out = utf8.AppendRune(out, r)
	}
	if len(raw)%2 == 1 {
		out = utf8.AppendRune(out, utf8.RuneError)
	}
	return out
}

// loneCRToLF replaces each "\r" that doesn't start a "\r\n" with "\n". The
// text keeps its length, so it is changed in place when it has any.
func loneCRToLF(text []byte) []byte {
	at := bytes.IndexByte(text, '\r')
	if at < 0 {
		return text
	}
	for i := at; i < len(text); i++ {
		if text[i] == '\r' && (i+1 == len(text) || text[i+1] != '\n') {
			text[i] = '\n'
		}
	}
	return text
}
