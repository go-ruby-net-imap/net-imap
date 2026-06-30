// Copyright (c) the go-ruby-net-imap/net-imap authors
//
// SPDX-License-Identifier: BSD-3-Clause

// This file holds the deterministic, ruby-free tests that drive the remaining
// error and edge branches to 100% coverage (the qemu cross-arch and Windows
// lanes have no ruby, so the suite must hold the gate without the MRI oracle).

package imap

import (
	"errors"
	"testing"
)

// TestIsResponseMarkers exercises the interface marker methods.
func TestIsResponseMarkers(t *testing.T) {
	var resps = []Response{
		&TaggedResponse{}, &UntaggedResponse{}, &ContinuationRequest{},
	}
	for _, r := range resps {
		r.isResponse() // no-op; presence is the point
	}
}

// TestParseErrorBranches drives the per-production error returns that the happy
// path does not reach.
func TestParseErrorBranches(t *testing.T) {
	bad := []string{
		// continuationRequest: respText with a bad code, and a missing CRLF.
		"+ [UNSEEN x]\r\n",
		"+ text",
		// tagged: missing tag atom, missing space, missing name, missing space,
		// bad respText, missing CRLF.
		" OK x\r\n",
		"a1\r\n",
		"a1 \r\n",
		"a1 OK\r\n",
		"a1 OK [UNSEEN x] t\r\n",
		// untagged: missing space, missing name.
		"*x\r\n",
		"* \x01\r\n",
		// numericData: bad number is impossible (digit-led), but missing space
		// after number, missing name, and a FETCH with no '(' all error.
		"* 5\r\n",
		"* 5 \x01\r\n",
		"* 5 FETCH x\r\n",
		"* 5 FETCH \r\n",
		// namedUntagged status responses missing the space.
		"* OK\r\n",
		// FLAGS missing space / bad list.
		"* FLAGS\r\n",
		"* FLAGS (x",
		// LIST missing pieces.
		"* LIST\r\n",
		"* LIST ()\r\n",
		"* LIST () /\r\n",
		"* LIST () \"/\" \r\n",
		"* LIST () \"/\" INBOX",
		// STATUS missing pieces.
		"* STATUS\r\n",
		"* STATUS box\r\n",
		"* STATUS box x\r\n",
		"* STATUS box (MESSAGES 1",
		"* STATUS box (MESSAGES 1)",
		// SEARCH bad number after space.
		"* SEARCH x\r\n",
		"* SEARCH 1",
		// CAPABILITY missing CRLF.
		"* CAPABILITY IMAP4rev1",
		// resp-text-code variants.
		"* OK [UIDVALIDITY]\r\n",
		"* OK [PERMANENTFLAGS]\r\n",
		"* OK [PERMANENTFLAGS x]\r\n",
		"* OK [BADCHARSET (x]\r\n",
		"* OK [UNSEEN 1\r\n", // missing ']'
	}
	for _, s := range bad {
		if _, err := ParseResponse(s); !errors.Is(err, ErrParse) {
			t.Errorf("ParseResponse(%q) err = %v, want ErrParse", s, err)
		}
	}
}

// TestQuotedEdges drives the quoted-string escape and termination branches.
func TestQuotedEdges(t *testing.T) {
	// Escaped quote inside a quoted mailbox name.
	r := mustParse(t, "* LIST () \"/\" \"a\\\"b\"\r\n").(*UntaggedResponse)
	if r.Data.(*MailboxList).Name != "a\"b" {
		t.Errorf("escaped name = %q", r.Data.(*MailboxList).Name)
	}
	// Unterminated quoted string, and a dangling escape at EOF.
	for _, s := range []string{
		"* LIST () \"/\" \"abc\r\n", // never closes (CRLF inside is consumed as bytes)
		"* LIST () \"/\" \"a\\",     // dangling escape, no closing quote
	} {
		if _, err := ParseResponse(s); !errors.Is(err, ErrParse) {
			t.Errorf("ParseResponse(%q) err = %v", s, err)
		}
	}
}

// TestNumberOverflow drives the strconv overflow branch in number().
func TestNumberOverflow(t *testing.T) {
	if _, err := ParseResponse("* 99999999999999999999 EXISTS\r\n"); !errors.Is(err, ErrParse) {
		t.Errorf("overflow err = %v", err)
	}
}

// TestNILVariants drives matchNIL's casing and boundary branches.
func TestNILVariants(t *testing.T) {
	// lowercase nil delimiter.
	r := mustParse(t, "* LIST () nil INBOX\r\n").(*UntaggedResponse)
	if r.Data.(*MailboxList).Delim != "" {
		t.Errorf("nil delim = %q", r.Data.(*MailboxList).Delim)
	}
	// "NILX" must NOT be matched as NIL by matchNIL (the following byte is an atom
	// char). Since the delimiter grammar is nstring (quoted | NIL) only, "NILX"
	// as a bare delimiter then fails the string production — proving matchNIL
	// correctly declined it rather than swallowing "NIL".
	if _, err := ParseResponse("* LIST () NILX INBOX\r\n"); !errors.Is(err, ErrParse) {
		t.Errorf("NILX delim err = %v, want ErrParse", err)
	}
}

