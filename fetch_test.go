// Copyright (c) the go-ruby-net-imap/net-imap authors
//
// SPDX-License-Identifier: BSD-3-Clause

package imap

import (
	"errors"
	"reflect"
	"testing"
)

func fetchAttr(t *testing.T, s string) *OrderedAttr {
	t.Helper()
	r := mustParse(t, s).(*UntaggedResponse)
	fd := r.Data.(*FetchData)
	return fd.Attr
}

func TestFetchScalarAtts(t *testing.T) {
	a := fetchAttr(t, "* 12 FETCH (FLAGS (\\Seen) UID 4827313)\r\n")
	if !reflect.DeepEqual(a.Keys(), []string{"FLAGS", "UID"}) {
		t.Errorf("keys = %#v", a.Keys())
	}
	if fl, _ := a.Get("FLAGS"); !reflect.DeepEqual(fl, []Flag{"Seen"}) {
		t.Errorf("flags = %#v", fl)
	}
	if uid, _ := a.Get("UID"); uid != int64(4827313) {
		t.Errorf("uid = %v", uid)
	}
}

func TestFetchSizeAndInternalDate(t *testing.T) {
	a := fetchAttr(t, "* 23 FETCH (UID 4828442 RFC822.SIZE 44827)\r\n")
	if v, _ := a.Get("RFC822.SIZE"); v != int64(44827) {
		t.Errorf("size = %v", v)
	}
	a2 := fetchAttr(t, "* 14 FETCH (INTERNALDATE \"17-Jul-1996 02:44:25 -0700\")\r\n")
	if v, _ := a2.Get("INTERNALDATE"); v != "17-Jul-1996 02:44:25 -0700" {
		t.Errorf("internaldate = %v", v)
	}
}

func TestFetchModSeq(t *testing.T) {
	a := fetchAttr(t, "* 1 FETCH (MODSEQ (624140003) UID 5)\r\n")
	if v, _ := a.Get("MODSEQ"); v != int64(624140003) {
		t.Errorf("modseq = %v", v)
	}
}

func TestFetchLiteralBody(t *testing.T) {
	a := fetchAttr(t, "* 12 FETCH (BODY[TEXT] {6}\r\nWorld!)\r\n")
	if !reflect.DeepEqual(a.Keys(), []string{"BODY[TEXT]"}) {
		t.Errorf("keys = %#v", a.Keys())
	}
	if v, _ := a.Get("BODY[TEXT]"); v != "World!" {
		t.Errorf("body = %q", v)
	}
}

func TestFetchRFC822Literal(t *testing.T) {
	a := fetchAttr(t, "* 1 FETCH (RFC822 {4}\r\nbody)\r\n")
	if v, _ := a.Get("RFC822"); v != "body" {
		t.Errorf("rfc822 = %q", v)
	}
}

func TestFetchHeaderFieldsSection(t *testing.T) {
	// The quoted header-field names are normalised (quotes stripped).
	a := fetchAttr(t, "* 12 FETCH (BODY[HEADER.FIELDS (\"DATE\" \"FROM\")] {12}\r\nfoo: bar\r\n\r\n)\r\n")
	if !reflect.DeepEqual(a.Keys(), []string{"BODY[HEADER.FIELDS (DATE FROM)]"}) {
		t.Errorf("keys = %#v", a.Keys())
	}
	if v, _ := a.Get("BODY[HEADER.FIELDS (DATE FROM)]"); v != "foo: bar\r\n\r\n" {
		t.Errorf("hdr = %q", v)
	}
}

func TestFetchBodyPartial(t *testing.T) {
	a := fetchAttr(t, "* 1 FETCH (BODY[]<0> {3}\r\nabc)\r\n")
	if !reflect.DeepEqual(a.Keys(), []string{"BODY[]<0>"}) {
		t.Errorf("keys = %#v", a.Keys())
	}
}

func TestFetchNStringNIL(t *testing.T) {
	a := fetchAttr(t, "* 1 FETCH (RFC822.HEADER NIL)\r\n")
	if v, _ := a.Get("RFC822.HEADER"); v != "" {
		t.Errorf("nil header = %q", v)
	}
}

