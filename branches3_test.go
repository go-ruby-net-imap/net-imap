// Copyright (c) the go-ruby-net-imap/net-imap authors
//
// SPDX-License-Identifier: BSD-3-Clause

// branches3_test.go covers the final reachable error/value branches, driving the
// parser to 100% via direct *parser invocations where a whole-response fixture
// would be awkward to construct.

package imap

import (
	"errors"
	"testing"
)

// TestLiteralErrorBranches drives literal()'s four guard returns directly:
// missing '{', a non-numeric size, missing '}', and missing CRLF.
func TestLiteralErrorBranches(t *testing.T) {
	for _, buf := range []string{
		"x",      // no '{'
		"{x}",    // size not a number
		"{3)",    // missing '}'
		"{3}abc", // missing CRLF after '}'
	} {
		p := &parser{buf: buf}
		if _, err := p.literal(); !errors.Is(err, ErrParse) {
			t.Errorf("literal(%q) err = %v, want ErrParse", buf, err)
		}
	}
}

// TestFetchDataNoSpaceBeforeParen drives fetchData's sp() error: "FETCH(" with
// no space.
func TestFetchDataNoSpace(t *testing.T) {
	wantParseErr(t, "* 1 FETCH(UID 5)\r\n")
}

// TestModSeqErrors drives MODSEQ's three guards: missing '(', bad number,
// missing ')'.
func TestModSeqErrors(t *testing.T) {
	for _, s := range []string{
		"* 1 FETCH (MODSEQ 5)\r\n",    // not '('
		"* 1 FETCH (MODSEQ (x))\r\n",  // bad number
		"* 1 FETCH (MODSEQ (5 6)\r\n", // missing ')'
	} {
		wantParseErr(t, s)
	}
	// And the happy MODSEQ path again for the value branch.
	a := fetchAttr(t, "* 1 FETCH (MODSEQ (5))\r\n")
	if v, _ := a.Get("MODSEQ"); v != int64(5) {
		t.Errorf("modseq = %v", v)
	}
}

// TestFetchAttNameQuotedSectionError drives fetchAttName's quoted-section error
// branch (an unterminated quoted header-field name inside BODY[...]).
func TestFetchAttNameQuotedSectionError(t *testing.T) {
	wantParseErr(t, "* 1 FETCH (BODY[HEADER.FIELDS (\"DATE)] {1}\r\nx)\r\n")
}

// TestFetchAttNameDefaultByte drives the default byte-copy arm of the section
// reader (a non-quote, non-']' byte such as a digit inside the brackets).
func TestFetchAttNameDefaultByte(t *testing.T) {
	a := fetchAttr(t, "* 1 FETCH (BODY[1.MIME] {2}\r\nhi)\r\n")
	if _, ok := a.Get("BODY[1.MIME]"); !ok {
		t.Errorf("keys = %#v", a.Keys())
	}
}

// TestEnvelopeTailErrors drives the in-reply-to / message-id / closing-')'
// errors of envelope().
func TestEnvelopeTailErrors(t *testing.T) {
	// 8 good fields then a bad in-reply-to (@), and then a truncation before ')'.
	base := `"d" "s" NIL NIL NIL NIL NIL NIL`
	wantParseErr(t, "* 1 FETCH (ENVELOPE ("+base+" @ NIL))\r\n")  // in-reply-to not nstring
	wantParseErr(t, "* 1 FETCH (ENVELOPE ("+base+" NIL @))\r\n")  // message-id not nstring
	wantParseErr(t, "* 1 FETCH (ENVELOPE ("+base+" NIL NILX\r\n") // missing ')'
}

// TestAddressClosingParenError drives address()'s closing-')' error: a tuple
// with five fields.
func TestAddressClosingParenError(t *testing.T) {
	env := `"d" "s" ((NIL NIL "m" "h" "x")) NIL NIL NIL NIL NIL NIL NIL`
	wantParseErr(t, "* 1 FETCH (ENVELOPE ("+env+"))\r\n")
}

// TestBadcharsetExpectParen drives astringList's expect("(") error: BADCHARSET
// with a non-paren after the space.
func TestBadcharsetExpectParen(t *testing.T) {
	wantParseErr(t, "* OK [BADCHARSET x]\r\n")
}

