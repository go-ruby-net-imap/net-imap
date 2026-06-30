// Copyright (c) the go-ruby-net-imap/net-imap authors
//
// SPDX-License-Identifier: BSD-3-Clause

package imap

import (
	"errors"
	"testing"
	"time"
)

// TestEncodeUTF7Golden checks modified-UTF-7 encoding against MRI-verified
// golden vectors (Net::IMAP.encode_utf7).
func TestEncodeUTF7Golden(t *testing.T) {
	cases := []struct{ in, want string }{
		{"INBOX", "INBOX"},
		{"", ""},
		{"foo & bar", "foo &- bar"},
		{"&", "&-"},
		{"&&", "&-&-"},
		{"~peter/mail/台北/日本語", "~peter/mail/&U,BTFw-/&ZeVnLIqe-"},
		{"Drafts", "Drafts"},
		{"café", "caf&AOk-"},
		{"日本語", "&ZeVnLIqe-"},
		{"a/b", "a/b"},
	}
	for _, c := range cases {
		if got := EncodeUTF7(c.in); got != c.want {
			t.Errorf("EncodeUTF7(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestDecodeUTF7Golden checks modified-UTF-7 decoding against golden vectors,
// and round-trips them back through EncodeUTF7.
func TestDecodeUTF7Golden(t *testing.T) {
	cases := []struct{ in, want string }{
		{"INBOX", "INBOX"},
		{"", ""},
		{"foo &- bar", "foo & bar"},
		{"&-", "&"},
		{"&-&-", "&&"},
		{"~peter/mail/&U,BTFw-/&ZeVnLIqe-", "~peter/mail/台北/日本語"},
		{"caf&AOk-", "café"},
		{"&ZeVnLIqe-", "日本語"},
	}
	for _, c := range cases {
		if got := DecodeUTF7(c.in); got != c.want {
			t.Errorf("DecodeUTF7(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// Round-trip the printable/non-printable mix.
	for _, s := range []string{"INBOX", "café", "日本語", "a&b", "x/台北"} {
		if got := DecodeUTF7(EncodeUTF7(s)); got != s {
			t.Errorf("roundtrip %q -> %q", s, got)
		}
	}
}

// TestDecodeUTF7Lenient exercises the robustness branches for malformed input
// (an unterminated shift run, and stray non-alphabet bytes inside a shift). The
// decoder does not error: it decodes the recoverable code units and skips the
// rest, never panicking on hostile server output.
func TestDecodeUTF7Lenient(t *testing.T) {
	// Unterminated shift run (no closing '-' before end of string): the payload is
	// still decoded (drives the j>=len(s) tail branch).
	if got := DecodeUTF7("x&AOk"); got != "xé" {
		t.Errorf("unterminated = %q, want xé", got)
	}
	// A stray non-alphabet byte inside the shift run is skipped, not fatal.
	if got := DecodeUTF7("&AO!k-"); got == "" {
		t.Errorf("invalid-byte run decoded to empty; want a non-panicking decode")
	}
}

// TestSASLEncoders checks the SASL response encoders against MRI-verified
// golden vectors (Net::IMAP::SASL authenticators).
func TestSASLEncoders(t *testing.T) {
	if got := SASLEncode(SASLPlain("", "joe", "secret")); got != "AGpvZQBzZWNyZXQ=" {
		t.Errorf("PLAIN = %q", got)
	}
	if got := SASLEncode(SASLPlain("admin", "joe", "secret")); got != "YWRtaW4Aam9lAHNlY3JldA==" {
		t.Errorf("PLAIN authzid = %q", got)
	}
	if got := SASLLoginUser("joe"); got != "joe" {
		t.Errorf("LOGIN user = %q", got)
	}
	if got := SASLLoginPassword("secret"); got != "secret" {
		t.Errorf("LOGIN pass = %q", got)
	}
	if got := SASLCramMD5("joe", "secret", "<1234@host>"); got != "joe d626aad6bf2f5755c3838e083e4589e2" {
		t.Errorf("CRAM-MD5 = %q", got)
	}
	if got := SASLEncode(SASLXOAuth2("joe", "tok")); got != "dXNlcj1qb2UBYXV0aD1CZWFyZXIgdG9rAQE=" {
		t.Errorf("XOAUTH2 = %q", got)
	}
}

// TestFormatHelpers checks FormatDate / FormatDatetime against MRI golden output.
func TestFormatHelpers(t *testing.T) {
	tm := time.Date(2026, time.June, 30, 12, 5, 9, 0, time.UTC)
	if got := FormatDate(tm); got != "30-Jun-2026" {
		t.Errorf("FormatDate = %q", got)
	}
	if got := FormatDatetime(tm); got != "30-Jun-2026 12:05 +0000" {
		t.Errorf("FormatDatetime = %q", got)
	}
	// A negative offset exercises the sign branch.
	west := time.FixedZone("PST", -8*3600)
	tm2 := time.Date(2026, time.January, 2, 3, 4, 5, 0, west)
	if got := FormatDatetime(tm2); got != "2-Jan-2026 03:04 -0800" {
		t.Errorf("FormatDatetime west = %q", got)
	}
}

// TestMessageSet checks the MessageSet helper and its error path.
func TestMessageSet(t *testing.T) {
	got, err := MessageSet(3, 1, 2, 7)
	if err != nil {
		t.Fatalf("MessageSet: %v", err)
	}
	if got != "1:3,7" {
		t.Errorf("MessageSet = %q, want 1:3,7", got)
	}
	if got, _ := MessageSet(5, 0); got != "5,*" {
		t.Errorf("MessageSet star = %q, want 5,*", got)
	}
	if _, err := MessageSet(-1); !errors.Is(err, ErrInvalidData) {
		t.Errorf("MessageSet(-1) err = %v, want ErrInvalidData", err)
	}
}

// TestNewCommands checks the EXPUNGE/CHECK/CLOSE/UNSELECT/IDLE/AUTHENTICATE
// builders and IdleDone against the exact MRI byte forms.
func TestNewCommands(t *testing.T) {
	b := NewBuilder("T")
	check := func(c Command, err error, want string) {
		t.Helper()
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		if c.Bytes != want {
			t.Errorf("got %q want %q", c.Bytes, want)
		}
	}
	c, err := b.Expunge()
	check(c, err, "T0001 EXPUNGE\r\n")
	c, err = b.Check()
	check(c, err, "T0002 CHECK\r\n")
	c, err = b.Close()
	check(c, err, "T0003 CLOSE\r\n")
	c, err = b.Unselect()
	check(c, err, "T0004 UNSELECT\r\n")
	c, err = b.Idle()
	check(c, err, "T0005 IDLE\r\n")
	c, err = b.Authenticate("CRAM-MD5")
	check(c, err, "T0006 AUTHENTICATE CRAM-MD5\r\n")
	if got := IdleDone(); got != "DONE\r\n" {
		t.Errorf("IdleDone = %q", got)
	}
}

// TestBodyType checks the BodyType class dispatch over parsed bodies.
func TestBodyType(t *testing.T) {
	cases := []struct {
		resp string
		want string
	}{
		{`* 1 FETCH (BODYSTRUCTURE ("TEXT" "PLAIN" ("CHARSET" "US-ASCII") NIL NIL "7BIT" 25 3))`, "BodyTypeText"},
		{`* 1 FETCH (BODYSTRUCTURE ("IMAGE" "GIF" NIL NIL NIL "BASE64" 99))`, "BodyTypeBasic"},
		{`* 1 FETCH (BODYSTRUCTURE ("MESSAGE" "RFC822" NIL NIL NIL "7BIT" 99))`, "BodyTypeMessage"},
		{`* 1 FETCH (BODYSTRUCTURE ("MESSAGE" "DELIVERY-STATUS" NIL NIL NIL "7BIT" 99))`, "BodyTypeBasic"},
		{`* 1 FETCH (BODYSTRUCTURE (("TEXT" "PLAIN" NIL NIL NIL "7BIT" 1 1)("TEXT" "HTML" NIL NIL NIL "7BIT" 1 1) "ALTERNATIVE"))`, "BodyTypeMultipart"},
	}
	for _, c := range cases {
		r, err := ParseResponse(c.resp + "\r\n")
		if err != nil {
			t.Fatalf("parse %q: %v", c.resp, err)
		}
		fd := r.(*UntaggedResponse).Data.(*FetchData)
		bs, _ := fd.Attr.Get("BODYSTRUCTURE")
		if got := bs.(*BodyStructure).BodyType(); got != c.want {
			t.Errorf("BodyType(%q) = %q, want %q", c.resp, got, c.want)
		}
	}
}

// TestAppendCopyUID checks parsing of the APPENDUID / COPYUID resp-text-codes.
func TestAppendCopyUID(t *testing.T) {
	r, err := ParseResponse("* OK [APPENDUID 38505 3955] APPEND completed\r\n")
	if err != nil {
		t.Fatalf("parse APPENDUID: %v", err)
	}
	code := r.(*UntaggedResponse).Data.(*ResponseText).Code
	au, ok := code.Data.(*AppendUIDData)
	if !ok {
		t.Fatalf("APPENDUID data type %T", code.Data)
	}
	if au.UIDValidity != 38505 || au.AssignedUIDs != "3955" {
		t.Errorf("APPENDUID = %+v", au)
	}

	r2, err := ParseResponse("* OK [COPYUID 38505 304,319:320 3956:3958] Done\r\n")
	if err != nil {
		t.Fatalf("parse COPYUID: %v", err)
	}
	code2 := r2.(*UntaggedResponse).Data.(*ResponseText).Code
	cu, ok := code2.Data.(*CopyUIDData)
	if !ok {
		t.Fatalf("COPYUID data type %T", code2.Data)
	}
	if cu.UIDValidity != 38505 || cu.SourceUIDs != "304,319:320" || cu.AssignedUIDs != "3956:3958" {
		t.Errorf("COPYUID = %+v", cu)
	}
}

// TestAppendCopyUIDErrors drives the malformed-input error branches of the
// APPENDUID / COPYUID resp-text-code parsing.
func TestAppendCopyUIDErrors(t *testing.T) {
	bad := []string{
		"* OK [APPENDUID]\r\n",     // missing SP + uidvalidity
		"* OK [APPENDUID x 3]\r\n", // uidvalidity not a number
		"* OK [APPENDUID 5]\r\n",   // missing SP + assigned uid-set
		"* OK [COPYUID]\r\n",       // missing SP + uidvalidity
		"* OK [COPYUID y 1 2]\r\n", // uidvalidity not a number
		"* OK [COPYUID 5]\r\n",     // missing SP + source set
		"* OK [COPYUID 5 1:2]\r\n", // missing SP + dest set
	}
	for _, s := range bad {
		if _, err := ParseResponse(s); err == nil {
			t.Errorf("ParseResponse(%q) succeeded, want error", s)
		}
	}
}
