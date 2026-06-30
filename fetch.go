// Copyright (c) the go-ruby-net-imap/net-imap authors
//
// SPDX-License-Identifier: BSD-3-Clause

package imap

import "strings"

// fetchData parses `* n FETCH (att …)` after the seqno n has been read.
//
// Each msg-att value's Go type mirrors MRI's FetchData#attr:
//
//	FLAGS              -> []Flag
//	UID                -> int64
//	RFC822.SIZE        -> int64
//	INTERNALDATE       -> string (the quoted date verbatim)
//	ENVELOPE           -> *Envelope
//	BODYSTRUCTURE/BODY -> *BodyStructure (when the value is parenthesised)
//	BODY[...]/RFC822*  -> string ("" only when NIL)  (nstring section data)
//	MODSEQ             -> int64
func (p *parser) fetchData(seqno int64) (Response, error) {
	if err := p.sp(); err != nil {
		return nil, err
	}
	if err := p.expect("("); err != nil {
		return nil, err
	}
	attr := newOrderedAttr()
	for p.peek() != ')' {
		if attr.Len() > 0 {
			if err := p.sp(); err != nil {
				return nil, err
			}
		}
		name, val, err := p.msgAtt()
		if err != nil {
			return nil, err
		}
		attr.Set(name, val)
	}
	p.pos++ // ')' — the loop only exits on it
	if err := p.crlf(); err != nil {
		return nil, err
	}
	fd := &FetchData{Seqno: seqno, Attr: attr}
	return &UntaggedResponse{Name: "FETCH", Data: fd, RawData: p.buf}, nil
}

// msgAtt reads one "NAME value" attribute and returns its canonical name and
// parsed value.
func (p *parser) msgAtt() (string, any, error) {
	name, err := p.fetchAttName()
	if err != nil {
		return "", nil, err
	}
	upper := upcaseASCII(name)
	// Every msg-att is "att SP value"; the att name ends at the space, so consume
	// exactly one separator here (its error path is shared by all atts).
	if err := p.sp(); err != nil {
		return "", nil, err
	}
	switch {
	case upper == "FLAGS":
		fl, err := p.flagList()
		if err != nil {
			return "", nil, err
		}
		return upper, fl, nil
	case upper == "MODSEQ":
		// MODSEQ is wrapped in parens: "MODSEQ (12345)".
		if err := p.expect("("); err != nil {
			return "", nil, err
		}
		n, err := p.number()
		if err != nil {
			return "", nil, err
		}
		if err := p.expect(")"); err != nil {
			return "", nil, err
		}
		return upper, n, nil
	case upper == "UID", upper == "RFC822.SIZE":
		n, err := p.number()
		if err != nil {
			return "", nil, err
		}
		return upper, n, nil
	case upper == "INTERNALDATE":
		s, err := p.quoted()
		if err != nil {
			return "", nil, err
		}
		return upper, s, nil
	case upper == "ENVELOPE":
		env, err := p.envelope()
		if err != nil {
			return "", nil, err
		}
		return upper, env, nil
	case upper == "BODYSTRUCTURE", upper == "BODY":
		// A bare BODY / BODYSTRUCTURE introduces a body structure; a BODY[…]
		// section (the bracket was folded into the name) is an nstring, handled
		// by the default arm because upper then contains the "[".
		bs, err := p.bodyStructure()
		if err != nil {
			return "", nil, err
		}
		return upper, bs, nil
	default:
		// nstring-valued atts: RFC822, RFC822.HEADER, RFC822.TEXT, BODY[...].
		ns, err := p.nstring()
		if err != nil {
			return "", nil, err
		}
		return name, ns.str, nil
	}
}

