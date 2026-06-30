// Copyright (c) the go-ruby-net-imap/net-imap authors
//
// SPDX-License-Identifier: BSD-3-Clause

package imap

import "strconv"

// The socket / TLS transport is the seam. This file does not open a connection;
// it assembles a complete response buffer (a line plus any embedded literals)
// from two host-supplied callbacks, mirroring Net::IMAP::ResponseReader without
// touching a socket.

// ReadLineFunc reads the next CRLF-terminated line from the connection,
// including the trailing CRLF. It returns the line and nil on success, or a
// non-nil error (e.g. io.EOF) at end of stream. The host wires this to its
// buffered socket reader.
type ReadLineFunc func() (string, error)

// ReadLiteralFunc reads exactly n bytes of literal payload from the connection.
// The host wires this to a full read of n octets off the socket.
type ReadLiteralFunc func(n int) (string, error)

// Reader assembles complete response buffers from a line reader and a literal
// reader, then parses them. It is the parser-side seam: the host supplies the
// two transport callbacks and Reader does the IMAP framing (detecting a trailing
// `{n}\r\n`, reading n literal bytes, and continuing until the line is complete).
type Reader struct {
	readLine    ReadLineFunc
	readLiteral ReadLiteralFunc
}

// NewReader returns a Reader driven by the host's line and literal readers.
func NewReader(readLine ReadLineFunc, readLiteral ReadLiteralFunc) *Reader {
	return &Reader{readLine: readLine, readLiteral: readLiteral}
}

// ReadBuffer assembles one complete response buffer: it reads a line, and while
// that line ends with a `{n}\r\n` literal marker it reads n literal bytes plus
// the next line, splicing them into the buffer (Net::IMAP::ResponseReader's
// read_response_buffer). The returned buffer is suitable for ParseResponse.
func (r *Reader) ReadBuffer() (string, error) {
	buf := ""
	for {
		line, err := r.readLine()
		if err != nil {
			return "", err
		}
		buf += line
		n, ok := literalSize(buf)
		if !ok {
			return buf, nil
		}
		lit, err := r.readLiteral(n)
		if err != nil {
			return "", err
		}
		buf += lit
	}
}

// ReadResponse assembles a complete buffer and parses it.
func (r *Reader) ReadResponse() (Response, error) {
	buf, err := r.ReadBuffer()
	if err != nil {
		return nil, err
	}
	return ParseResponse(buf)
}

// literalSize reports the literal byte count when buf ends with `{n}\r\n`, the
// marker ResponseReader keys on. It mirrors the /\{(\d+)\}\r\n\z/n test.
func literalSize(buf string) (int, bool) {
	if len(buf) < len("{0}\r\n") || buf[len(buf)-2:] != CRLF {
		return 0, false
	}
	// Walk back over CRLF and the closing brace's digits to the opening brace.
	i := len(buf) - 2 // index of '\r'
	if i < 1 || buf[i-1] != '}' {
		return 0, false
	}
	j := i - 2 // last digit
	k := j
	for k >= 0 && isDigit(buf[k]) {
		k--
	}
	if k == j || k < 0 || buf[k] != '{' {
		return 0, false
	}
	n, err := strconv.Atoi(buf[k+1 : j+1])
	if err != nil {
		return 0, false
	}
	return n, true
}
