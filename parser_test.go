// Copyright (c) the go-ruby-net-imap/net-imap authors
//
// SPDX-License-Identifier: BSD-3-Clause

package imap

import (
	"errors"
	"reflect"
	"testing"
)

func mustParse(t *testing.T, s string) Response {
	t.Helper()
	r, err := ParseResponse(s)
	if err != nil {
		t.Fatalf("ParseResponse(%q): %v", s, err)
	}
	return r
}

func TestParseSimpleStatus(t *testing.T) {
	r := mustParse(t, "a001 OK LOGIN completed\r\n").(*TaggedResponse)
	if r.Tag != "a001" || r.Name != "OK" || r.Data.Text != "LOGIN completed" || r.Data.Code != nil {
		t.Errorf("tagged = %#v / %#v", r, r.Data)
	}
	if r.RawData != "a001 OK LOGIN completed\r\n" {
		t.Errorf("raw = %q", r.RawData)
	}
}

func TestParseTaggedWithCode(t *testing.T) {
	r := mustParse(t, "a002 NO [ALERT] System down\r\n").(*TaggedResponse)
	if r.Name != "NO" || r.Data.Code == nil || r.Data.Code.Name != "ALERT" ||
		r.Data.Code.Data != nil || r.Data.Text != "System down" {
		t.Errorf("= %#v / %#v", r, r.Data.Code)
	}
	r2 := mustParse(t, "a003 BAD Command unknown\r\n").(*TaggedResponse)
	if r2.Name != "BAD" {
		t.Errorf("name = %q", r2.Name)
	}
}

func TestParseContinuation(t *testing.T) {
	r := mustParse(t, "+ Ready for more\r\n").(*ContinuationRequest)
	if r.Data.Text != "Ready for more" {
		t.Errorf("text = %q", r.Data.Text)
	}
	r2 := mustParse(t, "+ \r\n").(*ContinuationRequest)
	if r2.Data.Text != "" {
		t.Errorf("empty cont text = %q", r2.Data.Text)
	}
	// "+\r\n" with no space.
	r3 := mustParse(t, "+\r\n").(*ContinuationRequest)
	if r3.Data.Text != "" {
		t.Errorf("nospace cont = %q", r3.Data.Text)
	}
}

func TestParseNumericData(t *testing.T) {
	for _, c := range []struct {
		in   string
		name string
		n    int64
	}{
		{"* 22 EXISTS\r\n", "EXISTS", 22},
		{"* 0 RECENT\r\n", "RECENT", 0},
		{"* 9 EXPUNGE\r\n", "EXPUNGE", 9},
	} {
		r := mustParse(t, c.in).(*UntaggedResponse)
		if r.Name != c.name || r.Data.(int64) != c.n {
			t.Errorf("%q -> %#v", c.in, r)
		}
	}
}

func TestParseRespTextCodes(t *testing.T) {
	cases := []struct {
		in   string
		name string
		data any
	}{
		{"* OK [UNSEEN 12] msg\r\n", "UNSEEN", int64(12)},
		{"* OK [UIDVALIDITY 3857529045] x\r\n", "UIDVALIDITY", int64(3857529045)},
		{"* OK [UIDNEXT 4392] x\r\n", "UIDNEXT", int64(4392)},
		{"* OK [HIGHESTMODSEQ 715194045007] x\r\n", "HIGHESTMODSEQ", int64(715194045007)},
		{"* OK [READ-WRITE] x\r\n", "READ-WRITE", nil},
		{"* OK [READ-ONLY] x\r\n", "READ-ONLY", nil},
		{"* OK [TRYCREATE] x\r\n", "TRYCREATE", nil},
	}
	for _, c := range cases {
		r := mustParse(t, c.in).(*UntaggedResponse)
		rt := r.Data.(*ResponseText)
		if rt.Code.Name != c.name || !reflect.DeepEqual(rt.Code.Data, c.data) {
			t.Errorf("%q -> code %#v", c.in, rt.Code)
		}
	}
}