func TestFetchEnvelope(t *testing.T) {
	s := `* 12 FETCH (ENVELOPE ("Wed, 17 Jul 1996 02:23:25 -0700 (PDT)" "IMAP4rev1 WG mtg summary and minutes" (("Terry Gray" NIL "gray" "cac.washington.edu")) (("Terry Gray" NIL "gray" "cac.washington.edu")) (("Terry Gray" NIL "gray" "cac.washington.edu")) ((NIL NIL "imap" "cac.washington.edu")) ((NIL NIL "minutes" "CNRI.Reston.VA.US")("John Klensin" NIL "KLENSIN" "MIT.EDU")) NIL NIL "<B27397-0100000@cac.washington.edu>"))` + "\r\n"
	a := fetchAttr(t, s)
	envAny, _ := a.Get("ENVELOPE")
	env := envAny.(*Envelope)
	if env.Date != "Wed, 17 Jul 1996 02:23:25 -0700 (PDT)" {
		t.Errorf("date = %q", env.Date)
	}
	if env.Subject != "IMAP4rev1 WG mtg summary and minutes" {
		t.Errorf("subject = %q", env.Subject)
	}
	if len(env.From) != 1 || env.From[0].Name != "Terry Gray" || env.From[0].Mailbox != "gray" ||
		env.From[0].Host != "cac.washington.edu" || env.From[0].Route != "" {
		t.Errorf("from = %#v", env.From)
	}
	if len(env.To) != 1 || env.To[0].Name != "" || env.To[0].Mailbox != "imap" {
		t.Errorf("to = %#v", env.To)
	}
	if len(env.Cc) != 2 || env.Cc[1].Name != "John Klensin" || env.Cc[1].Mailbox != "KLENSIN" {
		t.Errorf("cc = %#v", env.Cc)
	}
	if env.Bcc != nil || env.InReplyTo != "" {
		t.Errorf("bcc/inreplyto = %#v / %q", env.Bcc, env.InReplyTo)
	}
	if env.MessageID != "<B27397-0100000@cac.washington.edu>" {
		t.Errorf("message-id = %q", env.MessageID)
	}
}

func TestFetchEnvelopeAllNIL(t *testing.T) {
	a := fetchAttr(t, "* 1 FETCH (ENVELOPE (NIL \"subj\" NIL NIL NIL NIL NIL NIL NIL NIL))\r\n")
	env, _ := a.Get("ENVELOPE")
	e := env.(*Envelope)
	if e.Date != "" || e.Subject != "subj" || e.From != nil || e.To != nil {
		t.Errorf("env = %#v", e)
	}
}

func TestFetchBodyStructureText(t *testing.T) {
	s := "* 1 FETCH (BODYSTRUCTURE (\"TEXT\" \"PLAIN\" (\"CHARSET\" \"US-ASCII\") NIL NIL \"7BIT\" 2279 48 NIL NIL NIL NIL))\r\n"
	a := fetchAttr(t, s)
	bsAny, _ := a.Get("BODYSTRUCTURE")
	bs := bsAny.(*BodyStructure)
	if bs.Multipart || bs.MediaType != "TEXT" || bs.Subtype != "PLAIN" {
		t.Errorf("bs = %#v", bs)
	}
	if v, _ := bs.Params.Get("CHARSET"); v != "US-ASCII" {
		t.Errorf("charset = %v", v)
	}
	if bs.Encoding != "7BIT" || bs.Size != 2279 || bs.Lines != 48 {
		t.Errorf("enc/size/lines = %q/%d/%d", bs.Encoding, bs.Size, bs.Lines)
	}
	// md5 / disposition / language / location all NIL -> 4 extension items.
	if len(bs.Extension) != 4 {
		t.Errorf("extension = %#v", bs.Extension)
	}
}

