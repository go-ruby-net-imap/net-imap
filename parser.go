// Copyright (c) the go-ruby-net-imap/net-imap authors
//
// SPDX-License-Identifier: BSD-3-Clause

package imap

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrParse is returned (wrapped) when a response buffer does not match the IMAP
// response grammar.
var ErrParse = errors.New("imap: parse error")

// Response is the common interface of the three response shapes ParseResponse
// returns: *TaggedResponse, *UntaggedResponse and *ContinuationRequest.
type Response interface{ isResponse() }

func (*TaggedResponse) isResponse()      {}
func (*UntaggedResponse) isResponse()    {}
func (*ContinuationRequest) isResponse() {}

// ParseResponse parses one complete response buffer — a single line, with any
// IMAP literals already spliced in as raw bytes (the [Reader] produces exactly
// such a buffer). It returns a *TaggedResponse, *UntaggedResponse or
// *ContinuationRequest, mirroring Net::IMAP::ResponseParser#parse.
//
// The returned RawData field carries buf verbatim. ParseResponse is byte-faithful
// to MRI for the IMAP4rev1 response grammar (status responses, EXISTS/RECENT/
// EXPUNGE, FLAGS, LIST/LSUB, STATUS, SEARCH, CAPABILITY, FETCH including
// ENVELOPE / FLAGS / INTERNALDATE / RFC822* / UID / BODY[…] and the
// resp-text-codes). BODYSTRUCTURE is returned as a *BodyStructure tree (see its
// doc for the boundary versus MRI's BodyType* struct tower).
func ParseResponse(buf string) (Response, error) {
	p := &parser{buf: buf}
	switch {
	case strings.HasPrefix(buf, "+"):
		return p.continuationRequest()
	case strings.HasPrefix(buf, "*"):
		return p.untagged()
	default:
		return p.tagged()
	}
}

// parser is a small recursive-descent / tokenising cursor over one buffer.
type parser struct {
	buf string
	pos int
}

func (p *parser) errf(format string, a ...any) error {
	return fmt.Errorf("%w: %s (at %d in %q)", ErrParse, fmt.Sprintf(format, a...), p.pos, p.buf)
}

// --- low-level cursor helpers ---

func (p *parser) eof() bool { return p.pos >= len(p.buf) }

func (p *parser) peek() byte {
	if p.eof() {
		return 0
	}
	return p.buf[p.pos]
}

// expect consumes the literal token tok or errors.
func (p *parser) expect(tok string) error {
	if !strings.HasPrefix(p.buf[p.pos:], tok) {
		return p.errf("expected %q", tok)
	}
	p.pos += len(tok)
	return nil
}

// sp consumes exactly one space.
func (p *parser) sp() error { return p.expect(" ") }

// crlf consumes the trailing CRLF (tolerating bytes already consumed exactly).
func (p *parser) crlf() error { return p.expect(CRLF) }

// --- top-level productions ---

func (p *parser) continuationRequest() (Response, error) {
	p.pos++ // consume '+' (the dispatcher guaranteed it)
	// MRI tolerates "+\r\n" and "+ text\r\n"; a space is optional.
	if p.peek() == ' ' {
		p.pos++
	}
	rt, err := p.respText()
	if err != nil {
		return nil, err
	}
	if err := p.crlf(); err != nil {
		return nil, err
	}
	return &ContinuationRequest{Data: rt, RawData: p.buf}, nil
}

func (p *parser) tagged() (Response, error) {
	tag, err := p.atomToken()
	if err != nil {
		return nil, err
	}
	if err := p.sp(); err != nil {
		return nil, err
	}
	name, err := p.atomToken()
	if err != nil {
		return nil, err
	}
	name = upcaseASCII(name)
	if name != "OK" && name != "NO" && name != "BAD" {
		return nil, p.errf("unexpected tagged response name %q", name)
	}
	if err := p.sp(); err != nil {
		return nil, err
	}
	rt, err := p.respText()
	if err != nil {
		return nil, err
	}
	if err := p.crlf(); err != nil {
		return nil, err
	}
	return &TaggedResponse{Tag: tag, Name: name, Data: rt, RawData: p.buf}, nil
}

