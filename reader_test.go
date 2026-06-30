// Copyright (c) the go-ruby-net-imap/net-imap authors
//
// SPDX-License-Identifier: BSD-3-Clause

package imap

import (
	"errors"
	"io"
	"strings"
	"testing"
)

// scriptedConn feeds a fixed sequence of lines and literal payloads, mirroring
// what the host's socket reader would produce.
type scriptedConn struct {
	lines    []string
	literals []string
	li, pi   int
}

func (c *scriptedConn) readLine() (string, error) {
	if c.li >= len(c.lines) {
		return "", io.EOF
	}
	s := c.lines[c.li]
	c.li++
	return s, nil
}

func (c *scriptedConn) readLiteral(n int) (string, error) {
	if c.pi >= len(c.literals) {
		return "", io.ErrUnexpectedEOF
	}
	s := c.literals[c.pi]
	c.pi++
	if len(s) != n {
		return "", errors.New("wrong literal size requested")
	}
	return s, nil
}

func TestReaderSingleLine(t *testing.T) {
	c := &scriptedConn{lines: []string{"* 22 EXISTS\r\n"}}
	r := NewReader(c.readLine, c.readLiteral)
	resp, err := r.ReadResponse()
	if err != nil {
		t.Fatal(err)
	}
	if resp.(*UntaggedResponse).Name != "EXISTS" {
		t.Errorf("resp = %#v", resp)
	}
}

func TestReaderWithLiteral(t *testing.T) {
	// "* 1 FETCH (RFC822 {4}\r\n" + "body" + ")\r\n".
	c := &scriptedConn{
		lines:    []string{"* 1 FETCH (RFC822 {4}\r\n", ")\r\n"},
		literals: []string{"body"},
	}
	r := NewReader(c.readLine, c.readLiteral)
	buf, err := r.ReadBuffer()
	if err != nil {
		t.Fatal(err)
	}
	want := "* 1 FETCH (RFC822 {4}\r\nbody)\r\n"
	if buf != want {
		t.Errorf("buf = %q, want %q", buf, want)
	}
}

func TestReaderTwoLiterals(t *testing.T) {
	c := &scriptedConn{
		lines:    []string{"* 1 FETCH (BODY[1] {2}\r\n", " BODY[2] {3}\r\n", ")\r\n"},
		literals: []string{"aa", "bbb"},
	}
	r := NewReader(c.readLine, c.readLiteral)
	buf, err := r.ReadBuffer()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf, "aa") || !strings.Contains(buf, "bbb") {
		t.Errorf("buf = %q", buf)
	}
}

func TestReaderLineError(t *testing.T) {
	c := &scriptedConn{}
	r := NewReader(c.readLine, c.readLiteral)
	if _, err := r.ReadBuffer(); !errors.Is(err, io.EOF) {
		t.Errorf("err = %v", err)
	}
	if _, err := r.ReadResponse(); !errors.Is(err, io.EOF) {
		t.Errorf("ReadResponse err = %v", err)
	}
}

func TestReaderLiteralError(t *testing.T) {
	c := &scriptedConn{lines: []string{"* 1 FETCH (RFC822 {4}\r\n"}}
	r := NewReader(c.readLine, c.readLiteral)
	if _, err := r.ReadBuffer(); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("err = %v", err)
	}
}

func TestLiteralSizeDetection(t *testing.T) {
	cases := []struct {
		in string
		n  int
		ok bool
	}{
		{"x {5}\r\n", 5, true},
		{"x {0}\r\n", 0, true},
		{"x {12}\r\n", 12, true},
		{"plain\r\n", 0, false},
		{"x\r\n", 0, false},
		{"no crlf", 0, false},
		{"{5}x\r\n", 0, false},                   // marker not at end
		{"x}\r\n", 0, false},                     // no opening brace
		{"x {}\r\n", 0, false},                   // no digits
		{"{5}\r\n", 5, true},                     // whole buffer is the marker
		{"\r\n", 0, false},                       // too short / no brace
		{"{x}\r\n", 0, false},                    // non-digit inside braces
		{"{99999999999999999999}\r\n", 0, false}, // overflow
	}
	for _, c := range cases {
		n, ok := literalSize(c.in)
		if ok != c.ok || (ok && n != c.n) {
			t.Errorf("literalSize(%q) = (%d,%v), want (%d,%v)", c.in, n, ok, c.n, c.ok)
		}
	}
}