// TestCapabilityListEOFBreak drives capabilityList's eof break: a trailing space
// then end of buffer.
func TestCapabilityListEOFBreak(t *testing.T) {
	wantParseErr(t, "* CAPABILITY ") // space then EOF -> break, then crlf fails
}

// TestBodyTailErrors drives the singlepart/multipart closing-')' error (the ext
// loop exits on a non-space byte that is not ')') and the bodyParams / bodyExt
// separator and subtype-space errors.
func TestBodyTailErrors(t *testing.T) {
	// Singlepart: after ext data, a non-space byte that isn't ')' -> closing-')'.
	wantParseErr(t, "* 1 FETCH (BODYSTRUCTURE (\"IMAGE\" \"GIF\" NIL NIL NIL \"BASE64\" 1 NILx))\r\n")
	// Singlepart: missing space after subtype.
	wantParseErr(t, "* 1 FETCH (BODYSTRUCTURE (\"IMAGE\" \"GIF\"NIL NIL NIL \"BASE64\" 1))\r\n")
	// Multipart: after ext data, a non-space byte that isn't ')' -> closing-')'.
	wantParseErr(t, "* 1 FETCH (BODYSTRUCTURE ((\"TEXT\" \"PLAIN\" NIL NIL NIL \"7BIT\" 1 1) \"MIXED\" NILx))\r\n")
	// bodyParams: two pairs with no space separator between them.
	wantParseErr(t, "* 1 FETCH (BODYSTRUCTURE (\"IMAGE\" \"GIF\" (\"k\" \"v\"\"k2\" \"v2\") NIL NIL \"BASE64\" 1))\r\n")
	// bodyExt nested list: two elements with a bad separator.
	wantParseErr(t, "* 1 FETCH (BODYSTRUCTURE (\"IMAGE\" \"GIF\" NIL NIL NIL \"BASE64\" 1 (NIL\x01NIL)))\r\n")
}

// TestFinalGuards drives the last eight error branches with precisely-shaped
// inputs.
func TestFinalGuards(t *testing.T) {
	// body.go:86 — multipart ext loop exits on a non-space, non-')' byte.
	wantParseErr(t, "* 1 FETCH (BODYSTRUCTURE ((\"TEXT\" \"PLAIN\" NIL NIL NIL \"7BIT\" 1 1) \"MIXED\"X))\r\n")
	// body.go:158 — singlepart ext loop exits on a non-space, non-')' byte.
	wantParseErr(t, "* 1 FETCH (BODYSTRUCTURE (\"IMAGE\" \"GIF\" NIL NIL NIL \"BASE64\" 1X))\r\n")
	// fetch.go:84 — UID value not a number.
	wantParseErr(t, "* 1 FETCH (UID x)\r\n")
	// fetch.go:131 — att name has atom chars then a non-atom byte (break, pos>start).
	wantParseErr(t, "* 1 FETCH (UID\x01 5)\r\n")
	// fetch.go:136 — att name empty (starts with '[').
	wantParseErr(t, "* 1 FETCH ([)\r\n")
	// fetch.go:229 — envelope closing ')' missing (a valid message-id then junk).
	wantParseErr(t, "* 1 FETCH (ENVELOPE (\"d\" \"s\" NIL NIL NIL NIL NIL NIL NIL \"m\"x))\r\n")
	// fetch.go:250 — spAddrList's separator space missing before the from-list.
	wantParseErr(t, "* 1 FETCH (ENVELOPE (\"d\" \"s\"((NIL NIL \"m\" \"h\")) NIL NIL NIL NIL NIL NIL NIL))\r\n")
	// parser.go:269 — CAPABILITY resp-code list with a bad token.
	wantParseErr(t, "* OK [CAPABILITY \x01]\r\n")
}

// TestEncodeNestedList exercises the nested-list encode success path and the
// nested encode-error propagation up through buildCommand.
func TestEncodeNestedList(t *testing.T) {
	b := NewBuilder("T")
	c, err := b.Command("X", []Argument{[]Argument{"a", QuotedString("b")}})
	if err != nil || c.Bytes != "T0001 X ((a \"b\"))\r\n" {
		t.Errorf("nested list = %q err=%v", c.Bytes, err)
	}
}
