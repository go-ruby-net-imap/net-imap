// Copyright (c) the go-ruby-net-imap/net-imap authors
//
// SPDX-License-Identifier: BSD-3-Clause

package imap

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ErrInvalidData is returned by command builders for an argument outside the
// set of types IMAP commands can serialise.
var ErrInvalidData = errors.New("imap: invalid command data")

// Argument is a value usable as an IMAP command argument. The accepted dynamic
// types mirror MRI's send_data dispatch:
//
//	nil              -> NIL
//	string           -> atom / quoted-string / literal (per send_string_data)
//	int / int64      -> a number
//	[]Argument       -> a parenthesised list
//	time.Time        -> a quoted date-time ("DD-Mon-YYYY HH:MM:SS +ZZZZ")
//	Date             -> a quoted date (DD-Mon-YYYY)
//	Flag             -> a backslash-prefixed atom (\Seen)
//	Atom             -> a raw atom, emitted verbatim
//	QuotedString     -> always a quoted string
//	Literal          -> always a literal
//	RawData          -> emitted verbatim with no quoting
//	*SequenceSet     -> the coalesced sequence-set form (1:3,7)
type Argument = any

// Atom is a command argument emitted verbatim as an atom (Net::IMAP::Atom).
type Atom string

// QuotedString is a command argument always emitted as a quoted string
// (Net::IMAP::QuotedString).
type QuotedString string

// Literal is a command argument always emitted as a literal (Net::IMAP::Literal):
// `{n}\r\n` followed by the bytes (the host sends the bytes after the server's
// continuation request).
type Literal string

// RawData is a command argument emitted verbatim with no quoting
// (Net::IMAP::RawData).
type RawData string

// Date is a calendar date emitted as the IMAP `DD-Mon-YYYY` quoted date.
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

// Command is a fully-built tagged command ready to write to the socket. Bytes
// is the complete octet sequence ("TAG CMD args\r\n"); Tag is the tag the host
// must match against the eventual tagged response. When the command carries one
// or more literals, Bytes stops just after the first `{n}\r\n`; the remaining
// segments live in Literals and the host writes each after receiving a
// continuation request. For literal-free commands Literals is empty and Bytes
// is the whole line.
type Command struct {
	Tag      string
	Bytes    string
	Literals []LiteralSegment
}

// LiteralSegment is the literal payload plus the bytes that follow it (up to and
// including the next literal's `{n}\r\n`, or the trailing CRLF). The host writes
// Data after a continuation request, then writes Tail.
type LiteralSegment struct {
	Data string
	Tail string
}

// Builder assembles tagged command lines with monotonic tags, mirroring
// Net::IMAP#generate_tag ("<prefix><4-digit>"). The zero value is not usable;
// use NewBuilder.
type Builder struct {
	prefix string
	tagno  int
}

// NewBuilder returns a Builder issuing tags "<prefix>0001", "<prefix>0002", …
// MRI's default prefix is "RUBY".
func NewBuilder(prefix string) *Builder { return &Builder{prefix: prefix} }

// NextTag returns the next tag and advances the counter (Net::IMAP#generate_tag).
func (b *Builder) NextTag() string {
	b.tagno++
	return fmt.Sprintf("%s%04d", b.prefix, b.tagno)
}

// Command builds a tagged command line: TAG SP CMD (SP arg)* CRLF, encoding each
// argument per MRI's send_data. It returns ErrInvalidData for an unsupported
// argument type or an out-of-range integer.
func (b *Builder) Command(cmd string, args ...Argument) (Command, error) {
	tag := b.NextTag()
	return buildCommand(tag, cmd, args...)
}

// buildCommand is the tag-fixed core of Command, factored out for testing.
func buildCommand(tag, cmd string, args ...Argument) (Command, error) {
	var sb strings.Builder
	var lits []LiteralSegment
	// pending points at where subsequent bytes go: either the main builder, or
	// the Tail of the last literal segment.
	emit := func(s string) {
		if n := len(lits); n > 0 {
			lits[n-1].Tail += s
		} else {
			sb.WriteString(s)
		}
	}
	startLiteral := func(data string) {
		// The "{n}\r\n" prefix has already been emitted by encodeData; open a new
		// segment whose Data is the payload and whose Tail accumulates what follows.
		lits = append(lits, LiteralSegment{Data: data})
	}

	sb.WriteString(tag + " " + cmd)
	for _, a := range args {
		emit(" ")
		if err := encodeData(a, emit, startLiteral); err != nil {
			return Command{}, err
		}
	}
	emit(CRLF)
	return Command{Tag: tag, Bytes: sb.String(), Literals: lits}, nil
}

