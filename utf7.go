// Copyright (c) the go-ruby-net-imap/net-imap authors
//
// SPDX-License-Identifier: BSD-3-Clause

package imap

import (
	"strings"
	"unicode/utf16"
)

// This file implements modified UTF-7 (RFC 3501 §5.1.3), the mailbox-name
// encoding Net::IMAP.encode_utf7 / decode_utf7 use. It differs from RFC 2152
// UTF-7 in two ways: "&" is the shift character instead of "+", and "," replaces
// "/" in the base64 alphabet (so mailbox names need no further quoting). A literal
// "&" encodes as "&-".

// utf7Alphabet is the modified-BASE64 alphabet: standard base64 with '/' -> ','.
const utf7Alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+,"

var utf7Decode [256]int8

func init() {
	for i := range utf7Decode {
		utf7Decode[i] = -1
	}
	for i := 0; i < len(utf7Alphabet); i++ {
		utf7Decode[utf7Alphabet[i]] = int8(i)
	}
}

// EncodeUTF7 encodes a UTF-8 string to a modified-UTF-7 mailbox name, mirroring
// Net::IMAP.encode_utf7. Printable ASCII (0x20–0x7e) passes through, with a bare
// "&" doubled to "&-"; runs of other characters are shifted into a "&…-" block of
// modified base64 over their UTF-16BE code units.
func EncodeUTF7(s string) string {
	var b strings.Builder
	runes := []rune(s)
	for i := 0; i < len(runes); {
		r := runes[i]
		if r == '&' {
			b.WriteString("&-")
			i++
			continue
		}
		if r >= 0x20 && r <= 0x7e {
			b.WriteRune(r)
			i++
			continue
		}
		// Collect a maximal run of non-printable-ASCII runes and shift-encode it.
		j := i
		for j < len(runes) && !(runes[j] >= 0x20 && runes[j] <= 0x7e) && runes[j] != '&' {
			j++
		}
		b.WriteByte('&')
		b.WriteString(encodeModBase64(runes[i:j]))
		b.WriteByte('-')
		i = j
	}
	return b.String()
}

// encodeModBase64 encodes a run of runes as modified base64 over their UTF-16BE
// units (no padding).
func encodeModBase64(runes []rune) string {
	units := utf16.Encode(runes)
	bytes := make([]byte, 0, len(units)*2)
	for _, u := range units {
		bytes = append(bytes, byte(u>>8), byte(u))
	}
	var out strings.Builder
	var acc uint32
	var bits int
	for _, by := range bytes {
		acc = acc<<8 | uint32(by)
		bits += 8
		for bits >= 6 {
			bits -= 6
			out.WriteByte(utf7Alphabet[(acc>>uint(bits))&0x3f])
		}
	}
	if bits > 0 {
		out.WriteByte(utf7Alphabet[(acc<<uint(6-bits))&0x3f])
	}
	return out.String()
}

// DecodeUTF7 decodes a modified-UTF-7 mailbox name to UTF-8, mirroring
// Net::IMAP.decode_utf7. "&-" decodes to a literal "&"; "&…-" decodes its
// modified-base64 payload as UTF-16BE; everything else passes through.
func DecodeUTF7(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '&' {
			b.WriteByte(s[i])
			i++
			continue
		}
		// Find the closing '-' (or end of string).
		j := i + 1
		for j < len(s) && s[j] != '-' {
			j++
		}
		payload := s[i+1 : j]
		if payload == "" {
			b.WriteByte('&') // "&-" -> "&"
		} else {
			b.WriteString(decodeModBase64(payload))
		}
		i = j + 1 // skip the '-' (or run off the end harmlessly)
		if j >= len(s) {
			i = j
		}
	}
	return b.String()
}

// decodeModBase64 decodes a modified-base64 payload into UTF-8 (via UTF-16BE).
// Invalid alphabet bytes are skipped, mirroring the gem's lenient behaviour.
func decodeModBase64(p string) string {
	var bytes []byte
	var acc uint32
	var bits int
	for i := 0; i < len(p); i++ {
		v := utf7Decode[p[i]]
		if v < 0 {
			continue
		}
		acc = acc<<6 | uint32(v)
		bits += 6
		if bits >= 8 {
			bits -= 8
			bytes = append(bytes, byte(acc>>uint(bits)))
		}
	}
	units := make([]uint16, 0, len(bytes)/2)
	for i := 0; i+1 < len(bytes); i += 2 {
		units = append(units, uint16(bytes[i])<<8|uint16(bytes[i+1]))
	}
	return string(utf16.Decode(units))
}
