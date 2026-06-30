// Copyright (c) the go-ruby-net-imap/net-imap authors
//
// SPDX-License-Identifier: BSD-3-Clause

package imap

// This file gives ergonomic builders for the IMAP4rev1 commands, each mirroring
// the argument list MRI's Net::IMAP method passes to send_command. They are thin
// wrappers over Builder.Command, so the same atom/quoted/literal encoding rules
// apply (e.g. a mailbox with a space becomes a quoted string).

// Capability builds `TAG CAPABILITY`.
func (b *Builder) Capability() (Command, error) { return b.Command("CAPABILITY") }

// Noop builds `TAG NOOP`.
func (b *Builder) Noop() (Command, error) { return b.Command("NOOP") }

// Logout builds `TAG LOGOUT`.
func (b *Builder) Logout() (Command, error) { return b.Command("LOGOUT") }

// StartTLS builds `TAG STARTTLS`.
func (b *Builder) StartTLS() (Command, error) { return b.Command("STARTTLS") }

// Login builds `TAG LOGIN user pass` (each argument string-encoded).
func (b *Builder) Login(user, password string) (Command, error) {
	return b.Command("LOGIN", user, password)
}

// Expunge builds `TAG EXPUNGE`.
func (b *Builder) Expunge() (Command, error) { return b.Command("EXPUNGE") }

// Check builds `TAG CHECK`.
func (b *Builder) Check() (Command, error) { return b.Command("CHECK") }

// Close builds `TAG CLOSE`.
func (b *Builder) Close() (Command, error) { return b.Command("CLOSE") }

// Unselect builds `TAG UNSELECT` (RFC 3691).
func (b *Builder) Unselect() (Command, error) { return b.Command("UNSELECT") }

// Idle builds `TAG IDLE` (RFC 2177). The host writes Bytes, waits for the "+ "
// continuation, then later writes IdleDone() to end the idle.
func (b *Builder) Idle() (Command, error) { return b.Command("IDLE") }

// IdleDone returns the `DONE\r\n` line that terminates an IDLE (it is untagged
// and carries no tag, mirroring Net::IMAP#idle_done's put_string("DONE\r\n")).
func IdleDone() string { return "DONE" + CRLF }

// Authenticate builds `TAG AUTHENTICATE mech` for the named SASL mechanism
// (emitted as a raw atom, e.g. PLAIN, LOGIN, CRAM-MD5, XOAUTH2). The continuation
// exchange (base64 challenge/response) is driven by the host using the SASL
// encoders in sasl.go.
func (b *Builder) Authenticate(mechanism string) (Command, error) {
	return b.Command("AUTHENTICATE", RawData(mechanism))
}

// Select builds `TAG SELECT mailbox`.
func (b *Builder) Select(mailbox string) (Command, error) {
	return b.Command("SELECT", mailbox)
}

// Examine builds `TAG EXAMINE mailbox`.
func (b *Builder) Examine(mailbox string) (Command, error) {
	return b.Command("EXAMINE", mailbox)
}

// Create builds `TAG CREATE mailbox`.
func (b *Builder) Create(mailbox string) (Command, error) {
	return b.Command("CREATE", mailbox)
}

// Delete builds `TAG DELETE mailbox`.
func (b *Builder) Delete(mailbox string) (Command, error) {
	return b.Command("DELETE", mailbox)
}

// Rename builds `TAG RENAME old new`.
func (b *Builder) Rename(oldName, newName string) (Command, error) {
	return b.Command("RENAME", oldName, newName)
}

// Subscribe builds `TAG SUBSCRIBE mailbox`.
func (b *Builder) Subscribe(mailbox string) (Command, error) {
	return b.Command("SUBSCRIBE", mailbox)
}

// Unsubscribe builds `TAG UNSUBSCRIBE mailbox`.
func (b *Builder) Unsubscribe(mailbox string) (Command, error) {
	return b.Command("UNSUBSCRIBE", mailbox)
}

// List builds `TAG LIST refname mailbox` (the mailbox glob).
func (b *Builder) List(refName, mailbox string) (Command, error) {
	return b.Command("LIST", refName, mailbox)
}