// ensureNumber mirrors NumValidator.ensure_number: IMAP numbers are unsigned
// 32-bit (number = 1*DIGIT, fitting nz-number / number ranges in the grammar).
func ensureNumber(n int64) error {
	if n < 0 || n > 1<<32-1 {
		return fmt.Errorf("%w: number %d out of range", ErrInvalidData, n)
	}
	return nil
}

// encodeData emits one argument's bytes, mirroring Net::IMAP#send_data (validate
// + emit, fused). emit appends ordinary bytes; startLiteral opens a literal
// segment after its "{n}\r\n" prefix has been emitted. It returns ErrInvalidData
// for an out-of-range integer, a nil *SequenceSet, or an unsupported type.
func encodeData(data any, emit func(string), startLiteral func(string)) error {
	switch d := data.(type) {
	case nil:
		emit("NIL")
	case string:
		encodeString(d, emit, startLiteral)
	case Atom:
		emit(string(d))
	case RawData:
		emit(string(d))
	case QuotedString:
		emit(quoteString(string(d)))
	case Literal:
		emit("{" + strconv.Itoa(len(d)) + "}" + CRLF)
		startLiteral(string(d))
	case Flag:
		emit("\\" + string(d))
	case int:
		if err := ensureNumber(int64(d)); err != nil {
			return err
		}
		emit(strconv.FormatInt(int64(d), 10))
	case int64:
		if err := ensureNumber(d); err != nil {
			return err
		}
		emit(strconv.FormatInt(d, 10))
	case time.Time:
		emit(encodeTime(d))
	case Date:
		emit(encodeDate(d))
	case *SequenceSet:
		if d == nil {
			return fmt.Errorf("%w: nil *SequenceSet", ErrInvalidData)
		}
		emit(d.String())
	case []Argument:
		emit("(")
		for i, item := range d {
			if i > 0 {
				emit(" ")
			}
			if err := encodeData(item, emit, startLiteral); err != nil {
				return err
			}
		}
		emit(")")
	default:
		return fmt.Errorf("%w: %T", ErrInvalidData, data)
	}
	return nil
}

// encodeString mirrors Net::IMAP#send_string_data: empty -> `""`; multiline or
// non-ASCII -> literal; bytes needing quoting -> quoted string; else a bare atom.
func encodeString(str string, emit func(string), startLiteral func(string)) {
	switch {
	case str == "":
		emit(`""`)
	case strings.ContainsAny(str, "\r\n"):
		emit("{" + strconv.Itoa(len(str)) + "}" + CRLF)
		startLiteral(str)
	case !isASCII(str):
		emit("{" + strconv.Itoa(len(str)) + "}" + CRLF)
		startLiteral(str)
	case needsQuoting(str):
		emit(quoteString(str))
	default:
		emit(str)
	}
}

// isASCII reports whether s is all 7-bit bytes.
func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// needsQuoting mirrors the /[(){ \x00-\x1f\x7f%*"\\]/n test in send_string_data.
func needsQuoting(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c <= 0x1f || c == 0x7f {
			return true
		}
		switch c {
		case '(', ')', '{', ' ', '%', '*', '"', '\\':
			return true
		}
	}
	return false
}

// quoteString mirrors Net::IMAP#send_quoted_string: wrap in double quotes,
// backslash-escaping `"` and `\`.
func quoteString(s string) string {
	var sb strings.Builder
	sb.WriteByte('"')
	for i := 0; i < len(s); i++ {
		if c := s[i]; c == '"' || c == '\\' {
			sb.WriteByte('\\')
		}
		sb.WriteByte(s[i])
	}
	sb.WriteByte('"')
	return sb.String()
}

var monthAbbr = [...]string{
	"Jan", "Feb", "Mar", "Apr", "May", "Jun",
	"Jul", "Aug", "Sep", "Oct", "Nov", "Dec",
}

// encodeDate mirrors Net::IMAP.encode_date: `DD-Mon-YYYY` (no quoting).
func encodeDate(d Date) string {
	return fmt.Sprintf("%d-%s-%04d", d.Day, monthAbbr[int(d.Month)-1], d.Year)
}

