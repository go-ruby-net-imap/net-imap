// Copyright (c) the go-ruby-net-imap/net-imap authors
//
// SPDX-License-Identifier: BSD-3-Clause

package imap

import (
	"fmt"
	"time"
)

// This file gives the public date/time-formatting and message-set helpers that
// mirror the Net::IMAP module methods of the same names.

// FormatDate mirrors Net::IMAP.format_date: an unquoted `DD-Mon-YYYY` (e.g.
// "30-Jun-2026"), used for the SEARCH date keys.
func FormatDate(t time.Time) string {
	return fmt.Sprintf("%d-%s-%04d", t.Day(), monthAbbr[int(t.Month())-1], t.Year())
}

// FormatDatetime mirrors Net::IMAP.format_datetime: an unquoted
// `DD-Mon-YYYY HH:MM +ZZZZ` (minute precision, no seconds), distinct from the
// APPEND date-time which carries seconds. It uses the time's own zone offset.
func FormatDatetime(t time.Time) string {
	_, off := t.Zone()
	sign := '+'
	if off < 0 {
		sign = '-'
		off = -off
	}
	return fmt.Sprintf("%d-%s-%04d %02d:%02d %c%02d%02d",
		t.Day(), monthAbbr[int(t.Month())-1], t.Year(),
		t.Hour(), t.Minute(),
		sign, off/3600, (off%3600)/60)
}

// MessageSet renders a list of message sequence numbers / UIDs as the IMAP
// sequence-set string, mirroring Net::IMAP::MessageSet#to_s (it is a thin alias
// over SequenceSet for the FETCH/STORE/COPY set argument). 0 means "*". It returns
// ErrInvalidData for an out-of-range value.
func MessageSet(nums ...int64) (string, error) {
	items := make([]any, len(nums))
	for i, n := range nums {
		items[i] = n
	}
	ss, err := NewSequenceSet(items...)
	if err != nil {
		return "", err
	}
	return ss.String(), nil
}