func TestFetchBodyStructureBasicNoParams(t *testing.T) {
	s := "* 1 FETCH (BODY (\"IMAGE\" \"GIF\" NIL NIL NIL \"BASE64\" 1234))\r\n"
	a := fetchAttr(t, s)
	bs, _ := a.Get("BODY")
	b := bs.(*BodyStructure)
	if b.MediaType != "IMAGE" || b.Subtype != "GIF" || b.Params != nil || b.Size != 1234 || b.Lines != -1 {
		t.Errorf("basic = %#v", b)
	}
}

func TestFetchBodyStructureMultipart(t *testing.T) {
	s := "* 1 FETCH (BODYSTRUCTURE ((\"TEXT\" \"PLAIN\" NIL NIL NIL \"7BIT\" 10 1)(\"TEXT\" \"HTML\" NIL NIL NIL \"7BIT\" 20 2) \"ALTERNATIVE\"))\r\n"
	a := fetchAttr(t, s)
	bs, _ := a.Get("BODYSTRUCTURE")
	b := bs.(*BodyStructure)
	if !b.Multipart || len(b.Parts) != 2 || b.MultipartSubtype != "ALTERNATIVE" {
		t.Errorf("multipart = %#v", b)
	}
	if b.Parts[0].Subtype != "PLAIN" || b.Parts[1].Subtype != "HTML" {
		t.Errorf("parts = %#v", b.Parts)
	}
}

func TestFetchBodyStructureMultipartExt(t *testing.T) {
	// Multipart with extension data (a param list after the subtype).
	s := "* 1 FETCH (BODYSTRUCTURE ((\"TEXT\" \"PLAIN\" NIL NIL NIL \"7BIT\" 10 1) \"MIXED\" (\"BOUNDARY\" \"x\")))\r\n"
	a := fetchAttr(t, s)
	bs, _ := a.Get("BODYSTRUCTURE")
	b := bs.(*BodyStructure)
	if len(b.Extension) != 1 {
		t.Errorf("ext = %#v", b.Extension)
	}
	list, ok := b.Extension[0].([]any)
	if !ok || len(list) != 2 || list[0] != "BOUNDARY" {
		t.Errorf("ext list = %#v", b.Extension[0])
	}
}

func TestFetchBodyStructureExtWithNumberAndNIL(t *testing.T) {
	// Exercise bodyExtItem's number and NIL branches via singlepart md5+lines.
	s := "* 1 FETCH (BODYSTRUCTURE (\"TEXT\" \"PLAIN\" NIL NIL NIL \"7BIT\" 5 1 \"d41d\" NIL 7))\r\n"
	a := fetchAttr(t, s)
	bs, _ := a.Get("BODYSTRUCTURE")
	b := bs.(*BodyStructure)
	if len(b.Extension) != 3 || b.Extension[0] != "d41d" || b.Extension[1] != nil || b.Extension[2] != int64(7) {
		t.Errorf("ext = %#v", b.Extension)
	}
}

func TestFetchErrors(t *testing.T) {
	bad := []string{
		"* 1 FETCH (UID)\r\n",                      // UID missing value
		"* 1 FETCH (FLAGS \\Seen)\r\n",             // flags missing '('
		"* 1 FETCH (ENVELOPE (NIL))\r\n",           // truncated envelope
		"* 1 FETCH (INTERNALDATE foo)\r\n",         // internaldate not quoted
		"* 1 FETCH (BODY[ {2}\r\nhi)\r\n",          // unterminated section
		"* 1 FETCH (UID 5\r\n",                     // missing ')'
		"* 1 FETCH (MODSEQ 5)\r\n",                 // modseq missing '('
		"* 1 FETCH (ENVELOPE (NIL NIL (NIL))\r\n",  // bad address list
		"* 1 FETCH (BODYSTRUCTURE (\"TEXT\"))\r\n", // truncated bodystructure
	}
	for _, s := range bad {
		if _, err := ParseResponse(s); !errors.Is(err, ErrParse) {
			t.Errorf("ParseResponse(%q) err = %v, want ErrParse", s, err)
		}
	}
}