// encodeTime mirrors Net::IMAP.encode_time: a quoted
// `"DD-Mon-YYYY HH:MM:SS +ZZZZ"`.
func encodeTime(t time.Time) string {
	_, off := t.Zone()
	sign := '+'
	if off < 0 {
		sign = '-'
		off = -off
	}
	return fmt.Sprintf(`"%d-%s-%04d %02d:%02d:%02d %c%02d%02d"`,
		t.Day(), monthAbbr[int(t.Month())-1], t.Year(),
		t.Hour(), t.Minute(), t.Second(),
		sign, off/3600, (off%3600)/60)
}

// SequenceSet is a set of message sequence numbers or UIDs. It coalesces sorted
// values into ranges the way Net::IMAP::SequenceSet#valid_string does
// ([3,1,2] -> "1:3"). A value of 0 stands for "*" (the largest in the mailbox).
type SequenceSet struct {
	// entries is the normalised, sorted, de-overlapped list of ranges; a bound of
	// maxSeq renders as "*".
	entries []seqRange
}

// maxSeq is the concrete internal value of "*": it is one past the largest valid
// 32-bit sequence number, so MRI's rule that "*" is the largest message — and so
// an open range n:* absorbs any number >= n but a bare * stays disjoint from a
// smaller number — falls out of plain integer coalescing.
const maxSeq = int64(1) << 32

// star is maxSeq, the internal bound that renders as "*".
const star = maxSeq

type seqRange struct{ Lo, Hi int64 }

// SeqRange is a closed range [Lo, Hi] for NewSequenceSet. Use Star for "*".
type SeqRange struct{ Lo, Hi int64 }

// Star is the sequence value "*" (the largest message in the mailbox).
const Star = int64(0)

// NewSequenceSet builds a coalesced sequence set from individual numbers and
// ranges. Numbers are int / int64 (0 meaning "*"); ranges are SeqRange. It
// returns ErrInvalidData for an unsupported element or a number below 0.
func NewSequenceSet(items ...any) (*SequenceSet, error) {
	var raw []seqRange
	add := func(lo, hi int64) { raw = append(raw, seqRange{lo, hi}) }
	for _, it := range items {
		switch v := it.(type) {
		case int:
			n, err := seqVal(int64(v))
			if err != nil {
				return nil, err
			}
			add(n, n)
		case int64:
			n, err := seqVal(v)
			if err != nil {
				return nil, err
			}
			add(n, n)
		case SeqRange:
			lo, err := seqVal(v.Lo)
			if err != nil {
				return nil, err
			}
			hi, err := seqVal(v.Hi)
			if err != nil {
				return nil, err
			}
			if lo > hi {
				lo, hi = hi, lo
			}
			add(lo, hi)
		default:
			return nil, fmt.Errorf("%w: sequence element %T", ErrInvalidData, it)
		}
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("%w: empty sequence set", ErrInvalidData)
	}
	return &SequenceSet{entries: coalesce(raw)}, nil
}

// seqVal maps an exposed sequence value to its internal form: 0 -> star
// (=maxSeq); negatives and out-of-range values are rejected.
func seqVal(n int64) (int64, error) {
	if n == Star {
		return star, nil
	}
	if n < 1 || n > 1<<32-1 {
		return 0, fmt.Errorf("%w: sequence value %d out of range", ErrInvalidData, n)
	}
	return n, nil
}

// coalesce sorts and merges adjacent/overlapping ranges by plain integer order
// (star == maxSeq sorts last by construction).
func coalesce(in []seqRange) []seqRange {
	sort.Slice(in, func(i, j int) bool {
		if in[i].Lo != in[j].Lo {
			return in[i].Lo < in[j].Lo
		}
		return in[i].Hi < in[j].Hi
	})
	var out []seqRange
	for _, r := range in {
		if len(out) == 0 {
			out = append(out, r)
			continue
		}
		last := &out[len(out)-1]
		if r.Lo <= last.Hi+1 { // overlapping or adjacent
			if r.Hi > last.Hi {
				last.Hi = r.Hi
			}
			continue
		}
		out = append(out, r)
	}
	return out
}

// String renders the set as the IMAP sequence-set form, e.g. "1:3,7,9:*".
func (s *SequenceSet) String() string {
	parts := make([]string, len(s.entries))
	for i, r := range s.entries {
		lo := seqStr(r.Lo)
		if r.Lo == r.Hi {
			parts[i] = lo
		} else {
			parts[i] = lo + ":" + seqStr(r.Hi)
		}
	}
	return strings.Join(parts, ",")
}

func seqStr(n int64) string {
	if n == star {
		return "*"
	}
	return strconv.FormatInt(n, 10)
}
