// Copyright (c) the go-ruby-net-imap/net-imap authors
//
// SPDX-License-Identifier: BSD-3-Clause

package imap

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestTagGeneration(t *testing.T) {
	b := NewBuilder("RUBY")
	for i, want := range []string{"RUBY0001", "RUBY0002", "RUBY0003"} {
		if got := b.NextTag(); got != want {
			t.Errorf("tag %d = %q, want %q", i, got, want)
		}
	}
}

func TestCommandFraming(t *testing.T) {
	cases := []struct {
		name string
		cmd  string
		args []Argument
		want string
	}{
		{"capability", "CAPABILITY", nil, "RUBY0001 CAPABILITY\r\n"},
		{"login-plain", "LOGIN", []Argument{"joe", "secret"}, "RUBY0001 LOGIN joe secret\r\n"},
		{"login-quoted", "LOGIN", []Argument{"joe", "pa ss"}, "RUBY0001 LOGIN joe \"pa ss\"\r\n"},
		{"empty-arg", "LOGIN", []Argument{"", "x"}, "RUBY0001 LOGIN \"\" x\r\n"},
		{"quote-escapes", "X", []Argument{`a"b\c`}, "RUBY0001 X \"a\\\"b\\\\c\"\r\n"},
		{"nil", "X", []Argument{nil}, "RUBY0001 X NIL\r\n"},
		{"int", "X", []Argument{42}, "RUBY0001 X 42\r\n"},
		{"int64", "X", []Argument{int64(7)}, "RUBY0001 X 7\r\n"},
		{"list", "X", []Argument{[]Argument{"a", 1, nil}}, "RUBY0001 X (a 1 NIL)\r\n"},
		{"atom", "X", []Argument{Atom("RAW")}, "RUBY0001 X RAW\r\n"},
		{"rawdata", "X", []Argument{RawData("RAW")}, "RUBY0001 X RAW\r\n"},
		{"quotedstring", "X", []Argument{QuotedString("hi")}, "RUBY0001 X \"hi\"\r\n"},
		{"flag", "X", []Argument{Flag("Seen")}, "RUBY0001 X \\Seen\r\n"},
		{"special-quote", "X", []Argument{"a(b)"}, "RUBY0001 X \"a(b)\"\r\n"},
		{"percent", "X", []Argument{"a%b"}, "RUBY0001 X \"a%b\"\r\n"},
		{"ctl-quote", "X", []Argument{"a\tb"}, "RUBY0001 X \"a\tb\"\r\n"},
		{"del-quote", "X", []Argument{"a\x7fb"}, "RUBY0001 X \"a\x7fb\"\r\n"},
		{"plain-atom-passthrough", "X", []Argument{"plainAtom"}, "RUBY0001 X plainAtom\r\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := NewBuilder("RUBY")
			got, err := b.Command(c.cmd, c.args...)
			if err != nil {
				t.Fatalf("Command: %v", err)
			}
			if got.Bytes != c.want {
				t.Errorf("Bytes = %q, want %q", got.Bytes, c.want)
			}
			if len(got.Literals) != 0 {
				t.Errorf("unexpected literals %v", got.Literals)
			}
		})
	}
}

func TestNonASCIIBecomesLiteral(t *testing.T) {
	b := NewBuilder("RUBY")
	cmd, err := b.Command("X", "café")
	if err != nil {
		t.Fatal(err)
	}
	want := "RUBY0001 X {5}\r\n"
	if cmd.Bytes != want {
		t.Errorf("Bytes = %q, want %q", cmd.Bytes, want)
	}
	if len(cmd.Literals) != 1 || cmd.Literals[0].Data != "café" || cmd.Literals[0].Tail != CRLF {
		t.Errorf("Literals = %#v", cmd.Literals)
	}
}

