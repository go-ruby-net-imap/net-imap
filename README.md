<p align="center"><img src="https://raw.githubusercontent.com/go-ruby-net-imap/brand/main/social/go-ruby-net-imap-net-imap.png" alt="go-ruby-net-imap/net-imap" width="720"></p>

# net-imap — go-ruby-net-imap

[![Docs](https://img.shields.io/badge/docs-mkdocs--material-DC2626)](https://go-ruby-net-imap.github.io/docs/)
[![License](https://img.shields.io/badge/license-BSD--3--Clause-blue)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.26.4%2B-00ADD8)](https://go.dev/dl/)
[![Coverage](https://img.shields.io/badge/coverage-100%25-1a7f37)](#tests--coverage)

**A pure-Go (no cgo) reimplementation of the deterministic, interpreter-independent
core of Ruby's [`Net::IMAP`](https://docs.ruby-lang.org/en/master/Net/IMAP.html)**
— the IMAP4rev1 command builder and the response-grammar parser from MRI 4.0.5
(`net-imap` 0.6.2). It builds the exact tagged-command octets a client sends and
parses the untagged / tagged / continuation responses a server returns into a
typed value model — **without any Ruby runtime, and without opening a socket**.

It is the IMAP backend for
[go-embedded-ruby](https://github.com/go-embedded-ruby/ruby), but is a
**standalone, reusable** module — a sibling of
[go-ruby-net-smtp](https://github.com/go-ruby-net-smtp/net-smtp) and
[go-ruby-regexp](https://github.com/go-ruby-regexp/regexp).

> **What it is — and isn't — the socket/TLS seam.** Encoding a command line
> (atom / quoted-string / literal argument encoding, sequence-sets, fetch-att
> lists) and parsing the IMAP response grammar (parenthesised lists, literals,
> `NIL`, numbers, quoted strings, the FETCH attribute and `ENVELOPE` grammars,
> resp-text-codes) is fully deterministic and needs no interpreter, so it lives
> here as pure Go. Reading bytes off a TLS socket — including reading exactly *N*
> bytes for a literal — is the **host's job**: the [`Reader`](#the-socket--literal-seam)
> takes a line-reader and a literal-reader callback, and the host (rbgo) wires
> those to its own connection.

## Features

Faithful port of `Net::IMAP`'s command builder + response parser, validated
byte-for-byte against the `ruby` binary on every supported platform:

- **Command build** — tagged commands with monotonic tags
  (`generate_tag` → `RUBY0001`): `LOGIN` / `SELECT` / `EXAMINE` / `LIST` /
  `LSUB` / `STATUS` / `FETCH` / `SEARCH` / `STORE` / `COPY` / `UID …` /
  `APPEND` / `CREATE` / `DELETE` / `RENAME` / `SUBSCRIBE` / `UNSUBSCRIBE` /
  `EXPUNGE` / `CHECK` / `CLOSE` / `UNSELECT` / `IDLE` (+ `IdleDone`) /
  `AUTHENTICATE` / `LOGOUT` / `CAPABILITY` / `NOOP` / `STARTTLS`, with MRI's
  exact argument encoding (`send_string_data`: empty → `""`, multiline /
  non-ASCII → literal `{n}\r\n…`, specials → quoted string, else a bare atom),
  `SequenceSet` coalescing (`[3,1,2]` → `1:3`, `n:*`), and fetch-att / flag lists.
- **Helpers** — modified-UTF-7 mailbox-name `EncodeUTF7` / `DecodeUTF7`
  (RFC 3501 §5.1.3, `&` shift); `FormatDate` / `FormatDatetime` and `MessageSet`;
  and the pure SASL initial-response encoders `SASLPlain` / `SASLLoginUser` /
  `SASLLoginPassword` / `SASLCramMD5` / `SASLXOAuth2` (+ `SASLEncode`), mirroring
  the `Net::IMAP::SASL` authenticators for the `AUTHENTICATE` exchange.
- **Response parse** — the IMAP response grammar: tagged (`a001 OK …`), untagged
  (`* …`) and continuation (`+ …`) responses; the data responses
  `n EXISTS` / `n RECENT` / `n EXPUNGE` / `FLAGS` / `LIST` / `LSUB` / `STATUS` /
  `SEARCH` / `CAPABILITY` / `n FETCH (…)`; parenthesised lists, literals, quoted
  strings, `NIL`, numbers; the FETCH attribute grammar (`FLAGS` / `ENVELOPE` /
  `INTERNALDATE` / `RFC822*` / `UID` / `RFC822.SIZE` / `MODSEQ` / `BODY[…]` /
  `BODYSTRUCTURE`); and resp-text-codes (`[UIDVALIDITY n]`, `[PERMANENTFLAGS …]`,
  `[CAPABILITY …]`, `[BADCHARSET …]`, `[APPENDUID n uid]`,
  `[COPYUID n src dst]`, …).
- **Typed value model** — `TaggedResponse` / `UntaggedResponse` /
  `ContinuationRequest`, `ResponseText` / `ResponseCode`, `MailboxList`,
  `StatusData`, `FetchData`, `Envelope`, `Address`, `Flag`, mirroring the
  `Net::IMAP::*` structs member-for-member.

CGO-free, dependency-free, **100% test coverage**, `gofmt` + `go vet` clean, and
green across the six 64-bit Go targets (amd64, arm64, riscv64, loong64, ppc64le,
s390x) and three OSes.

## Install

```sh
go get github.com/go-ruby-net-imap/net-imap
```

## Usage — building commands

```go
package main

import (
	"fmt"

	imap "github.com/go-ruby-net-imap/net-imap"
)

func main() {
	b := imap.NewBuilder("RUBY") // tags RUBY0001, RUBY0002, …

	c, _ := b.Login("joe", "pa ss")
	fmt.Printf("%q\n", c.Bytes) // "RUBY0001 LOGIN joe \"pa ss\"\r\n"

	set, _ := imap.NewSequenceSet(1, imap.SeqRange{Lo: 3, Hi: 5})
	f, _ := b.Fetch(set, "FLAGS", "UID")
	fmt.Printf("%q\n", f.Bytes) // "RUBY0002 FETCH 1,3:5 (FLAGS UID)\r\n"

	// A command with a literal stops Bytes after "{n}\r\n"; the host writes each
	// segment's Data after the server's continuation request, then its Tail.
	a, _ := b.Append("INBOX", []imap.Flag{"Seen"}, nil, "From: …\r\n\r\nbody")
	fmt.Printf("%q + %d literal(s)\n", a.Bytes, len(a.Literals))
}
```

## Usage — parsing responses

```go
r, _ := imap.ParseResponse("* 12 FETCH (FLAGS (\\Seen) UID 4827313)\r\n")
u := r.(*imap.UntaggedResponse)         // Name == "FETCH"
fd := u.Data.(*imap.FetchData)          // Seqno == 12
flags, _ := fd.Attr.Get("FLAGS")        // []imap.Flag{"Seen"}
uid, _ := fd.Attr.Get("UID")            // int64(4827313)
_ = flags
_ = uid
```

`ParseResponse` is byte-faithful to MRI for the IMAP4rev1 grammar. The
**`BODYSTRUCTURE`** attribute is returned as a uniform, fully-recursive
`*BodyStructure` tree (loss-free — every body-fld is captured); its
`BodyType()` method reports the MRI class it maps to (`BodyTypeText` /
`BodyTypeBasic` / `BodyTypeMessage` / `BodyTypeMultipart`), which the host (rbgo)
uses to instantiate the right Ruby struct over the field tree.

## The socket / literal seam

The transport is a seam the host supplies. `Reader` assembles a complete response
buffer — a line plus any embedded literals — from two callbacks, then parses it
(mirroring `Net::IMAP::ResponseReader` without touching a socket):

```go
rd := imap.NewReader(
	conn.ReadLine,             // func() (string, error)        — a CRLF-terminated line
	conn.ReadExactly,          // func(n int) (string, error)   — exactly n literal bytes
)
resp, err := rd.ReadResponse() // frames {n}\r\n literals, then ParseResponse
```

## Response value model

| IMAP wire form | Go type returned |
| --- | --- |
| `a001 OK …` | `*TaggedResponse` |
| `* …` | `*UntaggedResponse` |
| `+ …` | `*ContinuationRequest` |
| `n EXISTS/RECENT/EXPUNGE` | `int64` (in `UntaggedResponse.Data`) |
| `OK/NO/BAD/BYE/PREAUTH …` | `*ResponseText` (`Code` + `Text`) |
| `FLAGS (…)` | `[]Flag` |
| `LIST/LSUB …` | `*MailboxList` |
| `STATUS …` | `*StatusData` |
| `SEARCH …` | `[]int64` |
| `CAPABILITY …` | `[]string` |
| `n FETCH (…)` | `*FetchData` (`*Envelope`, `[]Flag`, `int64`, `*BodyStructure`, …) |

## Tests & coverage

The suite pairs deterministic, ruby-free tests (which alone hold coverage at
100%, so the qemu cross-arch and Windows lanes pass the gate) with a
**differential MRI oracle**: command bytes are compared against
`Net::IMAP#send_command` (via a socket-free `put_string` probe) and the
per-string `send_string_data` encoding, and a corpus of canned server responses
is parsed by both this package and MRI's `ResponseParser` and projected to a
canonical line for comparison. The oracle scripts `$stdout.binmode` so Windows
text-mode never pollutes the bytes, and skip themselves where `ruby` is absent.

```sh
COVERPKG=$(go list ./... | paste -sd, -)
go test -race -coverpkg="$COVERPKG" -coverprofile=cover.out ./...
go tool cover -func=cover.out | tail -1   # 100.0%
```

## License

BSD-3-Clause — see [LICENSE](LICENSE). Copyright the go-ruby-net-imap/net-imap authors.