func TestParseRespTextCodeLists(t *testing.T) {
	r := mustParse(t, "* OK [PERMANENTFLAGS (\\Deleted \\Seen \\*)] Limited\r\n").(*UntaggedResponse)
	flags := r.Data.(*ResponseText).Code.Data.([]Flag)
	if !reflect.DeepEqual(flags, []Flag{"Deleted", "Seen", "*"}) {
		t.Errorf("permanentflags = %#v", flags)
	}
	r2 := mustParse(t, "* OK [CAPABILITY IMAP4rev1 STARTTLS] ready\r\n").(*UntaggedResponse)
	caps := r2.Data.(*ResponseText).Code.Data.([]string)
	if !reflect.DeepEqual(caps, []string{"IMAP4REV1", "STARTTLS"}) {
		t.Errorf("cap code = %#v", caps)
	}
	r3 := mustParse(t, "* OK [BADCHARSET (US-ASCII UTF-8)] text\r\n").(*UntaggedResponse)
	bc := r3.Data.(*ResponseText).Code.Data.([]string)
	if !reflect.DeepEqual(bc, []string{"US-ASCII", "UTF-8"}) {
		t.Errorf("badcharset = %#v", bc)
	}
	// BADCHARSET with no list.
	r4 := mustParse(t, "* OK [BADCHARSET] text\r\n").(*UntaggedResponse)
	if r4.Data.(*ResponseText).Code.Data != nil {
		t.Errorf("bare badcharset data = %#v", r4.Data.(*ResponseText).Code.Data)
	}
}

func TestParseGenericCode(t *testing.T) {
	r := mustParse(t, "* NO [REFERRAL imap://example.com/] go away\r\n").(*UntaggedResponse)
	code := r.Data.(*ResponseText).Code
	if code.Name != "REFERRAL" || code.Data.(string) != "imap://example.com/" {
		t.Errorf("generic code = %#v", code)
	}
}

func TestParseStatusResponses(t *testing.T) {
	r := mustParse(t, "* PREAUTH IMAP4rev1 server ready\r\n").(*UntaggedResponse)
	if r.Name != "PREAUTH" || r.Data.(*ResponseText).Text != "IMAP4rev1 server ready" {
		t.Errorf("preauth = %#v", r)
	}
	r2 := mustParse(t, "* BYE Autologout\r\n").(*UntaggedResponse)
	if r2.Name != "BYE" {
		t.Errorf("bye = %#v", r2)
	}
}

func TestParseFlags(t *testing.T) {
	r := mustParse(t, "* FLAGS (\\Answered \\Flagged \\Deleted \\Seen \\Draft)\r\n").(*UntaggedResponse)
	want := []Flag{"Answered", "Flagged", "Deleted", "Seen", "Draft"}
	if !reflect.DeepEqual(r.Data.([]Flag), want) {
		t.Errorf("flags = %#v", r.Data)
	}
	// Empty + keyword flag.
	r2 := mustParse(t, "* FLAGS (\\Seen $Forwarded)\r\n").(*UntaggedResponse)
	if !reflect.DeepEqual(r2.Data.([]Flag), []Flag{"Seen", "$Forwarded"}) {
		t.Errorf("kw flags = %#v", r2.Data)
	}
	r3 := mustParse(t, "* FLAGS ()\r\n").(*UntaggedResponse)
	if len(r3.Data.([]Flag)) != 0 {
		t.Errorf("empty flags = %#v", r3.Data)
	}
}

func TestParseList(t *testing.T) {
	r := mustParse(t, "* LIST (\\Noselect) \"/\" ~/Mail/foo\r\n").(*UntaggedResponse)
	ml := r.Data.(*MailboxList)
	if !reflect.DeepEqual(ml.Attr, []Flag{"Noselect"}) || ml.Delim != "/" || ml.Name != "~/Mail/foo" {
		t.Errorf("list = %#v", ml)
	}
	r2 := mustParse(t, "* LIST () \"/\" INBOX\r\n").(*UntaggedResponse)
	ml2 := r2.Data.(*MailboxList)
	if len(ml2.Attr) != 0 || ml2.Name != "INBOX" {
		t.Errorf("list2 = %#v", ml2)
	}
	// NIL delimiter.
	r3 := mustParse(t, "* LIST () NIL INBOX\r\n").(*UntaggedResponse)
	if r3.Data.(*MailboxList).Delim != "" {
		t.Errorf("nil delim = %q", r3.Data.(*MailboxList).Delim)
	}
	// LSUB + quoted mailbox name.
	r4 := mustParse(t, "* LSUB () \".\" \"#news.comp\"\r\n").(*UntaggedResponse)
	if r4.Name != "LSUB" || r4.Data.(*MailboxList).Name != "#news.comp" {
		t.Errorf("lsub = %#v", r4)
	}
}

