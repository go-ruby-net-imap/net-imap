// Copyright (c) the go-ruby-net-imap/net-imap authors
//
// SPDX-License-Identifier: BSD-3-Clause

// Package imap is a pure-Go (CGO-free) reimplementation of the deterministic,
// interpreter-independent core of Ruby's Net::IMAP (MRI 4.0.5,
// net-imap 0.6.2): the IMAP4rev1 command builder and the response-grammar
// parser. The socket and TLS transport is a seam the host (go-embedded-ruby /
// rbgo) supplies; this package never opens a connection.
//
// # What it is — and isn't
//
// Building a tagged command line (atom / quoted-string / literal argument
// encoding, sequence-sets, fetch-att lists) and parsing the IMAP response
// grammar (tagged / untagged / continuation responses, parenthesised lists,
// literals, NIL, numbers, quoted strings, the FETCH attribute and ENVELOPE
// grammars, resp-text-codes) is fully deterministic and needs no interpreter,
// so it lives here as pure Go. Reading bytes off a TLS socket — including
// reading exactly N bytes for a literal — is the host's job; the [Reader] takes
// a line-reader and a literal-reader callback, and the host wires those to its
// own connection.
package imap

import "strings"

// CRLF is the IMAP line terminator.
const CRLF = "\r\n"

// ResponseText is the human-readable text of a status response, with an
// optional bracketed [resp-text-code]. It mirrors Net::IMAP::ResponseText
// (members: code, text).
type ResponseText struct {
	Code *ResponseCode
	Text string
}

// ResponseCode is a bracketed response code such as [ALERT],
// [UIDVALIDITY 1234] or [PERMANENTFLAGS (\Deleted \Seen \*)]. It mirrors
// Net::IMAP::ResponseCode (members: name, data). Data is nil, an int64, a
// string, a []int64, or a []Flag depending on the code.
type ResponseCode struct {
	Name string
	Data any
}

// AppendUIDData is the data of an [APPENDUID uidvalidity uid] response code,
// mirroring Net::IMAP::AppendUIDData (members: uidvalidity, assigned_uids).
// AssignedUIDs is the sequence-set string the server reported (e.g. "3955").
type AppendUIDData struct {
	UIDValidity  int64
	AssignedUIDs string
}

// CopyUIDData is the data of a [COPYUID uidvalidity src dst] response code,
// mirroring Net::IMAP::CopyUIDData (members: uidvalidity, source_uids,
// assigned_uids). The two UID sets are the sequence-set strings the server sent.
type CopyUIDData struct {
	UIDValidity  int64
	SourceUIDs   string
	AssignedUIDs string
}

// TaggedResponse is a tagged status response (`a001 OK …`). It mirrors
// Net::IMAP::TaggedResponse (members: tag, name, data, raw_data).
type TaggedResponse struct {
	Tag     string
	Name    string
	Data    *ResponseText
	RawData string
}

// UntaggedResponse is an untagged response (`* …`). It mirrors
// Net::IMAP::UntaggedResponse (members: name, data, raw_data). Data's concrete
// type depends on Name: int64 for EXISTS/RECENT/EXPUNGE, *ResponseText for
// OK/NO/BAD/BYE/PREAUTH, []Flag for FLAGS, *MailboxList for LIST/LSUB,
// *StatusData for STATUS, []int64 for SEARCH, []string for CAPABILITY, and
// *FetchData for FETCH.
type UntaggedResponse struct {
	Name    string
	Data    any
	RawData string
}

// ContinuationRequest is a command continuation request (`+ …`). It mirrors
// Net::IMAP::ContinuationRequest (members: data, raw_data).
type ContinuationRequest struct {
	Data    *ResponseText
	RawData string
}

// Flag is a system or keyword flag. System flags are reported by their bare
// name without the leading backslash and capitalised like Ruby's symbol
// (`\Seen` -> Flag("Seen")); the wildcard `\*` is Flag("*"). Keyword flags
// (no backslash) are reported verbatim.
type Flag string

// MailboxList is the data of a LIST or LSUB response. It mirrors
// Net::IMAP::MailboxList (members: attr, delim, name).
type MailboxList struct {
	Attr  []Flag
	Delim string // hierarchy delimiter, or "" for NIL
	Name  string
}

// StatusData is the data of a STATUS response. It mirrors
// Net::IMAP::StatusData (members: mailbox, attr). Attr maps each requested
// status item (MESSAGES, RECENT, UIDNEXT, UIDVALIDITY, UNSEEN, …) to its
// integer value, preserving the order the server sent them.
type StatusData struct {
	Mailbox string
	Attr    *OrderedInts
}

// FetchData is the data of a FETCH response. It mirrors Net::IMAP::FetchData
// (members: seqno, attr). Attr maps each fetch attribute name to its parsed
// value (see ParseResponse for the per-attribute value types), preserving the
// order the server sent them.
type FetchData struct {
	Seqno int64
	Attr  *OrderedAttr
}

// Address is one address from an ENVELOPE. It mirrors Net::IMAP::Address
// (members: name, route, mailbox, host). A field is "" where the wire form was
// NIL.
type Address struct {
	Name    string
	Route   string
	Mailbox string
	Host    string
}

// Envelope is the parsed ENVELOPE fetch attribute. It mirrors
// Net::IMAP::Envelope (members: date, subject, from, sender, reply_to, to, cc,
// bcc, in_reply_to, message_id). String members are "" where the wire form was
// NIL; address-list members are nil where the wire form was NIL.
type Envelope struct {
	Date      string
	Subject   string
	From      []Address
	Sender    []Address
	ReplyTo   []Address
	To        []Address
	Cc        []Address
	Bcc       []Address
	InReplyTo string
	MessageID string
}

// OrderedAttr is an insertion-ordered string-keyed map of FETCH attributes,
// mirroring the Ruby Hash that Net::IMAP::FetchData#attr returns (key order is
// the server's order).
type OrderedAttr struct {
	keys []string
	vals map[string]any
}

func newOrderedAttr() *OrderedAttr { return &OrderedAttr{vals: map[string]any{}} }

// Set inserts or replaces key. A repeated key keeps its original position.
func (o *OrderedAttr) Set(key string, val any) {
	if _, ok := o.vals[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = val
}

// Get returns the value for key and whether it was present.
func (o *OrderedAttr) Get(key string) (any, bool) { v, ok := o.vals[key]; return v, ok }

// Keys returns the attribute names in insertion order.
func (o *OrderedAttr) Keys() []string { return o.keys }

// Len reports the number of attributes.
func (o *OrderedAttr) Len() int { return len(o.keys) }

// OrderedInts is an insertion-ordered string-keyed map of integers, used for
// the STATUS attr hash.
type OrderedInts struct {
	keys []string
	vals map[string]int64
}

func newOrderedInts() *OrderedInts { return &OrderedInts{vals: map[string]int64{}} }

// Set inserts or replaces key, keeping an existing key's position.
func (o *OrderedInts) Set(key string, val int64) {
	if _, ok := o.vals[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = val
}

// Get returns the value for key and whether it was present.
func (o *OrderedInts) Get(key string) (int64, bool) { v, ok := o.vals[key]; return v, ok }

// Keys returns the item names in insertion order.
func (o *OrderedInts) Keys() []string { return o.keys }

// Len reports the number of items.
func (o *OrderedInts) Len() int { return len(o.keys) }

// upcaseASCII upper-cases the ASCII letters of s, matching the way MRI upcases
// capability tokens and atoms (response names) without touching non-ASCII bytes.
func upcaseASCII(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' {
			return r - ('a' - 'A')
		}
		return r
	}, s)
}