// Lsub builds `TAG LSUB refname mailbox`.
func (b *Builder) Lsub(refName, mailbox string) (Command, error) {
	return b.Command("LSUB", refName, mailbox)
}

// Status builds `TAG STATUS mailbox (ATTR …)` from the requested status items.
// Each item is emitted as a raw atom (MESSAGES, UIDNEXT, …).
func (b *Builder) Status(mailbox string, attrs ...string) (Command, error) {
	list := make([]Argument, len(attrs))
	for i, a := range attrs {
		list[i] = RawData(a)
	}
	return b.Command("STATUS", mailbox, list)
}

// Fetch builds `TAG FETCH set (att …)`. set is the sequence set; atts is the
// fetch attribute list, each emitted as a raw atom (FLAGS, UID, BODY[TEXT], …).
func (b *Builder) Fetch(set *SequenceSet, atts ...string) (Command, error) {
	return b.fetch("FETCH", set, atts)
}

// UIDFetch builds `TAG UID FETCH set (att …)`.
func (b *Builder) UIDFetch(set *SequenceSet, atts ...string) (Command, error) {
	return b.fetch("UID FETCH", set, atts)
}

func (b *Builder) fetch(cmd string, set *SequenceSet, atts []string) (Command, error) {
	list := make([]Argument, len(atts))
	for i, a := range atts {
		list[i] = RawData(a)
	}
	return b.Command(cmd, set, list)
}

// Store builds `TAG STORE set item value`. item is e.g. "+FLAGS" or "FLAGS";
// flags is the list of flags, emitted as a parenthesised flag list.
func (b *Builder) Store(set *SequenceSet, item string, flags []Flag) (Command, error) {
	return b.store("STORE", set, item, flags)
}

// UIDStore builds `TAG UID STORE set item value`.
func (b *Builder) UIDStore(set *SequenceSet, item string, flags []Flag) (Command, error) {
	return b.store("UID STORE", set, item, flags)
}

func (b *Builder) store(cmd string, set *SequenceSet, item string, flags []Flag) (Command, error) {
	list := make([]Argument, len(flags))
	for i, f := range flags {
		list[i] = f
	}
	return b.Command(cmd, set, RawData(item), list)
}

// Copy builds `TAG COPY set mailbox`.
func (b *Builder) Copy(set *SequenceSet, mailbox string) (Command, error) {
	return b.Command("COPY", set, mailbox)
}

// UIDCopy builds `TAG UID COPY set mailbox`.
func (b *Builder) UIDCopy(set *SequenceSet, mailbox string) (Command, error) {
	return b.Command("UID COPY", set, mailbox)
}

// Search builds `TAG SEARCH key …` from raw search-key tokens (e.g. "FROM",
// "smith", "SINCE", "1-Feb-2026"). Each token is emitted as a raw atom unless it
// is already an Argument (a QuotedString, Literal, *SequenceSet, …).
func (b *Builder) Search(keys ...Argument) (Command, error) {
	return b.searchCmd("SEARCH", keys)
}

// UIDSearch builds `TAG UID SEARCH key …`.
func (b *Builder) UIDSearch(keys ...Argument) (Command, error) {
	return b.searchCmd("UID SEARCH", keys)
}

func (b *Builder) searchCmd(cmd string, keys []Argument) (Command, error) {
	args := make([]Argument, len(keys))
	for i, k := range keys {
		if s, ok := k.(string); ok {
			args[i] = RawData(s)
		} else {
			args[i] = k
		}
	}
	return b.Command(cmd, args...)
}

// Append builds `TAG APPEND mailbox [(flags)] [datetime] {n}\r\n<message>`. When
// flags is non-empty it is emitted as a parenthesised flag list; when dt is
// non-nil it is emitted as a quoted date-time; the message is always a literal.
func (b *Builder) Append(mailbox string, flags []Flag, dt *Date, message string) (Command, error) {
	args := []Argument{mailbox}
	if len(flags) > 0 {
		list := make([]Argument, len(flags))
		for i, f := range flags {
			list[i] = f
		}
		args = append(args, list)
	}
	if dt != nil {
		args = append(args, *dt)
	}
	args = append(args, Literal(message))
	return b.Command("APPEND", args...)
}