func TestParseStatus(t *testing.T) {
	r := mustParse(t, "* STATUS blurdybloop (MESSAGES 231 UIDNEXT 44292)\r\n").(*UntaggedResponse)
	sd := r.Data.(*StatusData)
	if sd.Mailbox != "blurdybloop" {
		t.Errorf("mbox = %q", sd.Mailbox)
	}
	if !reflect.DeepEqual(sd.Attr.Keys(), []string{"MESSAGES", "UIDNEXT"}) {
		t.Errorf("keys = %#v", sd.Attr.Keys())
	}
	if v, _ := sd.Attr.Get("MESSAGES"); v != int64(231) {
		t.Errorf("MESSAGES = %v", v)
	}
	if _, ok := sd.Attr.Get("MISSING"); ok || sd.Attr.Len() != 2 {
		t.Errorf("attr len/missing wrong")
	}
}

func TestParseSearch(t *testing.T) {
	r := mustParse(t, "* SEARCH 2 84 882\r\n").(*UntaggedResponse)
	if !reflect.DeepEqual(r.Data.([]int64), []int64{2, 84, 882}) {
		t.Errorf("search = %#v", r.Data)
	}
	r2 := mustParse(t, "* SEARCH\r\n").(*UntaggedResponse)
	if len(r2.Data.([]int64)) != 0 {
		t.Errorf("empty search = %#v", r2.Data)
	}
}

func TestParseCapability(t *testing.T) {
	r := mustParse(t, "* CAPABILITY IMAP4rev1 STARTTLS AUTH=GSSAPI\r\n").(*UntaggedResponse)
	want := []string{"IMAP4REV1", "STARTTLS", "AUTH=GSSAPI"}
	if !reflect.DeepEqual(r.Data.([]string), want) {
		t.Errorf("cap = %#v", r.Data)
	}
	// Trailing space then EOL — no extra empty token.
	r2 := mustParse(t, "* CAPABILITY IMAP4rev1\r\n").(*UntaggedResponse)
	if !reflect.DeepEqual(r2.Data.([]string), []string{"IMAP4REV1"}) {
		t.Errorf("cap2 = %#v", r2.Data)
	}
}

func TestParseOrderedAttrReplace(t *testing.T) {
	// Exercise the OrderedAttr / OrderedInts replace path (repeated key keeps pos).
	oa := newOrderedAttr()
	oa.Set("A", 1)
	oa.Set("B", 2)
	oa.Set("A", 3)
	if !reflect.DeepEqual(oa.Keys(), []string{"A", "B"}) {
		t.Errorf("keys = %#v", oa.Keys())
	}
	if v, _ := oa.Get("A"); v != 3 {
		t.Errorf("A = %v", v)
	}
	oi := newOrderedInts()
	oi.Set("X", 1)
	oi.Set("X", 9)
	if v, _ := oi.Get("X"); v != 9 || oi.Len() != 1 {
		t.Errorf("oi = %v len %d", v, oi.Len())
	}
}

func TestParseErrors(t *testing.T) {
	bad := []string{
		"",                               // empty -> tagged path, no atom
		"* \r\n",                         // missing name
		"* 5 BOGUS\r\n",                  // unknown numeric response
		"a1 FOO bar\r\n",                 // bad tagged name
		"* WEIRD stuff\r\n",              // unsupported untagged
		"* 5 EXISTS",                     // missing CRLF
		"a1 OK text",                     // tagged missing CRLF
		"* STATUS box (MESSAGES)\r\n",    // status item without value
		"* FLAGS \\Seen)\r\n",            // flag list missing '('
		"* LIST (\\Noselect \"/\" x\r\n", // unterminated flag list
		"* OK [UNSEEN x] t\r\n",          // non-numeric code arg
		"+ ",                             // continuation missing CRLF
		"* OK [\r\n",                     // empty code name
		"* CAPABILITY \x01\r\n",          // bad atom char after space in cap
	}
	for _, s := range bad {
		if _, err := ParseResponse(s); !errors.Is(err, ErrParse) {
			t.Errorf("ParseResponse(%q) err = %v, want ErrParse", s, err)
		}
	}
}

func TestParseLiteralOutOfRange(t *testing.T) {
	// A literal claiming more bytes than the buffer holds.
	_, err := ParseResponse("* 1 FETCH (RFC822 {99}\r\nshort)\r\n")
	if !errors.Is(err, ErrParse) {
		t.Errorf("literal overrun err = %v", err)
	}
}