// fetchAttName reads a fetch attribute name, including a BODY[...] / BODY.PEEK[...]
// section. MRI normalises the section by stripping quotes from header-field names
// inside it, e.g. BODY[HEADER.FIELDS ("DATE" "FROM")] -> BODY[HEADER.FIELDS (DATE FROM)].
func (p *parser) fetchAttName() (string, error) {
	// The att name is letters/digits/'.' up to a '[' (the optional section) or a
	// space; '[' is not an atom-special in IMAP, so read the name ourselves.
	start := p.pos
	for !p.eof() {
		c := p.buf[p.pos]
		if c == '[' || c == ' ' || c == ')' {
			break
		}
		if !isAtomChar(c) {
			break
		}
		p.pos++
	}
	if p.pos == start {
		return "", p.errf("expected fetch attribute name")
	}
	atom := p.buf[start:p.pos]
	if p.peek() != '[' {
		return atom, nil
	}
	// Read the bracketed section, normalising quoted header-field names.
	var sb strings.Builder
	sb.WriteString(atom)
	sb.WriteByte('[')
	p.pos++ // consume '['
	for {
		if p.eof() {
			return "", p.errf("unterminated fetch section")
		}
		c := p.buf[p.pos]
		switch c {
		case ']':
			p.pos++
			sb.WriteByte(']')
			// optional "<partial>" origin octet, e.g. BODY[]<0.1024>.
			if p.peek() == '<' {
				sb.WriteByte('<')
				p.pos++
				for !p.eof() && p.buf[p.pos] != '>' {
					sb.WriteByte(p.buf[p.pos])
					p.pos++
				}
				if err := p.expect(">"); err != nil {
					return "", err
				}
				sb.WriteByte('>')
			}
			return sb.String(), nil
		case '"':
			s, err := p.quoted()
			if err != nil {
				return "", err
			}
			sb.WriteString(s) // dropped quotes, matching MRI
		default:
			sb.WriteByte(c)
			p.pos++
		}
	}
}

// envelope parses the parenthesised ENVELOPE structure into *Envelope.
func (p *parser) envelope() (*Envelope, error) {
	if err := p.expect("("); err != nil {
		return nil, err
	}
	date, err := p.nstring()
	if err != nil {
		return nil, err
	}
	subject, err := p.spNstring()
	if err != nil {
		return nil, err
	}
	from, err := p.spAddrList()
	if err != nil {
		return nil, err
	}
	sender, err := p.spAddrList()
	if err != nil {
		return nil, err
	}
	replyTo, err := p.spAddrList()
	if err != nil {
		return nil, err
	}
	to, err := p.spAddrList()
	if err != nil {
		return nil, err
	}
	cc, err := p.spAddrList()
	if err != nil {
		return nil, err
	}
	bcc, err := p.spAddrList()
	if err != nil {
		return nil, err
	}
	inReplyTo, err := p.spNstring()
	if err != nil {
		return nil, err
	}
	messageID, err := p.spNstring()
	if err != nil {
		return nil, err
	}
	if err := p.expect(")"); err != nil {
		return nil, err
	}
	return &Envelope{
		Date: date.str, Subject: subject.str,
		From: from, Sender: sender, ReplyTo: replyTo,
		To: to, Cc: cc, Bcc: bcc,
		InReplyTo: inReplyTo.str, MessageID: messageID.str,
	}, nil
}

// spNstring consumes a separating space then an nstring.
func (p *parser) spNstring() (nstr, error) {
	if err := p.sp(); err != nil {
		return nstr{}, err
	}
	return p.nstring()
}

// spAddrList consumes a separating space then an address list (or NIL).
func (p *parser) spAddrList() ([]Address, error) {
	if err := p.sp(); err != nil {
		return nil, err
	}
	if p.matchNIL() {
		return nil, nil
	}
	if err := p.expect("("); err != nil {
		return nil, err
	}
	var addrs []Address
	for p.peek() == '(' {
		a, err := p.address()
		if err != nil {
			return nil, err
		}
		addrs = append(addrs, a)
	}
	if err := p.expect(")"); err != nil {
		return nil, err
	}
	return addrs, nil
}

// address parses one "(name route mailbox host)" tuple. The caller only enters
// when the next byte is '(', so it is consumed directly.
func (p *parser) address() (Address, error) {
	p.pos++ // '('
	name, err := p.nstring()
	if err != nil {
		return Address{}, err
	}
	route, err := p.spNstring()
	if err != nil {
		return Address{}, err
	}
	mailbox, err := p.spNstring()
	if err != nil {
		return Address{}, err
	}
	host, err := p.spNstring()
	if err != nil {
		return Address{}, err
	}
	if err := p.expect(")"); err != nil {
		return Address{}, err
	}
	return Address{Name: name.str, Route: route.str, Mailbox: mailbox.str, Host: host.str}, nil
}