func (p *parser) untagged() (Response, error) {
	p.pos++ // consume '*' (the dispatcher guaranteed it)
	if err := p.sp(); err != nil {
		return nil, err
	}
	// numeric-data responses: `* n EXISTS|RECENT|EXPUNGE|FETCH`.
	if isDigit(p.peek()) {
		return p.numericData()
	}
	name, err := p.atomToken()
	if err != nil {
		return nil, err
	}
	name = upcaseASCII(name)
	return p.namedUntagged(name)
}

func (p *parser) numericData() (Response, error) {
	n, err := p.number()
	if err != nil {
		return nil, err
	}
	if err := p.sp(); err != nil {
		return nil, err
	}
	name, err := p.atomToken()
	if err != nil {
		return nil, err
	}
	name = upcaseASCII(name)
	switch name {
	case "EXISTS", "RECENT", "EXPUNGE":
		if err := p.crlf(); err != nil {
			return nil, err
		}
		return &UntaggedResponse{Name: name, Data: n, RawData: p.buf}, nil
	case "FETCH":
		return p.fetchData(n)
	default:
		return nil, p.errf("unknown numeric response %q", name)
	}
}

func (p *parser) namedUntagged(name string) (Response, error) {
	switch name {
	case "OK", "NO", "BAD", "BYE", "PREAUTH":
		if err := p.sp(); err != nil {
			return nil, err
		}
		rt, err := p.respText()
		if err != nil {
			return nil, err
		}
		if err := p.crlf(); err != nil {
			return nil, err
		}
		return &UntaggedResponse{Name: name, Data: rt, RawData: p.buf}, nil
	case "FLAGS":
		return p.flagsResp(name)
	case "LIST", "LSUB":
		return p.listResp(name)
	case "STATUS":
		return p.statusResp(name)
	case "SEARCH":
		return p.searchResp(name)
	case "CAPABILITY":
		return p.capabilityResp(name)
	default:
		return nil, p.errf("unsupported untagged response %q", name)
	}
}

// --- resp-text and resp-text-code ---

func (p *parser) respText() (*ResponseText, error) {
	var code *ResponseCode
	if p.peek() == '[' {
		c, err := p.respTextCode()
		if err != nil {
			return nil, err
		}
		code = c
		// MRI: a single space separates the code from the text; the text may be
		// empty (the space too, when the line ends right after "]").
		if p.peek() == ' ' {
			p.pos++
		}
	}
	text := p.text()
	return &ResponseText{Code: code, Text: text}, nil
}

// text reads up to (not including) the trailing CRLF.
func (p *parser) text() string {
	end := strings.Index(p.buf[p.pos:], CRLF)
	if end < 0 {
		s := p.buf[p.pos:]
		p.pos = len(p.buf)
		return s
	}
	s := p.buf[p.pos : p.pos+end]
	p.pos += end
	return s
}

