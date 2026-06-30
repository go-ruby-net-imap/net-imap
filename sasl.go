// Copyright (c) the go-ruby-net-imap/net-imap authors
//
// SPDX-License-Identifier: BSD-3-Clause

package imap

import (
	"crypto/hmac"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
)

// This file gives the deterministic SASL response encoders Net::IMAP uses for the
// AUTHENTICATE exchange (Net::IMAP::SASL::*Authenticator). These are pure
// transformations of credentials (and, for challenge-response mechanisms, the
// server's challenge); the surrounding continuation I/O is the host's job. Each
// function returns the *raw* SASL response octets; the caller base64-encodes them
// for the wire with SASLEncode (PLAIN/LOGIN/CRAM-MD5/XOAUTH2 all transmit base64).

// SASLEncode base64-encodes a raw SASL response for the wire, mirroring the
// strict (no-newline) base64 Net::IMAP writes after a continuation request.
func SASLEncode(raw string) string {
	return base64.StdEncoding.EncodeToString([]byte(raw))
}

// SASLPlain returns the raw PLAIN initial response (RFC 4616): the octets
// authzid NUL authcid NUL passwd. With an empty authzid (the common case) this is
// "\0user\0pass". Encode with SASLEncode for the wire.
func SASLPlain(authzid, username, password string) string {
	return authzid + "\x00" + username + "\x00" + password
}

// SASLLoginUser returns the raw LOGIN first response: the username verbatim
// (Net::IMAP::SASL::LoginAuthenticator's first process result). Encode with
// SASLEncode for the wire.
func SASLLoginUser(username string) string { return username }

// SASLLoginPassword returns the raw LOGIN second response: the password verbatim.
// Encode with SASLEncode for the wire.
func SASLLoginPassword(password string) string { return password }

// SASLCramMD5 returns the raw CRAM-MD5 response (RFC 2195) for the given decoded
// server challenge: "username SP hex(HMAC-MD5(challenge, password))". The caller
// base64-decodes the server challenge first, then base64-encodes this result with
// SASLEncode.
func SASLCramMD5(username, password, challenge string) string {
	mac := hmac.New(md5.New, []byte(password))
	mac.Write([]byte(challenge))
	return username + " " + hex.EncodeToString(mac.Sum(nil))
}

// SASLXOAuth2 returns the raw XOAUTH2 initial response: the octets
// "user=USER\x01auth=Bearer TOKEN\x01\x01". Encode with SASLEncode for the wire.
func SASLXOAuth2(username, token string) string {
	return "user=" + username + "\x01auth=Bearer " + token + "\x01\x01"
}
