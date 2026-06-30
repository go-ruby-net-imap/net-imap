// Copyright (c) the go-ruby-net-imap/net-imap authors
//
// SPDX-License-Identifier: BSD-3-Clause

// branches2_test.go finishes off the last reachable error / value branches to
// 100% coverage: trailing-CRLF omissions, literal-astring paths, and the flag /
// capability tokenizer error returns.

package imap

import (
	"reflect"
	"testing"
)

// TestMissingCRLFBranches drives every production's final crlf() error return by
// supplying an otherwise-valid response with a stray byte where CRLF must be.
func TestMissingCRLFBranches(t *testing.T) {
	for _, s := range []string{
		"* 22 EXISTS X\r\n",              // numericData crlf
		"* OK textX",                     // namedUntagged status crlf (no CRLF at all)
		"* FLAGS (\\Seen)X\r\n",          // flagsResp crlf
		"* LIST () \"/\" INBOXX",         // listResp crlf (text after name, no CRLF)
		"* STATUS box (MESSAGES 1)X\r\n", // statusResp crlf
		"* SEARCH 1 2X\r\n",              // searchResp crlf
		"* CAPABILITY A BX",              // capabilityResp crlf
		"a1 OK doneX",                    // tagged crlf
		"+ textX",                        // continuation crlf
		"* 1 FETCH (UID 5)X\r\n",         // fetchData crlf
	} {
		wantParseErr(t, s)
	}
}

// TestListDelimSpaceError drives listResp's space-after-delim and
// space-after-attrs error returns.
func TestListSpacingErrors(t *testing.T) {
	wantParseErr(t, "* LIST ()X\r\n")       // no space after attr list
	wantParseErr(t, "* LIST () \"/\"X\r\n") // no space after delimiter
}

// TestStatusMailboxError drives statusResp's mailbox() error and the
// space-after-mailbox error.
func TestStatusMailboxErrors(t *testing.T) {
	wantParseErr(t, "* STATUS \x01 (MESSAGES 1)\r\n") // bad mailbox name
	wantParseErr(t, "* STATUS boxX\r\n")              // no space / '(' after mailbox
}

// TestFlagTokenErrors drives flag()'s atomToken error (a backslash followed by a
// non-atom) and flagList's missing ')'.
func TestFlagTokenErrors(t *testing.T) {
	wantParseErr(t, "* FLAGS (\\)\r\n")          // '\' then ')' -> empty atom
	wantParseErr(t, "* FLAGS (\\Seen \x01)\r\n") // bad keyword flag
}

// TestCapabilityTokenError drives capabilityList's atomToken error after a space.
func TestCapabilityTokenError(t *testing.T) {
	wantParseErr(t, "* CAPABILITY A \x01\r\n")
}

// TestCapabilityCodeBreakOnBracket drives capabilityList's "]" break inside a
// resp-text-code with a trailing space before the bracket.
func TestCapabilityCodeBreakOnBracket(t *testing.T) {
	// "[CAPABILITY A ]" — a space then ']' breaks the loop.
	r := mustParse(t, "* OK [CAPABILITY A ] text\r\n").(*UntaggedResponse)
	caps := r.Data.(*ResponseText).Code.Data.([]string)
	if !reflect.DeepEqual(caps, []string{"A"}) {
		t.Errorf("caps = %#v", caps)
	}
}

// TestBadCharsetLiteralAndErrors drives astringList's literal astring element,
// its inner astring error, and its closing-')' error, plus the literal '}' and
// CRLF error branches via a literal astring.
func TestBadCharsetLiteralAndErrors(t *testing.T) {
	// A literal astring inside BADCHARSET exercises astring's literal arm.
	r := mustParse(t, "* OK [BADCHARSET ({3}\r\nabc)] text\r\n").(*UntaggedResponse)
	list := r.Data.(*ResponseText).Code.Data.([]string)
	if !reflect.DeepEqual(list, []string{"abc"}) {
		t.Errorf("badcharset literal = %#v", list)
	}
	// astringList: a second element after a space, then a bad inner element.
	wantParseErr(t, "* OK [BADCHARSET (a \x01)] text\r\n")
	// astringList: missing ')' (EOF inside list) via a literal that overruns.
	wantParseErr(t, "* OK [BADCHARSET ({9}\r\nabc)] text\r\n") // literal claims 9, buffer shorter
	// literal '}' error: "{3" with no '}'.
	wantParseErr(t, "* OK [BADCHARSET ({3)] text\r\n")
	// literal CRLF error: "{3}abc" with no CRLF after the brace.
	wantParseErr(t, "* OK [BADCHARSET ({3}abc)] text\r\n")
}

// TestRespCodeNameEmpty drives respCodeName's empty-name error.
func TestRespCodeNameEmpty(t *testing.T) {
	wantParseErr(t, "* OK [ ] text\r\n") // space right after '[' -> empty code name
}