func (p *parser) respTextCode() (*ResponseCode, error) {
	p.pos++ // consume '[' (respText guaranteed it)
	name, err := p.respCodeName()
	if err != nil {
		return nil, err
	}
	name = upcaseASCII(name)
	var data any
	switch name {
	case "ALERT", "PARSE", "READ-ONLY", "READ-WRITE", "TRYCREATE",
		"NOMODSEQ", "COMPRESSIONACTIVE", "NOTSAVED", "HASCHILDREN", "CLOSED":
		// atom-only codes: no data.
	case "UIDVALIDITY", "UIDNEXT", "UNSEEN", "HIGHESTMODSEQ", "MODSEQ", "MAXSIZE":
		if err := p.sp(); err != nil {
			return nil, err
		}
		n, err := p.number()
		if err != nil {
			return nil, err
		}
		data = n
	case "PERMANENTFLAGS":
		if err := p.sp(); err != nil {
			return nil, err
		}
		fl, err := p.flagList()
		if err != nil {
			return nil, err
		}
		data = fl
	case "CAPABILITY":
		caps, err := p.capabilityList()
		if err != nil {
			return nil, err
		}
		data = caps
	case "APPENDUID":
		if err := p.sp(); err != nil {
			return nil, err
		}
		uv, err := p.number()
		if err != nil {
			return nil, err
		}
		if err := p.sp(); err != nil {
			return nil, err
		}
		set := p.uidSet()
		data = &AppendUIDData{UIDValidity: uv, AssignedUIDs: set}
	case "COPYUID":
		if err := p.sp(); err != nil {
			return nil, err
		}
		uv, err := p.number()
		if err != nil {
			return nil, err
		}
		if err := p.sp(); err != nil {
			return nil, err
		}
		src := p.uidSet()
		if err := p.sp(); err != nil {
			return nil, err
		}
		dst := p.uidSet()
		data = &CopyUIDData{UIDValidity: uv, SourceUIDs: src, AssignedUIDs: dst}
	case "BADCHARSET":
		// Optional space-separated charset list in parentheses.
		if p.peek() == ' ' {
			p.pos++
			list, err := p.astringList()
			if err != nil {
				return nil, err
			}
			data = list
		}
	default:
		// Generic: capture the remaining bracketed text verbatim as the data
		// (a string), matching MRI's fallback for unknown codes.
		if p.peek() == ' ' {
			p.pos++
			data = p.bracketText()
		}
	}
	if err := p.expect("]"); err != nil {
		return nil, err
	}
	return &ResponseCode{Name: name, Data: data}, nil
}

// respCodeName reads the code name: letters, digits and '-'.
func (p *parser) respCodeName() (string, error) {
	start := p.pos
	for !p.eof() {
		c := p.buf[p.pos]
		if isAtomChar(c) && c != ']' && c != ' ' {
			p.pos++
			continue
		}
		break
	}
	if p.pos == start {
		return "", p.errf("empty resp-text-code name")
	}
	return p.buf[start:p.pos], nil
}

// bracketText reads up to the closing ']'.
// uidSet reads a sequence-set token (digits, ':', ',', '*') verbatim, the form
// the APPENDUID/COPYUID codes carry. MRI wraps it in a SequenceSet; we keep the
// canonical string, which equals that SequenceSet's #to_s for the server's form.
func (p *parser) uidSet() string {
	start := p.pos
	for !p.eof() {
		c := p.buf[p.pos]
		if (c >= '0' && c <= '9') || c == ':' || c == ',' || c == '*' {
			p.pos++
			continue
		}
		break
	}
	return p.buf[start:p.pos]
}

func (p *parser) bracketText() string {
	start := p.pos
	for !p.eof() && p.buf[p.pos] != ']' {
		p.pos++
	}
	return p.buf[start:p.pos]
}