// TestAddressErrors drives the address-tuple error branches in address().
func TestAddressErrors(t *testing.T) {
	bad := []string{
		"* 1 FETCH (ENVELOPE (NIL NIL (() NIL NIL NIL NIL NIL NIL NIL NIL))\r\n",       // addr tuple missing '('
		"* 1 FETCH (ENVELOPE (NIL NIL ((NIL) NIL NIL NIL NIL NIL NIL NIL NIL))\r\n",    // addr tuple truncated
		"* 1 FETCH (ENVELOPE (NIL NIL ((NIL NIL NIL NIL NIL NIL NIL NIL NIL NIL))\r\n", // addr tuple no ')'
		"* 1 FETCH (ENVELOPE (NIL NIL (\r\n",                                           // open addr list never closes
	}
	for _, s := range bad {
		if _, err := ParseResponse(s); !errors.Is(err, ErrParse) {
			t.Errorf("ParseResponse(%q) err = %v", s, err)
		}
	}
}

// TestBodyStructureErrors drives the bodystructure / bodyparams / bodyExtItem
// error branches.
func TestBodyStructureErrors(t *testing.T) {
	bad := []string{
		"* 1 FETCH (BODYSTRUCTURE x)\r\n",                                                 // not '('
		"* 1 FETCH (BODYSTRUCTURE (\"TEXT\" \"PLAIN\" (\"K\") NIL NIL \"7BIT\" 5 1))\r\n", // param key without value
		"* 1 FETCH (BODYSTRUCTURE (\"TEXT\" \"PLAIN\" (x x) NIL NIL \"7BIT\" 5 1))\r\n",   // param key not a string
		"* 1 FETCH (BODYSTRUCTURE ((\"TEXT\" \"PLAIN\" NIL NIL NIL \"7BIT\" 5 1)))\r\n",   // multipart missing subtype
		"* 1 FETCH (BODYSTRUCTURE (\"TEXT\" \"PLAIN\" NIL NIL NIL \"7BIT\" 5 1 (x)\r\n",   // ext list never closes
		"* 1 FETCH (BODYSTRUCTURE (\"TEXT\" \"PLAIN\" NIL NIL NIL \"7BIT\" 5))\r\n",       // TEXT missing line count
		"* 1 FETCH (BODYSTRUCTURE (\"TEXT\" \"PLAIN\" NIL NIL NIL \"7BIT\"))\r\n",         // missing size
		"* 1 FETCH (BODYSTRUCTURE (\"IMAGE\" \"GIF\" (\"K\" \"V\" )))\r\n",                // param missing value before ')'
	}
	for _, s := range bad {
		if _, err := ParseResponse(s); !errors.Is(err, ErrParse) {
			t.Errorf("ParseResponse(%q) err = %v", s, err)
		}
	}
}

// TestBodyParamsList drives a multi-pair param map (the params loop's separator).
func TestBodyParamsList(t *testing.T) {
	a := fetchAttr(t, "* 1 FETCH (BODY (\"TEXT\" \"PLAIN\" (\"CHARSET\" \"UTF-8\" \"FORMAT\" \"flowed\") NIL NIL \"7BIT\" 5 1))\r\n")
	bs, _ := a.Get("BODY")
	b := bs.(*BodyStructure)
	if b.Params.Len() != 2 {
		t.Errorf("params = %#v", b.Params.Keys())
	}
	if v, _ := b.Params.Get("FORMAT"); v != "flowed" {
		t.Errorf("format = %v", v)
	}
}

// TestMsgAttBodyVsBodyStructure drives the BODY-with-paren (bodystructure) vs
// BODY[...] (nstring) disambiguation and the multi-att separator.
func TestMsgAttSeparator(t *testing.T) {
	a := fetchAttr(t, "* 1 FETCH (UID 5 BODY (\"TEXT\" \"PLAIN\" NIL NIL NIL \"7BIT\" 5 1) FLAGS (\\Seen))\r\n")
	if a.Len() != 3 {
		t.Errorf("atts = %#v", a.Keys())
	}
	if _, ok := a.Get("BODY"); !ok {
		t.Errorf("BODY missing: %#v", a.Keys())
	}
}

// TestSequenceCoalesceEdges drives the coalesce/hiKey branches not hit above.
func TestSequenceCoalesceEdges(t *testing.T) {
	cases := []struct {
		items []any
		want  string
	}{
		{[]any{SeqRange{1, 3}, SeqRange{2, 10}}, "1:10"},      // r extends last
		{[]any{SeqRange{1, 10}, SeqRange{2, 3}}, "1:10"},      // r inside last (hiKey keeps last)
		{[]any{SeqRange{1, Star}, 3}, "1:*"},                  // last is star, r inside
		{[]any{SeqRange{1, 3}, SeqRange{5, Star}}, "1:3,5:*"}, // disjoint, second open
		{[]any{Star, 5}, "5,*"},                               // a star and a number, disjoint
		{[]any{SeqRange{1, 2}, SeqRange{4, Star}}, "1:2,4:*"},
	}
	for _, c := range cases {
		ss, err := NewSequenceSet(c.items...)
		if err != nil {
			t.Fatalf("%v: %v", c.items, err)
		}
		if got := ss.String(); got != c.want {
			t.Errorf("SequenceSet(%v) = %q, want %q", c.items, got, c.want)
		}
	}
}

// TestCapabilityCodeTrailing drives capabilityList's "]" / EOF break inside a
// resp-text-code.
func TestCapabilityCodeTrailing(t *testing.T) {
	r := mustParse(t, "* OK [CAPABILITY IMAP4rev1] ready\r\n").(*UntaggedResponse)
	caps := r.Data.(*ResponseText).Code.Data.([]string)
	if len(caps) != 1 || caps[0] != "IMAP4REV1" {
		t.Errorf("caps = %#v", caps)
	}
}

// TestEmptyBuffer drives ParseResponse's tagged path on an empty string.
func TestEmptyBuffer(t *testing.T) {
	if _, err := ParseResponse(""); !errors.Is(err, ErrParse) {
		t.Errorf("empty err = %v", err)
	}
}