func TestMultilineBecomesLiteral(t *testing.T) {
	b := NewBuilder("RUBY")
	cmd, err := b.Command("X", "a\r\nb")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Bytes != "RUBY0001 X {4}\r\n" {
		t.Errorf("Bytes = %q", cmd.Bytes)
	}
	if len(cmd.Literals) != 1 || cmd.Literals[0].Data != "a\r\nb" {
		t.Errorf("Literals = %#v", cmd.Literals)
	}
}

func TestLiteralTypeAndTail(t *testing.T) {
	b := NewBuilder("RUBY")
	// A literal followed by a trailing arg: the second arg's bytes land in the
	// literal segment's Tail.
	cmd, err := b.Command("X", Literal("hi"), "end")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Bytes != "RUBY0001 X {2}\r\n" {
		t.Errorf("Bytes = %q", cmd.Bytes)
	}
	if len(cmd.Literals) != 1 || cmd.Literals[0].Data != "hi" || cmd.Literals[0].Tail != " end\r\n" {
		t.Errorf("Literals = %#v", cmd.Literals)
	}
}

func TestTwoLiterals(t *testing.T) {
	b := NewBuilder("RUBY")
	cmd, err := b.Command("X", Literal("aa"), Literal("bbb"))
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Bytes != "RUBY0001 X {2}\r\n" {
		t.Errorf("Bytes = %q", cmd.Bytes)
	}
	if len(cmd.Literals) != 2 {
		t.Fatalf("want 2 literals, got %#v", cmd.Literals)
	}
	if cmd.Literals[0].Data != "aa" || cmd.Literals[0].Tail != " {3}\r\n" {
		t.Errorf("lit0 = %#v", cmd.Literals[0])
	}
	if cmd.Literals[1].Data != "bbb" || cmd.Literals[1].Tail != CRLF {
		t.Errorf("lit1 = %#v", cmd.Literals[1])
	}
}