// astringList reads "(a b c)" of astrings into a []string.
func (p *parser) astringList() ([]string, error) {
	if err := p.expect("("); err != nil {
		return nil, err
	}
	var out []string
	for p.peek() != ')' {
		if len(out) > 0 {
			if err := p.sp(); err != nil {
				return nil, err
			}
		}
		s, err := p.astring()
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	p.pos++ // ')' — the loop only exits on it
	return out, nil
}

// --- FLAGS / LIST / STATUS / SEARCH / CAPABILITY ---

func (p *parser) flagsResp(name string) (Response, error) {
	if err := p.sp(); err != nil {
		return nil, err
	}
	fl, err := p.flagList()
	if err != nil {
		return nil, err
	}
	if err := p.crlf(); err != nil {
		return nil, err
	}
	return &UntaggedResponse{Name: name, Data: fl, RawData: p.buf}, nil
}

func (p *parser) listResp(name string) (Response, error) {
	if err := p.sp(); err != nil {
		return nil, err
	}
	attrs, err := p.flagList()
	if err != nil {
		return nil, err
	}
	if err := p.sp(); err != nil {
		return nil, err
	}
	delim, err := p.nstring()
	if err != nil {
		return nil, err
	}
	if err := p.sp(); err != nil {
		return nil, err
	}
	mbox, err := p.mailbox()
	if err != nil {
		return nil, err
	}
	if err := p.crlf(); err != nil {
		return nil, err
	}
	ml := &MailboxList{Attr: attrs, Delim: delim.str, Name: mbox}
	return &UntaggedResponse{Name: name, Data: ml, RawData: p.buf}, nil
}

func (p *parser) statusResp(name string) (Response, error) {
	if err := p.sp(); err != nil {
		return nil, err
	}
	mbox, err := p.mailbox()
	if err != nil {
		return nil, err
	}
	if err := p.sp(); err != nil {
		return nil, err
	}
	if err := p.expect("("); err != nil {
		return nil, err
	}
	attr := newOrderedInts()
	for p.peek() != ')' {
		if attr.Len() > 0 {
			if err := p.sp(); err != nil {
				return nil, err
			}
		}
		key, err := p.atomToken()
		if err != nil {
			return nil, err
		}
		if err := p.sp(); err != nil {
			return nil, err
		}
		n, err := p.number()
		if err != nil {
			return nil, err
		}
		attr.Set(upcaseASCII(key), n)
	}
	p.pos++ // ')' — the loop only exits on it
	if err := p.crlf(); err != nil {
		return nil, err
	}
	sd := &StatusData{Mailbox: mbox, Attr: attr}
	return &UntaggedResponse{Name: name, Data: sd, RawData: p.buf}, nil
}

func (p *parser) searchResp(name string) (Response, error) {
	var nums []int64
	for p.peek() == ' ' {
		p.pos++
		n, err := p.number()
		if err != nil {
			return nil, err
		}
		nums = append(nums, n)
	}
	if err := p.crlf(); err != nil {
		return nil, err
	}
	if nums == nil {
		nums = []int64{}
	}
	return &UntaggedResponse{Name: name, Data: nums, RawData: p.buf}, nil
}

func (p *parser) capabilityResp(name string) (Response, error) {
	caps, err := p.capabilityList()
	if err != nil {
		return nil, err
	}
	if err := p.crlf(); err != nil {
		return nil, err
	}
	return &UntaggedResponse{Name: name, Data: caps, RawData: p.buf}, nil
}

// capabilityList reads " CAP CAP …" (each preceded by a space), up-casing each
// token like MRI.
func (p *parser) capabilityList() ([]string, error) {
	var caps []string
	for p.peek() == ' ' {
		p.pos++
		if p.peek() == ']' || p.eof() {
			break
		}
		tok, err := p.atomToken()
		if err != nil {
			return nil, err
		}
		caps = append(caps, upcaseASCII(tok))
	}
	if caps == nil {
		caps = []string{}
	}
	return caps, nil
}

// --- flags ---

// flagList reads "(\Seen \Deleted keyword …)" into []Flag.
func (p *parser) flagList() ([]Flag, error) {
	if err := p.expect("("); err != nil {
		return nil, err
	}
	flags := []Flag{}
	for p.peek() != ')' {
		if len(flags) > 0 {
			if err := p.sp(); err != nil {
				return nil, err
			}
		}
		f, err := p.flag()
		if err != nil {
			return nil, err
		}
		flags = append(flags, f)
	}
	p.pos++ // ')' — the loop only exits on it
	return flags, nil
}

// flag reads one flag: `\Atom`, `\*`, or a bare keyword atom.
func (p *parser) flag() (Flag, error) {
	if p.peek() == '\\' {
		p.pos++
		if p.peek() == '*' {
			p.pos++
			return Flag("*"), nil
		}
		atom, err := p.atomToken()
		if err != nil {
			return "", err
		}
		// MRI capitalises system flags to a symbol (\Seen -> :Seen); it keeps the
		// server's casing of the atom, which for the standard flags is already
		// capitalised. We pass the atom through verbatim.
		return Flag(atom), nil
	}
	atom, err := p.atomToken()
	if err != nil {
		return "", err
	}
	return Flag(atom), nil
}

// --- tokens: atom / number / string / nstring / astring / mailbox ---

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// isAtomChar reports whether c may appear in an IMAP atom (ATOM-CHAR): any CHAR
// except atom-specials "(){ %*\"\\]" and control chars / space.
func isAtomChar(c byte) bool {
	if c <= 0x1f || c == 0x7f || c >= 0x80 {
		return false
	}
	switch c {
	case '(', ')', '{', ' ', '%', '*', '"', '\\', ']':
		return false
	}
	return true
}

func (p *parser) atomToken() (string, error) {
	start := p.pos
	for !p.eof() && isAtomChar(p.buf[p.pos]) {
		p.pos++
	}
	if p.pos == start {
		return "", p.errf("expected atom")
	}
	return p.buf[start:p.pos], nil
}

func (p *parser) number() (int64, error) {
	start := p.pos
	for !p.eof() && isDigit(p.buf[p.pos]) {
		p.pos++
	}
	if p.pos == start {
		return 0, p.errf("expected number")
	}
	n, err := strconv.ParseInt(p.buf[start:p.pos], 10, 64)
	if err != nil {
		return 0, p.errf("number overflow: %v", err)
	}
	return n, nil
}

// nstr is a nullable string result: ok=false means the wire form was NIL.
type nstr struct {
	str string
	ok  bool
}

// nstring reads a string or NIL.
func (p *parser) nstring() (nstr, error) {
	if p.matchNIL() {
		return nstr{}, nil
	}
	s, err := p.str()
	if err != nil {
		return nstr{}, err
	}
	return nstr{str: s, ok: true}, nil
}

// matchNIL consumes a literal "NIL" (case-insensitive) when present.
func (p *parser) matchNIL() bool {
	if len(p.buf)-p.pos >= 3 {
		s := p.buf[p.pos : p.pos+3]
		if (s == "NIL" || s == "nil" || upcaseASCII(s) == "NIL") &&
			(p.pos+3 >= len(p.buf) || !isAtomChar(p.buf[p.pos+3]) || p.buf[p.pos+3] == ']') {
			p.pos += 3
			return true
		}
	}
	return false
}

// str reads a quoted string or a literal.
func (p *parser) str() (string, error) {
	switch p.peek() {
	case '"':
		return p.quoted()
	case '{':
		return p.literal()
	default:
		return "", p.errf("expected string")
	}
}

// astring reads an atom, a quoted string, or a literal.
func (p *parser) astring() (string, error) {
	switch p.peek() {
	case '"':
		return p.quoted()
	case '{':
		return p.literal()
	default:
		return p.atomToken()
	}
}

// quoted reads a double-quoted string, undoing `\"` and `\\` escapes.
func (p *parser) quoted() (string, error) {
	if err := p.expect(`"`); err != nil {
		return "", err
	}
	var sb strings.Builder
	for {
		if p.eof() {
			return "", p.errf("unterminated quoted string")
		}
		c := p.buf[p.pos]
		switch c {
		case '"':
			p.pos++
			return sb.String(), nil
		case '\\':
			p.pos++
			if p.eof() {
				return "", p.errf("dangling escape in quoted string")
			}
			sb.WriteByte(p.buf[p.pos])
			p.pos++
		default:
			sb.WriteByte(c)
			p.pos++
		}
	}
}

// literal reads "{n}\r\n" + exactly n bytes already spliced into the buffer.
func (p *parser) literal() (string, error) {
	if err := p.expect("{"); err != nil {
		return "", err
	}
	n, err := p.number()
	if err != nil {
		return "", err
	}
	if err := p.expect("}"); err != nil {
		return "", err
	}
	if err := p.crlf(); err != nil {
		return "", err
	}
	if p.pos+int(n) > len(p.buf) {
		return "", p.errf("literal of %d bytes runs past buffer", n)
	}
	s := p.buf[p.pos : p.pos+int(n)]
	p.pos += int(n)
	return s, nil
}

// mailbox reads a mailbox name: the case-insensitive atom "INBOX" maps to
// "INBOX" (MRI canonicalises it); otherwise an astring verbatim.
func (p *parser) mailbox() (string, error) {
	s, err := p.astring()
	if err != nil {
		return "", err
	}
	if strings.EqualFold(s, "INBOX") {
		return "INBOX", nil
	}
	return s, nil
}
