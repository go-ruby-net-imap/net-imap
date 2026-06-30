// Copyright (c) the go-ruby-net-imap/net-imap authors
//
// SPDX-License-Identifier: BSD-3-Clause

// branches_test.go drives the deep, per-field error-return branches of the
// recursive ENVELOPE / address / BODYSTRUCTURE productions to 100%. Each case
// truncates or corrupts a valid structure at one specific field so the matching
// `if err != nil { return … }` fires.

package imap

import (
	"errors"
	"strings"
	"testing"
)

func wantParseErr(t *testing.T, s string) {
	t.Helper()
	if _, err := ParseResponse(s); !errors.Is(err, ErrParse) {
		t.Errorf("ParseResponse(%q) err = %v, want ErrParse", s, err)
	}
}

// TestEnvelopeFieldErrors corrupts each of the ten envelope fields in turn. A
// bare unquoted atom where a string/list is expected fails that field's parse.
func TestEnvelopeFieldErrors(t *testing.T) {
	// Field templates: a valid 10-field envelope, then we replace each field's
	// value with the invalid token "@" (not a string, not '(', not NIL).
	bad := "@"
	addr := `((NIL NIL "m" "h"))`
	str := `"x"`
	fields := []string{str, str, addr, addr, addr, addr, addr, str, str, str}
	for i := range fields {
		parts := make([]string, len(fields))
		copy(parts, fields)
		parts[i] = bad
		s := "* 1 FETCH (ENVELOPE (" + strings.Join(parts, " ") + "))\r\n"
		wantParseErr(t, s)
	}
	// Missing opening '(' of the envelope.
	wantParseErr(t, "* 1 FETCH (ENVELOPE NIL)\r\n")
	// Missing closing ')' of the envelope.
	wantParseErr(t, "* 1 FETCH (ENVELOPE ("+strings.Join(fields, " ")+"\r\n")
}

// TestAddressFieldErrors corrupts each address tuple field.
func TestAddressFieldErrors(t *testing.T) {
	// A valid singleton from-list with each of the 4 address fields corrupted.
	good := []string{"NIL", "NIL", `"m"`, `"h"`}
	for i := range good {
		parts := make([]string, len(good))
		copy(parts, good)
		parts[i] = "@"
		env := `"d" "s" ((` + strings.Join(parts, " ") + `)) NIL NIL NIL NIL NIL NIL NIL`
		wantParseErr(t, "* 1 FETCH (ENVELOPE ("+env+"))\r\n")
	}
	// Address tuple missing its closing ')'.
	env := `"d" "s" ((NIL NIL "m" "h" NIL NIL NIL NIL NIL NIL NIL`
	wantParseErr(t, "* 1 FETCH (ENVELOPE ("+env+"))\r\n")
}

// TestBodyStructureFieldErrors corrupts each singlepart body field.
func TestBodyStructureFieldErrors(t *testing.T) {
	// Singlepart non-text body: type subtype param id desc enc size, then ext.
	// Corrupt each leading field with "@".
	good := []string{`"IMAGE"`, `"GIF"`, "NIL", "NIL", "NIL", `"BASE64"`, "123"}
	for i := range good {
		parts := make([]string, len(good))
		copy(parts, good)
		parts[i] = "@"
		// size field corruption: a non-number; for the first two (type/subtype) a
		// non-string; either way the field parse fails.
		s := "* 1 FETCH (BODYSTRUCTURE (" + strings.Join(parts, " ") + "))\r\n"
		wantParseErr(t, s)
	}
	// A TEXT body missing its line count after size.
	wantParseErr(t, "* 1 FETCH (BODYSTRUCTURE (\"TEXT\" \"PLAIN\" NIL NIL NIL \"7BIT\" 5 @))\r\n")
	// Body params: key not a string, then a key with no value.
	wantParseErr(t, "* 1 FETCH (BODYSTRUCTURE (\"IMAGE\" \"GIF\" (@ \"v\") NIL NIL \"BASE64\" 1))\r\n")
	wantParseErr(t, "* 1 FETCH (BODYSTRUCTURE (\"IMAGE\" \"GIF\" (\"k\" @) NIL NIL \"BASE64\" 1))\r\n")
	// Multipart: a nested part that is itself malformed.
	wantParseErr(t, "* 1 FETCH (BODYSTRUCTURE ((@) \"MIXED\"))\r\n")
	// Multipart: subtype not a string.
	wantParseErr(t, "* 1 FETCH (BODYSTRUCTURE ((\"TEXT\" \"PLAIN\" NIL NIL NIL \"7BIT\" 1 1) @))\r\n")
	// Body-ext nested-list element malformed.
	wantParseErr(t, "* 1 FETCH (BODYSTRUCTURE (\"IMAGE\" \"GIF\" NIL NIL NIL \"BASE64\" 1 (\"\\)))\r\n")
}

// TestSectionPartialErrors drives the BODY[...]<partial> error branch.
func TestSectionPartialErrors(t *testing.T) {
	// A section with an unterminated <partial>.
	wantParseErr(t, "* 1 FETCH (BODY[]<0 {1}\r\nx)\r\n")
}

// TestFetchDataMissingParen drives fetchData's missing-'(' and missing-')'/CRLF.
func TestFetchDataMissingPieces(t *testing.T) {
	wantParseErr(t, "* 1 FETCH FLAGS\r\n")           // no '('
	wantParseErr(t, "* 1 FETCH (FLAGS (\\Seen)\r\n") // no ')'
	wantParseErr(t, "* 1 FETCH (FLAGS (\\Seen))")    // no CRLF
}

// TestAstringListSeparator drives the multi-element astringList branch
// (BADCHARSET with two charsets exercises the separator; one with a bad element
// exercises the inner error).
func TestAstringListBranches(t *testing.T) {
	wantParseErr(t, "* OK [BADCHARSET (\"a\" @\r\n") // unterminated / bad second element
}

// TestStatusItemNotAtom drives statusResp's atom-name error path.
func TestStatusItemErrors(t *testing.T) {
	wantParseErr(t, "* STATUS box (MESSAGES 1 X y)\r\n") // item value not a number
	wantParseErr(t, "* STATUS box (\x01 1)\r\n")         // bad item name
}

// TestBareBodyTruncated drives the bodystructure att when the buffer ends right
// after the att name and its space.
func TestBareBodyTruncated(t *testing.T) {
	wantParseErr(t, "* 1 FETCH (BODY ")
	wantParseErr(t, "* 1 FETCH (BODY")
}
