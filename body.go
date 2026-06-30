// Copyright (c) the go-ruby-net-imap/net-imap authors
//
// SPDX-License-Identifier: BSD-3-Clause

package imap

// BodyStructure is the parsed BODY / BODYSTRUCTURE of a message part.
//
// # Boundary versus MRI
//
// MRI builds a tower of typed structs (BodyTypeText, BodyTypeBasic,
// BodyTypeMessage, BodyTypeMultipart, …) and assigns each body-fld-* slot a
// named member. This package instead returns one uniform, fully-recursive tree:
// every field of the body grammar is captured (so no information is lost), but
// the dispatch to a specific Ruby struct class — and the field-name mapping of
// the per-type extension data — is left to the host (rbgo), which knows the Ruby
// class registry. Parts is non-nil only for a multipart body; for a singlepart
// body the leading nstring/number fields populate the named slots and Extension
// holds the remaining body-ext-* items verbatim as a generic list.
//
// This is the one documented-partial attribute: the parse is complete and
// loss-free, but it is not mapped to MRI's BodyType* struct names here.
type BodyStructure struct {
	// Multipart is true when the body is a multipart/* (a parenthesised list of
	// nested parts followed by the subtype).
	Multipart bool

	// Parts holds the nested parts of a multipart body (nil for singlepart).
	Parts []*BodyStructure
	// MultipartSubtype is the subtype of a multipart body ("MIXED", "ALTERNATIVE").
	MultipartSubtype string

	// Singlepart fields (the body-type-1part grammar):
	MediaType   string // body-fld-type, e.g. "TEXT" / "IMAGE" / "MESSAGE"
	Subtype     string // body-fld-subtype, e.g. "PLAIN"
	Params      *OrderedAttr
	ContentID   string
	Description string
	Encoding    string
	Size        int64
	Lines       int64 // text body line count (TEXT subtype only); -1 if absent

	// Extension holds any remaining body-ext-* items (md5, disposition,
	// language, location, and the per-type extension data) as a generic list of
	// parsed values, in wire order.
	Extension []any
}

// BodyType returns the name of the MRI Net::IMAP struct class this body maps to,
// driving the same dispatch ResponseParser does: "BodyTypeMultipart" for a
// multipart; "BodyTypeMessage" for a MESSAGE/RFC822 part; "BodyTypeText" for a
// TEXT/* part; otherwise "BodyTypeBasic". rbgo uses this to instantiate the right
// Ruby class over the loss-free field tree.
func (b *BodyStructure) BodyType() string {
	if b.Multipart {
		return "BodyTypeMultipart"
	}
	switch upcaseASCII(b.MediaType) {
	case "MESSAGE":
		if upcaseASCII(b.Subtype) == "RFC822" {
			return "BodyTypeMessage"
		}
		return "BodyTypeBasic"
	case "TEXT":
		return "BodyTypeText"
	default:
		return "BodyTypeBasic"
	}
}

// bodyStructure parses a "(...)" body / body-structure into *BodyStructure.
func (p *parser) bodyStructure() (*BodyStructure, error) {
	if err := p.expect("("); err != nil {
		return nil, err
	}
	if p.peek() == '(' {
		return p.multipartBody()
	}
	return p.singlepartBody()
}

func (p *parser) multipartBody() (*BodyStructure, error) {
	bs := &BodyStructure{Multipart: true}
	for p.peek() == '(' {
		part, err := p.bodyStructure()
		if err != nil {
			return nil, err
		}
		bs.Parts = append(bs.Parts, part)
	}
	if err := p.sp(); err != nil {
		return nil, err
	}
	sub, err := p.str()
	if err != nil {
		return nil, err
	}
	bs.MultipartSubtype = sub
	// Optional multipart extension data (params, disposition, …) up to ')'.
	for p.peek() == ' ' {
		p.pos++
		v, err := p.bodyExtItem()
		if err != nil {
			return nil, err
		}
		bs.Extension = append(bs.Extension, v)
	}
	if err := p.expect(")"); err != nil {
		return nil, err
	}
	return bs, nil
}

func (p *parser) singlepartBody() (*BodyStructure, error) {
	bs := &BodyStructure{Lines: -1}
	mtype, err := p.str()
	if err != nil {
		return nil, err
	}
	bs.MediaType = mtype
	if err := p.sp(); err != nil {
		return nil, err
	}
	sub, err := p.str()
	if err != nil {
		return nil, err
	}
	bs.Subtype = sub
	if err := p.sp(); err != nil {
		return nil, err
	}
	params, err := p.bodyParams()
	if err != nil {
		return nil, err
	}
	bs.Params = params
	id, err := p.spNstring()
	if err != nil {
		return nil, err
	}
	bs.ContentID = id.str
	desc, err := p.spNstring()
	if err != nil {
		return nil, err
	}
	bs.Description = desc.str
	enc, err := p.spNstring()
	if err != nil {
		return nil, err
	}
	bs.Encoding = enc.str
	if err := p.sp(); err != nil {
		return nil, err
	}
	size, err := p.number()
	if err != nil {
		return nil, err
	}
	bs.Size = size
	// TEXT bodies carry a trailing line count.
	if upcaseASCII(bs.MediaType) == "TEXT" {
		if err := p.sp(); err != nil {
			return nil, err
		}
		lines, err := p.number()
		if err != nil {
			return nil, err
		}
		bs.Lines = lines
	}
	// Remaining body-ext-1part items (md5, disposition, language, location, …).
	for p.peek() == ' ' {
		p.pos++
		v, err := p.bodyExtItem()
		if err != nil {
			return nil, err
		}
		bs.Extension = append(bs.Extension, v)
	}
	if err := p.expect(")"); err != nil {
		return nil, err
	}
	return bs, nil
}

// bodyParams reads the body-fld-param: "(k v k v …)" or NIL.
func (p *parser) bodyParams() (*OrderedAttr, error) {
	if p.matchNIL() {
		return nil, nil
	}
	if err := p.expect("("); err != nil {
		return nil, err
	}
	params := newOrderedAttr()
	for p.peek() != ')' {
		if params.Len() > 0 {
			if err := p.sp(); err != nil {
				return nil, err
			}
		}
		k, err := p.str()
		if err != nil {
			return nil, err
		}
		if err := p.sp(); err != nil {
			return nil, err
		}
		v, err := p.nstring()
		if err != nil {
			return nil, err
		}
		params.Set(k, v.str)
	}
	p.pos++ // ')' — the loop only exits on it
	return params, nil
}

// bodyExtItem reads one generic body-extension item: NIL, a number, a string, or
// a parenthesised list (recursively). It is loss-free but untyped.
func (p *parser) bodyExtItem() (any, error) {
	switch {
	case p.matchNIL():
		return nil, nil
	case p.peek() == '(':
		p.pos++
		var list []any
		for p.peek() != ')' {
			if len(list) > 0 {
				if err := p.sp(); err != nil {
					return nil, err
				}
			}
			v, err := p.bodyExtItem()
			if err != nil {
				return nil, err
			}
			list = append(list, v)
		}
		p.pos++ // ')' — the loop only exits on it
		return list, nil
	case isDigit(p.peek()):
		return p.number()
	default:
		return p.str()
	}
}