func TestLiteralInsideList(t *testing.T) {
	b := NewBuilder("RUBY")
	cmd, err := b.Command("X", []Argument{Literal("hi"), "z"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Bytes != "RUBY0001 X ({2}\r\n" {
		t.Errorf("Bytes = %q", cmd.Bytes)
	}
	if len(cmd.Literals) != 1 || cmd.Literals[0].Tail != " z)\r\n" {
		t.Errorf("Literals = %#v", cmd.Literals)
	}
}

func TestDateAndTimeArgs(t *testing.T) {
	b := NewBuilder("RUBY")
	cmd, err := b.Command("X", Date{2026, time.June, 30})
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Bytes != "RUBY0001 X 30-Jun-2026\r\n" {
		t.Errorf("date = %q", cmd.Bytes)
	}
	loc := time.FixedZone("", -7*3600)
	tm := time.Date(1996, time.July, 17, 2, 44, 25, 0, loc)
	c2, _ := buildCommand("t", "X", tm)
	if c2.Bytes != "t X \"17-Jul-1996 02:44:25 -0700\"\r\n" {
		t.Errorf("time = %q", c2.Bytes)
	}
	// Positive offset path.
	loc2 := time.FixedZone("", 5*3600+30*60)
	tm2 := time.Date(2026, time.January, 2, 3, 4, 5, 0, loc2)
	c3, _ := buildCommand("t", "X", tm2)
	if c3.Bytes != "t X \"2-Jan-2026 03:04:05 +0530\"\r\n" {
		t.Errorf("time+ = %q", c3.Bytes)
	}
}

func TestInvalidData(t *testing.T) {
	b := NewBuilder("RUBY")
	for _, bad := range []Argument{
		3.14,                        // float unsupported
		-1,                          // negative number
		int64(1) << 33,              // too big
		[]Argument{3.14},            // nested invalid
		(*SequenceSet)(nil),         // nil sequence set
		[]Argument{[]Argument{4.2}}, // deeply nested invalid
		map[string]int{"a": 1},      // wholly unsupported type
	} {
		if _, err := b.Command("X", bad); !errors.Is(err, ErrInvalidData) {
			t.Errorf("Command(%T) err = %v, want ErrInvalidData", bad, err)
		}
	}
}

func TestEncodeInvalidInList(t *testing.T) {
	// encodeData must surface an error from a nested element that slips past the
	// validator path (exercise the encode-time error branch directly).
	err := encodeData([]Argument{3.14}, func(string) {}, func(string) {})
	if !errors.Is(err, ErrInvalidData) {
		t.Errorf("encodeData err = %v", err)
	}
	if err := encodeData(3.14, func(string) {}, func(string) {}); !errors.Is(err, ErrInvalidData) {
		t.Errorf("encodeData scalar err = %v", err)
	}
}

func TestSequenceSet(t *testing.T) {
	cases := []struct {
		items []any
		want  string
	}{
		{[]any{1}, "1"},
		{[]any{1, 2, 3}, "1:3"},
		{[]any{3, 1, 2}, "1:3"},
		{[]any{SeqRange{1, 5}}, "1:5"},
		{[]any{1, 2, 4, 5}, "1:2,4:5"},
		{[]any{10, 20, 30}, "10,20,30"},
		{[]any{1, 1, 2}, "1:2"},
		{[]any{SeqRange{1, Star}}, "1:*"},
		{[]any{Star}, "*"},
		{[]any{SeqRange{4, 2}}, "2:4"},
		{[]any{SeqRange{2, 4}, 7, SeqRange{9, 11}}, "2:4,7,9:11"},
		{[]any{int64(5), int64(6)}, "5:6"},
		{[]any{SeqRange{5, Star}, 1}, "1,5:*"},
		{[]any{SeqRange{Star, Star}}, "*"},
		{[]any{1, SeqRange{3, 5}, SeqRange{2, 2}}, "1:5"},
	}
	for _, c := range cases {
		ss, err := NewSequenceSet(c.items...)
		if err != nil {
			t.Fatalf("NewSequenceSet(%v): %v", c.items, err)
		}
		if got := ss.String(); got != c.want {
			t.Errorf("SequenceSet(%v) = %q, want %q", c.items, got, c.want)
		}
	}
}

func TestSequenceSetErrors(t *testing.T) {
	for _, bad := range [][]any{
		{},                // empty
		{0.5},             // bad element type
		{-1},              // negative
		{int64(-2)},       // negative int64
		{SeqRange{-1, 3}}, // bad range lo
		{SeqRange{1, -3}}, // bad range hi
		{int64(1) << 33},  // too big
	} {
		if _, err := NewSequenceSet(bad...); !errors.Is(err, ErrInvalidData) {
			t.Errorf("NewSequenceSet(%v) err = %v, want ErrInvalidData", bad, err)
		}
	}
}

func TestHighLevelCommands(t *testing.T) {
	ss, _ := NewSequenceSet(1, SeqRange{3, 5})
	type tc struct {
		name string
		fn   func(*Builder) (Command, error)
		want string
	}
	cases := []tc{
		{"capability", func(b *Builder) (Command, error) { return b.Capability() }, "T0001 CAPABILITY\r\n"},
		{"noop", func(b *Builder) (Command, error) { return b.Noop() }, "T0001 NOOP\r\n"},
		{"logout", func(b *Builder) (Command, error) { return b.Logout() }, "T0001 LOGOUT\r\n"},
		{"starttls", func(b *Builder) (Command, error) { return b.StartTLS() }, "T0001 STARTTLS\r\n"},
		{"login", func(b *Builder) (Command, error) { return b.Login("a", "b") }, "T0001 LOGIN a b\r\n"},
		{"select", func(b *Builder) (Command, error) { return b.Select("INBOX") }, "T0001 SELECT INBOX\r\n"},
		{"examine", func(b *Builder) (Command, error) { return b.Examine("INBOX") }, "T0001 EXAMINE INBOX\r\n"},
		{"create", func(b *Builder) (Command, error) { return b.Create("box") }, "T0001 CREATE box\r\n"},
		{"delete", func(b *Builder) (Command, error) { return b.Delete("box") }, "T0001 DELETE box\r\n"},
		{"rename", func(b *Builder) (Command, error) { return b.Rename("a", "b") }, "T0001 RENAME a b\r\n"},
		{"subscribe", func(b *Builder) (Command, error) { return b.Subscribe("box") }, "T0001 SUBSCRIBE box\r\n"},
		{"unsubscribe", func(b *Builder) (Command, error) { return b.Unsubscribe("box") }, "T0001 UNSUBSCRIBE box\r\n"},
		{"list", func(b *Builder) (Command, error) { return b.List("", "*") }, "T0001 LIST \"\" \"*\"\r\n"},
		{"lsub", func(b *Builder) (Command, error) { return b.Lsub("", "*") }, "T0001 LSUB \"\" \"*\"\r\n"},
		{"status", func(b *Builder) (Command, error) { return b.Status("box", "MESSAGES", "UIDNEXT") }, "T0001 STATUS box (MESSAGES UIDNEXT)\r\n"},
		{"fetch", func(b *Builder) (Command, error) { return b.Fetch(ss, "FLAGS", "UID") }, "T0001 FETCH 1,3:5 (FLAGS UID)\r\n"},
		{"uidfetch", func(b *Builder) (Command, error) { return b.UIDFetch(ss, "UID") }, "T0001 UID FETCH 1,3:5 (UID)\r\n"},
		{"store", func(b *Builder) (Command, error) { return b.Store(ss, "+FLAGS", []Flag{"Seen"}) }, "T0001 STORE 1,3:5 +FLAGS (\\Seen)\r\n"},
		{"uidstore", func(b *Builder) (Command, error) { return b.UIDStore(ss, "FLAGS", []Flag{"Deleted"}) }, "T0001 UID STORE 1,3:5 FLAGS (\\Deleted)\r\n"},
		{"copy", func(b *Builder) (Command, error) { return b.Copy(ss, "box") }, "T0001 COPY 1,3:5 box\r\n"},
		{"uidcopy", func(b *Builder) (Command, error) { return b.UIDCopy(ss, "box") }, "T0001 UID COPY 1,3:5 box\r\n"},
		{"search", func(b *Builder) (Command, error) { return b.Search("FROM", "smith") }, "T0001 SEARCH FROM smith\r\n"},
		{"search-arg", func(b *Builder) (Command, error) { return b.Search("SUBJECT", QuotedString("hi there")) }, "T0001 SEARCH SUBJECT \"hi there\"\r\n"},
		{"uidsearch", func(b *Builder) (Command, error) { return b.UIDSearch("ALL") }, "T0001 UID SEARCH ALL\r\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := c.fn(NewBuilder("T"))
			if err != nil {
				t.Fatal(err)
			}
			if got.Bytes != c.want {
				t.Errorf("= %q, want %q", got.Bytes, c.want)
			}
		})
	}
}

func TestAppend(t *testing.T) {
	b := NewBuilder("T")
	// No flags, no date.
	c1, err := b.Append("INBOX", nil, nil, "msg")
	if err != nil {
		t.Fatal(err)
	}
	if c1.Bytes != "T0001 APPEND INBOX {3}\r\n" || len(c1.Literals) != 1 || c1.Literals[0].Data != "msg" {
		t.Errorf("append plain = %q %#v", c1.Bytes, c1.Literals)
	}
	// With flags + date.
	b2 := NewBuilder("T")
	c2, err := b2.Append("INBOX", []Flag{"Seen", "Draft"}, &Date{2026, time.June, 30}, "x")
	if err != nil {
		t.Fatal(err)
	}
	if c2.Bytes != "T0001 APPEND INBOX (\\Seen \\Draft) 30-Jun-2026 {1}\r\n" {
		t.Errorf("append full = %q", c2.Bytes)
	}
	if !strings.HasSuffix(c2.Literals[0].Tail, CRLF) {
		t.Errorf("append tail = %q", c2.Literals[0].Tail)
	}
}
